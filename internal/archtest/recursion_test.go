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

// schemaRecursion is the one mutual recursion the OpenAPI lowering is allowed to
// have: lowering a schema resolves the references inside it, and resolving a
// reference lowers what it names.
//
// micro-compiler-design §5 records this as the reason schema, compose and
// resolve cannot be separated into packages. Everything else in the compiler is
// expected to be acyclic, and a method joining this set is a method that can no
// longer be converted, tested or moved on its own.
var schemaRecursion = []string{
	"buildComposedVariant", "buildTuple", "carriedSchemaRef", "composedVariant",
	"fillAdditional", "fillAllOf", "fillModelProperties", "hoistSubSchema",
	"lower", "lowerAllOf", "lowerArray", "lowerBesideUnmodeledUnion",
	"lowerCoDeclaredUnion", "lowerDistributedUnion", "lowerModel", "lowerOneOfAnyOf",
	"lowerSchemaBody", "lowerTyped", "lowerUnion", "lowerUntyped", "patternProps",
	"refSiteRef", "refTypeRef", "resolveSchemaRef", "schemaBody", "schemaRef",
	"schemaRefHomed",
}

// TestLowererRecursion_IsOnlyTheSchemaWalk pins which lowerer methods are
// mutually recursive, and holds the answer to the set above.
//
// It exists because measuring this with a regex got it wrong by more than
// double. A pattern ending a function at the first `}` in column zero runs past
// any one-line method — `func (l *lowerer) appendDiag(d ir.Diagnostic) { … }` has
// none — so every such method absorbed its successor's body and inherited its
// calls. The diagnostic recorder came out calling schemaRef, which stitched the
// whole diagnostic path into the schema cycle. A parser cannot make that mistake:
// it is told where a function ends.
//
// The set is asserted rather than merely reported so the claim is checked. A
// method that joins the recursion changes what the remaining conversion work is,
// and that should fail here rather than be discovered by someone measuring again.
func TestLowererRecursion_IsOnlyTheSchemaWalk(t *testing.T) {
	t.Parallel()
	calls := lowererCallGraph(t)
	require.NotEmpty(t, calls, "no lowerer methods found; the parse or the receiver name changed")

	var cycles [][]string
	for _, c := range stronglyConnected(calls) {
		if len(c) > 1 {
			sort.Strings(c)
			cycles = append(cycles, c)
		}
	}
	sort.Slice(cycles, func(i, j int) bool { return len(cycles[i]) > len(cycles[j]) })
	require.NotEmpty(t, cycles, "the schema walk is mutually recursive; finding none means the graph is wrong")

	assert.Equal(t, schemaRecursion, cycles[0],
		"the schema recursion's membership changed; a method joining it can no longer be converted alone")
}

// lowererCallGraph maps each method on *lowerer to the methods it calls on its
// own receiver. Calls are read as l.<name>(…) selectors, which is exact within a
// method whose receiver is named l — the parser supplies the boundaries a
// pattern cannot.
func lowererCallGraph(t *testing.T) map[string]map[string]bool {
	t.Helper()
	files, err := filepath.Glob(filepath.Join("..", "..", "compilers", "openapi", "*.go"))
	require.NoError(t, err)

	graph := map[string]map[string]bool{}
	for _, path := range files {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		require.NoError(t, err, "parse %s", path)

		for _, decl := range f.Decls {
			name, recv, ok := lowererMethod(decl)
			if !ok {
				continue
			}
			out := map[string]bool{}
			ast.Inspect(decl.(*ast.FuncDecl).Body, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				if sel, ok := call.Fun.(*ast.SelectorExpr); ok {
					if id, ok := sel.X.(*ast.Ident); ok && id.Name == recv && sel.Sel.Name != name {
						out[sel.Sel.Name] = true
					}
				}
				return true
			})
			graph[name] = out
		}
	}
	return graph
}

// lowererMethod reports whether decl is a method on *lowerer, with its name and
// receiver identifier.
func lowererMethod(decl ast.Decl) (name, recv string, ok bool) {
	fn, isFunc := decl.(*ast.FuncDecl)
	if !isFunc || fn.Recv == nil || len(fn.Recv.List) != 1 || fn.Body == nil {
		return "", "", false
	}
	star, isPtr := fn.Recv.List[0].Type.(*ast.StarExpr)
	if !isPtr {
		return "", "", false
	}
	id, isIdent := star.X.(*ast.Ident)
	if !isIdent || id.Name != "lowerer" || len(fn.Recv.List[0].Names) != 1 {
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
