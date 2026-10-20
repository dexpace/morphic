package lintrules

import (
	"go/ast"
	"go/types"
	"slices"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/types/typeutil"
)

// registryWrites refuses building or writing a type registry outside ir and
// compilers/compile. Reaching the registry only through compile.Types is what
// makes one node per source coordinate (invariant 3) mechanical rather than a
// convention each compiler re-honours.
//
// A registry is any map from ir.TypeID to ir.TypeDef, whatever its type is
// named: an unnamed one converts to ir.TypeRegistry without a conversion to see,
// so it is caught where it is made. A compile.Settlement may rewrite the
// registry Settle hands it, which compile.Types checks for a changed node count.
func (l layout) registryWrites() *analysis.Analyzer {
	return &analysis.Analyzer{
		Name: "registrywrites",
		Doc:  "refuse building or writing an ir.TypeRegistry outside ir and compilers/compile",
		Run:  l.runRegistryWrites,
	}
}

const (
	builtMessage   = "builds a type registry (a map from ir.TypeID to ir.TypeDef) outside ir and compilers/compile; go through compile.Types"
	writtenMessage = "writes a type registry outside ir and compilers/compile; go through compile.Types, or rewrite a node in a compile.Settlement"
	genericMessage = "passes a type registry to a generic function, whose writes this rule cannot read; keep registry writes in ir and compilers/compile"
)

// mapsWriters are the functions of package maps that write the map they take
// first.
var mapsWriters = []string{"Copy", "DeleteFunc", "Insert"}

func (l layout) runRegistryWrites(pass *analysis.Pass) (any, error) {
	if l.framework(pass.Pkg.Path()) || !l.inModule(pass.Pkg.Path()) {
		return nil, nil
	}
	reg := typeNamed(pass.Pkg, irPath, "TypeRegistry")
	if reg == nil {
		return nil, nil // a package that reaches no ir holds none of its registries
	}
	c := registryCheck{l: l, pass: pass, core: reg.Type().Underlying(), settled: settledRegistries(pass)}
	for _, f := range productionFiles(pass) {
		for n := range ast.Preorder(f) {
			c.node(n)
		}
	}
	return nil, nil
}

// registryCheck finds the registry builds and writes in one package.
type registryCheck struct {
	l    layout
	pass *analysis.Pass
	// core is ir.TypeRegistry's underlying map type.
	core types.Type
	// settled holds the registry parameter of each Settle method of a
	// compile.Settlement the package declares.
	settled map[types.Object]bool
}

// isRegistry reports whether t is a map from ir.TypeID to ir.TypeDef.
func (c registryCheck) isRegistry(t types.Type) bool {
	return t != nil && types.Identical(t.Underlying(), c.core)
}

func (c registryCheck) node(n ast.Node) {
	switch n := n.(type) {
	case *ast.CompositeLit:
		if c.isRegistry(c.pass.TypesInfo.TypeOf(n)) {
			c.pass.Reportf(n.Pos(), builtMessage)
		}
	case *ast.CallExpr:
		c.call(n)
	case *ast.AssignStmt:
		for _, lhs := range n.Lhs {
			c.target(lhs)
		}
	case *ast.RangeStmt:
		c.target(n.Key)
		c.target(n.Value)
	default:
	}
}

// target reports e when it is an element of a registry, other than one of a
// registry a Settle method is handed.
func (c registryCheck) target(e ast.Expr) {
	ix, ok := ast.Unparen(e).(*ast.IndexExpr)
	if !ok || !c.isRegistry(c.pass.TypesInfo.TypeOf(ix.X)) {
		return
	}
	if id, isIdent := ast.Unparen(ix.X).(*ast.Ident); isIdent && c.settled[c.pass.TypesInfo.Uses[id]] {
		return
	}
	c.pass.Reportf(ix.Pos(), writtenMessage)
}

// call reports a call that builds a registry, writes one, or hands one to a
// generic function of the module's that this rule cannot read into.
func (c registryCheck) call(call *ast.CallExpr) {
	switch fn := typeutil.Callee(c.pass.TypesInfo, call).(type) {
	case *types.Builtin:
		c.builtin(call, fn.Name())
	case *types.Func:
		c.function(call, fn)
	default:
	}
}

func (c registryCheck) builtin(call *ast.CallExpr, name string) {
	if len(call.Args) == 0 || !c.isRegistry(c.pass.TypesInfo.TypeOf(call.Args[0])) {
		return
	}
	switch name {
	case "make":
		c.pass.Reportf(call.Pos(), builtMessage)
	case "delete", "clear":
		c.pass.Reportf(call.Pos(), writtenMessage)
	default:
	}
}

func (c registryCheck) function(call *ast.CallExpr, fn *types.Func) {
	if fn.Pkg() != nil && fn.Pkg().Path() == "maps" && slices.Contains(mapsWriters, fn.Name()) &&
		len(call.Args) > 0 && c.isRegistry(c.pass.TypesInfo.TypeOf(call.Args[0])) {
		c.pass.Reportf(call.Pos(), writtenMessage)
		return
	}
	// Only a package's function is generic, so one of these has a package.
	inst, generic := c.pass.TypesInfo.Instances[calleeIdent(call.Fun)]
	if !generic {
		return
	}
	switch {
	case c.isRegistry(c.pass.TypesInfo.TypeOf(call)):
		c.pass.Reportf(call.Pos(), builtMessage) // maps.Clone and maps.Collect among them
	case c.l.inModule(fn.Pkg().Path()) && !c.l.framework(fn.Pkg().Path()) && c.anyRegistry(inst.TypeArgs):
		c.pass.Reportf(call.Pos(), genericMessage)
	default:
	}
}

func (c registryCheck) anyRegistry(args *types.TypeList) bool {
	for t := range args.Types() {
		if c.isRegistry(t) {
			return true
		}
	}
	return false
}

// calleeIdent returns the identifier naming the function fun calls, through an
// explicit instantiation or a qualifier: the one Instances records.
func calleeIdent(fun ast.Expr) *ast.Ident {
	e := ast.Unparen(fun)
	switch ix := e.(type) {
	case *ast.IndexExpr:
		e = ast.Unparen(ix.X)
	case *ast.IndexListExpr:
		e = ast.Unparen(ix.X)
	default:
	}
	if sel, ok := e.(*ast.SelectorExpr); ok {
		return sel.Sel
	}
	id, _ := e.(*ast.Ident)
	return id
}

// settledRegistries returns the registry parameter of each Settle method the
// package declares on a type that is a compile.Settlement.
func settledRegistries(pass *analysis.Pass) map[types.Object]bool {
	settled := map[types.Object]bool{}
	tn := typeNamed(pass.Pkg, compilePath, "Settlement")
	if tn == nil {
		return settled
	}
	iface, _ := tn.Type().Underlying().(*types.Interface)
	for _, f := range productionFiles(pass) {
		for _, d := range f.Decls {
			fd, ok := d.(*ast.FuncDecl)
			if !ok || fd.Recv == nil || fd.Name.Name != "Settle" {
				continue
			}
			fn, isFunc := pass.TypesInfo.Defs[fd.Name].(*types.Func)
			if isFunc && iface != nil && types.Implements(fn.Signature().Recv().Type(), iface) {
				settled[fn.Signature().Params().At(0)] = true
			}
		}
	}
	return settled
}
