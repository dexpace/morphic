package lintrules

import (
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"
	"strings"

	"golang.org/x/tools/go/analysis"
)

// idDerivations refuses, in a compiler other than compilers/compile, an ir ID
// built otherwise than by the framework, and a compile.Space built from
// anything but one string literal. The derivation stays with the compiler, but
// the grammar around it does not: the kind prefix, the namespace, and a minted
// node's namespace of its own (GitHub #162). A namespace assembled from data
// can lose the separator before the path (GitHub #141).
//
// An ID type is one ir declares with a name ending in ID. pass and irverify
// convert an ID back to look a node up, so only compilers are held.
func (l layout) idDerivations() *analysis.Analyzer {
	return &analysis.Analyzer{
		Name: "idderivations",
		Doc:  "refuse a compiler deriving an ir ID or a compile.Space otherwise than through the framework",
		Run:  l.runIDDerivations,
	}
}

const (
	convertedID  = "converts a value to ir.%s outside compilers/compile; derive the ID through the framework"
	spelledID    = "spells an ir.%s as a constant outside compilers/compile; derive the ID through the framework"
	gluedID      = "glues an ir.%s together outside compilers/compile; derive the ID through the framework"
	spaceMessage = "builds a compile.Space from something other than one string literal; a namespace is a literal, or the path glued onto it loses its separator"
)

func (l layout) runIDDerivations(pass *analysis.Pass) (any, error) {
	path := pass.Pkg.Path()
	if !l.within(path, "compilers") || l.within(path, "compilers/compile") {
		return nil, nil
	}
	c := idCheck{pass: pass, ids: idTypes(imported(pass.Pkg, irPath))}
	if space := typeNamed(pass.Pkg, compilePath, "Space"); space != nil {
		c.space = space.Type()
	}
	if len(c.ids) == 0 && c.space == nil {
		return nil, nil // a package that reaches neither builds none of them
	}
	for _, f := range productionFiles(pass) {
		ast.Inspect(f, func(n ast.Node) bool { return !c.reported(n) })
	}
	return nil, nil
}

// idTypes returns the ID types ir declares, or none when ir is nil.
func idTypes(ir *types.Package) map[types.Type]string {
	ids := map[types.Type]string{}
	if ir == nil {
		return ids
	}
	for _, name := range ir.Scope().Names() {
		if tn, ok := ir.Scope().Lookup(name).(*types.TypeName); ok && strings.HasSuffix(name, "ID") {
			ids[tn.Type()] = name
		}
	}
	return ids
}

// idCheck finds the IDs and namespaces one compiler package builds itself.
type idCheck struct {
	pass *analysis.Pass
	// ids names each of ir's ID types.
	ids map[types.Type]string
	// space is compile.Space, or nil when the package reaches none.
	space types.Type
}

// reported reports n when it builds an ID or a namespace, and says whether it
// did, so what n holds is not reported again.
func (c idCheck) reported(n ast.Node) bool {
	switch n := n.(type) {
	case *ast.CallExpr:
		return c.conversion(n)
	case *ast.BinaryExpr:
		return c.glued(n.Pos(), c.pass.TypesInfo.TypeOf(n))
	case *ast.AssignStmt:
		return n.Tok == token.ADD_ASSIGN && c.glued(n.Pos(), c.pass.TypesInfo.TypeOf(n.Lhs[0]))
	case ast.Expr:
		return c.constant(n)
	default:
		return false
	}
}

// conversion reports a conversion to an ID type, and one to compile.Space of
// anything but a string literal.
func (c idCheck) conversion(call *ast.CallExpr) bool {
	// A call that converts nothing has a function's type in Fun, never an ID's
	// or a namespace's.
	tv := c.pass.TypesInfo.Types[call.Fun]
	if name, isID := c.idType(tv.Type); isID {
		c.pass.Reportf(call.Pos(), convertedID, name)
		return true
	}
	if c.isSpace(tv.Type) && !stringLiteral(call.Args[0]) {
		c.pass.Reportf(call.Pos(), spaceMessage)
		return true
	}
	return false
}

// glued reports an ID or a namespace of type t that a + or a += builds: the
// only operators that yield a string.
func (c idCheck) glued(pos token.Pos, t types.Type) bool {
	if name, isID := c.idType(t); isID {
		c.pass.Reportf(pos, gluedID, name)
		return true
	}
	if c.isSpace(t) {
		c.pass.Reportf(pos, spaceMessage)
		return true
	}
	return false
}

// constant reports a string constant that becomes an ID, unless it is empty
// or names an ID constant, whose declaration is where its literal is read; and
// one that becomes a compile.Space, unless it is a string literal or names a
// compile.Space constant.
func (c idCheck) constant(e ast.Expr) bool {
	tv := c.pass.TypesInfo.Types[e]
	if tv.Value == nil || c.namesConstant(e, tv.Type) {
		return false
	}
	if name, isID := c.idType(tv.Type); isID && constant.StringVal(tv.Value) != "" {
		c.pass.Reportf(e.Pos(), spelledID, name)
		return true
	}
	if c.isSpace(tv.Type) && !stringLiteral(e) {
		c.pass.Reportf(e.Pos(), spaceMessage)
		return true
	}
	return false
}

// namesConstant reports whether e names a constant declared with type t.
func (c idCheck) namesConstant(e ast.Expr, t types.Type) bool {
	var id *ast.Ident
	switch e := e.(type) {
	case *ast.Ident:
		id = e
	case *ast.SelectorExpr:
		id = e.Sel
	default:
		return false
	}
	obj, ok := c.pass.TypesInfo.Uses[id].(*types.Const)
	return ok && types.Identical(obj.Type(), t)
}

// idType names the ID type t is, through an alias.
func (c idCheck) idType(t types.Type) (string, bool) {
	name, ok := c.ids[types.Unalias(t)]
	return name, ok
}

// isSpace reports whether t is compile.Space.
func (c idCheck) isSpace(t types.Type) bool {
	return c.space != nil && t != nil && types.Identical(t, c.space)
}

// stringLiteral reports whether e is one string literal.
func stringLiteral(e ast.Expr) bool {
	lit, ok := ast.Unparen(e).(*ast.BasicLit)
	return ok && lit.Kind == token.STRING
}
