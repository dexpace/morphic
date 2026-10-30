// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package resolve_test

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"

	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi/internal/load"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/compilers/openapi/internal/resolve"
	"github.com/dexpace/morphic/engine/reader"
)

// TestHops_ListsTheReferencesAChainPassesThrough pins Hops over an alias
// chain: each alias the chain passes after the entry's own $ref, nearest first
// and read as this document's, none for the declaration it ends at or for an
// inline entry, and no more than the bound reads.
func TestHops_ListsTheReferencesAChainPassesThrough(t *testing.T) {
	t.Parallel()
	for hops, want := range map[int]int{0: 0, 3: 3, resolve.MaxRefChain + 4: resolve.MaxRefChain} {
		doc, _, err := load.Load(t.Context(), 0, openapitest.SourceOf(chainedAliasSpec(hops)), load.Options{})
		require.NoError(t, err)
		require.NotNil(t, doc)
		scope := resolve.Scope{Doc: doc.Doc, SelfPath: doc.Source.Path}
		item, ok := doc.Doc.Paths.Get("/a")
		require.True(t, ok)
		assert.Empty(t, resolve.Hops[soa.PathItem](scope, item), "an inline entry has no hops")
		param := item.GetObject().Get().GetParameters()[0]

		got := resolve.Hops[soa.Parameter](scope, param)

		require.Len(t, got, want, "%d hops", hops)
		for i, hop := range got {
			assert.Equal(t, "#/components/parameters/P"+strconv.Itoa(i+1), string(hop.Ref.GetReference()),
				"hop %d is P%d, written as a $ref to the next", i, i)
			assert.False(t, hop.Foreign, "hop %d is written in this document", i)
		}
	}
}

// TestHops_AHopAnotherDocumentHoldsIsForeign pins where Hops reads a hop: past
// a $ref into another document, the hop written there is that document's, and
// a $ref from it back into the source reads the next hop as the source's.
func TestHops_AHopAnotherDocumentHoldsIsForeign(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	mainPath := filepath.Join(dir, "main.yaml")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "other.yaml"), []byte(`openapi: 3.1.0
info: {title: O, version: "1"}
paths:
  /x: {$ref: 'main.yaml#/paths/~1b'}
`), 0o600))
	src := compilers.Source{Path: mainPath, Data: []byte(`openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /a: {$ref: './other.yaml#/paths/~1x'}
  /b: {$ref: '#/paths/~1c'}
  /c: {get: {responses: {"200": {description: OK}}}}
`)}
	doc, _, err := load.Load(t.Context(), 0, src, load.Options{Reader: reader.Disk{}})
	require.NoError(t, err)
	require.NotNil(t, doc)
	item, ok := doc.Doc.Paths.Get("/a")
	require.True(t, ok)

	got := resolve.Hops[soa.PathItem](resolve.Scope{Doc: doc.Doc, SelfPath: mainPath}, item)

	require.Len(t, got, 2, "other.yaml's /x, then the source's /b")
	assert.Equal(t, "main.yaml#/paths/~1b", string(got[0].Ref.GetReference()))
	assert.True(t, got[0].Foreign, "/x is written in other.yaml")
	assert.Equal(t, "#/paths/~1c", string(got[1].Ref.GetReference()))
	assert.False(t, got[1].Foreign, "/b is written in the source")
}

// TestChained_SaysWhetherHopsHasOneToList pins Chained against Hops: a chain
// passing a reference after its entry has a hop, and one whose entry names an
// item, or that is inline, has none.
func TestChained_SaysWhetherHopsHasOneToList(t *testing.T) {
	t.Parallel()
	const spec = `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /x: {$ref: '#/paths/~1a'}
  /a: {$ref: '#/paths/~1b'}
  /b:
    get: {responses: {"200": {description: ok}}}
`
	doc, _, err := load.Load(t.Context(), 0, compilers.Source{Path: "spec.yaml", Data: []byte(spec)}, load.Options{})
	require.NoError(t, err)
	scope := resolve.Scope{SelfPath: "spec.yaml", Doc: doc.Doc}
	for path, want := range map[string]bool{"/x": true, "/a": false, "/b": false} {
		rp, ok := doc.Doc.GetPaths().Get(path)
		require.True(t, ok)
		assert.Equal(t, want, resolve.Chained[soa.PathItem](rp), path)
		assert.Equal(t, want, len(resolve.Hops[soa.PathItem](scope, rp)) > 0, "%s: as Hops lists", path)
	}
}

// TestHops_ReadsNoScopeForAChainOfOneHop pins Hops's cost on the common chain,
// a use site naming an item that is no reference: there is no hop to read a
// scope for, so it reads none. Reading one before asking cost every $ref'd path
// item a pointer walk whose answer was thrown away.
func TestHops_ReadsNoScopeForAChainOfOneHop(t *testing.T) {
	doc, bare, _ := loadBeside(t)
	c, ok := doc.Doc.GetPaths().Get("/a")
	require.True(t, ok)
	require.True(t, c.IsReference())

	assert.Empty(t, resolve.Hops[soa.PathItem](bare, c))
	assert.Zero(t, testing.AllocsPerRun(10, func() { resolve.Hops[soa.PathItem](bare, c) }))
}
