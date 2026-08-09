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
	// The nullability predicate. JSON Schema conjoins keywords, so whether a
	// schema admits null is decided by its allOf conjuncts as much as by its own
	// type set, and a conjunct is reached through a $ref whose target is a schema
	// asked the same question. These four are not lowerings — they build no node
	// and report no diagnostic — but they live in the same package and the call
	// graph reads it whole, so the cycle is pinned here with the rest. It is
	// bounded by an explicit budget (maxNullConjuncts), which is what stops a
	// self-referential allOf rather than anything in this shape.
	{"allOfNullVerdict", "conjunctNullVerdict", "refNullVerdict", "schemaNullVerdict"},
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

// decl is one declaration the call graph holds: the name it is known by, and the
// receiver a method reaches its siblings through.
type decl struct {
	name     string // "lower", or "anchorWalk.walk" for a method
	recv     string // the receiver's identifier; empty unless this is a method that names one
	recvType string // the receiver's type; empty for a free function
	body     *ast.BlockStmt
}

// declOf reads fn into the one node the graph holds for it.
//
// A method is keyed "<receiver type>.<method>" rather than by its bare name.
// The graph refuses a name collision instead of resolving it, and walk exists
// both as a method on anchorWalk and as a name a free function is free to take,
// so the two have to stay distinguishable.
//
// A receiver with no identifier, or one spelled _, leaves recv empty: such a
// method cannot write a sibling's name, so it has no receiver edges to resolve.
func declOf(t *testing.T, fn *ast.FuncDecl) decl {
	t.Helper()
	require.NotNil(t, fn.Body, "a declaration with no body names no callees")

	d := decl{name: fn.Name.Name, body: fn.Body}
	if fn.Recv == nil {
		return d
	}
	require.Len(t, fn.Recv.List, 1, "a method declares exactly one receiver")

	field := fn.Recv.List[0]
	d.recvType = receiverTypeName(field.Type)
	require.NotEmpty(t, d.recvType,
		"unreadable receiver on %s; keying it as a free function would hide its edges", fn.Name.Name)
	d.name = d.recvType + "." + fn.Name.Name
	if len(field.Names) == 1 && field.Names[0].Name != "_" {
		d.recv = field.Names[0].Name
	}
	return d
}

// receiverTypeName returns the type a receiver names. Go's receiver grammar is
// [*]TypeName[TypeParams], so the pointer and then the type arguments come off
// and what is left has to be the name; anything else returns "" for declOf to
// refuse.
func receiverTypeName(expr ast.Expr) string {
	if star, isStar := expr.(*ast.StarExpr); isStar {
		expr = star.X
	}
	if idx, isIndex := expr.(*ast.IndexExpr); isIndex { // Receiver[T]
		expr = idx.X
	}
	if idx, isIndex := expr.(*ast.IndexListExpr); isIndex { // Receiver[K, V]
		expr = idx.X
	}
	id, isIdent := expr.(*ast.Ident)
	if !isIdent {
		return ""
	}
	return id.Name
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
// package is a selector this graph does not record, but no such edge can close a
// cycle, because Go refuses the import that would let the schema package call
// back. Every cycle there can be is inside one package.
//
// The packages are named rather than globbed, and
// TestLoweringCallGraph_ReadsEveryPackageThatLowers derives the same set from
// the tree so the naming cannot go stale in silence.
func loweringCallGraph(t *testing.T) map[string]map[string]bool {
	t.Helper()
	root := repoRoot(t)
	var files []string
	for _, pkg := range loweringPackages {
		found, err := filepath.Glob(filepath.Join(root, pkg, "*.go"))
		require.NoError(t, err)
		require.NotEmpty(t, found, "no sources under %s; the lowering moved and this pin no longer reads it", pkg)
		files = append(files, found...)
	}

	var decls []decl
	for _, path := range files {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		require.NoError(t, err, "parse %s", path)

		for _, d := range f.Decls {
			fn, isFunc := d.(*ast.FuncDecl)
			if !isFunc || fn.Body == nil {
				continue
			}
			decls = append(decls, declOf(t, fn))
		}
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
// The exception is a selector on d's own receiver. `w.walkMapping` is how one
// method names another, and the receiver's type is the one thing the parser can
// resolve without a type checker, so that selector is read as an edge to
// "<receiver type>.<name>". Every other selector stays unread, and the limit is
// worth stating rather than hiding: a free function calling a method, a method
// calling one on another value, and any call through an interface are edges this
// graph cannot see — resolving them needs types, and guessing them from a bare
// method name is the resolution the one-namespace rule refuses (GitHub #323).
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
	ast.Inspect(d.body, func(n ast.Node) bool {
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

	out := map[string]bool{}
	record := func(name string) {
		if known[name] {
			out[name] = true
		}
	}
	ast.Inspect(d.body, func(n ast.Node) bool {
		if sel, isSel := n.(*ast.SelectorExpr); isSel {
			if base, isIdent := sel.X.(*ast.Ident); isIdent && d.recv != "" && base.Name == d.recv {
				record(d.recvType + "." + sel.Sel.Name)
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
	known := map[string]bool{"Ref": true, "lower": true, "self": true}
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
			assert.Equal(t, tc.want, plantedCallees(t, src, known))
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
// The negative cases are the boundary. Without a type checker the graph cannot
// say what a selector on anything else resolves to, so it declines to guess
// rather than matching on the bare method name.
func TestCalleesOf_ResolvesAMethodThroughItsReceiver(t *testing.T) {
	t.Parallel()
	known := map[string]bool{"T.self": true, "T.other": true, "U.other": true, "lower": true}
	tests := []struct {
		name, decl string
		want       []string
	}{
		{
			name: "a sibling method called",
			decl: "func (w *T) self(x int) { w.other(x) }",
			want: []string{"T.other"},
		},
		{
			name: "the method itself",
			decl: "func (w *T) self(x int) { w.self(x - 1) }",
			want: []string{"T.self"},
		},
		{
			name: "a sibling method handed on as a value",
			decl: "func (w *T) self(x int) { apply(x, w.other) }",
			want: []string{"T.other"},
		},
		{
			name: "a value receiver",
			decl: "func (w T) self(x int) { w.other(x) }",
			want: []string{"T.other"},
		},
		{
			name: "a generic receiver",
			decl: "func (w *T[K]) self(x int) { w.other(x) }",
			want: []string{"T.other"},
		},
		{
			name: "a free function reached from a method",
			decl: "func (w *T) self(x int) { lower(x) }",
			want: []string{"lower"},
		},
		// The boundary. Each of these names a method the graph holds, and none of
		// them is an edge: u is some other value, and a receiver the declaration
		// leaves unnamed cannot be written in the body at all.
		{
			name: "a selector on something other than the receiver",
			decl: "func (w *T) self(u U) { u.other(1) }",
			want: nil,
		},
		{
			name: "a receiver the method does not name",
			decl: "func (*T) self(x int) { x.other() }",
			want: nil,
		},
		{
			name: "a blank receiver",
			decl: "func (_ *T) self(x int) { x.other() }",
			want: nil,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			src := "package p\n" + tc.decl + "\n"
			assert.Equal(t, tc.want, plantedCallees(t, src, known))
		})
	}
}

// plantedCallees parses one declaration out of src and returns its callees,
// sorted, with an empty result normalized to nil so a case expecting no edges
// reads as want: nil rather than want: []string{}.
func plantedCallees(t *testing.T, src string, known map[string]bool) []string {
	t.Helper()
	f, err := parser.ParseFile(token.NewFileSet(), "planted.go", src, 0)
	require.NoError(t, err)
	require.Len(t, f.Decls, 1, "plant exactly one declaration")
	fn, isFunc := f.Decls[0].(*ast.FuncDecl)
	require.True(t, isFunc)

	got := calleesOf(declOf(t, fn), known)
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

			got := declOf(t, fn)
			assert.Equal(t, tc.wantName, got.name)
			assert.Equal(t, tc.wantRecv, got.recv)
			assert.Equal(t, tc.wantRecvType, got.recvType)
		})
	}
}

// TestReceiverTypeName_RefusesWhatIsNotATypeName covers the guard declOf leans
// on. go/parser accepts a receiver go/types would reject, so a source file can
// reach this — and keying such a method as though it had no receiver would put
// it in the free functions' namespace, where a real free function of that name
// would then collide with it.
func TestReceiverTypeName_RefusesWhatIsNotATypeName(t *testing.T) {
	t.Parallel()
	f, err := parser.ParseFile(token.NewFileSet(), "planted.go", "package p\nfunc (w map[string]int) f() {}\n", 0)
	require.NoError(t, err)
	fn, isFunc := f.Decls[0].(*ast.FuncDecl)
	require.True(t, isFunc)

	assert.Empty(t, receiverTypeName(fn.Recv.List[0].Type),
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
