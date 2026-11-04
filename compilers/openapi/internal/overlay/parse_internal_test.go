// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package overlay

import (
	"bytes"
	"context"
	"testing"

	"github.com/google/go-cmp/cmp"
	soaoverlay "github.com/speakeasy-api/openapi/overlay"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/jsontree"
)

// libraryParse is what the library makes of an overlay document, which
// parseDocument must make of any document both read.
func libraryParse(data string) (*soaoverlay.Overlay, error) {
	return soaoverlay.ParseReader(bytes.NewReader([]byte(data)))
}

// TestParseDocument_ReadsWhatTheLibraryReads holds parseDocument to the
// library's own parse of an overlay document, ParseReader, for documents both
// read: JSON that the YAML parser reads, which parseDocument reads as JSON, and
// YAML, which it leaves to the library. The two give the same overlay, or the
// same error, so reading JSON itself changes no overlay the library applies.
func TestParseDocument_ReadsWhatTheLibraryReads(t *testing.T) {
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

			got, err := parseDocument(t.Context(), []byte(tc.data))

			if wantErr != nil {
				require.Error(t, err)
				assert.Equal(t, wantErr.Error(), err.Error(), "the library's error")
				assert.Nil(t, got)
				return
			}
			require.NoError(t, err)
			assert.Empty(t, cmp.Diff(want, got), "the library's overlay (-library +parseDocument)")
		})
	}
}

// TestParseDocument_ReadsWhatTheLibraryRefuses pins the overlays parseDocument
// reads that the library does not: JSON writing a surrogate pair or a solidus
// escape, as the characters they name, with the nodes in the update the
// positions the document writes. Half of a surrogate pair is the reader's
// error, and a done context is its own.
func TestParseDocument_ReadsWhatTheLibraryRefuses(t *testing.T) {
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

			doc, err := parseDocument(t.Context(), []byte(tc.in))

			require.NoError(t, err)
			require.Len(t, doc.Actions, 1)
			update := doc.Actions[0].Update
			assert.Equal(t, tc.want, update.Content[1].Value)
			assert.Equal(t, 1, update.Line)
			assert.Equal(t, len(`{"overlay": "1.0.0", "info": {"title": "o", "version": "1"}, "actions": [{"target": "$.info", "update": `)+1,
				update.Column, "where the document writes the update")
		})
	}

	doc, err := parseDocument(t.Context(), []byte(head+"x"+half+"y"+tail))
	assert.Nil(t, doc)
	var lone *jsontree.LoneSurrogateError
	require.ErrorAs(t, err, &lone)
	assert.Equal(t, half, lone.Escape)

	cancelled, cancel := context.WithCancel(t.Context())
	cancel()
	doc, err = parseDocument(cancelled, []byte(head+"x"+pair+"y"+tail))
	assert.Nil(t, doc)
	assert.ErrorIs(t, err, context.Canceled)
}
