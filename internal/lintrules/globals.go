// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package lintrules

import (
	"go/ast"
	"go/token"
	"go/types"
	"slices"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/types/typeutil"
)

// globalWrites refuses writing a package-level variable, outside init, under
// ir, pass, compilers or emitters (invariant 5, which engine.Engine's concurrent
// use rests on): an assignment, ++ or --, an element or field, the address, a
// writing method, or delete, clear, copy or append, directly or through a local
// alias.
//
// # Out of reach
//
// The rule reports writes it sees, and code init stores or starts. Any other
// way code bound at initialization can run later and write is out of reach: a
// closure a method returns, or reflection. Functions below state the rest.
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
		for _, d := range f.Decls {
			c := globalCheck{pass: pass, aliases: map[*types.Var]*types.Var{}}
			c.findAliases(d)
			for _, root := range afterInit(d) {
				c.check(root)
			}
		}
	}
	return nil, nil
}

// afterInit returns the code of d that can run once initialization is over: the
// body of a function or method but init, and in a declaration at package level
// or in init, which run once, each outermost function literal, each go
// statement and each selector not called there, as a method value is, since any
// of them can outlive initialization. What lies beneath such a selector is
// checked with it, so a method called there draws though it runs once. A method
// called there that keeps its receiver, as one returning a closure does, is out
// of reach.
func afterInit(d ast.Decl) []ast.Node {
	if fn, isFunc := d.(*ast.FuncDecl); isFunc && fn.Body != nil && (fn.Recv != nil || fn.Name.Name != "init") {
		return []ast.Node{fn.Body}
	}
	var roots []ast.Node
	called := map[ast.Expr]bool{}
	ast.Inspect(d, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.FuncLit:
			roots = append(roots, n.Body)
			return false
		case *ast.GoStmt:
			roots = append(roots, n.Call)
			return false
		case *ast.CallExpr:
			called[calleeOf(n.Fun)] = true
		case *ast.SelectorExpr:
			if called[n] {
				return true
			}
			roots = append(roots, n)
			return false
		default:
			// Anything else only holds what the cases above look for.
		}
		return true
	})
	return roots
}

// calleeOf returns the expression a call runs, with parentheses and a generic's
// type arguments stripped from it.
func calleeOf(fun ast.Expr) ast.Expr {
	fun = ast.Unparen(fun)
	switch x := fun.(type) {
	case *ast.IndexExpr:
		return x.X
	case *ast.IndexListExpr:
		return x.X
	default:
		return fun
	}
}

// globalCheck finds the writes in one top-level declaration: aliases maps each
// local that holds part of a package-level variable to the variable.
type globalCheck struct {
	pass    *analysis.Pass
	aliases map[*types.Var]*types.Var
}

// check reports each write under root.
func (c globalCheck) check(root ast.Node) {
	for n := range ast.Preorder(root) {
		switch n := n.(type) {
		case *ast.AssignStmt:
			// := cannot reach a package-level variable: its targets are new
			// locals, or locals of the same scope.
			if n.Tok != token.DEFINE {
				c.assign(n)
			}
		case *ast.IncDecStmt:
			verb := "increments"
			if n.Tok == token.DEC {
				verb = "decrements"
			}
			c.target(n.X, verb)
		case *ast.RangeStmt:
			if n.Tok == token.ASSIGN {
				c.target(n.Key, "assigns")
				c.target(n.Value, "assigns")
			}
		case *ast.UnaryExpr:
			c.addressOf(n)
		case *ast.CallExpr:
			c.builtin(n)
			c.methodExpression(n)
		case *ast.SelectorExpr:
			c.writingMethod(n)
		default:
			// Nothing else writes, or the write is out of reach: a channel send or
			// close is synchronized, and a value returned, or passed to a call or a
			// goroutine, is written by a callee this rule does not read.
		}
	}
}

func (c globalCheck) assign(n *ast.AssignStmt) {
	verb := "assigns"
	if n.Tok != token.ASSIGN {
		verb = "compound-assigns"
	}
	for _, lhs := range n.Lhs {
		c.target(lhs, verb)
	}
}

// target reports a write to e with verb, when e is rooted at a package-level
// variable. Beneath the variable it is a write to an element or field of it,
// whatever the verb. Assigning to a bare alias rebinds the local and writes
// nothing the variable holds, unlike taking its address or calling a method on
// it that can write, which hand out a value that is the variable's.
func (c globalCheck) target(e ast.Expr, verb string) {
	r, ok := c.rooted(e)
	if !ok || r.rebinding() {
		return
	}
	if !r.bare {
		verb = "writes an element or field of"
	}
	c.report(e.Pos(), verb, r)
}

// addressOf reports &x where x is rooted at a package-level variable. The
// pointer reaches whatever the variable holds, a bare alias included, and what
// is written through it afterwards, by a call, reflect or unsafe, is out of
// sight.
func (c globalCheck) addressOf(n *ast.UnaryExpr) {
	if n.Op != token.AND {
		return
	}
	if r, ok := c.rooted(n.X); ok {
		c.report(n.Pos(), "takes the address of", r)
	}
}

// writingMethod reports a method call or method value on an operand rooted at a
// package-level variable, when the method's receiver is a pointer, map or
// slice, through which it can write what the variable holds. The one listed
// receiver, *encoding/base64.Encoding, is read-only and stays legal, and an
// unlisted one draws until it is listed. A method on a struct or array value or
// an interface is out of reach, though it can write through a map, slice or
// pointer the value holds.
func (c globalCheck) writingMethod(n *ast.SelectorExpr) {
	sel, ok := c.pass.TypesInfo.Selections[n]
	if !ok {
		return // a qualified identifier, which is no selection
	}
	fn, ok := sel.Obj().(*types.Func)
	if !ok {
		return // a field
	}
	if !mayWrite(fn) {
		return
	}
	if r, ok := c.rooted(n.X); ok {
		c.report(n.Sel.Pos(), "uses a method that can write", r)
	}
}

// methodExpression reports (*T).M(v), which is v.M() with the receiver spelled
// as the first argument. A method expression not called is a function value,
// out of reach as any other is.
func (c globalCheck) methodExpression(call *ast.CallExpr) {
	n, ok := calleeOf(call.Fun).(*ast.SelectorExpr)
	if !ok {
		return
	}
	sel, ok := c.pass.TypesInfo.Selections[n]
	if !ok || sel.Kind() != types.MethodExpr {
		return
	}
	fn, ok := sel.Obj().(*types.Func)
	if !ok || !mayWrite(fn) {
		return
	}
	// The receiver is the first argument, which a call of it must give.
	if r, ok := c.rooted(call.Args[0]); ok {
		c.report(n.Sel.Pos(), "uses a method that can write", r)
	}
}

// mayWrite reports whether a method's receiver can write what it holds.
func mayWrite(fn *types.Func) bool {
	recv := fn.Signature().Recv().Type()
	return aliasable(recv) && types.TypeString(recv, nil) != "*encoding/base64.Encoding"
}

// builtin reports delete, clear, copy and append on an operand rooted at a
// package-level variable, which write what it holds. append counts whatever the
// slicing, as it writes the variable's array whenever capacity allows: a
// read-only extension is slices.Concat. The same writes made by a function the
// operand is passed to, as slices.Sort does, are out of reach, since the rule
// reads no callee's body and a list of writers would be kept by hand.
func (c globalCheck) builtin(call *ast.CallExpr) {
	b, ok := typeutil.Callee(c.pass.TypesInfo, call).(*types.Builtin)
	if !ok {
		return
	}
	if !slices.Contains([]string{"delete", "clear", "copy", "append"}, b.Name()) {
		return
	}
	// The first argument is the one written, and each takes at least one.
	if r, ok := c.rooted(call.Args[0]); ok {
		c.report(call.Pos(), "writes through "+b.Name()+" to", r)
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
func (c globalCheck) rooted(e ast.Expr) (reach, bool) {
	bare := true
	for {
		e = ast.Unparen(e)
		switch x := e.(type) {
		case *ast.Ident:
			return c.reachOf(x, bare)
		case *ast.SelectorExpr:
			if c.qualifier(x.X) {
				return c.reachOf(x.Sel, bare) // pkg.Name is the variable itself
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
func (c globalCheck) qualifier(e ast.Expr) bool {
	id, ok := e.(*ast.Ident)
	if !ok {
		return false
	}
	_, isPkg := c.pass.TypesInfo.Uses[id].(*types.PkgName)
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
func (c globalCheck) reachOf(id *ast.Ident, bare bool) (reach, bool) {
	v, ok := c.pass.TypesInfo.Uses[id].(*types.Var)
	if !ok {
		return reach{}, false
	}
	if v.Pkg() != nil && v.Pkg().Scope().Lookup(v.Name()) == v {
		return reach{global: v, bare: bare}, true
	}
	global, aliased := c.aliases[v]
	return reach{global: global, alias: v, bare: bare}, aliased
}

// report names the variable, qualified by its package when it is another's, and
// the alias the write goes through.
func (c globalCheck) report(pos token.Pos, verb string, r reach) {
	name := r.global.Name()
	if r.global.Pkg() != c.pass.Pkg {
		name = r.global.Pkg().Name() + "." + name
	}
	subject := "the package-level variable " + name
	if r.alias != nil {
		subject += " through its alias " + r.alias.Name()
	}
	c.pass.Reportf(pos, "%s %s; keep the state in a local or an argument", verb, subject)
}

// binding is a local and the one expression it receives.
type binding struct {
	local *types.Var
	src   ast.Expr
}

// findAliases takes to a fixed point the locals that hold part of a
// package-level variable: one receiving a value rooted at it, or at an alias,
// by :=, =, a var, a range clause or a return. It is flow-insensitive, so a
// local that ever holds the value holds it throughout, and the remedy is a
// separate local. Each pass that grows the set adds a local, so one more than
// the bindings settles it.
func (c globalCheck) findAliases(root ast.Node) {
	bindings := c.bindingsIn(root)
	for range len(bindings) + 1 {
		grew := false
		for _, b := range bindings {
			if _, done := c.aliases[b.local]; done {
				continue
			}
			if r, ok := c.rooted(b.src); ok {
				c.aliases[b.local] = r.global
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
func (c globalCheck) bindingsIn(root ast.Node) []binding {
	var out []binding
	for n := range ast.Preorder(root) {
		for _, b := range c.bindingsOf(n) {
			id, ok := ast.Unparen(b.target).(*ast.Ident)
			if !ok {
				continue
			}
			if v, isVar := c.pass.TypesInfo.ObjectOf(id).(*types.Var); isVar && aliasable(v.Type()) {
				out = append(out, binding{v, b.src})
			}
		}
	}
	return out
}

// given is a target of a binding and the one expression it receives.
type given struct{ target, src ast.Expr }

// bindingsOf returns what the statement, spec or function n binds: by := or =,
// a var, a range clause or a return, each target with the value it receives.
func (c globalCheck) bindingsOf(n ast.Node) []given {
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
		if !ranged(c.pass.TypesInfo.TypeOf(n.X)) {
			return nil
		}
		return []given{{n.Key, n.X}, {n.Value, n.X}}
	case *ast.FuncDecl:
		return returned(n.Type, n.Body)
	case *ast.FuncLit:
		return returned(n.Type, n.Body)
	default:
		return nil
	}
}

// returned returns each named result of a function with the value a return in
// its own body gives it, through which a deferred literal can write. A bare
// return binds nothing here, and a literal's returns bind the literal's results.
func returned(ft *ast.FuncType, body *ast.BlockStmt) []given {
	if ft.Results == nil || body == nil {
		return nil
	}
	var names []ast.Expr
	for _, field := range ft.Results.List {
		for _, id := range field.Names {
			names = append(names, id)
		}
	}
	var out []given
	ast.Inspect(body, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.FuncLit:
			return false
		case *ast.ReturnStmt:
			out = append(out, pair(names, n.Results)...)
		default:
			// Anything else only holds the returns.
		}
		return true
	})
	return out
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
// function are copies or out of reach, and an alias held in one, or in a slice
// or map literal, or reached through a conversion, a type assertion or a
// function result, is out of sight.
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
