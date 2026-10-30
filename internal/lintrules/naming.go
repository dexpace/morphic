// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package lintrules

import (
	"go/ast"
	"go/token"
	"go/types"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/types/typeutil"
)

// nameChannels refuses filling ir.Naming's Canonical or Hint outside ir and
// compilers/compile with anything but a call of a function one of them
// declares. Those fields are ABI, so a compiler's own segmentation would make
// one mean two things (GitHub #163, #54).
//
// A struct with ir.Naming's field names, in order, carries the channels too,
// whatever its type or theirs. The call must be the value itself: a framework
// call wrapped in a local one, a name carried in a variable, a field extended
// in place, filled by a range clause or through its address, is refused.
func (l layout) nameChannels() *analysis.Analyzer {
	return &analysis.Analyzer{
		Name: "namechannels",
		Doc:  "refuse filling ir.Naming's Canonical or Hint outside the naming grammar",
		Run:  l.runNameChannels,
	}
}

// The ways a name channel can be filled, each a reason for a finding. Each
// takes the field's name.
const (
	notFramework = "ir.Naming.%s is filled by something other than a call into ir or compilers/compile; use compile.NamingFor, compile.NamingHint or ir.CanonicalWords"
	severalValue = "ir.Naming.%s is filled from a call returning several values; fill it alone, from compile.NamingFor, compile.NamingHint or ir.CanonicalWords"
	extended     = "ir.Naming.%s is extended in place; fill it whole, from compile.NamingFor, compile.NamingHint or ir.CanonicalWords"
	byRange      = "ir.Naming.%s is filled by a range clause; fill it from compile.NamingFor, compile.NamingHint or ir.CanonicalWords"
	addressed    = "the address of ir.Naming.%s is taken, so what fills it is out of sight; fill it from compile.NamingFor, compile.NamingHint or ir.CanonicalWords"
)

func (l layout) runNameChannels(pass *analysis.Pass) (any, error) {
	if l.framework(pass.Pkg.Path()) || !l.inModule(pass.Pkg.Path()) {
		return nil, nil
	}
	naming := typeNamed(pass.Pkg, irPath, "Naming")
	if naming == nil {
		return nil, nil
	}
	shape, _ := naming.Type().Underlying().(*types.Struct) // ir declares it a struct
	c := nameCheck{pass: pass, shape: shape}
	for _, f := range productionFiles(pass) {
		for n := range ast.Preorder(f) {
			c.node(n)
		}
	}
	return nil, nil
}

// nameCheck finds the name channels one package fills. One filled through
// reflect, by a decoder writing an ir.Naming say, is out of its sight.
type nameCheck struct {
	pass *analysis.Pass
	// shape is ir.Naming's underlying struct.
	shape *types.Struct
}

// isChannel reports whether the field called name of the struct s is a name
// channel: Canonical or Hint of a struct with ir.Naming's fields.
func (c nameCheck) isChannel(s *types.Struct, name string) bool {
	return (name == "Canonical" || name == "Hint") && c.namedLikeNaming(s)
}

// namedLikeNaming reports whether s has ir.Naming's fields, by name and in
// order. Their types are not compared: a struct of those names converts to
// ir.Naming, or would once its type parameters are instantiated, or is a copy
// made to look like one. ir.Naming's fields are exported, so their names
// compare across packages.
func (c nameCheck) namedLikeNaming(s *types.Struct) bool {
	if s.NumFields() != c.shape.NumFields() {
		return false
	}
	for i := range s.NumFields() {
		if s.Field(i).Name() != c.shape.Field(i).Name() {
			return false
		}
	}
	return true
}

func (c nameCheck) node(n ast.Node) {
	switch n := n.(type) {
	case *ast.CompositeLit:
		c.literal(n)
	case *ast.AssignStmt:
		c.assign(n)
	case *ast.RangeStmt:
		c.rangeClause(n)
	case *ast.UnaryExpr: // & is the one unary operator a string takes
		if name, ok := c.channel(n.X); ok {
			c.pass.Reportf(n.Pos(), addressed, name)
		}
	default:
	}
}

// literal reports each name channel a struct literal fills, keyed or by
// position, with anything but a framework call, an element of a slice or map
// of them, its type elided, among them. A key may name a promoted field, so
// it is judged in the struct declaring it, not the literal's.
func (c nameCheck) literal(lit *ast.CompositeLit) {
	t := c.pass.TypesInfo.TypeOf(lit)
	if ptr, ok := t.(*types.Pointer); ok {
		t = ptr.Elem()
	}
	fields, isStruct := t.Underlying().(*types.Struct)
	if !isStruct {
		return
	}
	for i, elt := range lit.Elts {
		owner, name, value := fields, "", elt
		if kv, keyed := elt.(*ast.KeyValueExpr); keyed {
			key, _ := kv.Key.(*ast.Ident) // a struct literal's keys are its fields' names
			_, path, _ := types.LookupFieldOrMethod(t, false, c.pass.Pkg, key.Name)
			owner, name, value = declaringAlong(t, path), key.Name, kv.Value
		} else {
			name = fields.Field(i).Name()
		}
		if c.isChannel(owner, name) && !frameworkCall(c.pass.TypesInfo, value) {
			c.pass.Reportf(value.Pos(), notFramework, name)
		}
	}
}

// assign reports each name channel an assignment fills otherwise than whole,
// from a framework call paired with it: a multiple assignment is paired value
// by value, and one from a call returning several values is refused.
func (c nameCheck) assign(stmt *ast.AssignStmt) {
	for i, lhs := range stmt.Lhs {
		name, ok := c.channel(lhs)
		if !ok {
			continue
		}
		switch {
		case stmt.Tok != token.ASSIGN:
			c.pass.Reportf(lhs.Pos(), extended, name)
		case len(stmt.Rhs) != len(stmt.Lhs):
			c.pass.Reportf(lhs.Pos(), severalValue, name)
		case !frameworkCall(c.pass.TypesInfo, stmt.Rhs[i]):
			c.pass.Reportf(stmt.Rhs[i].Pos(), notFramework, name)
		default:
		}
	}
}

// rangeClause reports a name channel a range clause assigns.
func (c nameCheck) rangeClause(stmt *ast.RangeStmt) {
	for _, e := range []ast.Expr{stmt.Key, stmt.Value} {
		if name, ok := c.channel(e); ok {
			c.pass.Reportf(e.Pos(), byRange, name)
		}
	}
}

// frameworkCall reports whether e is a call of a function, not a method, that
// ir or compilers/compile declares.
func frameworkCall(info *types.Info, e ast.Expr) bool {
	call, ok := ast.Unparen(e).(*ast.CallExpr)
	if !ok {
		return false
	}
	fn, ok := typeutil.Callee(info, call).(*types.Func)
	if !ok || fn.Pkg() == nil || fn.Signature().Recv() != nil {
		return false
	}
	return fn.Pkg().Path() == irPath || fn.Pkg().Path() == compilePath
}

// channel names the name channel e selects, through any embedding or pointer.
func (c nameCheck) channel(e ast.Expr) (string, bool) {
	sel, ok := ast.Unparen(e).(*ast.SelectorExpr)
	if !ok {
		return "", false
	}
	selection, isSelection := c.pass.TypesInfo.Selections[sel]
	if !isSelection {
		return "", false // a qualified identifier, not a field
	}
	return sel.Sel.Name, c.isChannel(declaring(selection), sel.Sel.Name)
}

// declaring returns the struct that declares the field selection selects.
// Only a field is assigned or has its address taken.
func declaring(selection *types.Selection) *types.Struct {
	return declaringAlong(selection.Recv(), selection.Index())
}

// declaringAlong returns the struct that declares the field path reaches from
// t, stepping through the embedded fields before its last index, each a
// struct or a pointer to one.
func declaringAlong(t types.Type, path []int) *types.Struct {
	for _, i := range path[:max(len(path)-1, 0)] {
		t = structOf(t).Field(i).Type()
	}
	return structOf(t)
}

// structOf returns the struct t is, or points to.
func structOf(t types.Type) *types.Struct {
	if ptr, ok := t.Underlying().(*types.Pointer); ok {
		t = ptr.Elem()
	}
	s, _ := t.Underlying().(*types.Struct)
	return s
}
