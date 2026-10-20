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
// The call must be the value itself: a framework call wrapped in a local one, a
// name carried in a variable, a field extended in place, filled by a range
// clause or through its address, is refused. A multiple assignment is paired
// value by value; one from a call returning several values is refused.
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
	c := nameCheck{pass: pass, channels: map[types.Object]string{}}
	for _, name := range []string{"Canonical", "Hint"} {
		if field, _, _ := types.LookupFieldOrMethod(naming.Type(), false, naming.Pkg(), name); field != nil {
			c.channels[field] = name
		}
	}
	for _, f := range productionFiles(pass) {
		for n := range ast.Preorder(f) {
			c.node(n)
		}
	}
	return nil, nil
}

// nameCheck finds the name channels one package fills.
type nameCheck struct {
	pass *analysis.Pass
	// channels names each of ir.Naming's name-channel fields.
	channels map[types.Object]string
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
// position, with anything but a framework call. Only ir.Naming's literals have
// those fields, an element of a slice or map of them, its type elided, among
// them.
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
		var field types.Object = fields.Field(i)
		value := elt
		if kv, keyed := elt.(*ast.KeyValueExpr); keyed {
			key, _ := kv.Key.(*ast.Ident)
			field, value = c.pass.TypesInfo.ObjectOf(key), kv.Value
		}
		if name, ok := c.channels[field]; ok && !frameworkCall(c.pass.TypesInfo, value) {
			c.pass.Reportf(value.Pos(), notFramework, name)
		}
	}
}

// assign reports each name channel an assignment fills otherwise than whole,
// from a framework call paired with it.
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
	name, ok := c.channels[c.pass.TypesInfo.ObjectOf(sel.Sel)]
	return name, ok
}
