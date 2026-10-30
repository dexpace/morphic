// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package lintrules

import (
	"go/ast"
	"go/token"
	"go/types"
	"strings"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/types/typeutil"
)

// registryWrites refuses building or writing a document's registries, the maps
// ir.Document keys by ID, outside ir and the packages building documents: the
// type registry in compilers/compile alone, as a compiler goes through
// compile.Types, which makes one node per source coordinate (invariant 3)
// mechanical; any other in any compiler. Every other package edits one through
// an ir.Editor.
//
// A registry is matched by its map type, whatever it is named, and a generic
// by its type arguments. A compile.Settlement's Settle may rewrite the type
// registry it is handed, in its own body, which compile.Types checks for a
// changed node count.
func (l layout) registryWrites() *analysis.Analyzer {
	return &analysis.Analyzer{
		Name: "registrywrites",
		Doc:  "refuse building or writing a document's registries outside ir and the compilers",
		Run:  l.runRegistryWrites,
	}
}

const (
	closureMessage = "writes the registry Settle is handed from a function literal, which can run after Settle returns; write it in Settle itself"
	reboundMessage = "writes through the registry parameter Settle rebinds, which may name a registry it was not handed; leave the parameter bound"
)

func (l layout) runRegistryWrites(pass *analysis.Pass) (any, error) {
	if !l.inModule(pass.Pkg.Path()) {
		return nil, nil
	}
	regs := l.registriesHeld(pass.Pkg)
	if len(regs) == 0 {
		return nil, nil // the framework, or a package reaching no ir
	}
	c := registryCheck{l: l, pass: pass, regs: regs, settled: settlements(pass), ways: l.ways(pass.Pkg.Path())}
	for _, f := range productionFiles(pass) {
		for n := range ast.Preorder(f) {
			c.node(n)
		}
	}
	return nil, nil
}

// registryType is one of a document's registries: its map type, the noun its
// findings name it by, and the packages that may write it.
type registryType struct {
	core    *types.Map
	noun    string
	writers string
	// types records that it is the type registry, the one a compile.Settlement
	// rewrites.
	types bool
}

// registriesHeld returns the registries pkg is held to: every field of
// ir.Document that is a map keyed by a named string type, read as pkg sees ir.
// ir holds none, nor does compilers/compile; another compiler holds the type
// registry alone.
func (l layout) registriesHeld(pkg *types.Package) []registryType {
	doc := typeNamed(pkg, irPath, "Document")
	path := pkg.Path()
	if doc == nil || l.framework(path) {
		return nil
	}
	st, _ := doc.Type().Underlying().(*types.Struct) // ir declares Document a struct
	var regs []registryType
	for f := range st.Fields() {
		r, isReg := registryOf(f.Type())
		if !isReg || !r.types && l.within(path, "compilers") {
			continue
		}
		regs = append(regs, r)
	}
	return regs
}

// registryOf returns the registry a field of type t holds, if it is a map
// keyed by a named string type: the class of ID its key names.
func registryOf(t types.Type) (registryType, bool) {
	m, isMap := t.Underlying().(*types.Map)
	if !isMap {
		return registryType{}, false
	}
	key, named := types.Unalias(m.Key()).(*types.Named)
	if !named || !isString(key) {
		return registryType{}, false
	}
	r := registryType{core: m, noun: strings.ToLower(strings.TrimSuffix(key.Obj().Name(), "ID"))}
	r.types = r.noun == "type"
	r.writers = "ir and the compilers"
	if r.types {
		r.writers = "ir and compilers/compile"
	}
	return r, true
}

// isString reports whether t's underlying type is string.
func isString(t types.Type) bool {
	b, ok := t.Underlying().(*types.Basic)
	return ok && b.Kind() == types.String
}

// registryCheck finds the registry builds and writes in one package. One made
// through reflect, or inside a generic declared outside the module other than
// the maps functions, is out of its sight.
type registryCheck struct {
	l    layout
	pass *analysis.Pass
	// regs are the registries the package is held to.
	regs []registryType
	// settled is what the package's Settle methods may write.
	settled settlement
	// ways is how the package under check reaches a registry instead.
	ways ways
}

// ways is how a package reaches a registry, which ends the message a build
// draws and the one a write draws.
type ways struct{ built, written string }

// ways returns how the package at path reaches a registry: a compiler through
// compile.Types, and every other package through an ir.Editor.
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

// qualifier spells a type of package ir as ir spells it to its importers.
func qualifier(p *types.Package) string { return p.Name() }

// built reports r built at pos.
func (c registryCheck) built(pos token.Pos, r registryType) {
	c.pass.Reportf(pos, "builds %s registry (a map from %s to %s) outside %s; %s", r.named(),
		types.TypeString(r.core.Key(), qualifier), types.TypeString(r.core.Elem(), qualifier), r.writers, c.ways.built)
}

// written reports r written at pos.
func (c registryCheck) written(pos token.Pos, r registryType) {
	c.pass.Reportf(pos, "writes %s registry outside %s; %s", r.named(), r.writers, c.ways.written)
}

// named is r's noun with its article.
func (r registryType) named() string {
	if strings.ContainsRune("aeiou", rune(r.noun[0])) {
		return "an " + r.noun
	}
	return "a " + r.noun
}

// registry returns the registry t is, if any: one of the package's registry
// map types, or a generic's map to a registry's element keyed by a type
// parameter the registry's key satisfies. That one is caught where it is
// written: the key type alone, which makes it a registry, draws nothing as a
// type argument.
func (c registryCheck) registry(t types.Type) (registryType, bool) {
	m, isMap := t.Underlying().(*types.Map)
	if !isMap {
		return registryType{}, false
	}
	key, generic := m.Key().(*types.TypeParam)
	for _, r := range c.regs {
		if !generic && types.Identical(m, r.core) ||
			generic && types.Identical(m.Elem(), r.core.Elem()) && satisfies(r.core.Key(), key) {
			return r, true
		}
	}
	return registryType{}, false
}

func (c registryCheck) node(n ast.Node) {
	switch n := n.(type) {
	case *ast.CompositeLit:
		if r, ok := c.registry(c.pass.TypesInfo.TypeOf(n)); ok {
			c.built(n.Pos(), r)
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
// type registry a Settle method is handed, written in its own body while the
// parameter stays bound to it.
func (c registryCheck) target(e ast.Expr) {
	ix, ok := ast.Unparen(e).(*ast.IndexExpr)
	if !ok {
		return
	}
	r, isReg := c.registry(c.pass.TypesInfo.TypeOf(ix.X))
	if !isReg {
		return
	}
	id, _ := ast.Unparen(ix.X).(*ast.Ident) // nil, naming no parameter, when ix.X is no identifier
	bound, settled := c.settled.params[c.pass.TypesInfo.Uses[id]]
	switch {
	case !settled || !r.types:
		c.written(ix.Pos(), r)
	case !bound:
		c.pass.Reportf(ix.Pos(), reboundMessage)
	case c.settled.inClosure(ix.Pos()):
		c.pass.Reportf(ix.Pos(), closureMessage)
	default:
	}
}

// builtin reports make building a registry and delete or clear writing one,
// each of which takes the map first.
func (c registryCheck) builtin(call *ast.CallExpr, name string) {
	switch name {
	case "make", "delete", "clear":
	default:
		return
	}
	r, isReg := c.registry(c.pass.TypesInfo.TypeOf(call.Args[0]))
	switch {
	case !isReg:
	case name == "make":
		c.built(call.Pos(), r)
	default:
		c.written(call.Pos(), r)
	}
}

// instance reports an instantiation, called or not, that writes or builds a
// registry: a maps function writing one, and a generic returning one. It also
// reports a generic the module declares outside the framework instantiated
// with a registry or a registry's element type, from which it can make a
// registry whose writes this rule cannot see, whether it writes one or not. A
// key type alone draws nothing, so a generic set of IDs stays clean.
func (c registryCheck) instance(id *ast.Ident) {
	inst, ok := c.pass.TypesInfo.Instances[id]
	if !ok {
		return
	}
	generic := c.pass.TypesInfo.Uses[id]
	path := generic.Pkg().Path() // a generic is always declared in a package
	sig, isFunc := inst.Type.(*types.Signature)
	if r, isReg := c.registry(inst.TypeArgs.At(0)); isReg && path == "maps" && mapsWriter(generic.Name()) {
		c.written(id.Pos(), r) // each writes the map its first type argument is
		return
	}
	if c.l.framework(path) {
		return // trusted, as its functions are
	}
	if r, isReg := c.anyRegistry(sig, isFunc); isReg {
		c.built(id.Pos(), r) // maps.Clone and maps.Collect among them
		return
	}
	if r, isReg := c.registryArgs(inst.TypeArgs); isReg && c.l.inModule(path) {
		c.pass.Reportf(id.Pos(), "instantiates a generic with %s registry or %s, whose registry writes this rule "+
			"cannot read; keep registry writes in %s", r.named(), types.TypeString(r.core.Elem(), qualifier), r.writers)
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

// anyRegistry returns a registry one of the results of sig, a function's
// signature when isFunc is set, is.
func (c registryCheck) anyRegistry(sig *types.Signature, isFunc bool) (registryType, bool) {
	if !isFunc {
		return registryType{}, false
	}
	for v := range sig.Results().Variables() {
		if r, isReg := c.registry(v.Type()); isReg {
			return r, true
		}
	}
	return registryType{}, false
}

// registryArgs returns a registry one of args is, or whose element type one
// is.
func (c registryCheck) registryArgs(args *types.TypeList) (registryType, bool) {
	for t := range args.Types() {
		if r, isReg := c.registry(t); isReg {
			return r, true
		}
		for _, r := range c.regs {
			if types.Identical(t, r.core.Elem()) {
				return r, true
			}
		}
	}
	return registryType{}, false
}
