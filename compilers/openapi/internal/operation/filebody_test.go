// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package operation_test

import (
	"fmt"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/compilers/openapi/internal/operation"
	"github.com/dexpace/morphic/ir"
)

// TestBodyFile_ReadsTheSchemaAndTheMediaType holds a body's FileInfo to the
// reading of its schema and its media type, on both sides of an operation: a
// declared file is unmarked, a file the binary policy reads is marked and keeps
// the type its schema lowered to, a union is what all its branches admit, and a
// schema saying the body is anything but raw bytes is no file whatever its
// media type.
func TestBodyFile_ReadsTheSchemaAndTheMediaType(t *testing.T) {
	t.Parallel()
	const marker = "binary-media-type"
	disabled := openapi.Options{BinaryMedia: openapi.BinaryMedia{Disabled: true}}
	acme := openapi.Options{BinaryMedia: openapi.BinaryMedia{MediaTypes: []string{"application/vnd.acme"}}}
	for _, tc := range []struct {
		name      string
		mediaType string
		media     string
		opts      openapi.Options
		inferred  ir.Inferred
		file      bool
		target    ir.TypeID // "" names the position's own node
	}{
		{"a schemaless pdf", "application/pdf", "{}", openapi.Options{}, marker, true, "t/prim/any"},
		{"an empty schema on a pdf", "application/pdf", "{schema: {}}", openapi.Options{}, marker, true, "t/prim/any"},
		{"a string png", "image/png", "{schema: {type: string}}", openapi.Options{}, marker, true, "t/prim/string"},
		{"a documented string png", "image/png", "{schema: {type: string, description: PNG binary.}}",
			openapi.Options{}, marker, true, ""},
		{"a png bounded in octets", "image/png", "{schema: {type: string, maxLength: 9}}",
			openapi.Options{}, marker, true, ""},
		{"a png carrying an extension", "image/png", "{schema: {type: string, x-acme-kind: screenshot}}",
			openapi.Options{}, marker, true, ""},
		{"a wildcard image", "image/*", "{}", openapi.Options{}, marker, true, "t/prim/any"},
		{"a spelling of a binary type", "Image/PNG; q=1", "{}", openapi.Options{}, marker, true, "t/prim/any"},
		{"a format binary with no type", "image/png", "{schema: {format: binary}}", openapi.Options{}, "", true, ""},
		{"a binary string on JSON", "application/json", "{schema: {type: string, format: binary}}",
			openapi.Options{}, "", true, "t/prim/bytes"},
		{"a false schema on octet-stream", "application/octet-stream", "{schema: false}", openapi.Options{}, "", false, ""},
		{"an svg", "image/svg+xml", "{}", openapi.Options{}, "", false, "t/prim/any"},
		{"a schemaless JSON body", "application/json", "{}", openapi.Options{}, "", false, "t/prim/any"},
		{"a text string", "text/plain", "{schema: {type: string}}", openapi.Options{}, "", false, "t/prim/string"},
		{"a base64 png", "image/png", "{schema: {type: string, contentEncoding: base64}}",
			openapi.Options{}, "", false, ""},
		{"a format byte png", "image/png", "{schema: {type: string, format: byte}}", openapi.Options{}, "", false, ""},
		{"an object pdf", "application/pdf", "{schema: {type: object}}", openapi.Options{}, "", false, ""},
		{"a null png", "image/png", "{schema: {type: 'null'}}", openapi.Options{}, "", false, "t/prim/any"},
		{"a union of a string and raw bytes on octet-stream", "application/octet-stream",
			"{schema: {description: A value, anyOf: [{type: string}, {type: string, format: binary}]}}",
			openapi.Options{}, "", true, ""},
		{"raw bytes or null", "image/png", "{schema: {oneOf: [{type: string, format: binary}, {type: 'null'}]}}",
			openapi.Options{}, "", true, ""},
		{"a union of strings on a binary type", "image/png", "{schema: {type: string, anyOf: [{maxLength: 3}, {}]}}",
			openapi.Options{}, marker, true, ""},
		{"a union of a string and raw bytes on JSON", "application/json",
			"{schema: {anyOf: [{type: string}, {format: binary}]}}", openapi.Options{}, "", false, ""},
		{"a union with an object", "image/png", "{schema: {anyOf: [{type: string}, {type: object}]}}",
			openapi.Options{}, "", false, ""},
		{"a union nested in a union", "image/png", "{schema: {anyOf: [{anyOf: [{format: binary}]}]}}",
			openapi.Options{}, "", false, ""},
		{"a union of null alone", "image/png", "{schema: {oneOf: [{type: 'null'}]}}", openapi.Options{}, "", false, ""},
		{"a $ref to a true schema", "application/octet-stream", `{schema: {$ref: "#/components/schemas/Anything"}}`,
			openapi.Options{}, "", true, ""},
		{"a $ref to a false schema", "application/octet-stream", `{schema: {$ref: "#/components/schemas/Nothing"}}`,
			openapi.Options{}, "", false, ""},
		{"a $ref with contentEncoding beside it", "application/octet-stream",
			`{schema: {$ref: "#/components/schemas/Opaque", contentEncoding: base64}}`, openapi.Options{}, "", false, ""},
		{"a $ref with a format beside it", "image/png", `{schema: {$ref: "#/components/schemas/Text", format: uri}}`,
			openapi.Options{}, "", false, ""},
		{"a $ref with a type beside it", "application/octet-stream",
			`{schema: {$ref: "#/components/schemas/Opaque", type: string}}`, openapi.Options{}, "", false, ""},
		{"a $ref with a description beside it", "application/pdf",
			`{schema: {$ref: "#/components/schemas/Text", description: A PDF.}}`, openapi.Options{}, marker, true, ""},
		{"a union branch with contentEncoding beside its $ref", "application/octet-stream",
			`{schema: {anyOf: [{$ref: "#/components/schemas/Opaque", contentEncoding: base64}, {format: binary}]}}`,
			openapi.Options{}, "", false, ""},
		{"a chain whose middle writes contentEncoding", "application/octet-stream",
			`{schema: {$ref: "#/components/schemas/Encoded"}}`, openapi.Options{}, "", false, ""},
		{"the policy off", "application/pdf", "{}", disabled, "", false, "t/prim/any"},
		{"octet-stream with the policy off", "application/octet-stream", "{schema: {}}", disabled, "", true, "t/prim/bytes"},
		{"a caller's type", "application/vnd.acme", "{}", acme, marker, true, "t/prim/any"},
		{"a caller's list replacing the default", "application/pdf", "{}", acme, "", false, "t/prim/any"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			doc, _ := streamingSpec(t, openapitest.PathsSpec(`  /f:
    post:
      operationId: f
      requestBody:
        content:
          "`+tc.mediaType+`": `+tc.media+`
      responses:
        "200":
          description: ok
          content:
            "`+tc.mediaType+`": `+tc.media+`
components:
  schemas:
    Anything: true
    Nothing: false
    Opaque: {description: Says nothing}
    Text: {type: string}
    Encoded: {$ref: "#/components/schemas/Opaque", contentEncoding: base64}
`), tc.opts)
			op := openapitest.FindOp(t, doc, "f")
			for side, content := range map[string]ir.Content{
				"requestBody":   op.Request.Contents[0],
				"responses/200": op.Responses[0].Payload.Contents[0],
			} {
				if !tc.file {
					assert.Nil(t, content.File, side)
				} else {
					require.NotNil(t, content.File, side)
					assert.Equal(t, ir.FileInfo{ContentTypes: []string{tc.mediaType}, Inferred: tc.inferred},
						*content.File, side)
				}
				if tc.target != "" {
					assert.Equal(t, tc.target, content.Type.Target, side)
				}
			}
		})
	}
}

// TestBodyFile_ReadsAChainOfRefsUpToItsBound holds the reading of what a chain of
// $refs writes beside them to its bound: a body at the end of
// operation.MaxFileRefHops $refs to a schema saying nothing on
// application/octet-stream is a file, and one a $ref further is not, as a
// schema leaving its media type to say was no file before the reading.
func TestBodyFile_ReadsAChainOfRefsUpToItsBound(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		refs int
		file bool
	}{
		{operation.MaxFileRefHops, true},
		{operation.MaxFileRefHops + 1, false},
	} {
		t.Run(strconv.Itoa(tc.refs), func(t *testing.T) {
			t.Parallel()
			var components strings.Builder
			for i := 1; i < tc.refs; i++ {
				fmt.Fprintf(&components, "    C%d: {$ref: '#/components/schemas/C%d'}\n", i, i+1)
			}
			fmt.Fprintf(&components, "    C%d: {description: Says nothing}\n", tc.refs)
			doc, _ := streamingSpec(t, openapitest.PathsSpec(`  /f:
    put:
      operationId: f
      requestBody:
        content:
          application/octet-stream: {schema: {$ref: '#/components/schemas/C1'}}
      responses: {"204": {description: ok}}
components:
  schemas:
`+components.String()), openapi.Options{})
			content := openapitest.FindOp(t, doc, "f").Request.Contents[0]
			assert.Equal(t, tc.file, content.File != nil)
		})
	}
}

// TestBodyFile_ReadsKeywordsBesideA30Ref holds the reading beside a $ref to
// every version: a 3.0 document's keywords beside a $ref are read as the
// schema lowering reads them, so a format there keeps a body from being a file
// and a description does not.
func TestBodyFile_ReadsKeywordsBesideA30Ref(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		beside string
		file   bool
	}{
		{"format: byte", false},
		{"description: The bytes.", true},
	} {
		t.Run(tc.beside, func(t *testing.T) {
			t.Parallel()
			doc, _ := streamingSpec(t, openapitest.PathsSpecVer("3.0.3", `  /f:
    put:
      operationId: f
      requestBody:
        content:
          application/octet-stream: {schema: {$ref: '#/components/schemas/Opaque', `+tc.beside+`}}
      responses: {"204": {description: ok}}
components:
  schemas:
    Opaque: {description: Says nothing}
`), openapi.Options{})
			assert.Equal(t, tc.file, openapitest.FindOp(t, doc, "f").Request.Contents[0].File != nil)
		})
	}
}
