// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package e2e_test

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/compile"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/ir"
)

// TestCompile_NamesAHeaderSchemaBeneathEveryEncodingByTheHeader pins that a
// schema under a header of an Encoding Object is named by the header's key,
// whether the Encoding Object is a media type's, by name or by position, or
// one another nests, one level down, under its encoding, prefixEncoding or
// itemEncoding, in a request body and in a response (GitHub #1535). A
// component schema references each header schema's items, so each is a node.
func TestCompile_NamesAHeaderSchemaBeneathEveryEncodingByTheHeader(t *testing.T) {
	t.Parallel()
	const (
		byName     = "#/components/requestBodies/ByName/content/multipart~1form-data"
		byPosition = "#/components/requestBodies/ByPosition/content/multipart~1mixed"
		response   = "#/paths/~1a/post/responses/200/content/multipart~1form-data"
	)
	headers := map[string]string{
		"X-A": byName + "/encoding/f/headers/X-A/schema/items",
		"X-B": byName + "/encoding/f/encoding/g/headers/X-B/schema/items",
		"X-C": byName + "/encoding/h/prefixEncoding/0/headers/X-C/schema/items",
		"X-D": byName + "/encoding/j/itemEncoding/headers/X-D/schema/items",
		"X-E": byPosition + "/prefixEncoding/0/headers/X-E/schema/items",
		"X-F": byPosition + "/prefixEncoding/0/encoding/g/headers/X-F/schema/items",
		"X-G": byPosition + "/itemEncoding/headers/X-G/schema/items",
		"X-H": byPosition + "/itemEncoding/itemEncoding/headers/X-H/schema/items",
		"X-I": response + "/encoding/f/encoding/g/headers/X-I/schema/items",
	}
	doc, diags := parseVerified(t, nineHeadersSpec)
	require.NotNil(t, doc)
	assertNoErrorDiags(t, diags)
	for key, ref := range headers {
		id := ir.TypeID("t/anon/" + ref[len("#/"):])
		def, ok := doc.Types[id]
		require.True(t, ok, "a node at %s", ref)
		assert.Equal(t, compile.SubHint(key, "item"), def.Common().Name.Hint, ref)
	}
}

// nineHeadersSpec writes a header beneath each Encoding Object that test
// names, its schema's items referenced by a component schema.
const nineHeadersSpec = `openapi: 3.2.0
info: {title: t, version: "1"}
paths:
  /a:
    post:
      requestBody: {$ref: '#/components/requestBodies/ByName'}
      responses:
        '200':
          description: ok
          content:
            multipart/form-data:
              schema: {type: object, properties: {f: {type: object, properties: {g: {type: string}}}}}
              encoding:
                f:
                  encoding:
                    g:
                      headers:
                        X-I: {schema: {type: array, items: {type: object, properties: {i: {type: string}}}}}
components:
  schemas:
    RA: {$ref: '#/components/requestBodies/ByName/content/multipart~1form-data/encoding/f/headers/X-A/schema/items'}
    RB: {$ref: '#/components/requestBodies/ByName/content/multipart~1form-data/encoding/f/encoding/g/headers/X-B/schema/items'}
    RC: {$ref: '#/components/requestBodies/ByName/content/multipart~1form-data/encoding/h/prefixEncoding/0/headers/X-C/schema/items'}
    RD: {$ref: '#/components/requestBodies/ByName/content/multipart~1form-data/encoding/j/itemEncoding/headers/X-D/schema/items'}
    RE: {$ref: '#/components/requestBodies/ByPosition/content/multipart~1mixed/prefixEncoding/0/headers/X-E/schema/items'}
    RF: {$ref: '#/components/requestBodies/ByPosition/content/multipart~1mixed/prefixEncoding/0/encoding/g/headers/X-F/schema/items'}
    RG: {$ref: '#/components/requestBodies/ByPosition/content/multipart~1mixed/itemEncoding/headers/X-G/schema/items'}
    RH: {$ref: '#/components/requestBodies/ByPosition/content/multipart~1mixed/itemEncoding/itemEncoding/headers/X-H/schema/items'}
    RI: {$ref: '#/paths/~1a/post/responses/200/content/multipart~1form-data/encoding/f/encoding/g/headers/X-I/schema/items'}
  requestBodies:
    ByName:
      content:
        multipart/form-data:
          schema:
            type: object
            properties:
              f: {type: object, properties: {g: {type: string}}}
              h: {type: array, items: {type: string}}
              j: {type: array, items: {type: string}}
          encoding:
            f:
              contentType: multipart/form-data
              headers:
                X-A: {schema: {type: array, items: {type: object, properties: {a: {type: string}}}}}
              encoding:
                g:
                  headers:
                    X-B: {schema: {type: array, items: {type: object, properties: {b: {type: string}}}}}
            h:
              contentType: multipart/mixed
              prefixEncoding:
                - headers:
                    X-C: {schema: {type: array, items: {type: object, properties: {c: {type: string}}}}}
            j:
              contentType: multipart/mixed
              itemEncoding:
                headers:
                  X-D: {schema: {type: array, items: {type: object, properties: {d: {type: string}}}}}
    ByPosition:
      content:
        multipart/mixed:
          schema: {type: array, items: {type: object, properties: {g: {type: string}}}}
          prefixEncoding:
            - contentType: multipart/form-data
              headers:
                X-E: {schema: {type: array, items: {type: object, properties: {e: {type: string}}}}}
              encoding:
                g:
                  headers:
                    X-F: {schema: {type: array, items: {type: object, properties: {f: {type: string}}}}}
          itemEncoding:
            contentType: multipart/mixed
            headers:
              X-G: {schema: {type: array, items: {type: object, properties: {g: {type: string}}}}}
            itemEncoding:
              headers:
                X-H: {schema: {type: array, items: {type: object, properties: {h: {type: string}}}}}
`

// TestCompile_ReportsAReferenceNamingNothingBeneathANestedEncoding pins GitHub
// #1990: a header, or a header's schema, that references nothing is reported
// beneath each Encoding Object OpenAPI 3.2 nests under another's encoding,
// prefixEncoding and itemEncoding, as it is beneath the one nesting them, at
// each minor alike, and each nested one is kept verbatim with one info.
func TestCompile_ReportsAReferenceNamingNothingBeneathANestedEncoding(t *testing.T) {
	t.Parallel()
	const encoding = "/paths/~1a/post/requestBody/content/multipart~1form-data/encoding"
	headers := []string{
		encoding + "/doc/headers/X-Outer",
		encoding + "/doc/encoding/inner/headers/X-Inner",
		encoding + "/parts/itemEncoding/headers/X-Item",
		encoding + "/parts/prefixEncoding/0/headers/X-Prefix",
	}
	for _, version := range []string{"3.0.3", "3.1.0", "3.2.0"} {
		for _, header := range []struct{ body, below string }{
			{"{$ref: '#/components/headers/Ghost'}", ""},
			{"{schema: {$ref: '#/components/schemas/Ghost'}}", "/schema"},
		} {
			doc, diags := parseVerified(t, fmt.Sprintf(nestedEncodingsSpec, version, header.body))
			require.NotNil(t, doc)
			want := make([]string, 0, len(headers))
			for _, h := range headers {
				want = append(want, h+header.below)
			}
			var got []string
			for _, d := range unresolvedRefs(diags) {
				assert.Equal(t, ir.SeverityError, d.Severity, "%s %s", version, d.Provenance.Pointer)
				got = append(got, string(d.Provenance.Pointer))
			}
			assert.ElementsMatch(t, want, got, "%s, each header %s", version, header.body)
			for _, nested := range []string{"/doc/encoding", "/parts/prefixEncoding", "/parts/itemEncoding"} {
				assert.Equal(t, []ir.Severity{ir.SeverityInfo}, diagsAt(diags, diag.DegradedConstruct, encoding+nested),
					"%s %s", version, nested)
			}
		}
	}
}

// nestedEncodingsSpec is GitHub #1990's document at the version %[1]s, each
// header it writes being %[2]s: one beneath an Encoding Object, and one
// beneath each Encoding Object that one or its sibling nests.
const nestedEncodingsSpec = `openapi: %[1]s
info: {title: t, version: "1"}
paths:
  /a:
    post:
      requestBody:
        content:
          multipart/form-data:
            schema:
              type: object
              properties:
                doc: {type: object, properties: {inner: {type: string}}}
                parts: {type: array, items: {type: string}}
            encoding:
              doc:
                contentType: multipart/form-data
                headers:
                  X-Outer: %[2]s
                encoding:
                  inner:
                    headers:
                      X-Inner: %[2]s
              parts:
                contentType: multipart/mixed
                itemEncoding:
                  headers:
                    X-Item: %[2]s
                prefixEncoding:
                  - headers:
                      X-Prefix: %[2]s
      responses: {'200': {description: ok}}
`
