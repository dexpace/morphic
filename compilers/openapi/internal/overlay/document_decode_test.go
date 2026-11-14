// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package overlay_test

import (
	"bytes"
	"context"
	"testing"

	"github.com/google/go-cmp/cmp"
	soaoverlay "github.com/speakeasy-api/openapi/overlay"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/budget"
	"github.com/dexpace/morphic/compilers/openapi/internal/doctree"
)

// libraryParse is what the library makes of an overlay document, which the
// Overlay decoded from the document's tree must equal for any document both
// read.
func libraryParse(data string) (*soaoverlay.Overlay, error) {
	return soaoverlay.ParseReader(bytes.NewReader([]byte(data)))
}

// decodeFromTree reads data into a tree as every input is read, and decodes the
// library's Overlay from the tree's root with the decoder's own defaults.
func decodeFromTree(ctx context.Context, data string) (*soaoverlay.Overlay, error) {
	doc, err := doctree.Read(ctx, []byte(data), budget.Unbounded())
	if err != nil {
		return nil, err
	}
	var ov soaoverlay.Overlay
	if doc.Root.Kind == 0 {
		return &ov, nil // a stream holding nothing, which the library reads as an empty overlay
	}
	if err := doc.Root.Decode(&ov); err != nil {
		return nil, err
	}
	return &ov, nil
}

// TestDecodeFromTree_ReadsWhatTheLibraryReads holds the Overlay decoded from a
// document's tree to the library's own parse, ParseReader, for documents both
// read, JSON and YAML alike: the same overlay, or an error from each.
func TestDecodeFromTree_ReadsWhatTheLibraryReads(t *testing.T) {
	t.Parallel()
	const head = `{"overlay": "1.0.0", "info": {"title": "o", "version": "1"}, "actions": `
	cases := map[string]struct {
		data   string
		errors bool // whether the library refuses the document
	}{
		"a YAML overlay": {"overlay: 1.0.0\ninfo: {title: o, version: '1'}\nactions:\n" +
			"  - target: $.info\n    update: {description: d}\n", false},
		"a YAML overlay with a comment after flow": {head + `[]} # end` + "\n", false},
		"a JSON overlay": {head + `[{"target": "$.info", "update": {"description": "d", "x-n": [1, 2.5, null, true]}}]}`, false},
		"a pretty JSON overlay": {"{\n  \"overlay\": \"1.0.0\",\n  \"info\": {\n    \"title\": \"o\",\n" +
			"    \"version\": \"1\",\n    \"description\": \"a\\nb\"\n  },\n  \"actions\": [\n    {\n" +
			"      \"target\": \"$.paths\",\n      \"remove\": true\n    }\n  ]\n}\n", false},
		"a JSON overlay with extensions": {`{"overlay": "1.1.0", "x-speakeasy-jsonpath": "rfc9535", "x-a": {"b": 1}, ` +
			`"info": {"title": "o", "version": "1", "x-c": "d"}, "extends": "https://example.test/spec.json", ` +
			`"actions": [{"target": "$.a", "copy": "$.b", "description": "c", "x-e": 1}]}`, false},
		"a JSON overlay whose update is a scalar": {head + `[{"target": "$.info.title", "update": "T"}]}`, false},
		"a JSON null":                       {`null`, false},
		"empty":                             {"", false},
		"a comment":                         {"# nothing\n", false},
		"a JSON overlay of the wrong shape": {`{"overlay": ["1.0.0"], "info": 3, "actions": {"target": "$"}}`, true},
		"a JSON overlay with a wrong field": {head + `[{"target": ["$.info"], "remove": "yes"}]}`, true},
		"a JSON name written twice":         {head + `[], "overlay": "1.1.0"}`, true},
		"a JSON array":                      {`[1, 2]`, true},
		"a JSON string":                     {`"overlay"`, true},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			want, wantErr := libraryParse(tc.data)
			require.Equal(t, tc.errors, wantErr != nil, "the library's verdict on the document")

			got, err := decodeFromTree(t.Context(), tc.data)

			if wantErr != nil {
				require.Error(t, err, "the library's error was %v", wantErr)
				assert.Nil(t, got)
				return
			}
			require.NoError(t, err)
			assert.Empty(t, cmp.Diff(want, got), "the library's overlay (-library +tree)")
		})
	}
}

// TestDecodeFromTree_ReadsWhatTheLibraryRefuses pins the overlays the tree
// reads that the library does not: JSON writing a surrogate pair or a solidus
// escape, as the characters they name, with the nodes in the update the
// positions the document writes. Half of a surrogate pair is the reader's
// error, and a done context is its own.
func TestDecodeFromTree_ReadsWhatTheLibraryRefuses(t *testing.T) {
	t.Parallel()
	const head = `{"overlay": "1.0.0", "info": {"title": "o", "version": "1"}, "actions": ` +
		`[{"target": "$.info", "update": {"description": "`
	const tail = `"}}]}`
	half := `\u` + `d83d`
	pair := half + `\u` + `de00`

	for name, tc := range map[string]struct{ in, want string }{
		"a surrogate pair": {head + "x" + pair + "y" + tail, "x😀y"},
		"a solidus escape": {head + `a\/b` + tail, "a/b"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, err := libraryParse(tc.in)
			require.Error(t, err, "the library refuses it")

			doc, err := decodeFromTree(t.Context(), tc.in)

			require.NoError(t, err)
			require.Len(t, doc.Actions, 1)
			update := doc.Actions[0].Update
			assert.Equal(t, tc.want, update.Content[1].Value)
			assert.Equal(t, 1, update.Line)
			assert.Equal(t, len(`{"overlay": "1.0.0", "info": {"title": "o", "version": "1"}, "actions": [{"target": "$.info", "update": `)+1,
				update.Column, "where the document writes the update")
		})
	}

	doc, err := decodeFromTree(t.Context(), head+"x"+half+"y"+tail)
	assert.Nil(t, doc)
	var lone *doctree.LoneSurrogateError
	require.ErrorAs(t, err, &lone)
	assert.Equal(t, half, lone.Escape)

	cancelled, cancel := context.WithCancel(t.Context())
	cancel()
	doc, err = decodeFromTree(cancelled, head+"x"+pair+"y"+tail)
	assert.Nil(t, doc)
	assert.ErrorIs(t, err, context.Canceled)
}
