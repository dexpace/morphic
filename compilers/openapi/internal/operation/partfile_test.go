// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package operation_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/ir"
)

// filePart compiles a version body of media type mediaType whose one part, file,
// is declared part and encoded encoding ("" for no entry), under opts, beside a
// component Blob of raw bytes. It returns the part's entry, ok false where it
// has none.
func filePart(t *testing.T, version, mediaType, part, encoding string, opts openapi.Options) (ir.PartEncoding, bool) {
	t.Helper()
	enc := ""
	if encoding != "" {
		enc = "\n            encoding: {file: " + encoding + "}"
	}
	doc, _ := streamingSpec(t, openapitest.PathsSpecVer(version, `  /upload:
    post:
      operationId: upload
      requestBody:
        content:
          `+mediaType+`:
            schema: {type: object, properties: {file: `+part+`}}`+enc+`
      responses: {"200": {description: ok}}
components:
  schemas:
    Blob: {type: string, format: binary}
`), opts)
	entries := openapitest.FindOp(t, doc, "upload").Request.Contents[0].Encoding
	require.LessOrEqual(t, len(entries), 1, "one part is at most one entry")
	for _, pe := range entries {
		return pe, true
	}
	return ir.PartEncoding{}, false
}

// TestReadPartFile_ReadsDeclarationsEncodingAndVersion holds a part's Filename
// and marker to its declaration, its encoding entry's media types and the
// version's default media type, in a multipart body: raw bytes, an array of
// them, an untyped part from 3.1 and a part whose encoding names only binary
// types are files, and a part saying it is text is none, as a url-encoded one
// never is beyond what its own bytes say (GitHub #1214).
func TestReadPartFile_ReadsDeclarationsEncodingAndVersion(t *testing.T) {
	t.Parallel()
	const (
		marker    = "binary-media-type"
		multipart = "multipart/form-data"
		form      = "application/x-www-form-urlencoded"
		binary    = "{type: string, format: binary}"
	)
	disabled := openapi.Options{BinaryMedia: openapi.BinaryMedia{Disabled: true}}
	for _, tc := range []struct {
		name, version, mediaType, part, encoding string
		opts                                     openapi.Options
		file, multi                              bool
		inferred                                 ir.Inferred
	}{
		{"an array of binary", "3.1.0", multipart, "{type: array, items: " + binary + "}", "",
			openapi.Options{}, true, true, ""},
		{"an array of binary in 3.0", "3.0.3", multipart, "{type: array, items: " + binary + "}", "",
			openapi.Options{}, true, true, ""},
		{"an array of a $ref to binary", "3.1.0", multipart, "{type: array, items: {$ref: '#/components/schemas/Blob'}}",
			"", openapi.Options{}, true, true, ""},
		{"an array of untyped items", "3.1.0", multipart, "{type: array, items: {}}", "",
			openapi.Options{}, true, true, ""},
		{"an array of untyped items in 3.0", "3.0.3", multipart, "{type: array, items: {}}", "",
			openapi.Options{}, false, true, ""},
		{"an array writing no items", "3.1.0", multipart, "{type: array}", "", openapi.Options{}, false, true, ""},
		{"an array of base64", "3.1.0", multipart, "{type: array, items: {type: string, format: byte}}", "",
			openapi.Options{}, false, true, ""},
		{"an untyped part", "3.1.0", multipart, "{description: the image}", "", openapi.Options{}, true, false, ""},
		{"an untyped part in 3.0", "3.0.3", multipart, "{}", "", openapi.Options{}, false, false, ""},
		{"an untyped part of a binary type", "3.0.3", multipart, "{}", "{contentType: 'image/png, image/jpeg'}",
			openapi.Options{}, true, false, marker},
		{"an untyped part of octet-stream", "3.0.3", multipart, "{}", "{contentType: application/octet-stream}",
			openapi.Options{}, true, false, ""},
		{"a part of a binary type and a text one", "3.1.0", multipart, "{}", "{contentType: 'image/png, text/plain'}",
			openapi.Options{}, false, false, ""},
		{"a JSON part", "3.1.0", multipart, "{}", "{contentType: application/json}", openapi.Options{}, false, false, ""},
		{"a string part of a binary type", "3.1.0", multipart, "{type: string}", "{contentType: image/png}",
			openapi.Options{}, true, false, marker},
		{"a string part", "3.1.0", multipart, "{type: string}", "", openapi.Options{}, false, false, ""},
		{"a string naming its media type", "3.1.0", multipart, "{type: string, contentMediaType: image/png}", "",
			openapi.Options{}, false, false, ""},
		{"a base64 part of a binary type", "3.1.0", multipart, "{type: string, contentEncoding: base64}",
			"{contentType: image/png}", openapi.Options{}, false, false, ""},
		{"a null part", "3.1.0", multipart, "{type: 'null'}", "", openapi.Options{}, false, false, ""},
		{"a true part", "3.1.0", multipart, "true", "", openapi.Options{}, true, false, ""},
		{"a false part", "3.1.0", multipart, "false", "{contentType: image/png}", openapi.Options{}, false, false, ""},
		{"a format binary with no type", "3.0.3", multipart, "{format: binary}", "", openapi.Options{}, true, false, ""},
		{"a union of raw bytes and a string of a binary type", "3.1.0", multipart,
			"{oneOf: [" + binary + ", {type: string}]}", "{contentType: image/png}", openapi.Options{}, true, false, marker},
		{"a union of raw bytes and a string", "3.1.0", multipart, "{oneOf: [" + binary + ", {type: string}]}", "",
			openapi.Options{}, false, false, ""},
		{"a string around an untyped union", "3.1.0", multipart, "{type: string, anyOf: [{}, {maxLength: 9}]}", "",
			openapi.Options{}, false, false, ""},
		{"a binary type with the policy off", "3.1.0", multipart, "{}", "{contentType: image/png}",
			disabled, false, false, ""},
		{"octet-stream with the policy off", "3.1.0", multipart, "{}", "{contentType: application/octet-stream}",
			disabled, true, false, ""},
		{"a form's array of binary", "3.1.0", form, "{type: array, items: " + binary + "}", "",
			openapi.Options{}, false, true, ""},
		{"a form's part of a binary type", "3.1.0", form, "{}", "{contentType: image/png}",
			openapi.Options{}, false, false, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			pe, ok := filePart(t, tc.version, tc.mediaType, tc.part, tc.encoding, tc.opts)
			assert.Equal(t, tc.file || tc.multi || tc.encoding != "", ok, "an entry where the part carries something")
			assert.Equal(t, tc.file, pe.Filename, "filename")
			assert.Equal(t, tc.multi, pe.Multi, "multi")
			assert.Equal(t, tc.inferred, pe.Inferred, "marker")
		})
	}
}

// TestReadPartFile_AMarkerNamesOnlyAGuess holds the marker to a file only the
// binary policy reads: a part the merged type makes raw bytes is declared, in
// either branch order, though another declaration of it leaves its media type
// to say, so no marker rides on it, while a part declared a string alone is
// marked.
func TestReadPartFile_AMarkerNamesOnlyAGuess(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, body string
		inferred   ir.Inferred
	}{
		{"a string and raw bytes", `{allOf: [{type: object, properties: {file: {type: string}}}, ` +
			`{type: object, properties: {file: {type: string, format: binary}}}]}`, ""},
		{"raw bytes and a string", `{allOf: [{type: object, properties: {file: {type: string, format: binary}}}, ` +
			`{type: object, properties: {file: {type: string}}}]}`, ""},
		{"a string alone", `{type: object, properties: {file: {type: string}}}`, "binary-media-type"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			doc, _ := streamingSpec(t, openapitest.PathsSpec(`  /upload:
    post:
      operationId: upload
      requestBody:
        content:
          multipart/form-data:
            schema: `+tc.body+`
            encoding: {file: {contentType: image/png}}
      responses: {"200": {description: ok}}
`), openapi.Options{})
			entries := openapitest.FindOp(t, doc, "upload").Request.Contents[0].Encoding
			require.Len(t, entries, 1)
			for _, pe := range entries {
				assert.True(t, pe.Filename)
				assert.Equal(t, tc.inferred, pe.Inferred)
			}
		})
	}
}
