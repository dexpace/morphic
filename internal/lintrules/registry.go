package lintrules

import (
	"go/ast"
	"go/token"
	"go/types"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/types/typeutil"
)

// registryWrites refuses building or writing a type registry outside ir and
// compilers/compile. A compiler goes through compile.Types, which makes one
// node per source coordinate (invariant 3) mechanical rather than a convention,
// and any other package through an ir.Editor.
//
// A registry is any map from ir.TypeID to ir.TypeDef, whatever its type is
// named: an unnamed one converts to ir.TypeRegistry without a conversion to see,
// so it is caught where it is made, and a generic by its type arguments. A
// compile.Settlement's Settle may rewrite the registry it is handed, in its own
// body, which compile.Types checks for a changed node count.
func (l layout) registryWrites() *analysis.Analyzer {
	return &analysis.Analyzer{
		Name: "registrywrites",
		Doc:  "refuse building or writing an ir.TypeRegistry outside ir and compilers/compile",
		Run:  l.runRegistryWrites,
	}
}

const (
	builtMessage   = "builds a type registry (a map from ir.TypeID to ir.TypeDef) outside ir and compilers/compile; "
	writtenMessage = "writes a type registry outside ir and compilers/compile; "
	genericMessage = "instantiates a generic with a type registry or ir.TypeDef, whose registry writes this rule cannot read; keep registry writes in ir and compilers/compile"
	closureMessage = "writes the registry Settle is handed from a function literal, which can run after Settle returns; write it in Settle itself"
	reboundMessage = "writes through the registry parameter Settle rebinds, which may name a registry it was not handed; leave the parameter bound"
)

func (l layout) runRegistryWrites(pass *analysis.Pass) (any, error) {
	if l.framework(pass.Pkg.Path()) || !l.inModule(pass.Pkg.Path()) {
		return nil, nil
	}
	reg := typeNamed(pass.Pkg, irPath, "TypeRegistry")
	if reg == nil {
		return nil, nil // a package that reaches no ir holds none of its registries
	}
	core, _ := reg.Type().Underlying().(*types.Map) // ir declares it a map
	c := registryCheck{l: l, pass: pass, core: core, settled: settlements(pass), ways: l.ways(pass.Pkg.Path())}
	for _, f := range productionFiles(pass) {
		for n := range ast.Preorder(f) {
			c.node(n)
		}
	}
	return nil, nil
}

// registryCheck finds the registry builds and writes in one package. One made
// through reflect, or inside a generic declared outside the module other than
// the maps functions, is out of its sight.
type registryCheck struct {
	l    layout
	pass *analysis.Pass
	// core is ir.TypeRegistry's underlying map type.
	core *types.Map
	// settled is what the package's Settle methods may write.
	settled settlement
	// ways is how the package under check reaches the registry instead.
	ways ways
}

// ways is how a package reaches the registry, which ends the message a build
// draws and the one a write draws.
type ways struct{ built, written string }

// ways returns how the package at path reaches the registry: a compiler
// through compile.Types, and every other package through an ir.Editor.
func (l layout) ways(path string) ways {
	if l.within(path, "compilers") {
		return ways{
			built:   "go through compile.Types",
			written: "go through compile.Types, or rewrite a node in a compile.Settlement",
		}
	}
	const edit = "edit a document's registry through an ir.Editor"
	return ways{built: edit, written: edit}
}

// built reports a registry built at pos.
func (c registryCheck) built(pos token.Pos) { c.pass.Reportf(pos, "%s%s", builtMessage, c.ways.built) }

// written reports a registry written at pos.
func (c registryCheck) written(pos token.Pos) {
	c.pass.Reportf(pos, "%s%s", writtenMessage, c.ways.written)
}

// isRegistry reports whether t is a map from ir.TypeID to ir.TypeDef, or a
// generic's map to ir.TypeDef keyed by a type parameter ir.TypeID satisfies.
// That one is caught where it is written: ir.TypeID alone, which makes it a
// registry, draws nothing as a type argument.
func (c registryCheck) isRegistry(t types.Type) bool {
	m, isMap := t.Underlying().(*types.Map)
	if !isMap {
		return false
	}
	key, generic := m.Key().(*types.TypeParam)
	if !generic {
		return types.Identical(m, c.core)
	}
	return types.Identical(m.Elem(), c.core.Elem()) && satisfies(c.core.Key(), key)
}

func (c registryCheck) node(n ast.Node) {
	switch n := n.(type) {
	case *ast.CompositeLit:
		if c.isRegistry(c.pass.TypesInfo.TypeOf(n)) {
			c.built(n.Pos())
		}
	case *ast.CallExpr:
		if fn, isBuiltin := typeutil.Callee(c.pass.TypesInfo, n).(*types.Builtin); isBuiltin {
			c.builtin(n, fn.Name())
		}
	case *ast.Ident:
		c.instance(n)
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

// target reports e when it is an element of a registry, other than one of the
// registry a Settle method is handed, written in its own body while the
// parameter stays bound to it.
func (c registryCheck) target(e ast.Expr) {
	ix, ok := ast.Unparen(e).(*ast.IndexExpr)
	if !ok || !c.isRegistry(c.pass.TypesInfo.TypeOf(ix.X)) {
		return
	}
	id, _ := ast.Unparen(ix.X).(*ast.Ident) // nil, naming no parameter, when ix.X is no identifier
	bound, settled := c.settled.params[c.pass.TypesInfo.Uses[id]]
	switch {
	case !settled:
		c.written(ix.Pos())
	case !bound:
		c.pass.Reportf(ix.Pos(), reboundMessage)
	case c.settled.inClosure(ix.Pos()):
		c.pass.Reportf(ix.Pos(), closureMessage)
	default:
	}
}

func (c registryCheck) builtin(call *ast.CallExpr, name string) {
	if len(call.Args) == 0 || !c.isRegistry(c.pass.TypesInfo.TypeOf(call.Args[0])) {
		return
	}
	switch name {
	case "make":
		c.built(call.Pos())
	case "delete", "clear":
		c.written(call.Pos())
	default:
	}
}

// instance reports an instantiation, called or not, that writes or builds a
// registry: a maps function writing one, and a generic returning one. It also
// reports a generic the module declares outside the framework instantiated
// with a registry or ir.TypeDef, from which it can make a registry whose
// writes this rule cannot see, whether it writes one or not. ir.TypeID alone
// draws nothing, so a generic set of IDs stays clean.
func (c registryCheck) instance(id *ast.Ident) {
	inst, ok := c.pass.TypesInfo.Instances[id]
	if !ok {
		return
	}
	generic := c.pass.TypesInfo.Uses[id]
	path := generic.Pkg().Path() // a generic is always declared in a package
	sig, isFunc := inst.Type.(*types.Signature)
	switch {
	case path == "maps" && mapsWriter(generic.Name()) && c.isRegistry(inst.TypeArgs.At(0)):
		c.written(id.Pos()) // each writes the map its first type argument is
	case c.l.framework(path): // trusted, as its functions are
	case isFunc && c.anyRegistry(sig.Results()):
		c.built(id.Pos()) // maps.Clone and maps.Collect among them
	case c.l.inModule(path) && c.registryArgs(inst.TypeArgs):
		c.pass.Reportf(id.Pos(), genericMessage)
	default:
	}
}

// mapsWriter reports whether the function of package maps named name writes
// the map it takes first.
func mapsWriter(name string) bool {
	switch name {
	case "Copy", "DeleteFunc", "Insert":
		return true
	default:
		return false
	}
}

// anyRegistry reports whether one of vars is a registry.
func (c registryCheck) anyRegistry(vars *types.Tuple) bool {
	for v := range vars.Variables() {
		if c.isRegistry(v.Type()) {
			return true
		}
	}
	return false
}

// registryArgs reports whether one of args is a registry or ir.TypeDef.
func (c registryCheck) registryArgs(args *types.TypeList) bool {
	for t := range args.Types() {
		if c.isRegistry(t) || types.Identical(t, c.core.Elem()) {
			return true
		}
	}
	return false
}
