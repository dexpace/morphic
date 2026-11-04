// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package e2e_test

import (
	"encoding/json/jsontext"
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
// a 3.1 or 3.2 schema is read in the dialect the `$schema` of the schema no
// other holds that holds it names, else its document's jsonSchemaDialect, else
// its minor's own. Draft 4 spells an exclusive bound as a boolean modifying the
// bound beside it, so `minimum: 5, exclusiveMinimum: true` means `x > 5`
// there and is refused as a 3.1 bound elsewhere; a subschema reads its root's,
// and a `$schema` below a root names nothing.
func TestDocumentDialect_ASchemaIsReadInTheDialectItsRootNames(t *testing.T) {
	t.Parallel()
	const boolean = "{type: integer, minimum: 5, exclusiveMinimum: true}"
	const number = "{type: integer, exclusiveMinimum: 5}"
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
			map[jsontext.Pointer][2]ir.BigVal{}, []jsontext.Pointer{"/components/schemas/T"}},
		{"jsonSchemaDialect is the default", "openapi: 3.1.0\ninfo: {title: T, version: '1'}\njsonSchemaDialect: " + draft04 +
			"\npaths: {}\ncomponents:\n  schemas:\n    T: " + boolean +
			"\n    U: {$schema: 'https://json-schema.org/draft/2020-12/schema', type: integer, exclusiveMinimum: 5}\n",
			map[jsontext.Pointer][2]ir.BigVal{"/components/schemas/T": {"", "5"}, "/components/schemas/U": {"", "5"}}, nil},
		{"a $schema below a root names nothing", openapitest.ComponentSpec(
			"    T: {type: object, properties: {a: {$schema: " + draft04 + ", type: integer, minimum: 5, exclusiveMinimum: true}}}\n"),
			map[jsontext.Pointer][2]ir.BigVal{"/components/schemas/T/properties/a": {"5", ""}},
			[]jsontext.Pointer{"/components/schemas/T/properties/a"}},
		{"a 3.0 document names no dialect", openapitest.ComponentSpecVer("3.0.3",
			"    T: {$schema: 'https://json-schema.org/draft/2020-12/schema', type: integer, exclusiveMinimum: 5}\n"),
			map[jsontext.Pointer][2]ir.BigVal{}, []jsontext.Pointer{"/components/schemas/T"}},
		{"beside a $ref", openapitest.ComponentSpec("    B: {type: integer}\n" +
			"    T: {$schema: " + draft04 + ", $ref: '#/components/schemas/B', minimum: 5, exclusiveMinimum: true}\n"),
			map[jsontext.Pointer][2]ir.BigVal{"/components/schemas/T": {"", "5"}}, nil},
		{"across an allOf", openapitest.ComponentSpec("    T: {$schema: " + draft04 +
			", allOf: [{type: integer, minimum: 1, exclusiveMinimum: true}, {type: integer, minimum: 3}]}\n"),
			map[jsontext.Pointer][2]ir.BigVal{"/components/schemas/T": {"3", "1"}}, nil},
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
// dialect the table has no rows for, a later draft or one of the author's,
// reads as: the minor's own, with an openapi/unread-dialect warning at the
// `$schema` or jsonSchemaDialect naming it, and no error.
func TestDocumentDialect_ADialectWithNoRowsIsReadAsTheMinorsOwn(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, "openapi: 3.1.0\ninfo: {title: T, version: '1'}\n"+
		"jsonSchemaDialect: 'https://example.com/dialect'\npaths: {}\ncomponents:\n  schemas:\n"+
		"    T: {$schema: 'http://json-schema.org/draft-07/schema#', type: integer, exclusiveMinimum: 5}\n"+
		"    U: {type: integer, exclusiveMinimum: 6}\n")
	openapitest.RequireNoErrorDiags(t, diags)
	assert.Equal(t, []jsontext.Pointer{"/components/schemas/T/$schema", "/jsonSchemaDialect"},
		openapitest.PointersOf(diags, diag.UnreadDialect))
	openapitest.AssertHasCode(t, diags, diag.UnreadDialect, ir.SeverityWarning)
	assert.Equal(t, map[jsontext.Pointer][2]ir.BigVal{"/components/schemas/T": {"", "5"}, "/components/schemas/U": {"", "6"}},
		lowerBounds(doc))
}
