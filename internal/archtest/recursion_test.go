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

// loweringRecursions are every set of lowerings in compilers/openapi that call
// each other, and why each set is allowed to.
//
// A lowering inside one of these cannot be tested or moved on its own: its
// callees include something that calls back to it, so the whole set changes
// together or not at all. That is what makes the membership worth pinning rather
// than measuring — it decides the shape of the remaining work.
//
// The sets are no longer all methods. The schema walk converted to free
// functions and is just as recursive for it, which is the point: what binds
// these together is the call graph, not the receiver.
//
// The sets are listed longest first, which is also the order they matter in.
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
	// Property lookup through a composition. Finding a property by wire name
	// descends into a model's base and mixins, each of which is a model whose
	// properties are looked up the same way.
	{"propIDByWire", "propIDInComposition"},
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

// TestLoweringRecursion_IsOnlyTheKnownCycles pins which lowerings are mutually
// recursive, and holds the answer to the sets above.
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
// checking at all — and the two smaller ones are as unconvertible as the big one.
func TestLoweringRecursion_IsOnlyTheKnownCycles(t *testing.T) {
	t.Parallel()
	calls := loweringCallGraph(t)
	require.NotEmpty(t, calls, "no lowerings found; the parse or the package path changed")

	var cycles [][]string
	for _, c := range stronglyConnected(calls) {
		if len(c) > 1 {
			sort.Strings(c)
			cycles = append(cycles, c)
		}
	}
	sort.Slice(cycles, func(i, j int) bool { return len(cycles[i]) > len(cycles[j]) })
	require.NotEmpty(t, cycles, "the schema walk is mutually recursive; finding none means the graph is wrong")

	assert.Equal(t, loweringRecursions, cycles,
		"the lowering call graph's mutual recursion changed; anything joining one of these sets can no longer be moved alone")
}

// loweringCallGraph maps every lowering to the ones it depends on.
//
// Lowerings are free functions here, and only free functions are read: no method
// takes part in a lowering recursion today, and one that did would have to be
// added deliberately rather than picked up by a receiver name that no longer
// exists. GitHub #224 records the receivers this therefore does not see.
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

	type decl struct {
		name string
		body *ast.BlockStmt
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
			if !isFunc || fn.Body == nil || fn.Recv != nil {
				continue
			}
			decls = append(decls, decl{name: fn.Name.Name, body: fn.Body})
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
		graph[d.name] = calleesOf(d.body, d.name, known)
	}
	return graph
}

// calleesOf returns the lowerings body names, whether it calls them or hands
// them on as a value.
//
// Both are edges, and for the same reason: the name is written here, so this
// function decides which lowering runs and cannot be moved without it. What is
// not an edge is a function this one receives as a parameter — that name is
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
// Position is judged, scope is not: a local that shadowed a lowering's name
// would read as an edge to it. Nothing here does that, and the error runs in the
// safe direction for a pin whose job is to notice dependencies — a spurious
// member shows up in a pinned set and gets read, where a missing one is the
// silence #210 was about.
func calleesOf(body *ast.BlockStmt, self string, known map[string]bool) map[string]bool {
	skip := map[*ast.Ident]bool{}
	ast.Inspect(body, func(n ast.Node) bool {
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
	ast.Inspect(body, func(n ast.Node) bool {
		if call, isCall := n.(*ast.CallExpr); isCall {
			if fun, isIdent := call.Fun.(*ast.Ident); isIdent && fun.Name != self && known[fun.Name] {
				out[fun.Name] = true
			}
			return true
		}
		id, isIdent := n.(*ast.Ident)
		if isIdent && !skip[id] && id.Name != self && known[id.Name] {
			out[id.Name] = true
		}
		return true
	})
	return out
}

// stronglyConnected returns the graph's strongly connected components (Tarjan).
// Edges to names the graph does not hold — anything reached on some other
// receiver or in another package — are ignored, so only recursion within
// compilers/openapi's own lowerings is described.
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
		{name: "the function itself", body: "self(x)", want: nil},
		{name: "both forms of one name", body: "lower(x); apply(y, lower)", want: []string{"lower"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			src := "package p\nfunc self(x, y int) { " + tc.body + " }\n"
			f, err := parser.ParseFile(token.NewFileSet(), "planted.go", src, 0)
			require.NoError(t, err)
			fn, ok := f.Decls[0].(*ast.FuncDecl)
			require.True(t, ok)

			got := calleesOf(fn.Body, "self", known)
			names := make([]string, 0, len(got))
			for n := range got {
				names = append(names, n)
			}
			sort.Strings(names)
			assert.Equal(t, tc.want, orNil(names))
		})
	}
}

// orNil normalizes an empty slice to nil so a case expecting no edges reads as
// want: nil rather than want: []string{}.
func orNil(s []string) []string {
	if len(s) == 0 {
		return nil
	}
	return s
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
