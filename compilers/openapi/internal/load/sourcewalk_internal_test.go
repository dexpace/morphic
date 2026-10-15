package load

import (
	"encoding/json/jsontext"
	"go/ast"
	"go/types"
	"iter"
	"runtime"
	"slices"
	"strings"
	"testing"

	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/scan"
	typed "github.com/dexpace/morphic/internal/decls"
)

// soaPath is the import path of the library's OpenAPI model.
const soaPath = "github.com/speakeasy-api/openapi/openapi"

// TestSourceWalks_GoThroughWalkSource holds every pass of this package that
// walks the source document to walkSource, which walks the fields beside each
// path item $ref with it. A pass calling soa.Walk on the document itself sees
// none of them, and nothing else says so: the parameter-repeat check landed that
// way and let a repeat written beside a $ref compile clean. modelSiblings is
// the one other caller, since it finds the fields walkSource walks.
//
// It reads the package type-checked, so a call is found however it is spelled.
func TestSourceWalks_GoThroughWalkSource(t *testing.T) {
	t.Parallel()
	pkg, err := typed.Load(".")
	require.NoError(t, err)

	require.Equal(t, []string{"modelSiblings", "walkSource"}, documentWalkers(t, pkg))
}

// documentWalkers returns the functions of pkg that call soa.Walk with the
// OpenAPI document as its start, sorted.
func documentWalkers(t *testing.T, pkg *typed.Package) []string {
	t.Helper()
	lib, ok := pkg.Imported(soaPath)
	require.True(t, ok, "the package reads the library's model")
	walk := lib.Scope().Lookup("Walk")
	document := types.NewPointer(lib.Scope().Lookup("OpenAPI").Type())
	require.NotNil(t, walk)

	var walkers []string
	for _, file := range pkg.Syntax {
		for _, decl := range file.Decls {
			fn, isFunc := decl.(*ast.FuncDecl)
			if !isFunc || fn.Body == nil {
				continue
			}
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				call, isCall := n.(*ast.CallExpr)
				if !isCall || len(call.Args) != 2 || !callsObject(pkg, call.Fun, walk) {
					return true
				}
				if types.Identical(pkg.Info.TypeOf(call.Args[1]), document) {
					walkers = append(walkers, fn.Name.Name)
				}
				return true
			})
		}
	}
	slices.Sort(walkers)
	return slices.Compact(walkers)
}

// callsObject reports whether fun, a call's function expression, names obj.
func callsObject(pkg *typed.Package, fun ast.Expr, obj types.Object) bool {
	if index, generic := fun.(*ast.IndexExpr); generic {
		fun = index.X
	}
	var ident *ast.Ident
	switch f := fun.(type) {
	case *ast.SelectorExpr:
		ident = f.Sel
	case *ast.Ident:
		ident = f
	default:
		return false
	}
	return pkg.Info.Uses[ident] == obj
}

// TestLocationOf_RendersThePointer pins the round trip a located walk relies
// on for every site it reports: the location rendered back is the pointer,
// escapes and empty tokens past the first included.
func TestLocationOf_RendersThePointer(t *testing.T) {
	t.Parallel()
	for _, p := range []jsontext.Pointer{
		"/paths/~1a",
		"/paths/~1a~0b/put/callbacks/cb/{$request.body#~1url}",
		"/paths//get",
		"/webhooks/hook",
		"/x-a~1b~0c/X",
	} {
		assert.Equal(t, string(p), string(locationOf(p).ToJSONPointer()))
	}
}

// TestLocationOf_NoReachedObjectHasAnEmptyFirstToken pins the premise that
// lets locationOf render a reached object: the resolver takes "#/" for the
// document, not for a top-level key "", so no $ref reaches an object whose
// pointer starts with an empty token, which a located walk would render one
// segment short. Were it to, this parameter would be judged at
// "/allowReserved" rather than "//allowReserved".
func TestLocationOf_NoReachedObjectHasAnEmptyFirstToken(t *testing.T) {
	t.Parallel()
	_, diags, err := Load(t.Context(), 0, compilers.Source{Path: "spec.yaml", Data: []byte(`openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /a:
    get:
      parameters:
        - $ref: '#/'
      responses: {"200": {description: ok}}
"": {name: h, in: header, allowReserved: true, schema: {type: string}}
`)}, Options{})
	require.NoError(t, err)
	unresolved := false
	for _, d := range diags {
		unresolved = unresolved || d.Code == diag.UnresolvedRef && d.Provenance.Pointer == "/paths/~1a/get/parameters/0"
	}
	assert.True(t, unresolved, "the $ref reaches the document, which is no parameter")
	assert.Empty(t, reservedIn(t, diags))
}

// locatedSpec writes a path item whose walk meets a map key first, its method.
const locatedSpec = `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /a:
    get:
      parameters:
        - {name: q, in: query, schema: {type: string}}
      responses: {"200": {description: ok}}
`

// TestLocatedWalk_PlacesAWalkUnderItsObject pins what a walk started at an
// object other than the document renders once located: the object itself at
// the pointer, and what it holds beneath it, its method included, where alone
// the walk renders "//get".
func TestLocatedWalk_PlacesAWalkUnderItsObject(t *testing.T) {
	t.Parallel()
	item := modelOf(t, locatedSpec).Paths.GetOrZero("/a").GetObject()
	var sites []string
	for wi := range locatedWalk(reachedWalk(t.Context(), item), "/x-paths/X") {
		sites = append(sites, string(wi.Location.ToJSONPointer()))
	}
	require.NotEmpty(t, sites)
	assert.Equal(t, "/x-paths/X", sites[0])
	assert.Contains(t, sites, "/x-paths/X/get")
	assert.Contains(t, sites, "/x-paths/X/get/parameters/0")
	for _, site := range sites {
		assert.True(t, strings.HasPrefix(site, "/x-paths/X"), site)
		assert.NotContains(t, site, "//", site)
	}
}

// TestLocatedWalk_StopsWhenTheReaderDoes pins that a reader leaving early ends
// the walk: a range loop handed another item after it broke panics.
func TestLocatedWalk_StopsWhenTheReaderDoes(t *testing.T) {
	t.Parallel()
	item := modelOf(t, locatedSpec).Paths.GetOrZero("/a").GetObject()
	read := 0
	for range locatedWalk(reachedWalk(t.Context(), item), "/x-paths/X") {
		read++
		break
	}
	assert.Equal(t, 1, read)
}

// TestWalkSource_WalksTheFieldsWhereTheSourceWritesThem pins the order every
// pass reads the source in: a model's items right after its $ref, before the
// next path, as an inline item's would be, each model once though an alias
// reaches it twice, and no item for the wrapper a model's walk starts from.
func TestWalkSource_WalksTheFieldsWhereTheSourceWritesThem(t *testing.T) {
	t.Parallel()
	doc := modelOf(t, siblingSpec)
	sibs, _, _ := modelSiblings(t.Context(), doc, scan.InSource(0))
	sites := walkSites(walkSource(t.Context(), doc, sibs))

	index := func(site string) int {
		t.Helper()
		for i, s := range sites {
			if s == site {
				return i
			}
		}
		require.Failf(t, "site not walked", "%s", site)
		return -1
	}
	put := index("/paths/~1a/put")
	assert.Less(t, index("/paths/~1a"), put)
	assert.Less(t, put, index("/paths/~1b"), "the fields are walked before the next path")
	assert.Less(t, put, index("/paths/~1a/put/callbacks/cb/{$request.body#~1url}/delete"))
	count := 0
	for item := range walkSource(t.Context(), doc, sibs) {
		_ = item.Match(soa.Matcher{Operation: func(op *soa.Operation) error {
			if op == sibs.list[0].Item.Put() {
				count++
			}
			return nil
		}})
	}
	assert.Equal(t, 1, count, "a model is walked once, where it was found, though an alias reaches it twice")
	assert.Equal(t, walkSites(soa.Walk(t.Context(), doc)), walkSites(walkSource(t.Context(), doc, Siblings{})),
		"with no model the walk is the document's own")
}

// TestWalkSource_StopsWhenTheReaderDoes pins that a pass leaving the walk early,
// as one stopping at a fault does, stops it inside a model as well as outside.
func TestWalkSource_StopsWhenTheReaderDoes(t *testing.T) {
	t.Parallel()
	doc := modelOf(t, siblingSpec)
	sibs, _, _ := modelSiblings(t.Context(), doc, scan.InSource(0))
	for _, stopAt := range []string{"/paths/~1b", "/paths/~1a/put"} {
		var last string
		for item := range walkSource(t.Context(), doc, sibs) {
			last = string(item.Location.ToJSONPointer())
			if last == stopAt {
				break
			}
		}
		assert.Equal(t, stopAt, last)
	}
}

// TestWalkSource_ReleasesEveryWalkItOpens pins that a reader stopping inside a
// model nested in another leaves no walk running. Each open walk is a pulled
// iterator, whose goroutine lives until it is stopped or runs out, and the
// leak check does not see one left suspended. Not parallel, since it counts
// the process's goroutines.
func TestWalkSource_ReleasesEveryWalkItOpens(t *testing.T) {
	doc := modelOf(t, siblingSpec)
	sibs, _, _ := modelSiblings(t.Context(), doc, scan.InSource(0))
	nested := "/paths/~1a/put/callbacks/cb/{$request.body#~1url}/"
	const reads = 100
	before := runtime.NumGoroutine()
	for range reads {
		stopped := false
		for item := range walkSource(t.Context(), doc, sibs) {
			if strings.HasPrefix(string(item.Location.ToJSONPointer()), nested) {
				stopped = true
				break
			}
		}
		require.True(t, stopped, "the read stops with three walks open")
	}
	assert.Less(t, runtime.NumGoroutine()-before, reads, "no read leaves its walks suspended")
}

// TestWalkSource_WalksADocumentWithoutModelsAsTheLibraryDoes pins walkSource's
// cost where there is nothing to interleave: every pass of this phase walks
// the source through it, so it must cost what the library's own walk does.
func TestWalkSource_WalksADocumentWithoutModelsAsTheLibraryDoes(t *testing.T) {
	doc := modelOf(t, siblingSpec)
	drain := func(items iter.Seq[soa.WalkItem]) {
		for range items {
			continue
		}
	}
	library := testing.AllocsPerRun(10, func() { drain(soa.Walk(t.Context(), doc)) })
	source := testing.AllocsPerRun(10, func() { drain(walkSource(t.Context(), doc, Siblings{})) })
	assert.Equal(t, library, source)
}
