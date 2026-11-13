// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package resolve_test

import (
	"encoding/json/jsontext"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi/internal/bind"
	"github.com/dexpace/morphic/compilers/openapi/internal/load"
	"github.com/dexpace/morphic/compilers/openapi/internal/resolve"
	"github.com/dexpace/morphic/engine/reader"
)

// TestHops_ListsTheReferencesAChainPassesThrough pins Hops over an alias
// chain: each alias the chain passes after the entry's own $ref, nearest first,
// with the position the bindings' table reads it at, and none for the
// declaration it ends at, for an inline entry, or for a chain that fails or
// loops.
func TestHops_ListsTheReferencesAChainPassesThrough(t *testing.T) {
	t.Parallel()
	for _, hops := range []int{0, 3, 40} {
		doc := loaded(t, chainedAliasSpec(hops)+"    Gone: {$ref: '#/components/parameters/P0'}\n"+
			"    L1: {$ref: '#/components/parameters/L2'}\n    L2: {$ref: '#/components/parameters/L1'}\n"+
			"    Missing: {$ref: '#/components/parameters/Nope'}\n    Fails: {$ref: '#/components/parameters/Missing'}\n")
		item, ok := doc.Doc.Paths.Get("/a")
		require.True(t, ok)
		assert.Empty(t, resolve.Hops[soa.PathItem](doc.Bindings, item), "an inline entry has no hops")
		param := item.GetObject().Get().GetParameters()[0]

		got := resolve.Hops[soa.Parameter](doc.Bindings, param)

		require.Len(t, got, hops, "%d hops", hops)
		for i, hop := range got {
			pointer := jsontext.Pointer("/components/parameters/P" + strconv.Itoa(i))
			assert.Equal(t, bind.Position{Pointer: pointer}, hop.At, "hop %d is P%d", i, i)
			wrapper, _ := doc.Doc.Components.GetParameters().Get("P" + strconv.Itoa(i))
			assert.True(t, wrapper == hop.Ref, "hop %d is the wrapper read there", i)
		}
		for _, name := range []string{"L1", "Missing", "Fails"} {
			assert.Empty(t, resolve.Hops[soa.Parameter](doc.Bindings, doc.Doc.Components.GetParameters().GetOrZero(name)), name)
		}
	}
}

// TestHops_AHopAnotherDocumentHoldsIsPlacedThere pins where Hops places a
// hop: past a $ref into another document, the hop written there is at its
// position in that document, and a $ref from it back into the source places
// the next in the source.
func TestHops_AHopAnotherDocumentHoldsIsPlacedThere(t *testing.T) {
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
	other := documentNamed(t, doc.Bindings.Table(), filepath.Join(dir, "other.yaml"))

	got := resolve.Hops[soa.PathItem](doc.Bindings, item)

	require.Len(t, got, 2, "other.yaml's /x, then the source's /b")
	assert.Equal(t, bind.Position{Doc: other, Pointer: "/paths/~1x"}, got[0].At, "/x is written in other.yaml")
	assert.Equal(t, bind.Position{Pointer: "/paths/~1b"}, got[1].At, "/b is written in the source")
}

// documentNamed returns the document table read under name.
func documentNamed(t *testing.T, table *bind.Table, name string) bind.DocID {
	t.Helper()
	for id := range bind.DocID(table.Documents()) {
		if table.Document(id).Name == name {
			return id
		}
	}
	require.FailNow(t, "no document named "+name)
	return 0
}
