// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package e2e_test

import (
	"encoding/json/jsontext"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/ir"
)

// draft04 is a `$schema` naming JSON Schema draft 4, whose exclusive bounds are
// booleans modifying the bound beside them.
const draft04 = "'http://json-schema.org/draft-04/schema#'"

// lowerBounds returns the inclusive and exclusive lower bound of each scalar
// and property the source declares that holds one, by its position.
func lowerBounds(doc *ir.Document) map[jsontext.Pointer][2]ir.BigVal {
	out := map[jsontext.Pointer][2]ir.BigVal{}
	add := func(prov ir.Provenance, cons *ir.Constraints) {
		if prov.Source != 0 || cons == nil {
			return
		}
		var b [2]ir.BigVal
		if cons.Min != nil {
			b[0] = *cons.Min
		}
		if cons.ExclusiveMin != nil {
			b[1] = *cons.ExclusiveMin
		}
		if b != [2]ir.BigVal{} {
			out[prov.Pointer] = b
		}
	}
	for _, td := range doc.Types {
		switch n := td.(type) {
		case *ir.Scalar:
			add(n.Provenance, n.Constraints)
		case *ir.Model:
			for _, p := range n.Properties {
				add(p.Provenance, p.Constraints)
			}
		}
	}
	return out
}

// TestDocumentDialect_ASchemaIsReadInTheDialectItsRootNames pins GitHub #1449:
// a 3.1 or 3.2 schema is read in the dialect the `$schema` of the innermost
// resource root holding it names, else its document's jsonSchemaDialect, else
// its minor's own. Draft 4 spells an exclusive bound as a boolean modifying the
// bound beside it, so `minimum: 5, exclusiveMinimum: true` means `x > 5`
// there and is refused as a 3.1 bound elsewhere; a subschema reads its root's,
// a resource an $id embeds names its own (GitHub #1458), and a `$schema` below
// a root and in no such resource names nothing.
func TestDocumentDialect_ASchemaIsReadInTheDialectItsRootNames(t *testing.T) {
	t.Parallel()
	const boolean = "{type: integer, minimum: 5, exclusiveMinimum: true}"
	for _, tc := range []struct {
		name, src string
		bounds    map[jsontext.Pointer][2]ir.BigVal
		refused   []jsontext.Pointer
	}{
		{"the issue's schema", openapitest.ComponentSpec(
			"    T: {$schema: " + draft04 + ", type: integer, minimum: 5, exclusiveMinimum: true}\n"),
			map[jsontext.Pointer][2]ir.BigVal{"/components/schemas/T": {"", "5"}}, nil},
		{"at 3.2, its subschemas alike", openapitest.ComponentSpecVer("3.2.0",
			"    T: {$schema: "+draft04+", type: object, properties: {a: "+boolean+"}}\n"),
			map[jsontext.Pointer][2]ir.BigVal{"/components/schemas/T/properties/a": {"", "5"}}, nil},
		{"draft 4 refuses a 3.1 bound", openapitest.ComponentSpec("    T: {$schema: " + draft04 + ", type: integer, exclusiveMinimum: 5}\n"),
			map[jsontext.Pointer][2]ir.BigVal{}, []jsontext.Pointer{"/components/schemas/T/exclusiveMinimum"}},
		{"jsonSchemaDialect is the default", "openapi: 3.1.0\ninfo: {title: T, version: '1'}\njsonSchemaDialect: " + draft04 +
			"\npaths: {}\ncomponents:\n  schemas:\n    T: " + boolean +
			"\n    U: {$schema: 'https://json-schema.org/draft/2020-12/schema', type: integer, exclusiveMinimum: 5}\n",
			map[jsontext.Pointer][2]ir.BigVal{"/components/schemas/T": {"", "5"}, "/components/schemas/U": {"", "5"}}, nil},
		{"a $schema below a root names nothing", openapitest.ComponentSpec(
			"    T: {type: object, properties: {a: {$schema: " + draft04 + ", type: integer, minimum: 5, exclusiveMinimum: true}}}\n"),
			map[jsontext.Pointer][2]ir.BigVal{"/components/schemas/T/properties/a": {"5", ""}},
			[]jsontext.Pointer{"/components/schemas/T/properties/a/exclusiveMinimum"}},
		{"an $id-embedded resource names its own", openapitest.ComponentSpec(
			"    T: {type: object, properties: {a: {$id: 'https://example.com/a', $schema: " + draft04 +
				", type: integer, minimum: 5, exclusiveMinimum: true}}}\n"),
			map[jsontext.Pointer][2]ir.BigVal{"/components/schemas/T/properties/a": {"", "5"}}, nil},
		{"an embedded resource over its root's", openapitest.ComponentSpec("    T: {$schema: " + draft04 +
			", type: object, properties: {a: {$id: 'https://example.com/a', $schema: 'https://json-schema.org/draft/2020-12/schema'" +
			", type: integer, exclusiveMinimum: 5}, b: " + boolean + "}}\n"),
			map[jsontext.Pointer][2]ir.BigVal{"/components/schemas/T/properties/a": {"", "5"}, "/components/schemas/T/properties/b": {"", "5"}},
			nil},
		{"a 3.0 document names no dialect", openapitest.ComponentSpecVer("3.0.3",
			"    T: {$schema: 'https://json-schema.org/draft/2020-12/schema', type: integer, exclusiveMinimum: 5}\n"),
			map[jsontext.Pointer][2]ir.BigVal{}, []jsontext.Pointer{"/components/schemas/T/exclusiveMinimum"}},
		{"beside a $ref", openapitest.ComponentSpec("    B: {type: integer}\n" +
			"    T: {$schema: " + draft04 + ", $ref: '#/components/schemas/B', minimum: 5, exclusiveMinimum: true}\n"),
			map[jsontext.Pointer][2]ir.BigVal{"/components/schemas/T": {"", "5"}}, nil},
		{"across an allOf", openapitest.ComponentSpec("    T: {$schema: " + draft04 +
			", allOf: [{type: integer, minimum: 1, exclusiveMinimum: true}, {type: integer, minimum: 3}]}\n"),
			map[jsontext.Pointer][2]ir.BigVal{"/components/schemas/T": {"3", "1"}}, nil},
		{"the OAS dialect by an id it is published under", openapitest.ComponentSpecVer("3.2.0",
			"    T: {$schema: 'https://spec.openapis.org/oas/3.2/dialect/2025-09-17', type: integer, exclusiveMinimum: 5}\n"),
			map[jsontext.Pointer][2]ir.BigVal{"/components/schemas/T": {"", "5"}}, nil},
		{"an allOf reads each branch in its own", "openapi: 3.1.0\ninfo: {title: T, version: '1'}\njsonSchemaDialect: " +
			draft04 + "\npaths: {}\ncomponents:\n  schemas:\n    T: {allOf: [{$id: 'https://example.com/a', " +
			"$schema: 'https://json-schema.org/draft/2020-12/schema', type: integer, exclusiveMinimum: 2}, " + boolean + "]}\n",
			map[jsontext.Pointer][2]ir.BigVal{"/components/schemas/T": {"", "5"}}, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			doc, diags := parseFull(t, tc.src)
			assert.Equal(t, tc.refused, openapitest.PointersOf(diags, diag.ExclusiveBoundForm))
			assert.Equal(t, tc.bounds, lowerBounds(doc))
			for _, d := range diags {
				if d.Code != diag.ExclusiveBoundForm {
					assert.NotEqual(t, ir.SeverityError, d.Severity, "%s %s", d.Code, d.Message)
				}
			}
			assert.False(t, openapitest.HasDiag(diags, diag.UnreadDialect))
		})
	}
}

// TestDocumentDialect_AParameterReadsItsSchemasDialect pins the carrier a root
// at a parameter's schema bounds: the parameter takes the bound its schema's
// dialect reads.
func TestDocumentDialect_AParameterReadsItsSchemasDialect(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, openapitest.PathsSpec("  /p:\n    get:\n      parameters:\n"+
		"        - {name: q, in: query, schema: {$schema: "+draft04+", type: integer, minimum: 5, exclusiveMinimum: true}}\n"+
		"      responses: {'200': {description: ok}}\n"))
	openapitest.RequireNoErrorDiags(t, diags)
	q, ok := openapitest.ParamBySource(openapitest.FirstOp(t, doc.Services[0]), "q")
	require.True(t, ok)
	require.NotNil(t, q.Constraints)
	require.NotNil(t, q.Constraints.ExclusiveMin)
	assert.Equal(t, ir.BigVal("5"), *q.Constraints.ExclusiveMin)
	assert.Nil(t, q.Constraints.Min)
}

// TestDocumentDialect_AReferenceReadsWhatItNamesInItsRootsDialect pins that a
// schema a $ref names is read in the dialect of the root holding it, not the
// referrer's, so a subschema of a draft 4 root reads its boolean bound whichever
// declaration lowers first: the two declaration orders compile alike.
func TestDocumentDialect_AReferenceReadsWhatItNamesInItsRootsDialect(t *testing.T) {
	t.Parallel()
	const a = "    A: {type: object, properties: {x: {$ref: '#/components/schemas/T/properties/a'}}}\n"
	const root = "    T: {$schema: " + draft04 + ", type: object, properties: {a: {type: integer, minimum: 1, exclusiveMinimum: true}}}\n"
	first, firstDiags := parseFull(t, openapitest.ComponentSpec(a+root))
	second, secondDiags := parseFull(t, openapitest.ComponentSpec(root+a))
	openapitest.RequireNoErrorDiags(t, firstDiags)
	openapitest.RequireNoErrorDiags(t, secondDiags)
	assert.Equal(t, map[jsontext.Pointer][2]ir.BigVal{"/components/schemas/T/properties/a": {"", "1"}}, lowerBounds(first))
	assert.Empty(t, cmp.Diff(first.Types, second.Types), "the declaration order decides nothing")
}

// TestDocumentDialect_ADocumentOfSchemasNamesItsDialectAtItsRoot pins a schema
// file a reference reaches: its root's `$schema` is the dialect of every schema
// in it, as JSON Schema writes one at a document's root.
func TestDocumentDialect_ADocumentOfSchemasNamesItsDialectAtItsRoot(t *testing.T) {
	t.Parallel()
	doc, diags := compileMinors(t, minorRoot("3.1.0"),
		`{"$schema": "http://json-schema.org/draft-04/schema#", "components": {"schemas": {"S": `+
			`{"type": "integer", "minimum": 5, "exclusiveMinimum": true}}, "responses": {"R": {"description": "ok"}}}}`, openapi.NullableWidening{})
	openapitest.RequireNoErrorDiags(t, diags)
	assert.Equal(t, map[jsontext.Pointer]ir.BigVal{"/components/schemas/S": "5"}, extBounds(doc))
}

// TestDocumentDialect_ADialectWithNoRowsIsReadAsTheMinorsOwn pins what a
// dialect the table has no rows for, an earlier draft or one of the author's,
// reads as: the minor's own, with an openapi/unread-dialect warning at the
// `$schema` or jsonSchemaDialect naming it, and no error.
func TestDocumentDialect_ADialectWithNoRowsIsReadAsTheMinorsOwn(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, "openapi: 3.1.0\ninfo: {title: T, version: '1'}\n"+
		"jsonSchemaDialect: 'https://example.com/dialect'\npaths: {}\ncomponents:\n  schemas:\n"+
		"    T: {$schema: 'http://json-schema.org/draft-03/schema#', type: integer, exclusiveMinimum: 5}\n"+
		"    U: {type: integer, exclusiveMinimum: 6}\n")
	openapitest.RequireNoErrorDiags(t, diags)
	assert.Equal(t, []jsontext.Pointer{"/components/schemas/T/$schema", "/jsonSchemaDialect"},
		openapitest.PointersOf(diags, diag.UnreadDialect))
	openapitest.AssertHasCode(t, diags, diag.UnreadDialect, ir.SeverityWarning)
	assert.Equal(t, map[jsontext.Pointer][2]ir.BigVal{"/components/schemas/T": {"", "5"}, "/components/schemas/U": {"", "6"}},
		lowerBounds(doc))
}

// TestDocumentDialect_AValueNamingNoDialectChoosesNothing pins that a `$schema`
// no absolute URI names, whatever its shape, reads as though the schema wrote
// none: in a document defaulting to draft 4, its boolean bound reads as draft
// 4's, and nothing but validation's finding is reported of it.
func TestDocumentDialect_AValueNamingNoDialectChoosesNothing(t *testing.T) {
	t.Parallel()
	const bound = "type: integer, minimum: 5, exclusiveMinimum: true}\n"
	doc, diags := parseFull(t, "openapi: 3.1.0\ninfo: {title: T, version: '1'}\njsonSchemaDialect: "+draft04+
		"\npaths: {}\ncomponents:\n  schemas:\n    E: {$schema: '', "+bound+"    N: {$schema: 4, "+bound+
		"    R: {$schema: 'rel/x', "+bound+"    M: {$schema: {x: 1}, "+bound)
	assert.Empty(t, openapitest.PointersOf(diags, diag.ExclusiveBoundForm))
	assert.False(t, openapitest.HasDiag(diags, diag.UnreadDialect))
	want := map[jsontext.Pointer][2]ir.BigVal{}
	for _, name := range []string{"E", "N", "R", "M"} {
		want[jsontext.Pointer("/components/schemas/"+name)] = [2]ir.BigVal{"", "5"}
	}
	assert.Equal(t, want, lowerBounds(doc))
}

// TestDocumentDialect_AMalformedDialectIsValidationsFinding pins that a
// `$schema` or jsonSchemaDialect no absolute URI names is reported once, by the
// rule table's validation, and draws no openapi/unread-dialect besides: it
// names no dialect, and its schemas read their minor's own.
func TestDocumentDialect_AMalformedDialectIsValidationsFinding(t *testing.T) {
	t.Parallel()
	_, diags := parseFull(t, "openapi: 3.1.0\ninfo: {title: T, version: '1'}\njsonSchemaDialect: 'rel/dialect'\n"+
		"paths: {}\ncomponents:\n  schemas:\n    N: {$schema: 4, type: integer}\n    R: {$schema: '/rel/1.0.1', type: integer}\n")
	assert.False(t, openapitest.HasDiag(diags, diag.UnreadDialect))
	assert.Equal(t, []jsontext.Pointer{"/components/schemas/N/$schema", "/components/schemas/R/$schema", "/jsonSchemaDialect"},
		openapitest.PointersOf(diags, diag.Validation+"/validation-invalid-format"))
}

// TestDocumentDialect_ASchemaIsReadInItsOwnDocumentsDialect pins that a schema
// reached from another document is read in the dialect its own document's
// jsonSchemaDialect names, whichever document's content the lowering reads:
// draft 7's `dependencies` in other.yaml folds from an inline allOf branch the
// source's G names, and a draft 7 source's `definitions`, as `$defs`, leaves a
// body other.yaml declares to its media type, a file.
func TestDocumentDialect_ASchemaIsReadInItsOwnDocumentsDialect(t *testing.T) {
	t.Parallel()
	const head = "openapi: 3.1.0\ninfo: {title: T, version: '1'}\n"
	compile := func(t *testing.T, root, other string) *ir.Document {
		t.Helper()
		dir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(dir, "other.yaml"), []byte(head+other), 0o600))
		return compileSource(t, filepath.Join(dir, "root.yaml"), head+root)
	}
	t.Run("into a draft's document", func(t *testing.T) {
		t.Parallel()
		doc := compile(t, "paths: {}\ncomponents:\n  schemas:\n    G: {$ref: './other.yaml#/components/schemas/F'}\n",
			"jsonSchemaDialect: "+draft07+"\npaths: {}\ncomponents:\n  schemas:\n"+
				"    F: {type: object, allOf: [{dependencies: {a: [b]}}]}\n")
		f, ok := doc.Types[ir.TypeID("t/openapi@other.yaml/components/schemas/F")].(*ir.Model)
		require.True(t, ok)
		assert.Equal(t, ir.ReasonValidationOnly, unmodeledEntry(t, f.Unmodeled, "openapi:dependencies").Reason)
		assert.NotContains(t, f.Unmodeled, "openapi:allOf/0", "the branch folds")
	})
	t.Run("from another document into a draft's", func(t *testing.T) {
		t.Parallel()
		doc := compile(t, "jsonSchemaDialect: "+draft07+"\npaths:\n  /x: {$ref: './other.yaml#/paths/~1x'}\n"+
			"components:\n  schemas:\n    S: {type: string, definitions: {d: {type: integer}}}\n",
			"paths:\n  /x:\n    post:\n      operationId: postX\n      requestBody:\n        content:\n"+
				"          application/octet-stream: {schema: {$ref: './root.yaml#/components/schemas/S'}}\n"+
				"      responses: {'200': {description: ok}}\n")
		require.Len(t, doc.Services, 1)
		require.Len(t, doc.Services[0].Groups, 1)
		require.Len(t, doc.Services[0].Groups[0].Operations, 1)
		contents := doc.Services[0].Groups[0].Operations[0].Request.Contents
		require.Len(t, contents, 1)
		assert.NotNil(t, contents[0].File, "a string its draft writes nothing else of is a file")
	})
}
