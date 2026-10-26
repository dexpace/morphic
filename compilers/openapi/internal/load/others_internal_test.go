package load

import (
	"context"
	"encoding/json/jsontext"
	"errors"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
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
	held := pointersIn(trees, []string{"./a.yaml", "/r/a.yaml", "a.yaml"})

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

// TestLabelOf_IsTheLocationTheSourcesDirectoryReachesADocumentBy pins the
// label that qualifies another document's IDs: a path relative to the source's
// directory, written with '/', whether either path is relative or absolute,
// the two related through the working directory; the cleaned path where that
// cannot be read; and a URL as read.
func TestLabelOf_IsTheLocationTheSourcesDirectoryReachesADocumentBy(t *testing.T) {
	t.Parallel()
	// inWork reads paths as filepath.Abs does with /work as the working directory.
	inWork := func(p string) (string, error) {
		if filepath.IsAbs(p) {
			return p, nil
		}
		return filepath.Join("/work", p), nil
	}
	// unread reads them as filepath.Abs does where the working directory cannot
	// be read: an absolute path as it is, a relative one not at all.
	unread := func(p string) (string, error) {
		if filepath.IsAbs(p) {
			return p, nil
		}
		return "", errors.New("no working directory")
	}
	for _, tc := range []struct {
		self, key, want string
		abs             func(string) (string, error)
	}{
		{"/r/root.yaml", "/r/common.yaml", "common.yaml", inWork},
		{"/r/root.yaml", "/r/schemas/pet.yaml", "schemas/pet.yaml", inWork},
		{"/r/root.yaml", "/r/schemas/../pet.yaml", "pet.yaml", inWork},
		{"/r/root.yaml", "/s/pet.yaml", "../s/pet.yaml", inWork},
		{"root.yaml", "inner/pet.yaml", "inner/pet.yaml", unread},
		{"root.yaml", "/work/inner/pet.yaml", "inner/pet.yaml", inWork},
		{"specs/root.yaml", "/work/pet.yaml", "../pet.yaml", inWork},
		{"/work/specs/root.yaml", "inner/pet.yaml", "../inner/pet.yaml", inWork},
		{"root.yaml", "/s/pet.yaml", "/s/pet.yaml", unread},
		{"/r/root.yaml", "inner/pet.yaml", "inner/pet.yaml", unread},
		{"/r/root.yaml", "https://h/pet.yaml", "https://h/pet.yaml", unread},
	} {
		assert.Equal(t, tc.want, labelOf(tc.self, tc.key, tc.abs), "%s from %s", tc.key, tc.self)
	}
}
