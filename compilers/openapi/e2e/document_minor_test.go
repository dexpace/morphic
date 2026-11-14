// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package e2e_test

import (
	"encoding/json/jsontext"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/engine/reader"
	"github.com/dexpace/morphic/ir"
)

// minorRoot is a document declaring version whose Holder's property p is a
// $ref to the schema S of ext.yaml, whose one operation responds with the
// response R ext.yaml declares, and which declares B, the empty schema, for
// ext.yaml to reference back.
func minorRoot(version string) string {
	return "openapi: " + version + `
info: {title: Root, version: "1"}
paths:
  /x:
    get:
      responses:
        "200": {$ref: 'ext.yaml#/components/responses/R'}
components:
  schemas:
    Holder:
      type: object
      properties:
        p: {$ref: 'ext.yaml#/components/schemas/S'}
    B: {}
`
}

// minorExt is ext.yaml: head, which declares its version or none, then the
// schemas it declares, and the response R, written as response.
func minorExt(head, schemas, response string) string {
	return head + "components:\n  schemas:\n" + schemas + "  responses:\n    R: " + response + "\n"
}

// declaring is the head of a document declaring version.
func declaring(version string) string {
	return "openapi: " + version + "\ninfo: {title: Ext, version: \"1\"}\npaths: {}\n"
}

// minorResponse is a response every minor admits.
const minorResponse = "{description: ok}"

// compileMinors compiles root beside ext, read from memory, under the
// nullable-widening policy widening.
func compileMinors(t *testing.T, root, ext string, widening openapi.NullableWidening) (*ir.Document, []ir.Diagnostic) {
	t.Helper()
	dir := t.TempDir()
	memory, err := reader.NewMemory([]compilers.Source{{Path: filepath.Join(dir, "ext.yaml"), Data: []byte(ext)}})
	require.NoError(t, err)
	opts := openapi.Options{AllowExternalRefs: true, NullableWidening: widening}
	doc, diags, err := openapi.New().Compile(t.Context(),
		[]compilers.Source{{Path: filepath.Join(dir, "root.yaml"), Data: []byte(root)}},
		compilers.Options{Reader: memory, FormatOptions: opts})
	require.NoError(t, err)
	require.NotNil(t, doc, "%+v", diags)
	require.Len(t, doc.Sources, 2, "the root and ext.yaml")
	return doc, diags
}

// extFindings returns the pointers in ext.yaml at which code is reported.
func extFindings(diags []ir.Diagnostic, code string) []jsontext.Pointer {
	var out []jsontext.Pointer
	for _, d := range diags {
		if d.Code == code && d.Provenance.Source == 1 {
			out = append(out, d.Provenance.Pointer)
		}
	}
	return out
}

// extBounds returns the exclusive lower bound of each scalar ext.yaml declares,
// by its position there.
func extBounds(doc *ir.Document) map[jsontext.Pointer]ir.BigVal {
	out := map[jsontext.Pointer]ir.BigVal{}
	for _, td := range doc.Types {
		s, ok := td.(*ir.Scalar)
		if ok && s.Provenance.Source == 1 && s.Constraints != nil && s.Constraints.ExclusiveMin != nil {
			out[s.Provenance.Pointer] = *s.Constraints.ExclusiveMin
		}
	}
	return out
}

// TestDocumentMinor_EachDocumentIsReadAtTheMinorItDeclares pins GitHub #839:
// a document a reference reaches is read at the minor it declares, its schemas
// in that minor's dialect, its objects held to that minor's table and its
// Document.Sources entry naming it, in either direction between two minors, a
// version of two parts drawing no finding. A document declaring no version, or
// one the compiler does not read, is read at the source's, and the compile
// still stands.
func TestDocumentMinor_EachDocumentIsReadAtTheMinorItDeclares(t *testing.T) {
	t.Parallel()
	const exclusive30 = "    S: {type: integer, minimum: 5, exclusiveMinimum: true}\n"
	const exclusive31 = "    S: {type: integer, exclusiveMinimum: 5}\n"
	onS := map[jsontext.Pointer]ir.BigVal{"/components/schemas/S": "5"}
	none := map[jsontext.Pointer]ir.BigVal{}
	for _, tc := range []struct {
		name, root, ext, format string
		// bounds is each exclusive lower bound ext.yaml's scalars carry, by
		// position, and items where ext.yaml draws a missing-items warning.
		bounds map[jsontext.Pointer]ir.BigVal
		items  []jsontext.Pointer
	}{
		{"a 3.0 root reaching a 3.1 response", minorRoot("3.0.3"),
			minorExt(declaring("3.1.0"), exclusive31,
				"{description: ok, content: {application/json: {schema: {type: integer, exclusiveMinimum: 5}}}}"),
			"openapi@3.1", map[jsontext.Pointer]ir.BigVal{
				"/components/schemas/S": "5", "/components/responses/R/content/application~1json/schema": "5",
			}, nil},
		{"a 3.1 root reaching a 3.0 schema", minorRoot("3.1.0"), minorExt(declaring("3.0.3"), exclusive30, minorResponse),
			"openapi@3.0", onS, nil},
		{"a 3.0 array without items reached from 3.1", minorRoot("3.1.0"),
			minorExt(declaring("3.0.3"), "    S: {type: array}\n", minorResponse), "openapi@3.0", none,
			[]jsontext.Pointer{"/components/schemas/S"}},
		{"a 3.1 array without items reached from 3.0", minorRoot("3.0.3"),
			minorExt(declaring("3.1.0"), "    S: {type: array}\n", minorResponse), "openapi@3.1", none, nil},
		{"a 3.2 response with no description reached from 3.1", minorRoot("3.1.0"),
			minorExt(declaring("3.2.0"), exclusive31, "{content: {application/json: {schema: {type: string}}}}"),
			"openapi@3.2", onS, nil},
		{"a bare file reached from 3.0", minorRoot("3.0.3"), minorExt("", exclusive30, minorResponse), "openapi@3.0",
			onS, nil},
		{"a bare file reached from 3.1", minorRoot("3.1.0"), minorExt("", exclusive31, minorResponse), "openapi@3.1",
			onS, nil},
		{"a version the table does not cover", minorRoot("3.1.0"), minorExt(declaring("3.3.0"), exclusive31, minorResponse),
			"openapi@3.1", onS, nil},
		{"a version of two parts, which is judged only in the source", minorRoot("3.0.3"),
			minorExt(declaring(`"3.1"`), exclusive31, minorResponse), "openapi@3.1", onS, nil},
		{"a Swagger 2.0 file", minorRoot("3.1.0"),
			minorExt("swagger: \"2.0\"\ninfo: {title: Ext, version: \"1\"}\npaths: {}\n", exclusive31, minorResponse),
			"openapi@3.1", onS, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			doc, diags := compileMinors(t, tc.root, tc.ext, openapi.NullableWidening{})
			openapitest.RequireNoErrorDiags(t, diags)
			assert.Equal(t, tc.format, doc.Sources[1].Format)
			assert.Equal(t, tc.bounds, extBounds(doc))
			assert.Equal(t, tc.items, extFindings(diags, diag.MissingArrayItems))
		})
	}
}

// TestDocumentMinor_AnObjectIsHeldToItsDocumentsTable pins the other half of
// validating at a document's minor: a 3.1 response reached from a 3.2 root
// still requires the description 3.2 dropped, and the finding names 3.1.
func TestDocumentMinor_AnObjectIsHeldToItsDocumentsTable(t *testing.T) {
	t.Parallel()
	_, diags := compileMinors(t, minorRoot("3.2.0"),
		minorExt(declaring("3.1.0"), "    S: {type: string}\n", "{content: {application/json: {schema: {type: string}}}}"),
		openapi.NullableWidening{})
	var found []string
	for _, d := range diags {
		if d.Code == diag.Validation+"/validation-required-field" {
			found = append(found, d.Message)
		}
	}
	assert.Equal(t, []string{`Response Object field "description" is REQUIRED in OpenAPI 3.1 (Response Object)`}, found)
}

// TestDocumentMinor_ANullIsReadInTheDialectOfItsDocument pins nullable across a
// reference into a document of another minor, with the policy off: a 3.0
// schema's nullable adds null to its type and a 3.1 one's says nothing, each
// alias along a chain is read in its own document's dialect, whichever its end
// is in, a $ref site's own keywords in its document's once its chain is read,
// and the keys beside a 3.0 alias's $ref are later constructs of that
// document.
func TestDocumentMinor_ANullIsReadInTheDialectOfItsDocument(t *testing.T) {
	t.Parallel()
	const alias = "    S: {$ref: '#/components/schemas/B', type: string, nullable: true}\n    B: {}\n"
	const back = "    S: {$ref: 'root.yaml#/components/schemas/B', type: string, nullable: true}\n"
	besideRef := strings.Replace(minorRoot("3.1.0"), "p: {$ref: 'ext.yaml#/components/schemas/S'}",
		"p: {$ref: 'ext.yaml#/components/schemas/S', type: string, nullable: true}", 1)
	for _, tc := range []struct {
		name, root, ext string
		nullable        bool
		unknown, later  []jsontext.Pointer
	}{
		{"a 3.0 schema reached from 3.1", minorRoot("3.1.0"),
			minorExt(declaring("3.0.3"), "    S: {type: string, nullable: true}\n", minorResponse), true, nil, nil},
		{"a 3.0 alias reached from 3.1", minorRoot("3.1.0"), minorExt(declaring("3.0.3"), alias, minorResponse), true, nil,
			[]jsontext.Pointer{"/components/schemas/S/type", "/components/schemas/S/nullable"}},
		{"a 3.1 alias reached from 3.0", minorRoot("3.0.3"), minorExt(declaring("3.1.0"), alias, minorResponse), false,
			[]jsontext.Pointer{"/components/schemas/S/nullable"}, nil},
		{"a 3.0 alias over an end in 3.1", minorRoot("3.1.0"), minorExt(declaring("3.0.3"), back, minorResponse), true,
			nil, []jsontext.Pointer{"/components/schemas/S/type", "/components/schemas/S/nullable"}},
		{"a 3.1 alias over an end in 3.0", minorRoot("3.0.3"), minorExt(declaring("3.1.0"), back, minorResponse), false,
			[]jsontext.Pointer{"/components/schemas/S/nullable"}, nil},
		{"a 3.1 $ref site over a 3.0 schema", besideRef, minorExt(declaring("3.0.3"), "    S: {}\n", minorResponse),
			false, nil, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			doc, diags := compileMinors(t, tc.root, tc.ext, openapi.NullableWidening{Disabled: true})
			openapitest.RequireNoErrorDiags(t, diags)
			p := holderProps(t, doc)["p"].Type
			assert.Equal(t, tc.nullable, p.Nullable)
			assert.Empty(t, p.Inferred)
			assert.Equal(t, tc.unknown, extFindings(diags, diag.UnknownSchemaKeyword))
			assert.Equal(t, tc.later, extFindings(diags, diag.LaterVersionConstruct))
		})
	}
}

// TestDocumentMinor_AdmissionReadsEachSchemaInItsDialect pins what a
// refinement admits across a reference: a 3.1 alias's nullable admits no
// null, so no conjunct narrows the null default its target lends, which the
// position takes as it takes the alias written without it.
func TestDocumentMinor_AdmissionReadsEachSchemaInItsDialect(t *testing.T) {
	t.Parallel()
	root := `openapi: 3.0.3
info: {title: Root, version: "1"}
paths: {}
components:
  schemas:
    H:
      allOf:
        - {$ref: 'ext.yaml#/components/schemas/S'}
        - {type: string}
    M:
      type: object
      properties:
        p: {$ref: '#/components/schemas/H'}
`
	defaultOf := func(alias string) *ir.Value {
		ext := minorExt(declaring("3.1.0"), alias+"    U: {default: null}\n", minorResponse)
		doc, diags := compileMinors(t, root, ext, openapi.NullableWidening{})
		openapitest.RequireNoErrorDiags(t, diags)
		m, ok := doc.Types[componentID("M")].(*ir.Model)
		require.True(t, ok)
		p, ok := propByWire(m, "p")
		require.True(t, ok)
		return p.Default
	}
	plain := defaultOf("    S: {$ref: '#/components/schemas/U', type: string}\n")
	require.NotNil(t, plain, "no conjunct narrows the alias written plainly")
	assert.Equal(t, plain, defaultOf("    S: {$ref: '#/components/schemas/U', type: string, nullable: true}\n"))
}

// TestDocumentMinor_DocumentsOfTwoMinorsAreEachReadAtTheirOwn pins that two
// documents of different minors reached from one root share nothing of how
// they are read: each bound reads in its own document's dialect, and each
// Document.Sources entry names its own minor.
func TestDocumentMinor_DocumentsOfTwoMinorsAreEachReadAtTheirOwn(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	memory, err := reader.NewMemory([]compilers.Source{
		{Path: filepath.Join(dir, "a.yaml"), Data: []byte(minorExt(declaring("3.0.3"),
			"    S: {type: integer, minimum: 5, exclusiveMinimum: true}\n", minorResponse))},
		{Path: filepath.Join(dir, "b.yaml"), Data: []byte(minorExt(declaring("3.1.0"),
			"    S: {type: integer, exclusiveMinimum: 6}\n", minorResponse))},
	})
	require.NoError(t, err)
	root := `openapi: 3.2.0
info: {title: Root, version: "1"}
paths: {}
components:
  schemas:
    Holder:
      type: object
      properties:
        a: {$ref: 'a.yaml#/components/schemas/S'}
        b: {$ref: 'b.yaml#/components/schemas/S'}
`
	doc, diags, err := openapi.New().Compile(t.Context(),
		[]compilers.Source{{Path: filepath.Join(dir, "root.yaml"), Data: []byte(root)}},
		compilers.Options{Reader: memory, FormatOptions: openapi.Options{AllowExternalRefs: true}})
	require.NoError(t, err)
	require.NotNil(t, doc)
	openapitest.RequireNoErrorDiags(t, diags)
	formats := make([]string, 0, len(doc.Sources))
	for _, src := range doc.Sources {
		formats = append(formats, src.Format)
	}
	assert.Equal(t, []string{"openapi@3.2", "openapi@3.0", "openapi@3.1"}, formats)
	bounds := map[string]ir.BigVal{}
	for _, td := range doc.Types {
		if s, ok := td.(*ir.Scalar); ok && s.Constraints != nil && s.Constraints.ExclusiveMin != nil {
			bounds[doc.Sources[s.Provenance.Source].Path] = *s.Constraints.ExclusiveMin
		}
	}
	assert.Equal(t, map[string]ir.BigVal{filepath.Join(dir, "a.yaml"): "5", filepath.Join(dir, "b.yaml"): "6"}, bounds)
}

// TestDocumentMinor_AnEncodingDefaultsAtItsDocumentsMinor pins that a multipart
// part's default media type is read at the minor of the document writing its
// request body, whichever minor the root declares: a part writing no type is a
// file where 3.1 defaults its contentType to application/octet-stream, and no
// file in a 3.0 document, whose defaults state none for it.
func TestDocumentMinor_AnEncodingDefaultsAtItsDocumentsMinor(t *testing.T) {
	t.Parallel()
	for root, ext := range map[string]string{"3.0.3": "3.1.0", "3.1.0": "3.0.3"} {
		t.Run(ext+" from "+root, func(t *testing.T) {
			t.Parallel()
			doc, diags := compileMinors(t, "openapi: "+root+`
info: {title: Root, version: "1"}
paths:
  /u:
    post:
      operationId: upload
      requestBody: {$ref: 'ext.yaml#/components/requestBodies/B'}
      responses: {"204": {description: ok}}
`, declaring(ext)+`components:
  requestBodies:
    B:
      content:
        multipart/form-data:
          schema: {type: object, properties: {blob: {}}}
`, openapi.NullableWidening{})
			openapitest.RequireNoErrorDiags(t, diags)
			upload, ok := opByName(doc, "upload")
			require.True(t, ok)
			var files []bool
			for _, pe := range requestContent(t, upload).Encoding {
				files = append(files, pe.Filename)
			}
			if ext == "3.1.0" {
				assert.Equal(t, []bool{true}, files, "the 3.1 document's untyped part is a file")
			} else {
				assert.Empty(t, files, "the 3.0 document's untyped part is none")
			}
		})
	}
}
