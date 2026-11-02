// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

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

	"github.com/dexpace/morphic/compilers/openapi/internal/scan"
	typed "github.com/dexpace/morphic/internal/decls"
)

// soaPath is the import path of the library's OpenAPI model.
const soaPath = "github.com/speakeasy-api/openapi/openapi"

// TestSourceWalks_GoThroughWalkSource holds every pass of this package that
// walks the source document to walkSource, which walks the fields beside each
// path item $ref with it. A pass walking the document itself sees none of
// them, and nothing else says so: the parameter-repeat check landed that way
// and let a repeat written beside a $ref compile clean. modelSiblings is the
// one other walker, since it finds the fields walkSource walks.
//
// It reads the package type-checked, so a call is found however it is spelled.
func TestSourceWalks_GoThroughWalkSource(t *testing.T) {
	t.Parallel()
	pkg, err := typed.Load(".")
	require.NoError(t, err)
	lib, ok := pkg.Imported(soaPath)
	require.True(t, ok, "the package reads the library's model")
	document := types.NewPointer(lib.Scope().Lookup("OpenAPI").Type())

	require.Equal(t, []string{"modelSiblings", "walkSource"}, callersOf(t, pkg, pkg.Types.Scope().Lookup("walk"), document))
}

// TestLibraryWalks_GoThroughWalk holds every walk of a model this package makes
// to walk, which asks the context before each item: the library's own walk
// reads none (docs/parser-library.md#context-unread), so a walk around it is
// one a cancel does not stop.
func TestLibraryWalks_GoThroughWalk(t *testing.T) {
	t.Parallel()
	pkg, err := typed.Load(".")
	require.NoError(t, err)
	lib, ok := pkg.Imported(soaPath)
	require.True(t, ok, "the package reads the library's model")

	require.Equal(t, []string{"walk"}, callersOf(t, pkg, lib.Scope().Lookup("Walk"), nil))
}

// callersOf returns the functions of pkg that call fn with a start of type
// start, or with any start for a nil one, sorted.
func callersOf(t *testing.T, pkg *typed.Package, fn types.Object, start types.Type) []string {
	t.Helper()
	require.NotNil(t, fn)

	var walkers []string
	for _, file := range pkg.Syntax {
		for _, d := range file.Decls {
			decl, isFunc := d.(*ast.FuncDecl)
			if !isFunc || decl.Body == nil {
				continue
			}
			ast.Inspect(decl.Body, func(n ast.Node) bool {
				call, isCall := n.(*ast.CallExpr)
				if !isCall || len(call.Args) != 2 || !callsObject(pkg, call.Fun, fn) {
					return true
				}
				if start == nil || types.Identical(pkg.Info.TypeOf(call.Args[1]), start) {
					walkers = append(walkers, decl.Name.Name)
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
	for wi := range locatedWalk(soa.Walk(t.Context(), &soa.ReferencedPathItem{Object: item}), "/x-paths/X") {
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
	for range locatedWalk(soa.Walk(t.Context(), &soa.ReferencedPathItem{Object: item}), "/x-paths/X") {
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
	sibs, _ := modelSiblings(t.Context(), doc, scan.InSource(0))
	sites := walkSites(walkSource(t.Context(), doc, sibs, MediaTypes{}))

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
	for item := range walkSource(t.Context(), doc, sibs, MediaTypes{}) {
		_ = item.Match(soa.Matcher{Operation: func(op *soa.Operation) error {
			if op == sibs.list[0].Item.Put() {
				count++
			}
			return nil
		}})
	}
	assert.Equal(t, 1, count, "a model is walked once, where it was found, though an alias reaches it twice")
	assert.Equal(t, walkSites(soa.Walk(t.Context(), doc)), walkSites(walkSource(t.Context(), doc, Siblings{}, MediaTypes{})),
		"with no model the walk is the document's own")
}

// TestWalkSource_StopsWhenTheReaderDoes pins that a pass leaving the walk early,
// as one stopping at a fault does, stops it inside a model as well as outside.
func TestWalkSource_StopsWhenTheReaderDoes(t *testing.T) {
	t.Parallel()
	doc := modelOf(t, siblingSpec)
	sibs, _ := modelSiblings(t.Context(), doc, scan.InSource(0))
	for _, stopAt := range []string{"/paths/~1b", "/paths/~1a/put"} {
		var last string
		for item := range walkSource(t.Context(), doc, sibs, MediaTypes{}) {
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
	sibs, _ := modelSiblings(t.Context(), doc, scan.InSource(0))
	nested := "/paths/~1a/put/callbacks/cb/{$request.body#~1url}/"
	const reads = 100
	before := runtime.NumGoroutine()
	for range reads {
		stopped := false
		for item := range walkSource(t.Context(), doc, sibs, MediaTypes{}) {
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
// the source through it, so it must cost what the library's own walk does and
// a constant, the same for a document of any size, for asking the context
// before each item (walk).
func TestWalkSource_WalksADocumentWithoutModelsAsTheLibraryDoes(t *testing.T) {
	drain := func(items iter.Seq[soa.WalkItem]) {
		for range items {
			continue
		}
	}
	overheads := make([]float64, 0, 2)
	for _, spec := range []string{siblingSpec, siblingSpec + "x-pad: [" + strings.Repeat("{a: b}, ", 200) + "]\n"} {
		doc := modelOf(t, spec)
		library := testing.AllocsPerRun(10, func() { drain(soa.Walk(t.Context(), doc)) })
		source := testing.AllocsPerRun(10, func() { drain(walkSource(t.Context(), doc, Siblings{}, MediaTypes{})) })
		overheads = append(overheads, source-library)
	}
	assert.LessOrEqual(t, overheads[0], 4.0, "a few allocations for the walk, none per item")
	assert.Equal(t, overheads[0], overheads[1], "asking the context costs the same whatever the walk's length")
}
