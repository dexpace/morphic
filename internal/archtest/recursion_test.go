package archtest_test

import (
	"go/ast"
	"go/parser"
	"go/token"
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
var schemaRecursion = []string{
	"buildComposedVariant", "buildTuple", "carriedSchemaRef", "composedVariant",
	"fillAdditional", "fillAllOf", "fillModelProperties", "hoistSubSchema",
	"lower", "lowerAllOf", "lowerArray", "lowerBesideUnmodeledUnion",
	"lowerCoDeclaredUnion", "lowerDistributedUnion", "lowerModel", "lowerOneOfAnyOf",
	"lowerSchemaBody", "lowerTyped", "lowerUnion", "lowerUntyped", "patternProps",
	"refSiteRef", "refTypeRef", "resolveSchemaRef", "schemaBody", "schemaRef",
	"schemaRefHomed",
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

// loweringCallGraph maps every lowering in compilers/openapi — free function or
// lowerer method — to the ones it calls.
//
// It reads both call shapes: a bare name(…) for a free function, and l.name(…)
// for a call on the method's own receiver. Reading only the receiver form would
// have described an empty graph the moment the schema walk stopped being
// methods, which is exactly when the recursion still mattered most: those
// functions are as mutually recursive as the methods were, and #176 cannot split
// them across packages any more than it could before.
//
// A method value (Report: l.diag) is still not an edge here; #210 tracks that.
func loweringCallGraph(t *testing.T) map[string]map[string]bool {
	t.Helper()
	files, err := filepath.Glob(filepath.Join("..", "..", "compilers", "openapi", "*.go"))
	require.NoError(t, err)

	type decl struct {
		name string
		recv string
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
			if !isFunc || fn.Body == nil {
				continue
			}
			if name, recv, ok := lowererMethod(d); ok {
				decls = append(decls, decl{name: name, recv: recv, body: fn.Body})
				continue
			}
			if fn.Recv == nil {
				decls = append(decls, decl{name: fn.Name.Name, body: fn.Body})
			}
		}
	}

	known := map[string]bool{}
	for _, d := range decls {
		known[d.name] = true
	}

	graph := map[string]map[string]bool{}
	for _, d := range decls {
		out := map[string]bool{}
		ast.Inspect(d.body, func(n ast.Node) bool {
			call, isCall := n.(*ast.CallExpr)
			if !isCall {
				return true
			}
			switch fun := call.Fun.(type) {
			case *ast.Ident:
				if fun.Name != d.name && known[fun.Name] {
					out[fun.Name] = true
				}
			case *ast.SelectorExpr:
				id, isIdent := fun.X.(*ast.Ident)
				if isIdent && d.recv != "" && id.Name == d.recv && fun.Sel.Name != d.name {
					out[fun.Sel.Name] = true
				}
			}
			return true
		})
		graph[d.name] = out
	}
	return graph
}

// lowererMethod reports whether decl is a method on the lowerer, with its name
// and receiver identifier.
//
// Both receiver forms count. Every method is on *lowerer today, but a value
// receiver is still a method that can join a recursion, and reading only the
// pointer form would hide one — the blind spot is worth closing while there is
// nothing in it.
func lowererMethod(decl ast.Decl) (name, recv string, ok bool) {
	fn, isFunc := decl.(*ast.FuncDecl)
	if !isFunc || fn.Recv == nil || len(fn.Recv.List) != 1 || fn.Body == nil {
		return "", "", false
	}
	if len(fn.Recv.List[0].Names) != 1 {
		return "", "", false
	}
	base := fn.Recv.List[0].Type
	if star, isPtr := base.(*ast.StarExpr); isPtr {
		base = star.X
	}
	id, isIdent := base.(*ast.Ident)
	if !isIdent || id.Name != "lowerer" {
		return "", "", false
	}
	return fn.Name.Name, fn.Recv.List[0].Names[0].Name, true
}

// stronglyConnected returns the graph's strongly connected components (Tarjan).
// Edges to names that are not methods — anything reached on some other receiver —
// are ignored, so only the lowerer's own recursion is described.
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
			if _, isMethod := graph[w]; isMethod {
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
