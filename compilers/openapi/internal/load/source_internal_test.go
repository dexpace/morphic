// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package load

import (
	"encoding/json/jsontext"
	"math"
	"path/filepath"
	"testing"

	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/budget"
	"github.com/dexpace/morphic/compilers/openapi/internal/oas"
	"github.com/dexpace/morphic/compilers/openapi/internal/provenance"
	"github.com/dexpace/morphic/engine/reader"
	"github.com/dexpace/morphic/internal/ctxtest"
	"github.com/dexpace/morphic/ir"
)

// backRoot is a source whose /a response is a $ref into back.yaml, which
// refers back into the source by spelling, and whose own R lacks the
// description a response requires.
func backRoot(paths string) string {
	return `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
` + paths + `components:
  responses:
    R:
      content: {}
`
}

// backPath is a path item whose get operation's 200 response is ref.
func backPath(path, ref string) string {
	return "  " + path + ":\n    get:\n      responses:\n        \"200\": {$ref: '" + ref + "'}\n"
}

// TestSourceDocument_Keys pins the keys the source is held under.
func TestSourceDocument_Keys(t *testing.T) {
	t.Parallel()
	tree := &yaml.Node{}
	for _, c := range []struct {
		name string
		self sourceDocument
		want []string
	}{
		{"a clean path", sourceDocument{path: "dir/root.yaml", root: tree}, []string{"dir/root.yaml"}},
		{"a path cleaned", sourceDocument{path: "./dir/../root.yaml", root: tree}, []string{"./dir/../root.yaml", "root.yaml"}},
		{"a URL, as spelled", sourceDocument{path: "https://example.com/./a.yaml", root: tree}, []string{"https://example.com/./a.yaml"}},
		{"no path", sourceDocument{root: tree}, nil},
		{"no tree", sourceDocument{path: "root.yaml"}, nil},
	} {
		assert.Equal(t, c.want, c.self.keys(), c.name)
	}
}

// TestSourceDocument_Names pins which file names the resolver may open that
// name the source.
func TestSourceDocument_Names(t *testing.T) {
	t.Parallel()
	tree := &yaml.Node{}
	self := sourceDocument{path: "dir/root.yaml", root: tree}
	abs, err := filepath.Abs("dir/root.yaml")
	require.NoError(t, err)
	for _, c := range []struct {
		name string
		self sourceDocument
		file string
		want bool
	}{
		{"itself", self, "dir/root.yaml", true},
		{"through a directory", self, "dir/../dir/./root.yaml", true},
		{"its absolute path", self, abs, true},
		{"another file", self, "dir/other.yaml", false},
		{"from a source with no tree", sourceDocument{path: "dir/root.yaml"}, "dir/root.yaml", false},
		{"from a source named by URL", sourceDocument{path: "https://example.com/a.yaml", root: tree},
			"https://example.com/a.yaml", true},
		{"another spelling of a source named by URL", sourceDocument{path: "https://example.com/a.yaml", root: tree},
			"https://example.com/./a.yaml", false},
		{"from a source whose name parses as a URL", sourceDocument{path: "x:root.yaml", root: tree},
			"x:root.yaml", true},
	} {
		assert.Equal(t, c.want, c.self.names(c.file), c.name)
	}
}

// heldResolution returns spec's model, built as the source at path, and the
// resolution newResolution makes of it with external references allowed, which
// has held the source where the resolver looks for it.
func heldResolution(t *testing.T, spec, path string) (*soa.OpenAPI, *resolution) {
	t.Helper()
	data := []byte(spec)
	root, _, err := decodeStream(t.Context(), data)
	require.NoError(t, err)
	readyForModel(t.Context(), root)
	doc, err := unmarshal(t.Context(), data, root)
	require.NoError(t, err)
	self := newSourceDocument(path, data, root, judgment{}, treeOf(t.Context(), 0, root), nil)
	opts := Options{Reader: reader.Disk{}}
	ext := newExternal(t.Context(), doc, opts, newExternalReads(self, budget.Unbounded()))
	return doc, newResolution(t.Context(), doc, Siblings{}, MediaTypes{}, self, opts, &ext)
}

// TestTreeOf_IndexesUnderTheLoadsContext pins that the tree's pointer
// index is built under the context the load runs under, once a node is first
// asked about (scan.Pointers).
func TestTreeOf_IndexesUnderTheLoadsContext(t *testing.T) {
	t.Parallel()
	root := decodeRoot(t, "openapi: 3.1.0\ninfo: {title: T, version: \"1\"}\n")
	ctx := &ctxtest.Countdown{Context: t.Context(), Left: math.MaxInt}

	tree := treeOf(ctx, 0, root)
	require.Zero(t, ctx.Calls, "nothing is indexed before a node is asked about")

	assert.True(t, treeHolds(tree, root.Content[0]))
	assert.Positive(t, ctx.Calls)
}

// TestWalkedFrom_AnswersAtTheLastNodeItReaches pins that a pointer
// leading nowhere is placed at the last node it reached, and that a tree
// holding no node places every finding at the reference that reached it.
func TestWalkedFrom_AnswersAtTheLastNodeItReaches(t *testing.T) {
	t.Parallel()
	root := decodeRoot(t, "openapi: 3.1.0\ninfo: {title: T, version: \"1\"}\n")
	tree := treeOf(t.Context(), 0, root)

	for _, locate := range []oas.Locator{walkedFrom(root, tree.Node), reachedLocator(tree, root)} {
		got := locate("/info/nowhere/at/all")
		assert.Equal(t, jsontext.Pointer("/info"), got.Pointer, "placed at /info, the last node the pointer reached")
	}
	assert.Equal(t, ir.Provenance{Source: 0}, walkedFrom(&yaml.Node{Kind: yaml.DocumentNode}, tree.Node)("/x"), "an empty tree is placed by source alone")

	assert.False(t, treeHolds(provenance.Tree{}, root), "a tree that holds nothing holds no node")
	assert.True(t, treeHolds(tree, root.Content[0]), "and one holds the nodes it indexed")
	foreign := reachedLocator(provenance.Tree{}, root)("/info")
	assert.Equal(t, ir.Provenance{Source: foreign.Source, Position: foreign.Position}, foreign)
	assert.Equal(t, 2, foreign.Position.Line)
	assert.False(t, holds(ir.Diagnostic{Provenance: foreign}), "marked foreign")
}
