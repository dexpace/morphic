package archtest_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// loweringRecursions are every recursion among the lowerings in
// compilers/openapi, and why each one is allowed to be recursive.
//
// A set of more than one is mutual recursion, and none of its lowerings can be
// tested or moved on its own: its callees include something that calls back to
// it, so the whole set changes together or not at all. A set of one is a
// lowering that calls itself — moveable, but pinned all the same, because
// recursion is permitted here only against an explicit depth counter and this is
// the check that says which functions need one.
//
// The sets are neither all methods nor all free functions. The schema walk
// converted to free functions and is just as recursive for it, and the anchor
// walk is a pair of methods; what binds a set together is the call graph, not
// the receiver.
//
// The sets are listed longest first, ties broken by first member — the order the
// pin produces, and also the order they matter in.
var loweringRecursions = [][]string{
	// The schema walk. Lowering a schema resolves the references inside it, and
	// resolving a reference lowers what it names. micro-compiler-design §5
	// records this as the reason schema, compose and resolve cannot be separated
	// into packages.
	schemaRecursion,
	// Callbacks. An operation may declare callbacks, each of which is a path item
	// holding operations of its own (ir-design §8.1), so the operation lowering
	// reaches itself through them.
	{"lowerCallbackOps", "lowerCallbacks", "lowerOperation"},
	// The $dynamicAnchor index. walk hands a mapping to walkMapping, which walks
	// each of that mapping's values back through walk. Bounded by charge, which
	// refuses past maxDynamicAnchorDepth or a spent node budget and records the
	// refusal, so a caller learns the index is partial.
	{"anchorWalk.walk", "anchorWalk.walkMapping"},
	// Property lookup through a composition. Finding a property by wire name
	// descends into a model's base and mixins, each of which is a model whose
	// properties are looked up the same way.
	{"propIDByWire", "propIDInComposition"},
	// Multipart body parts. A body may compose with allOf instead of declaring
	// properties, and each branch is a schema whose parts are read the same way.
	// Bounded by maxPartCompositionDepth.
	{"bodyParts"},
}

// schemaRecursion is the largest of them, named because the design refers to it
// directly and because #176's package split is sized by it. Its members are free
// functions now; the set did not change when they stopped being methods.
// The two exported names are the walk's entry points, which is what the
// operation lowering reaches it by; the rest of the set is unexported because
// nothing outside the schema package has any business entering mid-walk.
var schemaRecursion = []string{
	"CarriedRef", "Ref", "buildComposedVariant", "buildTuple",
	"composedVariant", "fillAdditional", "fillAllOf", "fillModelProperties",
	"hoistSubSchema", "lower", "lowerAllOf", "lowerArray",
	"lowerBesideUnmodeledUnion", "lowerCoDeclaredUnion", "lowerDistributedUnion",
	"lowerModel", "lowerOneOfAnyOf", "lowerSchemaBody", "lowerTyped", "lowerUnion",
	"lowerUntyped", "patternProps", "refSiteRef", "refTypeRef", "resolveSchemaRef",
	"schemaBody", "schemaRefHomed",
}

// loweringPackages are the directories whose sources the call graph reads,
// repo-relative. A lowering is a function over the immutable context, so a
// package declaring one belongs here — which is a fact about the tree, and
// TestLoweringCallGraph_ReadsEveryPackageThatLowers derives it rather than
// trusting this list.
var loweringPackages = []string{
	"compilers/openapi",
	"compilers/openapi/internal/auth",
	"compilers/openapi/internal/operation",
	"compilers/openapi/internal/schema",
}

// TestLoweringRecursion_IsOnlyTheKnownCycles pins which lowerings are recursive,
// and holds the answer to the sets above.
//
// It exists because measuring this with a regex got it wrong by more than
// double. A pattern ending a function at the first `}` in column zero runs past
// any one-line method — `func (l *lowerer) appendDiag(d ir.Diagnostic) { … }` has
// none — so every such method absorbed its successor's body and inherited its
// calls. The diagnostic recorder came out calling schemaRef, which stitched the
// whole diagnostic path into the schema cycle. A parser cannot make that mistake:
// it is told where a function ends.
//
// Every cycle is asserted, not only the largest. Checking one would let a new
// recursion appear anywhere else unnoticed, which is the same blindness as not
// checking at all — and the smaller ones are as unconvertible as the big one.
func TestLoweringRecursion_IsOnlyTheKnownCycles(t *testing.T) {
	t.Parallel()
	calls := loweringCallGraph(t)
	require.NotEmpty(t, calls, "no lowerings found; the parse or the package path changed")

	var cycles [][]string
	for _, c := range stronglyConnected(calls) {
		if len(c) == 1 && !calls[c[0]][c[0]] {
			continue
		}
		sort.Strings(c)
		cycles = append(cycles, c)
	}
	// Ties are broken by first member because there is more than one pair now,
	// and sort.Slice is not stable: length alone would leave the two pairs in
	// whichever order the sort happened to land them.
	sort.Slice(cycles, func(i, j int) bool {
		if len(cycles[i]) != len(cycles[j]) {
			return len(cycles[i]) > len(cycles[j])
		}
		return cycles[i][0] < cycles[j][0]
	})
	require.NotEmpty(t, cycles, "the schema walk is mutually recursive; finding none means the graph is wrong")

	assert.Equal(t, loweringRecursions, cycles,
		"the lowering call graph's recursion changed; anything joining one of these sets can no longer be moved alone")
}

// decl is one declaration the call graph holds: the name it is known by, the
// receiver a method reaches its siblings through, and the scope its selectors
// resolve against.
type decl struct {
	name     string // "lower", or "anchorWalk.walk" for a method
	recv     string // the receiver's identifier; empty unless this is a method that names one
	recvType string // the receiver's type; empty for a free function
	fn       *ast.FuncDecl
	scope    *pkgScope
}

// declOf reads fn into the one node the graph holds for it.
//
// A method is keyed "<receiver type>.<method>" rather than by its bare name.
// The graph refuses a name collision instead of resolving it, and walk exists
// both as a method on anchorWalk and as a name a free function is free to take,
// so the two have to stay distinguishable.
//
// A receiver with no identifier, or one spelled _, leaves recv empty: such a
// method cannot write its own name, so nothing in its body resolves through it.
func declOf(t *testing.T, fn *ast.FuncDecl, scope *pkgScope) decl {
	t.Helper()
	require.NotNil(t, fn.Body, "a declaration with no body names no callees")
	require.NotNil(t, scope, "a declaration resolves against its package's scope")

	d := decl{name: fn.Name.Name, fn: fn, scope: scope}
	if fn.Recv == nil {
		return d
	}
	require.Len(t, fn.Recv.List, 1, "a method declares exactly one receiver")

	field := fn.Recv.List[0]
	d.recvType = typeNameOf(field.Type)
	require.NotEmpty(t, d.recvType,
		"unreadable receiver on %s; keying it as a free function would hide its edges", fn.Name.Name)
	d.name = d.recvType + "." + fn.Name.Name
	if len(field.Names) == 1 && field.Names[0].Name != "_" {
		d.recv = field.Names[0].Name
	}
	return d
}

// typeNameOf returns the type a type expression names, or "" when it names none
// a package this pin reads could declare a method on.
//
// It reads the grammar a receiver, a field, a parameter and a var all share:
// [*]TypeName[TypeArgs]. The pointer and then the type arguments come off, and
// what is left has to be an identifier — so `pkg.T`, `[]T`, `map[K]V`, a func
// type and a literal interface are all refused rather than reduced to some inner
// name, because a value of one is not a value of the type that name would key.
func typeNameOf(expr ast.Expr) string {
	if star, isStar := expr.(*ast.StarExpr); isStar {
		expr = star.X
	}
	if idx, isIndex := expr.(*ast.IndexExpr); isIndex { // T[K]
		expr = idx.X
	}
	if idx, isIndex := expr.(*ast.IndexListExpr); isIndex { // T[K, V]
		expr = idx.X
	}
	id, isIdent := expr.(*ast.Ident)
	if !isIdent {
		return ""
	}
	return id.Name
}

// maxResolveDepth bounds how far pkgScope.typeOf follows a chain of selectors
// and nested calls. A source expression is finite, so the walk terminates
// without it; the cap is written down because recursion here is permitted only
// against one.
const maxResolveDepth = 8

// pkgScope is what one package declares that a selector can be resolved
// against: the fields of each struct it defines, the type each of its
// declarations returns, and the nodes the graph holds for it.
//
// It is deliberately not a type checker. Declarations are all it reads, so a
// type they do not settle — an interface value's dynamic type, a type another
// package declares, a range variable's element — resolves to nothing and the
// selector on it stays unread. Matching such a selector on its bare method name
// instead would tie every `.Ref` in the package to whatever lowering shares the
// name, which is the resolution the one-namespace rule refuses.
type pkgScope struct {
	fields  map[string]map[string]string // struct type -> field -> the type it names
	results map[string]string            // node name -> the single type it returns
	nodes   map[string]bool              // the node names this package declares
}

// newPkgScope returns an empty scope, which resolves nothing.
func newPkgScope() *pkgScope {
	return &pkgScope{
		fields:  map[string]map[string]string{},
		results: map[string]string{},
		nodes:   map[string]bool{},
	}
}

// addTypes indexes the struct fields f declares.
//
// Only a named field counts. An embedded one promotes the method set of
// whatever it embeds, and a scope built from declarations cannot follow that, so
// it is left unresolved rather than resolved to the wrong receiver.
func (s *pkgScope) addTypes(f *ast.File) {
	for _, d := range f.Decls {
		gen, isGen := d.(*ast.GenDecl)
		if !isGen || gen.Tok != token.TYPE {
			continue
		}
		for _, spec := range gen.Specs {
			ts, isType := spec.(*ast.TypeSpec)
			if !isType {
				continue
			}
			if st, isStruct := ts.Type.(*ast.StructType); isStruct {
				s.fields[ts.Name.Name] = fieldTypes(st)
			}
		}
	}
}

// fieldTypes maps each named field of st to the type written beside it.
func fieldTypes(st *ast.StructType) map[string]string {
	out := map[string]string{}
	if st.Fields == nil {
		return out
	}
	for _, f := range st.Fields.List {
		named := typeNameOf(f.Type)
		if named == "" {
			continue
		}
		for _, id := range f.Names {
			out[id.Name] = named
		}
	}
	return out
}

// addDecl records d as a node of this package, along with the type it returns.
//
// A declaration returning anything but one named type records none: a caller
// assigning from it has nothing this scope can name, and half an answer is the
// guess the whole design refuses.
func (s *pkgScope) addDecl(d decl) {
	s.nodes[d.name] = true

	results := d.fn.Type.Results
	if results == nil || len(results.List) != 1 || len(results.List[0].Names) > 1 {
		return
	}
	if named := typeNameOf(results.List[0].Type); named != "" {
		s.results[d.name] = named
	}
}

// declares reports whether name is a node this package holds.
//
// A resolved selector is looked for in the package that resolved it, not in the
// graph's flat namespace. The type name came from one package's declarations, so
// the method has to as well: two packages declaring the same type name would
// otherwise let a field read on one match a method on the other.
func (s *pkgScope) declares(name string) bool {
	return s.nodes[name]
}

// typeOf resolves the type an expression evaluates to, named as this package
// declares it, or "" when its declarations do not say.
func (s *pkgScope) typeOf(x ast.Expr, env map[string]string, depth int) string {
	if depth > maxResolveDepth {
		return ""
	}
	switch v := x.(type) {
	case *ast.Ident:
		return env[v.Name]
	case *ast.ParenExpr:
		return s.typeOf(v.X, env, depth+1)
	case *ast.StarExpr: // a dereference; a pointer and its value key the same node
		return s.typeOf(v.X, env, depth+1)
	case *ast.UnaryExpr: // &T{…}
		if v.Op != token.AND {
			return ""
		}
		return s.typeOf(v.X, env, depth+1)
	case *ast.CompositeLit:
		return typeNameOf(v.Type)
	case *ast.CallExpr:
		return s.results[s.nodeNamed(v.Fun, env, depth+1)]
	case *ast.SelectorExpr:
		return s.fields[s.typeOf(v.X, env, depth+1)][v.Sel.Name]
	default:
		return ""
	}
}

// nodeNamed returns the node an expression names: the bare name for a free
// function, "<type>.<method>" when the base's type resolves, and "" when it does
// not.
func (s *pkgScope) nodeNamed(x ast.Expr, env map[string]string, depth int) string {
	switch v := x.(type) {
	case *ast.Ident:
		return v.Name
	case *ast.SelectorExpr:
		base := s.typeOf(v.X, env, depth+1)
		if base == "" {
			return ""
		}
		return base + "." + v.Sel.Name
	default:
		return ""
	}
}

// bindingsOf maps each identifier d's body can write to the type it holds.
//
// Only what d's own signature and body state outright is read: the receiver, the
// parameters and named results, a var with a written type, and a short
// declaration whose value is a composite literal or a call to something this
// package declares. A package-level var, a range variable, a type switch's
// binding and every other form a type checker would have to work out are left
// out, so a selector on one resolves to nothing.
//
// An identifier bound twice to different types is dropped. Go scopes a shadowed
// name and this does not, so the two bindings are indistinguishable here and
// neither of them can be trusted.
func bindingsOf(d decl) map[string]string {
	env := map[string]string{}
	shadowed := map[string]bool{}
	bind := func(name, named string) {
		if name == "" || name == "_" || named == "" {
			return
		}
		if held, seen := env[name]; seen && held != named {
			shadowed[name] = true
		}
		env[name] = named
	}

	bind(d.recv, d.recvType)
	bindFields(bind, d.fn.Type.Params)
	bindFields(bind, d.fn.Type.Results)
	ast.Inspect(d.fn.Body, func(n ast.Node) bool {
		switch v := n.(type) {
		case *ast.FuncLit:
			bindFields(bind, v.Type.Params)
			bindFields(bind, v.Type.Results)
		case *ast.ValueSpec:
			bindSpec(bind, d.scope, env, v)
		case *ast.AssignStmt:
			bindAssign(bind, d.scope, env, v)
		default:
			// No other node states a type outright for a name it introduces.
		}
		return true
	})

	for name := range shadowed {
		delete(env, name)
	}
	return env
}

// bindFields binds every name a parameter, result or receiver list declares to
// the type written beside it.
func bindFields(bind func(name, named string), list *ast.FieldList) {
	if list == nil {
		return
	}
	for _, f := range list.List {
		named := typeNameOf(f.Type)
		for _, id := range f.Names {
			bind(id.Name, named)
		}
	}
}

// bindSpec binds a var or const declaration: the type it writes when it writes
// one, and otherwise the type of the value standing beside each name.
func bindSpec(bind func(name, named string), scope *pkgScope, env map[string]string, spec *ast.ValueSpec) {
	if spec.Type != nil {
		named := typeNameOf(spec.Type)
		for _, id := range spec.Names {
			bind(id.Name, named)
		}
		return
	}
	if len(spec.Names) != len(spec.Values) {
		return
	}
	for i, id := range spec.Names {
		bind(id.Name, scope.typeOf(spec.Values[i], env, 0))
	}
}

// bindAssign binds a short variable declaration to the type of the value beside
// each name. A form spreading one value over several names — a call with two
// results, a comma-ok — says nothing about any of them here, so it binds none.
func bindAssign(bind func(name, named string), scope *pkgScope, env map[string]string, as *ast.AssignStmt) {
	if as.Tok != token.DEFINE || len(as.Lhs) != len(as.Rhs) {
		return
	}
	for i, lhs := range as.Lhs {
		id, isIdent := lhs.(*ast.Ident)
		if !isIdent {
			continue
		}
		bind(id.Name, scope.typeOf(as.Rhs[i], env, 0))
	}
}

// loweringCallGraph maps every lowering to the ones it depends on.
//
// Methods are read alongside free functions. Reading free functions alone left
// the anchor walk's recursion out of the graph entirely and would have let any
// new one join it unremarked (GitHub #224), which is the same silence the value
// edges below were about.
//
// The lowering spans packages now, and all of them are read into one namespace.
// That is sound rather than convenient: a call from openapi into the schema
// package is a selector this graph does not record — a package qualifier names
// no value, so nothing resolves it — but no such edge can close a cycle, because
// Go refuses the import that would let the schema package call back. Every cycle
// there can be is inside one package.
//
// The packages are named rather than globbed, and
// TestLoweringCallGraph_ReadsEveryPackageThatLowers derives the same set from
// the tree so the naming cannot go stale in silence.
func loweringCallGraph(t *testing.T) map[string]map[string]bool {
	t.Helper()
	root := repoRoot(t)
	// One list across the packages, but one scope inside each: the flat namespace
	// is what the collision check below polices, and a selector still resolves
	// only against the package it was written in.
	decls := make([]decl, 0, len(loweringPackages))
	for _, pkg := range loweringPackages {
		decls = append(decls, declsOf(t, parseSources(t, filepath.Join(root, pkg), pkg))...)
	}

	// One namespace across every package read. Go would allow the same name in
	// two of them; this graph would silently keep one and drop the other's edges,
	// so the collision is refused rather than resolved.
	known := map[string]bool{}
	for _, d := range decls {
		require.False(t, known[d.name],
			"two lowerings are named %q; the call graph cannot tell them apart", d.name)
		known[d.name] = true
	}

	graph := map[string]map[string]bool{}
	for _, d := range decls {
		graph[d.name] = calleesOf(d, known)
	}
	return graph
}

// declsOf reads one package's files into the declarations the graph holds, each
// carrying the scope its selectors resolve against.
//
// The scope is that one package and nothing else, which is the whole of what a
// parser can be sure of: an unqualified type name means the type declared
// alongside, and a name no declaration there settles stays unresolved. Its nodes
// and result types are filled after the declarations are built, over the same
// pointer every one of them holds.
func declsOf(t *testing.T, files []*ast.File) []decl {
	t.Helper()
	scope := newPkgScope()
	for _, f := range files {
		scope.addTypes(f)
	}
	var decls []decl
	for _, f := range files {
		for _, d := range f.Decls {
			fn, isFunc := d.(*ast.FuncDecl)
			if !isFunc || fn.Body == nil {
				continue
			}
			decls = append(decls, declOf(t, fn, scope))
		}
	}
	for _, d := range decls {
		scope.addDecl(d)
	}
	return decls
}

// parseSources parses one package's production sources.
func parseSources(t *testing.T, dir, pkg string) []*ast.File {
	t.Helper()
	paths, err := filepath.Glob(filepath.Join(dir, "*.go"))
	require.NoError(t, err)
	require.NotEmpty(t, paths, "no sources under %s; the lowering moved and this pin no longer reads it", pkg)

	var files []*ast.File
	for _, path := range paths {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		f, parseErr := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		require.NoError(t, parseErr, "parse %s", path)
		files = append(files, f)
	}
	require.NotEmpty(t, files, "only tests under %s; the lowering moved and this pin no longer reads it", pkg)
	return files
}

// calleesOf returns the lowerings d's body names, whether it calls them, hands
// them on as a value, or reaches them through its own receiver.
//
// All three are edges, and for the same reason: the name is written here, so
// this function decides which lowering runs and cannot be moved without it. What
// is not an edge is a function this one receives as a parameter — that name is
// written by its caller, and the caller's own body is where it is recorded.
// Reading call position alone missed three live handoffs to slices.ContainsFunc
// (GitHub #210).
//
// An identifier only counts in value position. A selector's field, a composite
// literal's key and a parameter's own name are identifiers too, and every one of
// them collides with some lowering here — `.Ref` on a parsed schema is the
// spelling that matters most, since it would otherwise tie the whole package to
// the schema entry point.
//
// The exception is a selector whose base has a type this package's declarations
// settle: `w.walkMapping` on the receiver, `w.walk` on a local built by a
// constructor, `groups.group` on a parameter. That selector is an edge to
// "<type>.<name>", because the method name is written here and this declaration
// cannot be moved without it. Reading only the receiver left the first shape and
// the second unread, which was GitHub #323.
//
// What stays unread is what the declarations do not settle: a call through an
// interface, a value of a type another package declares, a range variable. Those
// resolve to nothing and record nothing. The alternative — matching the bare
// method name against the graph — is the resolution the one-namespace rule
// refuses, and would tie every `.Ref` in the package to the schema entry point.
//
// A name may be d's own. Direct recursion is recursion, so the self edge is
// recorded and TestLoweringRecursion_IsOnlyTheKnownCycles reads a self-loop as a
// set of one. Dropping it would have kept anchorWalk.walk's descent into itself
// out of the graph next to the sibling call, and left every self-recursive
// lowering unpinned.
//
// Position is judged, scope is not: a local that shadowed a lowering's name
// would read as an edge to it. Nothing here does that, and the error runs in the
// safe direction for a pin whose job is to notice dependencies — a spurious
// member shows up in a pinned set and gets read, where a missing one is the
// silence #210 was about.
func calleesOf(d decl, known map[string]bool) map[string]bool {
	skip := map[*ast.Ident]bool{}
	ast.Inspect(d.fn.Body, func(n ast.Node) bool {
		switch v := n.(type) {
		case *ast.SelectorExpr:
			skip[v.Sel] = true
		case *ast.KeyValueExpr:
			if id, ok := v.Key.(*ast.Ident); ok {
				skip[id] = true
			}
		case *ast.Field:
			for _, id := range v.Names {
				skip[id] = true
			}
		}
		return true
	})

	env := bindingsOf(d)
	out := map[string]bool{}
	record := func(name string) {
		if known[name] {
			out[name] = true
		}
	}
	ast.Inspect(d.fn.Body, func(n ast.Node) bool {
		if sel, isSel := n.(*ast.SelectorExpr); isSel {
			// Recorded against the resolving package rather than the flat
			// namespace, so a field read on one package's type can never match a
			// method on another's type of the same name.
			if name := d.scope.nodeNamed(sel, env, 0); d.scope.declares(name) {
				out[name] = true
			}
			return true
		}
		if call, isCall := n.(*ast.CallExpr); isCall {
			if fun, isIdent := call.Fun.(*ast.Ident); isIdent {
				record(fun.Name)
			}
			return true
		}
		id, isIdent := n.(*ast.Ident)
		if isIdent && !skip[id] {
			record(id.Name)
		}
		return true
	})
	return out
}

// stronglyConnected returns the graph's strongly connected components (Tarjan).
// Edges to names the graph does not hold — anything in another package, or
// reached by a route calleesOf does not read — are ignored, so only recursion
// within compilers/openapi's own lowerings is described.
func stronglyConnected(graph map[string]map[string]bool) [][]string {
	var (
		index  = map[string]int{}
		low    = map[string]int{}
		onStk  = map[string]bool{}
		stack  []string
		comps  [][]string
		nextID int
	)
	var visit func(string)
	visit = func(v string) {
		index[v], low[v] = nextID, nextID
		nextID++
		stack = append(stack, v)
		onStk[v] = true

		succ := make([]string, 0, len(graph[v]))
		for w := range graph[v] {
			if _, isLowering := graph[w]; isLowering {
				succ = append(succ, w)
			}
		}
		sort.Strings(succ)
		for _, w := range succ {
			switch {
			case !visited(index, w):
				visit(w)
				low[v] = min(low[v], low[w])
			case onStk[w]:
				low[v] = min(low[v], index[w])
			}
		}
		if low[v] == index[v] {
			var comp []string
			for {
				w := stack[len(stack)-1]
				stack = stack[:len(stack)-1]
				onStk[w] = false
				comp = append(comp, w)
				if w == v {
					break
				}
			}
			comps = append(comps, comp)
		}
	}
	names := make([]string, 0, len(graph))
	for n := range graph {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		if !visited(index, n) {
			visit(n)
		}
	}
	return comps
}

// visited reports whether Tarjan has assigned n an index. It is a helper rather
// than a bare map lookup because index 0 is a real index, so the zero value
// cannot distinguish "unvisited" from "first".
func visited(index map[string]int, n string) bool {
	_, ok := index[n]
	return ok
}

// TestLoweringCallGraph_RecordsAHandoffAsAnEdge holds the graph to the fix for
// GitHub #210. Reading call position alone described these three functions as
// depending on nothing, while each decides which lowering runs by naming it.
//
// It asserts the edges rather than the cycles because none of them closes one
// today: a dependency the graph cannot see is unheld whether or not it currently
// changes an answer, and that was the whole finding.
func TestLoweringCallGraph_RecordsAHandoffAsAnEdge(t *testing.T) {
	t.Parallel()
	graph := loweringCallGraph(t)

	handoffs := map[string]string{
		"classifyUnionSiblings":     "isInlineBranch",
		"unionBranchesDeclareShape": "branchDeclaresShape",
		"oneOfAnyOfHasNull":         "isNullSchema",
	}
	for from, to := range handoffs {
		require.Contains(t, graph, from, "the graph no longer holds %q", from)
		assert.True(t, graph[from][to],
			"%q hands %q on as a value, which is an edge; the graph records only calls again", from, to)
	}
}

// TestCalleesOf_SeparatesAValueFromWhatIsNotOne pins the two halves of the
// judgement calleesOf makes, on planted sources rather than on the tree: an
// identifier written in value position is an edge, and one that only looks like
// a name — a selector's field, a literal's key, a parameter — is not.
//
// The negative half is what the first attempt at #210 got wrong. Recording every
// matching identifier read `.Ref` on a parsed schema as a reference to the schema
// entry point, which tied nine unrelated functions into the walk's cycle.
func TestCalleesOf_SeparatesAValueFromWhatIsNotOne(t *testing.T) {
	t.Parallel()
	outside := map[string]bool{"Ref": true, "lower": true, "self": true}
	tests := []struct {
		name, body string
		want       []string
	}{
		{name: "a call", body: "lower(x)", want: []string{"lower"}},
		{name: "a value handed to another function", body: "apply(x, lower)", want: []string{"lower"}},
		{name: "a value assigned", body: "f := lower; _ = f", want: []string{"lower"}},
		{name: "a selector's field", body: "_ = x.Ref", want: nil},
		{name: "a composite literal's key", body: "_ = T{Ref: 1}", want: nil},
		// The shape #210 was filed about: merge.Merger{Report: l.diag}. A skip that
		// dropped the whole key-value pair rather than its key would take the
		// issue's own example back out of the graph.
		{name: "a composite literal's field value", body: "_ = T{Ref: lower}", want: []string{"lower"}},
		// One name in both positions at once, which is what makes the skip
		// positional rather than by name: a set keyed on the identifier's text
		// would drop the value along with the key.
		{name: "a key and a value spelled the same", body: "_ = T{lower: lower}", want: []string{"lower"}},
		{name: "a closure's parameter list", body: "_ = func(lower int) int { return 1 }", want: nil},
		// A free function that calls itself. The graph records the self edge so
		// the pin can report a self-loop, which is how bodyParts is held to its
		// depth counter.
		{name: "the function itself", body: "self(x)", want: []string{"self"}},
		{name: "both forms of one name", body: "lower(x); apply(y, lower)", want: []string{"lower"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			src := "package p\nfunc self(x, y int) { " + tc.body + " }\n"
			assert.Equal(t, tc.want, plantedCallees(t, src, outside))
		})
	}
}

// TestCalleesOf_ResolvesAMethodThroughItsReceiver pins the receiver half of the
// judgement, which is what GitHub #224 was about: a method names a sibling by
// selector, and reading no selector at all left every such edge out.
//
// Both shapes are planted because they are different edges. `w.walkMapping`
// closes a cycle with another declaration; `w.walk` inside walk closes one with
// nothing but itself, and a graph that recorded only the first would still call
// a directly recursive method dependency-free.
//
// A receiver with no identifier is the boundary here: nothing in such a body can
// name it, so the names it does write resolve to nothing.
func TestCalleesOf_ResolvesAMethodThroughItsReceiver(t *testing.T) {
	t.Parallel()
	outside := map[string]bool{"lower": true}
	tests := []struct {
		name, decls string
		want        []string
	}{
		{
			name:  "a sibling method called",
			decls: "func (w *T) other(x int) {}\nfunc (w *T) self(x int) { w.other(x) }",
			want:  []string{"T.other"},
		},
		{
			name:  "the method itself",
			decls: "func (w *T) self(x int) { w.self(x - 1) }",
			want:  []string{"T.self"},
		},
		{
			name:  "a sibling method handed on as a value",
			decls: "func (w *T) other(x int) {}\nfunc (w *T) self(x int) { apply(x, w.other) }",
			want:  []string{"T.other"},
		},
		{
			name:  "a value receiver",
			decls: "func (w T) other(x int) {}\nfunc (w T) self(x int) { w.other(x) }",
			want:  []string{"T.other"},
		},
		{
			name:  "a generic receiver",
			decls: "func (w *T[K]) other(x int) {}\nfunc (w *T[K]) self(x int) { w.other(x) }",
			want:  []string{"T.other"},
		},
		{
			name:  "a free function reached from a method",
			decls: "func (w *T) self(x int) { lower(x) }",
			want:  []string{"lower"},
		},
		{
			name:  "a receiver the method does not name",
			decls: "func (w *T) other(x int) {}\nfunc (*T) self() { w.other(1) }",
			want:  nil,
		},
		{
			name:  "a blank receiver",
			decls: "func (w *T) other(x int) {}\nfunc (_ *T) self() { w.other(1) }",
			want:  nil,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			src := "package p\n" + tc.decls + "\n"
			assert.Equal(t, tc.want, plantedCallees(t, src, outside))
		})
	}
}

// TestCalleesOf_ResolvesASelectorFromDeclarations pins the fix for GitHub #323:
// a selector resolves whenever the package's own declarations settle its base's
// type, not only when the base is the declaring method's receiver.
//
// The positive cases are the shapes the tree writes — a free function calling a
// method on a local it constructed, on a parameter, on a var; a method calling
// one on a field. The negative cases are the boundary, and they matter more:
// each of them writes a method name the graph holds, and each must come back
// empty. Resolving them would mean guessing, and a guess here attaches an edge
// to a lowering that never names it.
func TestCalleesOf_ResolvesASelectorFromDeclarations(t *testing.T) {
	t.Parallel()
	// Declared by every case, so a case's own line is only what it is about.
	const decls = "type T struct{ inner *U }\n" +
		"type U struct{}\n" +
		"func newT() *T { return nil }\n" +
		"func (u *U) other(x int) {}\n" +
		"func (w *T) reached(x int) {}\n"
	tests := []struct {
		name, subject string
		want          []string
	}{
		{
			name:    "a local built by a constructor",
			subject: "func self(x int) { w := newT(); w.reached(x) }",
			want:    []string{"T.reached", "newT"},
		},
		{
			name:    "a local built by a composite literal",
			subject: "func self(x int) { w := T{}; w.reached(x) }",
			want:    []string{"T.reached"},
		},
		{
			name:    "a local built by a pointer to a composite literal",
			subject: "func self(x int) { w := &T{}; w.reached(x) }",
			want:    []string{"T.reached"},
		},
		{
			name:    "a var declaring its own type",
			subject: "func self(x int) { var w T; w.reached(x) }",
			want:    []string{"T.reached"},
		},
		{
			name:    "a parameter",
			subject: "func self(w *T, x int) { w.reached(x) }",
			want:    []string{"T.reached"},
		},
		{
			name:    "a field of the receiver",
			subject: "func (w *T) self(x int) { w.inner.other(x) }",
			want:    []string{"U.other"},
		},
		{
			name:    "a field of a local",
			subject: "func self(x int) { w := newT(); w.inner.other(x) }",
			want:    []string{"U.other", "newT"},
		},
		{
			name:    "a method handed on as a value from a local",
			subject: "func self(x int) { w := newT(); apply(x, w.reached) }",
			want:    []string{"T.reached", "newT"},
		},
		// The boundary. Every one of these writes `other`, and none of them is an
		// edge, because nothing declared here settles what the base is.
		{
			name:    "a call through an interface",
			subject: "type I interface{ other(int) }\nfunc self(i I, x int) { i.other(x) }",
			want:    nil,
		},
		{
			name:    "a value of a type another package declares",
			subject: "func self(v *elsewhere.U, x int) { v.other(x) }",
			want:    nil,
		},
		{
			name:    "a range variable",
			subject: "func self(us []U, x int) { for _, u := range us { u.other(x) } }",
			want:    nil,
		},
		{
			name:    "a type switch's binding",
			subject: "func self(a any, x int) { switch v := a.(type) { case *U: v.other(x) } }",
			want:    nil,
		},
		{
			name:    "a name bound twice to different types",
			subject: "func self(x int) { w := &T{}; { w := &U{}; w.other(x) } }",
			want:    nil,
		},
		// An embedded field promotes U's methods onto T, and following that is
		// past what declarations settle: T declares no field named other.
		{
			name:    "a promoted method",
			subject: "type E struct{ *U }\nfunc self(e *E, x int) { e.other(x) }",
			want:    nil,
		},
		// The one negative whose base is declared: two returns a *T, and the
		// binding is still refused. A result list of two says which name holds
		// which type only by counting them off, and that is arithmetic on a
		// declaration rather than a reading of it.
		{
			name:    "a call with more than one result",
			subject: "func two() (*T, error) { return nil, nil }\nfunc self(x int) { w, _ := two(); w.reached(x) }",
			want:    []string{"two"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, plantedCallees(t, "package p\n"+decls+tc.subject+"\n", nil))
		})
	}
}

// plantedCallees returns the callees of the last declaration in src, sorted,
// with an empty result normalized to nil so a case expecting no edges reads as
// want: nil rather than want: []string{}.
//
// src is read as a whole package through the same declsOf the tree goes through,
// so a case can plant the type, the constructor and the sibling method its
// subject resolves against. outside names the lowerings the planted file does
// not declare, standing in for the other packages the real graph reads.
func plantedCallees(t *testing.T, src string, outside map[string]bool) []string {
	t.Helper()
	f, err := parser.ParseFile(token.NewFileSet(), "planted.go", src, 0)
	require.NoError(t, err)

	decls := declsOf(t, []*ast.File{f})
	require.NotEmpty(t, decls, "plant at least one declaration with a body")

	known := make(map[string]bool, len(outside)+len(decls))
	for name := range outside {
		known[name] = true
	}
	for _, d := range decls {
		known[d.name] = true
	}

	got := calleesOf(decls[len(decls)-1], known)
	names := make([]string, 0, len(got))
	for n := range got {
		names = append(names, n)
	}
	sort.Strings(names)
	if len(names) == 0 {
		return nil
	}
	return names
}

// TestDeclOf_NamesAMethodByItsReceiverType pins the key a method gets, over
// every receiver form Go allows. A method keyed by its bare name would collide
// with a free function of that name, and the graph refuses collisions rather
// than resolving them, so getting this wrong fails the whole pin rather than one
// edge.
func TestDeclOf_NamesAMethodByItsReceiverType(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, decl   string
		wantName     string
		wantRecv     string
		wantRecvType string
	}{
		{name: "a free function", decl: "func f() {}", wantName: "f"},
		{
			name: "a value receiver", decl: "func (w T) f() {}",
			wantName: "T.f", wantRecv: "w", wantRecvType: "T",
		},
		{
			name: "a pointer receiver", decl: "func (w *T) f() {}",
			wantName: "T.f", wantRecv: "w", wantRecvType: "T",
		},
		{
			name: "one type parameter", decl: "func (w *T[K]) f() {}",
			wantName: "T.f", wantRecv: "w", wantRecvType: "T",
		},
		{
			name: "two type parameters", decl: "func (w T[K, V]) f() {}",
			wantName: "T.f", wantRecv: "w", wantRecvType: "T",
		},
		{
			name: "an unnamed receiver", decl: "func (*T) f() {}",
			wantName: "T.f", wantRecvType: "T",
		},
		{
			name: "a blank receiver", decl: "func (_ *T) f() {}",
			wantName: "T.f", wantRecvType: "T",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f, err := parser.ParseFile(token.NewFileSet(), "planted.go", "package p\n"+tc.decl+"\n", 0)
			require.NoError(t, err)
			fn, isFunc := f.Decls[0].(*ast.FuncDecl)
			require.True(t, isFunc)

			got := declOf(t, fn, newPkgScope())
			assert.Equal(t, tc.wantName, got.name)
			assert.Equal(t, tc.wantRecv, got.recv)
			assert.Equal(t, tc.wantRecvType, got.recvType)
		})
	}
}

// TestTypeNameOf_RefusesWhatIsNotATypeName covers the guard declOf leans on and
// the one every binding leans on.
//
// On a receiver it is a correctness guard: go/parser accepts a receiver go/types
// would reject, so a source file can reach it, and keying such a method as
// though it had no receiver would put it in the free functions' namespace where
// a real free function of that name would collide with it.
//
// On a field or a parameter it is the refusal itself. A value of `[]U` or of
// `elsewhere.U` is not a value of the type this graph would key from the inner
// name, so reducing to that name would resolve a selector onto the wrong
// receiver — quietly, and only for whichever lowering shares the spelling.
func TestTypeNameOf_RefusesWhatIsNotATypeName(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, typ string
		want      string
	}{
		{name: "a type name", typ: "U", want: "U"},
		{name: "a pointer to one", typ: "*U", want: "U"},
		{name: "an instantiated generic", typ: "U[K]", want: "U"},
		{name: "a map", typ: "map[string]U"},
		{name: "a slice", typ: "[]U"},
		{name: "an array", typ: "[3]U"},
		{name: "a channel", typ: "chan U"},
		{name: "a type another package declares", typ: "elsewhere.U"},
		{name: "a func type", typ: "func(U) U"},
		{name: "a literal interface", typ: "interface{ other(int) }"},
		{name: "a literal struct", typ: "struct{ u U }"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f, err := parser.ParseFile(token.NewFileSet(), "planted.go", "package p\nvar v "+tc.typ+"\n", 0)
			require.NoError(t, err)
			gen, isGen := f.Decls[0].(*ast.GenDecl)
			require.True(t, isGen)
			spec, isValue := gen.Specs[0].(*ast.ValueSpec)
			require.True(t, isValue)

			assert.Equal(t, tc.want, typeNameOf(spec.Type),
				"a type this graph cannot key must be refused, not read as some other name")
		})
	}
}

// TestTypeNameOf_RefusesAReceiverGoTypesWouldReject shows the refusal above is
// reachable from a receiver and not only from a field: go/parser accepts this
// declaration, so a source file can carry one to declOf, which requires a name
// rather than keying the method as a free function.
func TestTypeNameOf_RefusesAReceiverGoTypesWouldReject(t *testing.T) {
	t.Parallel()
	f, err := parser.ParseFile(token.NewFileSet(), "planted.go", "package p\nfunc (w map[string]int) f() {}\n", 0)
	require.NoError(t, err)
	fn, isFunc := f.Decls[0].(*ast.FuncDecl)
	require.True(t, isFunc)

	assert.Empty(t, typeNameOf(fn.Recv.List[0].Type),
		"a receiver that names no type must be refused, not read as some other name")
}

// TestLoweringCallGraph_RecordsAMethodEdge holds the graph to the fix for GitHub
// #224 on the tree rather than on a planted source. anchorWalk's two methods
// call each other and the walk descends into itself, and a graph built from free
// functions alone described every one of those edges as absent.
//
// It names charge as well, which closes no cycle: an edge the graph cannot see
// is unheld whether or not it currently changes an answer.
func TestLoweringCallGraph_RecordsAMethodEdge(t *testing.T) {
	t.Parallel()
	graph := loweringCallGraph(t)

	edges := [][2]string{
		{"anchorWalk.walk", "anchorWalk.walkMapping"},
		{"anchorWalk.walkMapping", "anchorWalk.walk"},
		{"anchorWalk.walk", "anchorWalk.walk"},
		{"anchorWalk.walk", "anchorWalk.charge"},
	}
	for _, e := range edges {
		// Membership is read with a comma-ok rather than require.Contains, which
		// would print every node in the graph and its edges before the message.
		_, held := graph[e[0]]
		require.True(t, held, "the graph no longer holds %q", e[0])
		assert.True(t, graph[e[0]][e[1]],
			"%q reaches %q through its receiver; the graph records only free functions again", e[0], e[1])
	}
}

// TestLoweringCallGraph_ResolvesAMethodOnAValue holds the graph to the fix for
// GitHub #323 on the tree. Each of these writes a method name and so decides
// which lowering runs, and a graph resolving only the declaring method's own
// receiver described every one of them as absent: dynamicAnchors reached
// newAnchorWalk and nothing else, and LowerService reached neither of the two
// methods that assemble its groups.
//
// The bases are the shapes these packages write: a local a constructor
// returned, a pointer parameter, and a composite literal. A resolution covering
// one of them and not the rest would still fail here.
//
// Not every selector resolves, and one declaration next to these shows why.
// anchorWalk.walkMapping reads its pairs through w.view.MappingPairs, and the
// field's type is qualified — nodeview declares it, and no package this pin
// reads holds a node for it. So it resolves to nothing, rather than to whatever
// this flat namespace might hold under the same trailing name.
func TestLoweringCallGraph_ResolvesAMethodOnAValue(t *testing.T) {
	t.Parallel()
	graph := loweringCallGraph(t)

	edges := [][2]string{
		{"dynamicAnchors", "anchorWalk.walk"},      // w := newAnchorWalk(…)
		{"LowerService", "serviceGroups.finalize"}, // groups := newServiceGroups()
		{"lowerPathItem", "serviceGroups.group"},   // a *serviceGroups parameter
		{"lowerWebhooks", "serviceGroups.group"},   // the same, in the other caller
		{"soleAnchorSite", "AnchorIndex.sites"},    // an *AnchorIndex parameter
		{"dynamicHop", "AnchorIndex.sites"},        // the same, in the other caller
		{"optionsFrom", "Options.withDefaults"},    // Options{}.withDefaults()
	}
	for _, e := range edges {
		_, held := graph[e[0]]
		require.True(t, held, "the graph no longer holds %q", e[0])
		assert.True(t, graph[e[0]][e[1]],
			"%q names %q, which decides which lowering runs; the selector is unresolved again", e[0], e[1])
	}
}

// TestLoweringCallGraph_ReadsEveryPackageThatLowers derives the set of packages
// the pin has to read, instead of trusting the list beside it.
//
// A lowering is a function over the immutable context (micro-compiler-design
// §4), so a package declaring one holds lowerings by definition. A package added
// without being listed is a directory the pin silently stops reading: its
// recursions never appear, the pinned sets still match, and nothing says so.
// That is the same class of unheld invariant as the value edges this file was
// changed for — one level up, in the input rather than the graph.
func TestLoweringCallGraph_ReadsEveryPackageThatLowers(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	found := map[string]bool{}
	err := filepath.WalkDir(filepath.Join(root, "compilers"), func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !isProductionGoFile(d.Name()) {
			return err
		}
		f, parseErr := parser.ParseFile(token.NewFileSet(), p, nil, parser.SkipObjectResolution)
		if parseErr != nil {
			return parseErr
		}
		if !declaresLowering(f) {
			return nil
		}
		rel, relErr := filepath.Rel(root, filepath.Dir(p))
		if relErr != nil {
			return relErr
		}
		found[filepath.ToSlash(rel)] = true
		return nil
	})
	require.NoError(t, err)
	require.NotEmpty(t, found, "no package declares a lowering; the walk or the context type moved")

	derived := make([]string, 0, len(found))
	for pkg := range found {
		derived = append(derived, pkg)
	}
	sort.Strings(derived)
	listed := append([]string(nil), loweringPackages...)
	sort.Strings(listed)

	assert.Equal(t, derived, listed,
		"the packages this pin reads must be the packages that lower; a listed one that no longer "+
			"lowers is dead, and an unlisted one is a recursion the pin cannot see")
}

// declaresLowering reports whether f declares a function taking the lowering
// context by value, which is what every lowering in this compiler is.
func declaresLowering(f *ast.File) bool {
	for _, d := range f.Decls {
		fn, isFunc := d.(*ast.FuncDecl)
		if !isFunc || fn.Type.Params == nil {
			continue
		}
		for _, p := range fn.Type.Params.List {
			sel, isSel := p.Type.(*ast.SelectorExpr)
			if !isSel || sel.Sel.Name != "Ctx" {
				continue
			}
			if pkg, isIdent := sel.X.(*ast.Ident); isIdent && pkg.Name == "lowering" {
				return true
			}
		}
	}
	return false
}
