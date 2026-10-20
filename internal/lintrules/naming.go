package lintrules

import (
	"go/ast"
	"go/token"
	"go/types"

	"golang.org/x/tools/go/analysis"
)

// nameChannels refuses filling ir.Naming's Canonical or Hint outside ir and
// compilers/compile with anything but a call of a function one of them
// declares. Those fields are ABI, so a compiler's own segmentation would make
// one mean two things (GitHub #163, #54).
//
// A struct that converts to ir.Naming carries the channels too, whatever its
// type is named. The call must be the value itself: a framework call wrapped in
// a local one, a name carried in a variable, a field extended in place, filled
// by a range clause or through its address, is refused.
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
// channel: Canonical or Hint of a struct that converts to ir.Naming.
func (c nameCheck) isChannel(s *types.Struct, name string) bool {
	return (name == "Canonical" || name == "Hint") && types.IdenticalIgnoreTags(s, c.shape)
}

func (c nameCheck) node(n ast.Node) {
	switch n := n.(type) {
	case *ast.CompositeLit:
		c.literal(n)
	case *ast.AssignStmt:
		c.assign(n)
	case *ast.RangeStmt:
		c.rangeClause(n)
	case *ast.UnaryExpr:
		if name, ok := c.channel(n.X); ok && n.Op == token.AND {
			c.pass.Reportf(n.Pos(), addressed, name)
		}
	default:
	}
}

// literal reports each name channel a struct literal fills, keyed or by
// position, with anything but a framework call, an element of a slice or map
// of them, its type elided, among them.
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
		name, value := fields.Field(i).Name(), elt
		if kv, keyed := elt.(*ast.KeyValueExpr); keyed {
			key, _ := kv.Key.(*ast.Ident) // a struct literal's keys are its fields' names
			name, value = key.Name, kv.Value
		}
		if c.isChannel(fields, name) && !frameworkCall(c.pass.TypesInfo, value) {
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

// declaring returns the struct that declares the field selection selects,
// stepping through the embedded fields on its path. Only a field is assigned
// or has its address taken, so every step is a struct or a pointer to one.
func declaring(selection *types.Selection) *types.Struct {
	t := selection.Recv()
	path := selection.Index()
	for _, i := range path[:len(path)-1] {
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
