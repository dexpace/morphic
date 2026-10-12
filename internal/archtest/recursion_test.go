package archtest_test

import (
	"cmp"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"io/fs"
	"maps"
	"path/filepath"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/tools/go/packages"
)

// loweringRecursions are every recursion among the lowerings in
// compilers/openapi, and why each is allowed to be recursive.
//
// A set of more than one is mutual recursion: none of its lowerings can be
// tested or moved alone, so the set changes together. A set of one calls
// itself; it is pinned too, because recursion is permitted only against an
// explicit depth counter and this check says which functions need one.
//
// A set is bound by the call graph, not the receiver. Sets are listed longest
// first, ties broken by first member, the order the pin produces.
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
	// Format detection's read of a root mapping's merge keys. A `<<` may name a
	// mapping that merges another, so following one is following a chain the
	// document's size does not bound. Bounded by maxMergeDepth.
	{"probeFromMapping", "probeFromMerge"},
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
// directly and because #176's package split is sized by it. The two exported
// names are the walk's entry points, which is how the operation lowering
// reaches it; the rest is unexported because nothing outside the schema package
// has any business entering mid-walk. The scalar hoisters and the encoding
// reader are members because contentSchema's value is a schema, so lowering it
// re-enters the walk from a scalar position. Discriminator lowering is one
// because resolving a mapping target can hoist the position it names.
var schemaRecursion = []string{
	"CarriedRef", "Ref", "buildComposedVariant", "buildTuple", "buildUnion",
	"composedVariant", "contentSchemaRef", "discriminatorDefault",
	"discriminatorMapping", "fillAdditional", "fillAllOf",
	"fillModelProperties", "hoistByteScalar", "hoistContentScalar",
	"hoistFormatScalar", "hoistSubSchema", "lower", "lowerAllOf", "lowerArray",
	"lowerBesideUnmodeledUnion", "lowerCoDeclaredUnion", "lowerDiscriminator",
	"lowerDistributedUnion", "lowerModel", "lowerOneOfAnyOf", "lowerSchemaBody",
	"lowerTyped", "lowerUnion", "lowerUntyped", "patternProps", "refSiteRef",
	"refTypeRef", "resolveMappingTarget", "resolvePointer", "resolveSchemaRef",
	"scalarEncoding", "scalarTypeID", "schemaBody", "schemaRefHomed",
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

// TestLoweringRecursion_IsOnlyTheKnownCycles pins which lowerings are
// recursive, and holds the answer to the sets above.
//
// The graph is read from the type checker rather than from syntax alone: a
// parser has to guess what each name refers to, and every wrong guess is an
// edge missed or invented, which the pin cannot tell from a real change.
//
// Every cycle is asserted, not only the largest, or a new recursion could
// appear anywhere else unnoticed.
func TestLoweringRecursion_IsOnlyTheKnownCycles(t *testing.T) {
	t.Parallel()
	calls := loweringCallGraph(t)
	require.NotEmpty(t, calls, "no lowerings found; the load or the package path changed")

	var cycles [][]string
	for _, c := range stronglyConnected(calls) {
		if len(c) == 1 && !calls[c[0]][c[0]] {
			continue
		}
		slices.Sort(c)
		cycles = append(cycles, c)
	}
	// Ties are broken by first member because there is more than one pair now,
	// and the sort is not stable: length alone would leave the two pairs in
	// whichever order the sort happened to land them.
	slices.SortFunc(cycles, func(a, b []string) int {
		return cmp.Or(cmp.Compare(len(b), len(a)), cmp.Compare(a[0], b[0]))
	})
	require.NotEmpty(t, cycles, "the schema walk is mutually recursive; finding none means the graph is wrong")

	assert.Equal(t, loweringRecursions, cycles,
		"the lowering call graph's recursion changed; anything joining one of these sets can no longer be moved alone")
}

// typedPkg is one type-checked package of the lowering: its production files
// and what the checker resolved in them.
type typedPkg struct {
	files []*ast.File
	info  *types.Info
}

// loweringCallGraph maps every lowering to the ones it depends on.
//
// A node is a function declared with a body, keyed "name" or "Recv.name". An
// edge is any identifier the type checker resolves to another node: a call, a
// function handed on as a value, or a method reached through a value. A call
// through an interface method names no declaration, so it records nothing.
// Calls between the packages are edges too, but none can close a cycle: Go
// refuses the import that would let the callee's package call back.
func loweringCallGraph(t *testing.T) map[string]map[string]bool {
	t.Helper()
	patterns := make([]string, len(loweringPackages))
	for i, pkg := range loweringPackages {
		patterns[i] = "./" + pkg
	}
	cfg := &packages.Config{
		Mode: packages.NeedName | packages.NeedFiles | packages.NeedSyntax |
			packages.NeedTypes | packages.NeedTypesInfo | packages.NeedImports,
		Dir: repoRoot(t),
	}
	loaded, err := packages.Load(cfg, patterns...)
	require.NoError(t, err)
	require.Len(t, loaded, len(patterns), "a lowering package did not load; the pin no longer reads it")

	pkgs := make([]typedPkg, 0, len(loaded))
	for _, p := range loaded {
		require.Empty(t, p.Errors, "type-check %s", p.PkgPath)
		pkgs = append(pkgs, typedPkg{files: p.Syntax, info: p.TypesInfo})
	}
	return callGraphOf(t, pkgs)
}

// graphNode is one function of the graph: the body its edges are read from,
// and what the checker resolved in that body's package.
type graphNode struct {
	body *ast.BlockStmt
	info *types.Info
}

// callGraphOf builds the graph over pkgs, one namespace across all of them.
// Go allows one name in two packages; the graph would keep one and drop the
// other's edges, so a collision is refused rather than resolved.
func callGraphOf(t *testing.T, pkgs []typedPkg) map[string]map[string]bool {
	t.Helper()
	names := map[*types.Func]string{}
	nodes := map[string]graphNode{}
	for _, p := range pkgs {
		for _, fd := range funcDecls(p.files) {
			fn, ok := p.info.Defs[fd.Name].(*types.Func)
			require.True(t, ok, "%s is not a function to the type checker", fd.Name.Name)
			name := nodeName(fn)
			_, taken := nodes[name]
			require.False(t, taken, "two lowerings are named %q; the call graph cannot tell them apart", name)
			names[fn] = name
			nodes[name] = graphNode{body: fd.Body, info: p.info}
		}
	}

	graph := make(map[string]map[string]bool, len(nodes))
	for name, n := range nodes {
		graph[name] = calleesIn(n.body, n.info, names)
	}
	return graph
}

// calleesIn returns the nodes that body refers to, by what the checker resolved
// each identifier to: a local shadowing a node's name is not an edge, nor is a
// field of the same name.
func calleesIn(body *ast.BlockStmt, info *types.Info, names map[*types.Func]string) map[string]bool {
	out := map[string]bool{}
	for n := range ast.Preorder(body) {
		id, isIdent := n.(*ast.Ident)
		if !isIdent {
			continue
		}
		fn, isFunc := info.Uses[id].(*types.Func)
		if !isFunc {
			continue
		}
		if name, held := names[fn.Origin()]; held {
			out[name] = true
		}
	}
	return out
}

// funcDecls returns the functions in files that have a body.
func funcDecls(files []*ast.File) []*ast.FuncDecl {
	var out []*ast.FuncDecl
	for _, f := range files {
		for _, d := range f.Decls {
			fd, isFunc := d.(*ast.FuncDecl)
			if !isFunc || fd.Body == nil {
				continue
			}
			out = append(out, fd)
		}
	}
	return out
}

// nodeName keys a function "name", or "Recv.name" for a method, so a method and
// a free function of one name stay distinguishable. The receiver is keyed by the
// type it declares, through any pointer or alias it was spelled with.
func nodeName(fn *types.Func) string {
	recv := fn.Signature().Recv()
	if recv == nil {
		return fn.Name()
	}
	typ := types.Unalias(recv.Type())
	if ptr, isPtr := typ.(*types.Pointer); isPtr {
		typ = types.Unalias(ptr.Elem())
	}
	if named, isNamed := typ.(*types.Named); isNamed {
		return named.Obj().Name() + "." + fn.Name()
	}
	return fn.Name()
}

// plantedGraph type-checks src, which imports nothing, and returns its graph.
func plantedGraph(t *testing.T, src string) map[string]map[string]bool {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "planted.go", src, 0)
	require.NoError(t, err)
	info := &types.Info{Defs: map[*ast.Ident]types.Object{}, Uses: map[*ast.Ident]types.Object{}}
	_, err = (&types.Config{}).Check("p", fset, []*ast.File{f}, info)
	require.NoError(t, err)
	return callGraphOf(t, []typedPkg{{files: []*ast.File{f}, info: info}})
}

// TestLoweringCallGraph_FollowsWhatTheTypeCheckerResolves plants sources whose
// edges are decided by what the checker resolves a name to: the references a
// parser alone gets wrong, and the ones that must stay out of the graph.
func TestLoweringCallGraph_FollowsWhatTheTypeCheckerResolves(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		src  string
		want map[string]map[string]bool
	}{
		{"multi-result bind types the receiver", `package p
type T struct{}
func (T) hop() { m, _ := mk(); m.hop() }
func mk() (T, bool) { return T{}, true }`,
			map[string]map[string]bool{"T.hop": {"T.hop": true, "mk": true}, "mk": {}}},
		{"function value is an edge", `package p
func a() { f := b; f() }
func b() { a() }`,
			map[string]map[string]bool{"a": {"b": true}, "b": {"a": true}}},
		{"shadowing local is not an edge", `package p
func a() { b() }
func b() { a := 1; _ = a }`,
			map[string]map[string]bool{"a": {"b": true}, "b": {}}},
		{"method of an instantiated type resolves to its declaration", `package p
type L[T any] struct{}
func (L[T]) walk() { a() }
func a() { var l L[int]; l.walk() }`,
			map[string]map[string]bool{"L.walk": {"a": true}, "a": {"L.walk": true}}},
		{"method declared through an alias keys by its type", `package p
type T struct{}
type A = T
func (A) m() { f() }
func (*A) n() { f() }
func f() { T{}.m(); (&T{}).n() }`,
			map[string]map[string]bool{"T.m": {"f": true}, "T.n": {"f": true}, "f": {"T.m": true, "T.n": true}}},
		{"call through an interface records nothing", `package p
type I interface{ m() }
type T struct{}
func (T) m() { use(nil) }
func use(i I) { i.m() }`,
			map[string]map[string]bool{"T.m": {"use": true}, "use": {}}},
		{"a closure's body belongs to the function that holds it", `package p
func a() { func() { b() }() }
func b() {}`,
			map[string]map[string]bool{"a": {"b": true}, "b": {}}},
		{"a field named like a node is not an edge", `package p
type T struct{ b int }
func a(t T) int { return t.b }
func b() {}`,
			map[string]map[string]bool{"a": {}, "b": {}}},
		{"a composite literal's key is not an edge", `package p
type T struct{ b int }
func a() T { return T{b: 1} }
func b() {}`,
			map[string]map[string]bool{"a": {}, "b": {}}},
		{"a key and a value spelled the same are one edge", `package p
type T struct{ b func() }
func a() T { return T{b: b} }
func b() {}`,
			map[string]map[string]bool{"a": {"b": true}, "b": {}}},
		{"a method handed on as a value is an edge", `package p
type T struct{}
func (T) m() {}
func a(t T) { apply(t.m) }
func apply(f func()) { f() }`,
			map[string]map[string]bool{"T.m": {}, "a": {"T.m": true, "apply": true}, "apply": {}}},
		{"a promoted method resolves to the embedded type's declaration", `package p
type U struct{}
func (U) m() {}
type E struct{ U }
func a(e E) { e.m() }`,
			map[string]map[string]bool{"U.m": {}, "a": {"U.m": true}}},
		{"a method on a range variable resolves", `package p
type U struct{}
func (U) m() {}
func a(us []U) { for _, u := range us { u.m() } }`,
			map[string]map[string]bool{"U.m": {}, "a": {"U.m": true}}},
		{"a method on a type switch's binding resolves", `package p
type U struct{}
func (U) m() {}
func a(x any) { switch v := x.(type) { case U: v.m() } }`,
			map[string]map[string]bool{"U.m": {}, "a": {"U.m": true}}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, plantedGraph(t, tc.src))
		})
	}
}

// stronglyConnected returns the graph's strongly connected components (Tarjan).
// Every edge names a node of the graph, because calleesIn records no other, so
// only recursion within the packages read is described.
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

		for _, w := range slices.Sorted(maps.Keys(graph[v])) {
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
	names := slices.Sorted(maps.Keys(graph))
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
// GitHub #323 on the tree. Each edge names a method and so decides which
// lowering runs; a graph resolving only the declaring method's own receiver
// described every one as absent. The bases cover the shapes these packages
// write: a constructor's local, a pointer parameter, a composite literal, a
// multi-result bind.
//
// Not every selector reaches a node: anchorWalk.walkMapping reads its pairs
// through w.view.MappingPairs, a method of a type nodeview declares, and no
// package this pin reads holds a node for it. It adds no edge.
func TestLoweringCallGraph_ResolvesAMethodOnAValue(t *testing.T) {
	t.Parallel()
	graph := loweringCallGraph(t)

	edges := [][2]string{
		{"dynamicAnchors", "anchorWalk.walk"},       // w := newAnchorWalk(…)
		{"LowerService", "serviceGroups.finalize"},  // groups := newServiceGroups()
		{"lowerPathItem", "serviceGroups.group"},    // a *serviceGroups parameter
		{"lowerWebhooks", "serviceGroups.group"},    // the same, in the other caller
		{"soleAnchorSite", "AnchorIndex.sites"},     // an *AnchorIndex parameter
		{"dynamicHop", "AnchorIndex.sites"},         // the same, in the other caller
		{"optionsFrom", "Options.withDefaults"},     // Options{}.withDefaults()
		{"probeFromMapping", "sniffProbe.fillFrom"}, // probe, merges, err := …
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

	derived := slices.Sorted(maps.Keys(found))
	listed := slices.Sorted(slices.Values(loweringPackages))

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
