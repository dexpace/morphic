// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package lintrules

import (
	"go/ast"
	"go/token"
	"go/types"
	"slices"

	"golang.org/x/tools/go/analysis"
)

// globalWrites refuses writing a package-level variable, outside init, in a
// package under ir, pass, compilers or emitters: a stage holds no mutable state
// (invariant 5), which is what engine.Engine's concurrent use rests on.
// Whoever declares the variable, a write is an assignment, ++ or --, an element
// or field, the address taken, a pointer method called, or delete, clear, copy
// or append on it, directly or through a local alias. Reads stay legal.
//
// The rule reads one package's syntax, so what it cannot see is listed on the
// function that owns each limit: check, rooted, aliasable, addressOf,
// pointerMethod and builtin.
func (l layout) globalWrites() *analysis.Analyzer {
	return &analysis.Analyzer{
		Name: "globalwrites",
		Doc:  "refuse writing a package-level variable outside init in a pipeline package",
		Run:  l.runGlobalWrites,
	}
}

func (l layout) runGlobalWrites(pass *analysis.Pass) (any, error) {
	path := pass.Pkg.Path()
	pipeline := slices.ContainsFunc([]string{"ir", "pass", "compilers", "emitters"}, func(dir string) bool {
		return l.within(path, dir)
	})
	if !pipeline { // a package outside the roots is out of reach
		return nil, nil
	}
	for _, f := range productionFiles(pass) {
		for _, root := range afterInit(f) {
			scope := globalScope{pass: pass, aliases: map[*types.Var]*types.Var{}}
			scope.check(root)
		}
	}
	return nil, nil
}

// afterInit returns the code of f that can run once initialization is over:
// the body of each function and method but init, and each outermost function
// literal in a declaration at package level, which can be called later. A
// literal inside init is init's.
func afterInit(f *ast.File) []ast.Node {
	var roots []ast.Node
	for _, d := range f.Decls {
		fn, isFunc := d.(*ast.FuncDecl)
		switch {
		case !isFunc:
			ast.Inspect(d, func(n ast.Node) bool {
				lit, isLit := n.(*ast.FuncLit)
				if isLit {
					roots = append(roots, lit.Body)
				}
				return !isLit
			})
		case fn.Body != nil && (fn.Recv != nil || fn.Name.Name != "init"):
			roots = append(roots, fn.Body)
		}
	}
	return roots
}

// globalScope holds one top-level body's findings: aliases maps each local that
// holds part of a package-level variable to the variable.
type globalScope struct {
	pass    *analysis.Pass
	aliases map[*types.Var]*types.Var
}

// check reports each write under root, after finding its aliases.
func (s globalScope) check(root ast.Node) {
	s.findAliases(root)
	for n := range ast.Preorder(root) {
		switch n := n.(type) {
		case *ast.AssignStmt:
			// := cannot reach a package-level variable: its targets are new
			// locals, or locals of the same scope.
			if n.Tok != token.DEFINE {
				s.assign(n)
			}
		case *ast.IncDecStmt:
			verb := "increments"
			if n.Tok == token.DEC {
				verb = "decrements"
			}
			s.target(n.X, verb)
		case *ast.RangeStmt:
			if n.Tok == token.ASSIGN {
				s.target(n.Key, "assigns")
				s.target(n.Value, "assigns")
			}
		case *ast.UnaryExpr:
			s.addressOf(n)
		case *ast.CallExpr:
			s.builtin(n)
		case *ast.SelectorExpr:
			s.pointerMethod(n)
		default:
			// Nothing else writes, or the write is out of reach: a channel send or
			// close is synchronized, and a value returned, or passed to a call or a
			// goroutine, is written by a callee this rule does not read.
		}
	}
}

func (s globalScope) assign(n *ast.AssignStmt) {
	verb := "assigns"
	if n.Tok != token.ASSIGN {
		verb = "compound-assigns"
	}
	for _, lhs := range n.Lhs {
		s.target(lhs, verb)
	}
}

// target reports a write to e with verb, when e is rooted at a package-level
// variable. Beneath the variable it is a write to an element or field of it,
// whatever the verb. Assigning to a bare alias rebinds the local and writes
// nothing the variable holds, unlike taking its address or calling a pointer
// method on it, which hand out a value that is the variable's.
func (s globalScope) target(e ast.Expr, verb string) {
	r, ok := s.rooted(e)
	if !ok || r.rebinding() {
		return
	}
	if !r.bare {
		verb = "writes an element or field of"
	}
	s.report(e.Pos(), verb, r)
}

// addressOf reports &x where x is rooted at a package-level variable. The
// pointer reaches whatever the variable holds, a bare alias included, and what
// is written through it afterwards, by a call, reflect or unsafe, is out of
// sight.
func (s globalScope) addressOf(n *ast.UnaryExpr) {
	if n.Op != token.AND {
		return
	}
	if r, ok := s.rooted(n.X); ok {
		s.report(n.Pos(), "takes the address of", r)
	}
}

// pointerMethod reports a method call or method value that makes Go take the
// address of an operand rooted at a package-level variable: a pointer-receiver
// method on a value, whether the method is promoted or not, and whether the
// path to the value crosses a pointer or not. A variable holding a pointer is
// out of reach, as is a value-receiver method on a named map or slice: the
// receiver is the variable's value, and the method's own contract decides
// whether the call is safe, as for (*regexp.Regexp).MatchString. Such a method
// can write: a counter behind new(atomic.Int64) passes.
func (s globalScope) pointerMethod(n *ast.SelectorExpr) {
	sel, ok := s.pass.TypesInfo.Selections[n]
	if !ok {
		return // a qualified identifier, which is no selection
	}
	fn, ok := sel.Obj().(*types.Func)
	if !ok {
		return // a field
	}
	if _, ptr := types.Unalias(fn.Signature().Recv().Type()).(*types.Pointer); !ptr || !receiverIsValue(sel) {
		return
	}
	if r, ok := s.rooted(n.X); ok {
		s.report(n.Sel.Pos(), "calls a pointer method on", r)
	}
}

// receiverIsValue reports whether the operand a method selection calls its
// method on, after the embedded fields it promotes the method through, is not
// a pointer, so that a pointer-receiver method is handed its address.
func receiverIsValue(sel *types.Selection) bool {
	t := sel.Recv()
	path := sel.Index()
	for _, i := range path[:len(path)-1] {
		if p, ok := t.Underlying().(*types.Pointer); ok {
			t = p.Elem()
		}
		st, _ := t.Underlying().(*types.Struct) // only a struct embeds
		t = st.Field(i).Type()
	}
	_, ptr := t.Underlying().(*types.Pointer)
	return !ptr
}

// builtin reports delete, clear, copy and append on an operand rooted at a
// package-level variable, which write what it holds. append counts whatever the
// slicing, as it writes the variable's array whenever capacity allows: a
// read-only extension is slices.Concat. The same writes made by a function the
// operand is passed to, as slices.Sort does, are out of reach, since the rule
// reads no callee's body and a list of writers would be kept by hand.
func (s globalScope) builtin(call *ast.CallExpr) {
	id, ok := ast.Unparen(call.Fun).(*ast.Ident)
	if !ok {
		return
	}
	b, ok := s.pass.TypesInfo.Uses[id].(*types.Builtin)
	if !ok {
		return
	}
	if !slices.Contains([]string{"delete", "clear", "copy", "append"}, b.Name()) {
		return
	}
	// The first argument is the one written, and each takes at least one.
	if r, ok := s.rooted(call.Args[0]); ok {
		s.report(call.Pos(), "writes through "+b.Name()+" to", r)
	}
}

// reach is where an operand is rooted: the package-level variable, the local
// alias it goes through if any, and whether the operand is the root itself.
type reach struct {
	global *types.Var
	alias  *types.Var
	bare   bool
}

// rebinding reports whether the operand is a local alias itself, which an
// assignment replaces without writing what the variable holds. Only target asks.
func (r reach) rebinding() bool { return r.alias != nil && r.bare }

// rooted returns where e is rooted: the variable left when parentheses, field
// selections, indexes, slicings, dereferences and & are stripped from it, found
// by what its identifier resolves to and never by spelling. A nil e, a range
// clause's absent target, is rooted nowhere. What a call returns, a conversion
// or a type assertion yields, and a variable held in a struct, array or
// interface, are not roots, so a function's result is out of reach, as is an
// alias across functions.
func (s globalScope) rooted(e ast.Expr) (reach, bool) {
	bare := true
	for {
		e = ast.Unparen(e)
		switch x := e.(type) {
		case *ast.Ident:
			return s.reachOf(x, bare)
		case *ast.SelectorExpr:
			if s.qualifier(x.X) {
				return s.reachOf(x.Sel, bare) // pkg.Name is the variable itself
			}
		default:
			// Any other form is stripped below, or is rooted nowhere.
		}
		inner, ok := operandOf(e)
		if !ok {
			return reach{}, false
		}
		e, bare = inner, false
	}
}

// qualifier reports whether e is the name of an imported package.
func (s globalScope) qualifier(e ast.Expr) bool {
	id, ok := e.(*ast.Ident)
	if !ok {
		return false
	}
	_, isPkg := s.pass.TypesInfo.Uses[id].(*types.PkgName)
	return isPkg
}

// operandOf returns the operand e selects from, indexes, slices, dereferences or
// takes the address of.
func operandOf(e ast.Expr) (ast.Expr, bool) {
	switch x := e.(type) {
	case *ast.SelectorExpr:
		return x.X, true
	case *ast.IndexExpr:
		return x.X, true
	case *ast.SliceExpr:
		return x.X, true
	case *ast.StarExpr:
		return x.X, true
	case *ast.UnaryExpr:
		return x.X, x.Op == token.AND
	default:
		return nil, false
	}
}

// reachOf returns where the variable id names is rooted, when it is a
// package-level variable or a local alias of one.
func (s globalScope) reachOf(id *ast.Ident, bare bool) (reach, bool) {
	v, ok := s.pass.TypesInfo.Uses[id].(*types.Var)
	if !ok {
		return reach{}, false
	}
	if v.Pkg() != nil && v.Pkg().Scope().Lookup(v.Name()) == v {
		return reach{global: v, bare: bare}, true
	}
	global, aliased := s.aliases[v]
	return reach{global: global, alias: v, bare: bare}, aliased
}

// report names the variable, qualified by its package when it is another's, and
// the alias the write goes through.
func (s globalScope) report(pos token.Pos, verb string, r reach) {
	name := r.global.Name()
	if r.global.Pkg() != s.pass.Pkg {
		name = r.global.Pkg().Name() + "." + name
	}
	subject := "the package-level variable " + name
	if r.alias != nil {
		subject += " through its alias " + r.alias.Name()
	}
	s.pass.Reportf(pos, "%s %s; keep the state in a local or an argument", verb, subject)
}

// binding is a local and the one expression it receives.
type binding struct {
	local *types.Var
	src   ast.Expr
}

// findAliases takes to a fixed point the locals that hold part of a
// package-level variable: one receiving a value rooted at it, or at an alias,
// by :=, =, a var or a range clause. It is flow-insensitive, so a local that
// ever holds the value holds it throughout, and the remedy is a separate local.
// Each pass that grows the set adds a local, so one more than the bindings
// settles it.
func (s globalScope) findAliases(root ast.Node) {
	bindings := s.bindingsIn(root)
	for range len(bindings) + 1 {
		grew := false
		for _, b := range bindings {
			if _, done := s.aliases[b.local]; done {
				continue
			}
			if r, ok := s.rooted(b.src); ok {
				s.aliases[b.local] = r.global
				grew = true
			}
		}
		if !grew {
			return
		}
	}
}

// bindingsIn returns every local under root that can alias what it receives:
// one a binding gives a value of a type that shares what it points to.
func (s globalScope) bindingsIn(root ast.Node) []binding {
	var out []binding
	for n := range ast.Preorder(root) {
		for _, b := range s.bindingsOf(n) {
			id, ok := ast.Unparen(b.target).(*ast.Ident)
			if !ok {
				continue
			}
			if v, isVar := s.pass.TypesInfo.ObjectOf(id).(*types.Var); isVar && aliasable(v.Type()) {
				out = append(out, binding{v, b.src})
			}
		}
	}
	return out
}

// given is a target of a binding and the one expression it receives.
type given struct{ target, src ast.Expr }

// bindingsOf returns what the statement or spec n binds: by := or =, a var, or
// a range clause, each target with the value it receives.
func (s globalScope) bindingsOf(n ast.Node) []given {
	switch n := n.(type) {
	case *ast.AssignStmt:
		return pair(n.Lhs, n.Rhs)
	case *ast.ValueSpec:
		names := make([]ast.Expr, len(n.Names))
		for i, id := range n.Names {
			names[i] = id
		}
		return pair(names, n.Values)
	case *ast.RangeStmt:
		if !ranged(s.pass.TypesInfo.TypeOf(n.X)) {
			return nil
		}
		return []given{{n.Key, n.X}, {n.Value, n.X}}
	default:
		return nil
	}
}

// pair returns each target with the one value it receives: its own value, or
// for v, ok := m[k] the value of the one expression to its first target, which
// a function call's results are not rooted at.
func pair(lhs, rhs []ast.Expr) []given {
	var out []given
	switch {
	case len(lhs) == len(rhs):
		for i := range lhs {
			out = append(out, given{lhs[i], rhs[i]})
		}
	case len(lhs) == 2 && len(rhs) == 1:
		out = append(out, given{lhs[0], rhs[0]})
	}
	return out
}

// aliasable reports whether a local of type t shares what it was given: a map,
// a slice or a pointer. A struct, an array, an interface, a channel and a
// function are copies or out of reach, and an alias held in one, or reached
// through a conversion, a type assertion or a function result, is out of sight.
func aliasable(t types.Type) bool {
	switch t.Underlying().(type) {
	case *types.Map, *types.Slice, *types.Pointer:
		return true
	default:
		return false
	}
}

// ranged reports whether a range over a value of type t hands out elements the
// value holds: a map, a slice, an array, or a pointer to one. A channel, a
// string or a function hands out values it does not.
func ranged(t types.Type) bool {
	if p, ok := t.Underlying().(*types.Pointer); ok {
		t = p.Elem()
	}
	switch t.Underlying().(type) {
	case *types.Map, *types.Slice, *types.Array:
		return true
	default:
		return false
	}
}
