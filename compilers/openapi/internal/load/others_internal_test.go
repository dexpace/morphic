// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package load

import (
	"context"
	"encoding/json/jsontext"
	"math"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi/internal/bind"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/internal/ctxtest"
)

// servedReader answers every request with one document declaring X.
type servedReader struct{}

// ReadDocument answers req.
func (servedReader) ReadDocument(context.Context, compilers.DocumentRequest) ([]byte, error) {
	return []byte("X: {type: string}\n"), nil
}

// loadReferencing loads a source at /r/root.yaml whose component A is a $ref
// to ref, reading every other document through servedReader, and returns it
// with the code of each diagnostic the load reported.
func loadReferencing(t *testing.T, ref string) (*Document, []string) {
	t.Helper()
	doc, diags, err := Load(t.Context(), 0, compilers.Source{Path: "/r/root.yaml", Data: []byte(
		"openapi: 3.1.0\ninfo: {title: T, version: \"1\"}\npaths: {}\ncomponents:\n  schemas:\n    A: {$ref: '" + ref + "'}\n",
	)}, Options{Reader: servedReader{}})
	require.NoError(t, err)
	require.NotNil(t, doc)
	codes := make([]string, 0, len(diags))
	for _, d := range diags {
		codes = append(codes, d.Code)
	}
	return doc, codes
}

// TestOthers_NamesADocumentByEverySpellingItIsReadUnder pins how a document a
// reference reads is found again: by the name the binder spells it with, which
// for a URL is as written, and by the key its request was read under, which
// escapes what the URL's syntax requires. A name no read answered, or one no
// request could be built from, finds nothing; nor does an index past the list.
// A space is no URI reference, which validation reports, and the document is
// read all the same.
func TestOthers_NamesADocumentByEverySpellingItIsReadUnder(t *testing.T) {
	t.Parallel()
	doc, codes := loadReferencing(t, "http://h/a b.yaml#/X")
	require.Equal(t, []string{diag.Validation + "/validation-invalid-format"}, codes)
	others := doc.Others.List()
	require.Len(t, others, 1)
	ext := others[0]
	assert.Equal(t, 1, ext.Index, "the first entry after the source")
	assert.Equal(t, "http://h/a%20b.yaml", ext.Label)
	assert.Equal(t, "http://h/a%20b.yaml", ext.Info.Path)
	assert.Equal(t, "http://h/a b.yaml", doc.Bindings.Table().Document(1).Name, "sanity: the binder's spelling")

	for name, want := range map[string]bool{
		"http://h/a b.yaml":   true,
		"http://h/a%20b.yaml": true,
		"http://h/c.yaml":     false,
		"http://h/%zz":        false,
	} {
		got, ok := doc.Others.Named(name)
		assert.Equal(t, want, ok, name)
		if want {
			assert.Equal(t, ext, got, name)
		}
	}
	for index, want := range map[int]bool{0: false, 1: true, 2: false} {
		_, ok := doc.Others.At(index)
		assert.Equal(t, want, ok, "index %d", index)
	}
	_, ok := Others{}.At(1)
	assert.False(t, ok, "an empty list holds no index")
}

// TestPointersIn_PlacesANodeInWhicheverTreeHoldsIt pins the pointers of a
// document read prepared under two keys, each its own tree: a node of either is
// placed where it is written, an alias's target where the anchor is, and a
// node neither holds has no pointer, so it is placed by its position alone.
func TestPointersIn_PlacesANodeInWhicheverTreeHoldsIt(t *testing.T) {
	t.Parallel()
	trees := map[string]*yaml.Node{}
	for _, key := range []string{"a.yaml", "./a.yaml"} {
		var tree yaml.Node
		require.NoError(t, yaml.Unmarshal([]byte("A: &a {x: 1}\nB: *a\n"), &tree))
		trees[key] = &tree
	}
	trees["/r/a.yaml"] = trees["a.yaml"]
	held := pointersIn(t.Context(), trees, []string{"./a.yaml", "/r/a.yaml", "a.yaml"})

	for key, tree := range trees {
		x := tree.Content[0].Content[3].Alias.Content[1]
		p, ok := held(x)
		assert.True(t, ok, key)
		assert.Equal(t, jsontext.Pointer("/A/x"), p, key)
	}
	p, ok := held(&yaml.Node{Kind: yaml.ScalarNode, Value: "1"})
	assert.False(t, ok)
	assert.Empty(t, p)
}

// TestPointersIn_IndexesUnderTheLoadsContext pins that each tree's pointer
// index is built under the context the load runs under (scan.Pointers).
func TestPointersIn_IndexesUnderTheLoadsContext(t *testing.T) {
	t.Parallel()
	var tree yaml.Node
	require.NoError(t, yaml.Unmarshal([]byte("A: {x: 1}\n"), &tree))
	ctx := &ctxtest.Countdown{Context: t.Context(), Left: math.MaxInt}

	_, ok := pointersIn(ctx, map[string]*yaml.Node{"a.yaml": &tree}, []string{"a.yaml"})(tree.Content[0])

	assert.True(t, ok)
	assert.Positive(t, ctx.Calls)
}

// TestLabelOf_IsTheLocationTheSourcesDirectoryReachesADocumentBy pins the
// label that qualifies another document's IDs: a path relative to the source's
// directory, written with '/', where both are relative or both absolute; the
// cleaned path where one is relative and the other absolute, which nothing
// relates without the working directory, never read; and a URL as read.
func TestLabelOf_IsTheLocationTheSourcesDirectoryReachesADocumentBy(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		self, key, want string
		relative        bool
	}{
		{"/r/root.yaml", "/r/common.yaml", "common.yaml", true},
		{"/r/root.yaml", "/r/schemas/pet.yaml", "schemas/pet.yaml", true},
		{"/r/root.yaml", "/r/schemas/../pet.yaml", "pet.yaml", true},
		{"/r/root.yaml", "/s/pet.yaml", "../s/pet.yaml", true},
		{"root.yaml", "inner/pet.yaml", "inner/pet.yaml", true},
		{"specs/root.yaml", "pet.yaml", "../pet.yaml", true},
		{"root.yaml", "/work/inner/pet.yaml", "/work/inner/pet.yaml", false},
		{"/work/specs/root.yaml", "inner/pet.yaml", "inner/pet.yaml", false},
		{"/r/root.yaml", "https://h/pet.yaml", "https://h/pet.yaml", false},
	} {
		label, relative := sourceDocument{path: tc.self}.labelOf(tc.key)
		assert.Equal(t, tc.want, label, "%s from %s", tc.key, tc.self)
		assert.Equal(t, tc.relative, relative, "%s from %s", tc.key, tc.self)
	}
}

// TestShownPathOf_NamesADocumentAsTheHandedPathNamesTheSource pins the path
// Document.Sources records for another document: beside the path the caller
// handed, as relative as it is, where its label is relative, though the
// references were read against the source's absolute Location, a label
// climbing out of the handed directory keeping its dot segments, which a link
// to that directory gives another meaning than cleaning them away would; else
// as read.
func TestShownPathOf_NamesADocumentAsTheHandedPathNamesTheSource(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		self sourceDocument
		key  string
		want string
	}{
		{sourceDocument{path: "/r/api/root.yaml", handed: "api/root.yaml"}, "/r/api/common.yaml", "api/common.yaml"},
		{sourceDocument{path: "/r/api/root.yaml", handed: "root.yaml"}, "/r/common.yaml", "../common.yaml"},
		{sourceDocument{path: "/r/api/root.yaml", handed: "/l/api/root.yaml"}, "/r/api/s/c.yaml", "/l/api/s/c.yaml"},
		{sourceDocument{path: "/r/api/root.yaml", handed: "api/root.yaml"}, "/r/common.yaml", "api/../common.yaml"},
		{sourceDocument{path: "/r/api/root.yaml", handed: "/l/api/root.yaml"}, "/r/common.yaml", "/l/api/../common.yaml"},
		{sourceDocument{path: "/r/api/root.yaml", handed: "api/root.yaml"}, "/common.yaml", "api/../../common.yaml"},
		{sourceDocument{path: "/r/api/root.yaml", handed: "root.yaml"}, "/r/api/file:x.yaml", "./file:x.yaml"},
		{sourceDocument{path: "/r/api/root.yaml", handed: "root.yaml"}, "https://h/c.yaml", "https://h/c.yaml"},
		{sourceDocument{path: "/r/api/root.yaml", handed: "https://h/root.yaml"}, "/r/api/c.yaml", "/r/api/c.yaml"},
		{sourceDocument{path: "api/root.yaml", handed: "api/root.yaml"}, "api/x/../c.yaml", "api/c.yaml"},
		{sourceDocument{path: "api/root.yaml"}, "/abs/c.yaml", "/abs/c.yaml"},
		{sourceDocument{path: "root.yaml", handed: "root.yaml"}, "file:x.yaml", "file:x.yaml"},
	} {
		assert.Equal(t, filepath.FromSlash(tc.want), tc.self.shownPathOf(tc.key), "%s from %+v", tc.key, tc.self)
	}
}

// TestUnreachedKeys_NamesWhatOnlyAPassThatDidNotStandRead pins what leaves
// Document.Sources: a document the table holds nothing of, unless the parser's
// resolver read it too, whose reference objects the table need not see.
func TestUnreachedKeys_NamesWhatOnlyAPassThatDidNotStandRead(t *testing.T) {
	t.Parallel()
	docs := map[string]string{
		"/a.yaml": "$defs: {T: {type: integer}}\n",
		"/z.yaml": "S: {$id: '/a.yaml', $defs: {T: {type: string}}}\n",
	}
	var root yaml.Node
	require.NoError(t, yaml.Unmarshal([]byte("components:\n  schemas:\n"+
		"    A: {$ref: 'a.yaml#/$defs/T'}\n    Z: {$ref: 'z.yaml#/S'}\n"), &root))
	table := bind.Bind(t.Context(), &root, bind.Config{Self: "/spec.yaml",
		Locate: func(uri, _ string) (string, bool) { return "/" + strings.TrimPrefix(uri, "/"), true },
		Open: func(name string) (*yaml.Node, error) {
			var n yaml.Node
			return &n, yaml.Unmarshal([]byte(docs[name]), &n)
		}})

	assert.Equal(t, map[string]bool{"/a.yaml": true}, unreachedKeys(table, nil))
	assert.Empty(t, unreachedKeys(table, map[string]bool{"/a.yaml": true}))
}
