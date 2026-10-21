package load

import (
	"context"
	"encoding/json/jsontext"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi/internal/bind"
	"github.com/dexpace/morphic/ir"
)

// servedReader answers every request with one document declaring X.
type servedReader struct{}

// ReadDocument answers req.
func (servedReader) ReadDocument(context.Context, compilers.DocumentRequest) ([]byte, error) {
	return []byte("X: {type: string}\n"), nil
}

// loadReferencing loads a source at /r/root.yaml whose component A is a $ref
// to ref, reading every other document through servedReader.
func loadReferencing(t *testing.T, ref string) *Document {
	t.Helper()
	doc, diags, err := Load(t.Context(), 0, compilers.Source{Path: "/r/root.yaml", Data: []byte(
		"openapi: 3.1.0\ninfo: {title: T, version: \"1\"}\npaths: {}\ncomponents:\n  schemas:\n    A: {$ref: '" + ref + "'}\n",
	)}, Options{Reader: servedReader{}})
	require.NoError(t, err)
	require.Empty(t, diags)
	require.NotNil(t, doc)
	return doc
}

// TestOthers_NamesADocumentByEverySpellingItIsReadUnder pins how a document a
// reference reads is found again: by the name the binder spells it with, which
// for a URL is as written, and by the key its request was read under, which
// escapes what the URL's syntax requires. A name no read answered, or one no
// request could be built from, finds nothing; nor does an index past the list.
func TestOthers_NamesADocumentByEverySpellingItIsReadUnder(t *testing.T) {
	t.Parallel()
	doc := loadReferencing(t, "http://h/a b.yaml#/X")
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

// TestLabelOf_IsTheLocationTheSourcesDirectoryReachesADocumentBy pins the
// label that qualifies another document's IDs: a path relative to the source's
// directory, written with '/', or the cleaned path where none reaches it, and a
// URL as read.
func TestLabelOf_IsTheLocationTheSourcesDirectoryReachesADocumentBy(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ self, key, want string }{
		{"/r/root.yaml", "/r/common.yaml", "common.yaml"},
		{"/r/root.yaml", "/r/schemas/pet.yaml", "schemas/pet.yaml"},
		{"/r/root.yaml", "/r/schemas/../pet.yaml", "pet.yaml"},
		{"/r/root.yaml", "/s/pet.yaml", "../s/pet.yaml"},
		{"root.yaml", "/s/pet.yaml", "/s/pet.yaml"},
		{"/r/root.yaml", "https://h/pet.yaml", "https://h/pet.yaml"},
	} {
		assert.Equal(t, tc.want, labelOf(tc.self, tc.key), "%s from %s", tc.key, tc.self)
	}
}

// TestPlacer_PlacesAFindingInTheDocumentThatHoldsIt pins where a finding at a
// position of the bindings' table is placed: the source's at its own
// Provenance, another document's in that document's Document.Sources entry,
// and one in a document no entry lists, which a load never produces, at the
// source's root rather than nowhere.
func TestPlacer_PlacesAFindingInTheDocumentThatHoldsIt(t *testing.T) {
	t.Parallel()
	doc := loadReferencing(t, "./ext.yaml#/X")
	at := func(p jsontext.Pointer) ir.Provenance { return ir.Provenance{Pointer: "/at" + p} }
	ext := bind.Position{Doc: 1, Pointer: "/X"}

	listed := bound{bindings: doc.Bindings, others: doc.Others}.placer(at)
	assert.Equal(t, ir.Provenance{Pointer: "/at/components"}, listed(bind.Position{Pointer: "/components"}))
	assert.Equal(t, ir.Provenance{Source: 1, Pointer: "/X"}, listed(ext))

	unlisted := bound{bindings: doc.Bindings}.placer(at)
	assert.Equal(t, ir.Provenance{Pointer: "/at"}, unlisted(ext))
}
