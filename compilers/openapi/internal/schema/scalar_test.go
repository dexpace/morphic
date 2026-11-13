// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package schema_test

import (
	"encoding/json/jsontext"
	"fmt"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/compilers/openapi/internal/schema"
	"github.com/dexpace/morphic/ir"
)

func TestScalar_UnknownFormatPerBaseType(t *testing.T) {
	t.Parallel()
	spec := openapitest.ComponentSpec(`    S:
      type: object
      properties:
        i: {type: integer, format: weird}
        n: {type: number, format: weird}
        b: {type: boolean, format: weird}
        s: {type: string, format: weird}
`)
	doc, diags := schema.LowerSpec(t, spec)
	openapitest.RequireNoErrorDiags(t, diags)
	m := typeByName(doc, "S").(*ir.Model)
	bases := map[string]ir.PrimKind{}
	for _, p := range m.Properties {
		sc, ok := doc.Types[p.Type.Target].(*ir.Scalar)
		require.True(t, ok, "prop %s hoisted as scalar", p.WireName)
		require.NotNil(t, sc.Base)
		bases[p.WireName] = doc.Types[sc.Base.Target].(*ir.Primitive).Prim
		assert.Equal(t, "weird", sc.Encoding.Name)
	}
	assert.Equal(t, ir.PrimInteger, bases["i"])
	assert.Equal(t, ir.PrimNumber, bases["n"])
	assert.Equal(t, ir.PrimBool, bases["b"])
	assert.Equal(t, ir.PrimString, bases["s"])
}

// TestScalar_NumericFormatPairings pins which numeric (type, format) pairings
// select a shared primitive and which hoist a Scalar that keeps the format. An
// integer width selects its primitive under either numeric type. A float or
// decimal format on an integer admits only integral values, which no primitive
// narrower than integer holds, so it hoists as an unknown format does.
func TestScalar_NumericFormatPairings(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		typ, format string
		prim        ir.PrimKind // the primitive the pairing selects; "" = it hoists
		base        ir.PrimKind // the hoisted Scalar's base
	}{
		{typ: "integer", format: "int8", prim: ir.PrimInt8},
		{typ: "integer", format: "int16", prim: ir.PrimInt16},
		{typ: "integer", format: "int32", prim: ir.PrimInt32},
		{typ: "integer", format: "int64", prim: ir.PrimInt64},
		{typ: "integer", format: "uint8", prim: ir.PrimUint8},
		{typ: "integer", format: "uint16", prim: ir.PrimUint16},
		{typ: "integer", format: "uint32", prim: ir.PrimUint32},
		{typ: "integer", format: "uint64", prim: ir.PrimUint64},
		{typ: "number", format: "int8", prim: ir.PrimInt8},
		{typ: "number", format: "int16", prim: ir.PrimInt16},
		{typ: "number", format: "int32", prim: ir.PrimInt32},
		{typ: "number", format: "int64", prim: ir.PrimInt64},
		{typ: "number", format: "uint8", prim: ir.PrimUint8},
		{typ: "number", format: "uint16", prim: ir.PrimUint16},
		{typ: "number", format: "uint32", prim: ir.PrimUint32},
		{typ: "number", format: "uint64", prim: ir.PrimUint64},
		{typ: "number", format: "float", prim: ir.PrimFloat32},
		{typ: "number", format: "double", prim: ir.PrimFloat64},
		{typ: "number", format: "decimal", prim: ir.PrimDecimal},
		{typ: "number", format: "decimal128", prim: ir.PrimDecimal128},

		// A pairing no primitive holds hoists over its type's primitive.
		{typ: "integer", format: "float", base: ir.PrimInteger},
		{typ: "integer", format: "double", base: ir.PrimInteger},
		{typ: "integer", format: "decimal", base: ir.PrimInteger},
		{typ: "integer", format: "decimal128", base: ir.PrimInteger},
		{typ: "integer", format: "Int8", base: ir.PrimInteger},     // a format name is case-sensitive
		{typ: "number", format: "double-int", base: ir.PrimNumber}, // a registry format with no primitive
		{typ: "number", format: "float16", base: ir.PrimNumber},    // a width no PrimKind names
		{typ: "string", format: "double-int", base: ir.PrimString}, // no number's format the table pairs
	} {
		t.Run(tc.typ+"/"+tc.format, func(t *testing.T) {
			t.Parallel()
			doc, diags := schema.LowerSpec(t, openapitest.ComponentSpec(fmt.Sprintf(
				"    H:\n      type: object\n      properties:\n        p: {type: %s, format: %s}\n",
				tc.typ, tc.format)))
			assert.Empty(t, diags, "the format is homed either way, so nothing is reported")
			h, ok := typeByName(doc, "H").(*ir.Model)
			require.True(t, ok, "H lowers to a model")
			require.Len(t, h.Properties, 1)
			assert.Empty(t, h.Properties[0].Unmodeled, "the format is homed either way, so nothing is kept")
			target := h.Properties[0].Type.Target

			if tc.prim != "" {
				assert.Equal(t, ir.PrimTypeID(tc.prim), target, "the position resolves to the shared primitive")
				prim, ok := doc.Types[target].(*ir.Primitive)
				require.True(t, ok, "the primitive is interned")
				assert.Equal(t, tc.prim, prim.Prim)
				for id, td := range doc.Types {
					assert.NotEqual(t, ir.KindScalar, td.Kind(), "%s: no node is hoisted for the position", id)
				}
				return
			}
			sc, ok := doc.Types[target].(*ir.Scalar)
			require.True(t, ok, "the position hoists a Scalar")
			require.NotNil(t, sc.Base)
			assert.Equal(t, ir.PrimTypeID(tc.base), sc.Base.Target)
			require.NotNil(t, sc.Encoding)
			assert.Equal(t, tc.format, sc.Encoding.Name, "the format is kept verbatim")
		})
	}
}

// TestScalar_ANumberAsTextIsItsNumberOnAString pins a numeric format written on
// a `type: string`: the number it selects under `number` is the Scalar's Base,
// the logical type, its Encoding names a number written as its text, and the
// string it travels as is the WireType, as byte's bytes and string are
// (docs/compilers/openapi.md#string-formats). A contentEncoding naming another
// encoding says the string is that encoding's text, so the format selects
// nothing and is kept, and the Scalar is over the string with no WireType; a
// media type or a schema of the content beside it leaves the format lowered.
func TestScalar_ANumberAsTextIsItsNumberOnAString(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		format, extra string
		base          ir.PrimKind
		name          string
		kept          bool
		wire          bool
	}{
		{format: "int8", base: ir.PrimInt8, name: "string", wire: true},
		{format: "int16", base: ir.PrimInt16, name: "string", wire: true},
		{format: "int32", base: ir.PrimInt32, name: "string", wire: true},
		{format: "int64", base: ir.PrimInt64, name: "string", wire: true},
		{format: "uint8", base: ir.PrimUint8, name: "string", wire: true},
		{format: "uint16", base: ir.PrimUint16, name: "string", wire: true},
		{format: "uint32", base: ir.PrimUint32, name: "string", wire: true},
		{format: "uint64", base: ir.PrimUint64, name: "string", wire: true},
		{format: "float", base: ir.PrimFloat32, name: "string", wire: true},
		{format: "double", base: ir.PrimFloat64, name: "string", wire: true},
		{format: "decimal", base: ir.PrimDecimal, name: "string", wire: true},
		{format: "decimal128", base: ir.PrimDecimal128, name: "string", wire: true},
		{format: "int64", extra: ", contentEncoding: base64", base: ir.PrimString, name: "base64", kept: true},
		{format: "double", extra: ", contentEncoding: base32, contentMediaType: text/plain", base: ir.PrimString,
			name: "base32", kept: true},
		{format: "int64", extra: ", contentEncoding: string", base: ir.PrimInt64, name: "string", wire: true},
		{format: "int64", extra: ", contentMediaType: text/plain", base: ir.PrimInt64, name: "string", wire: true},
		{format: "int64", extra: ", contentSchema: {type: integer}", base: ir.PrimInt64, name: "string", wire: true},
	} {
		t.Run(tc.format+tc.extra, func(t *testing.T) {
			t.Parallel()
			doc, _ := schema.LowerSpec(t, openapitest.ComponentSpec(fmt.Sprintf(
				"    H:\n      type: object\n      properties:\n        p: {type: string, format: %s%s}\n", tc.format, tc.extra)))
			h, ok := typeByName(doc, "H").(*ir.Model)
			require.True(t, ok)
			sc, ok := doc.Types[h.Properties[0].Type.Target].(*ir.Scalar)
			require.True(t, ok, "the position hoists a Scalar")
			require.NotNil(t, sc.Base)
			assert.Equal(t, ir.PrimTypeID(tc.base), sc.Base.Target, "the base is the number the format selects, or the string an encoding overrides it with")
			require.NotNil(t, sc.Encoding)
			assert.Equal(t, tc.name, sc.Encoding.Name)
			if tc.wire {
				require.NotNil(t, sc.Encoding.WireType, "the number travels as a string")
				assert.Equal(t, ir.PrimTypeID(ir.PrimString), sc.Encoding.WireType.Target)
			} else {
				assert.Nil(t, sc.Encoding.WireType, "the string is the encoded text already")
			}
			_, kept := sc.Unmodeled["openapi:format"]
			assert.Equal(t, tc.kept, kept)
		})
	}
}

// TestScalar_ANumberAsTextKeepsItsBoundsVerbatim pins the value bounds beside a
// number carried as text. Its Constraints speak of the number, and none of them
// does: a length or a pattern bounds the text, and a numeric bound no string
// instance. Each is kept verbatim on the Scalar under no_ir_home, with an info
// at it, and neither the Scalar nor a property or parameter carrying it holds a
// bound. A format the table pairs with string keeps its bounds, and an alias an
// allOf refinement lowers to loses none of its own.
func TestScalar_ANumberAsTextKeepsItsBoundsVerbatim(t *testing.T) {
	t.Parallel()
	const bounds = `pattern: "^[0-9]+$", minLength: 1, maxLength: 20, minimum: 5, exclusiveMaximum: 100, multipleOf: 5`
	keywords := []string{"exclusiveMaximum", "maxLength", "minLength", "minimum", "multipleOf", "pattern"}
	doc, diags := parseFull(t, `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /n:
    get:
      parameters:
        - {name: q, in: query, schema: {type: string, format: int64, `+bounds+`}}
      responses: {"204": {description: ok}}
components:
  schemas:
    N: {type: string, format: decimal, `+bounds+`}
    R: {allOf: [{$ref: '#/components/schemas/N'}], type: string, format: int64, minimum: 5}
    H:
      type: object
      properties:
        p: {type: string, format: int64, `+bounds+`}
        d: {type: string, format: date, maxLength: 10}
`)
	openapitest.RequireNoErrorDiags(t, diags)
	h, ok := typeByName(doc, "H").(*ir.Model)
	require.True(t, ok)
	props := openapitest.PropsByWire(h.Properties)
	params := doc.Services[0].Groups[0].Operations[0].Params
	require.Len(t, params, 1)
	for at, carrier := range map[jsontext.Pointer]*ir.Constraints{
		"/components/schemas/N":              nil,
		"/components/schemas/H/properties/p": props["p"].Constraints,
		"/paths/~1n/get/parameters/0/schema": params[0].Constraints,
	} {
		assert.Nil(t, carrier, "%s: what carries the number holds no bound", at)
		sc, ok := nodeAt(doc, at).(*ir.Scalar)
		require.True(t, ok, "%s hoists a Scalar", at)
		assert.Nil(t, sc.Constraints, "%s: the number's Constraints hold no bound of its text", at)
		for _, keyword := range keywords {
			entry, kept := sc.Unmodeled["openapi:"+keyword]
			if assert.True(t, kept, "%s: %s is kept", at, keyword) {
				assert.Equal(t, ir.ReasonNoIRHome, entry.Reason)
				assert.Equal(t, at+jsontext.Pointer("/"+keyword), entry.Provenance.Pointer)
			}
			assert.True(t, openapitest.HasDiagCodeAt(diags, diag.DegradedConstruct, string(at)+"/"+keyword),
				"%s: keeping %s is reported at it", at, keyword)
		}
		assert.Len(t, sc.Unmodeled, len(keywords), "%s keeps nothing else", at)
	}
	require.NotNil(t, props["d"].Constraints, "a date keeps the length bound of its text")
	assert.Equal(t, new(int64(10)), props["d"].Constraints.MaxLength)

	r, ok := typeByName(doc, "R").(*ir.Scalar)
	require.True(t, ok, "the refinement owns an alias")
	_, kept := r.Unmodeled["openapi:minimum"]
	assert.True(t, kept || (r.Constraints != nil && r.Constraints.Min != nil), "the alias holds its bound somewhere")
}

// nodeAt returns the type node lowered at the source pointer at.
func nodeAt(doc *ir.Document, at jsontext.Pointer) ir.TypeDef {
	for _, td := range doc.Types {
		if td.Common().Provenance.Pointer == at {
			return td
		}
	}
	return nil
}

// TestContentVocabulary_LowersToEncoding pins where the 2020-12 content
// vocabulary lands: contentEncoding on Encoding.Name, contentMediaType on
// Encoding.MediaType and contentSchema on Encoding.Schema, on the Scalar node
// the position hoists rather than on the shared primitive every other
// declaration of that type also resolves to.
func TestContentVocabulary_LowersToEncoding(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name, schemas string
		at            ir.TypeID
		want          ir.Encoding
	}{
		{
			name:    "component",
			schemas: "    A: {type: string, contentEncoding: base64, contentMediaType: image/png}\n",
			at:      componentID("A"),
			want:    ir.Encoding{Name: "base64", MediaType: "image/png"},
		},
		{
			name:    "property",
			schemas: "    A: {type: object, properties: {p: {type: string, contentMediaType: text/csv}}}\n",
			at:      "t/anon/components/schemas/A/properties/p",
			want:    ir.Encoding{MediaType: "text/csv"},
		},
		{
			name:    "items",
			schemas: "    A: {type: array, items: {type: string, contentEncoding: base32}}\n",
			at:      "t/anon/components/schemas/A/items",
			want:    ir.Encoding{Name: "base32"},
		},
		{
			name:    "beside a known format",
			schemas: "    A: {type: string, format: date-time, contentMediaType: text/plain}\n",
			at:      componentID("A"),
			want:    ir.Encoding{MediaType: "text/plain"},
		},
		{
			name:    "format byte agrees with contentEncoding",
			schemas: "    A: {type: string, format: byte, contentEncoding: base64}\n",
			at:      componentID("A"),
			want:    ir.Encoding{Name: "base64", WireType: &ir.TypeRef{Target: "t/prim/string"}},
		},
		{
			name:    "contentSchema beside contentMediaType",
			schemas: "    A: {type: string, contentMediaType: application/json, contentSchema: {type: object, properties: {id: {type: string}}}}\n",
			at:      componentID("A"),
			want: ir.Encoding{
				MediaType: "application/json",
				Schema:    &ir.TypeRef{Target: "t/anon/components/schemas/A/contentSchema"},
			},
		},
		{
			name:    "contentSchema alone still hoists the scalar that holds it",
			schemas: "    A: {type: array, items: {type: string, contentSchema: {type: object}}}\n",
			at:      "t/anon/components/schemas/A/items",
			want: ir.Encoding{
				Schema: &ir.TypeRef{Target: "t/anon/components/schemas/A/items/contentSchema"},
			},
		},
		{
			name: "contentSchema naming a component resolves to it",
			schemas: "    A: {type: string, contentSchema: {$ref: '#/components/schemas/B'}}\n" +
				"    B: {type: object, properties: {id: {type: string}}}\n",
			at:   componentID("A"),
			want: ir.Encoding{Schema: &ir.TypeRef{Target: componentID("B")}},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			doc, diags := parseFull(t, openapitest.ComponentSpec(tc.schemas))
			openapitest.RequireNoErrorDiags(t, diags)

			sc, ok := doc.Types[tc.at].(*ir.Scalar)
			require.True(t, ok, "the content vocabulary needs a Scalar of its own at %s", tc.at)
			require.NotNil(t, sc.Encoding)
			assert.Empty(t, cmp.Diff(tc.want, *sc.Encoding))
			assert.Empty(t, sc.Unmodeled, "a keyword with a field is lowered, never also kept raw")
			if tc.want.Schema != nil {
				assert.Contains(t, doc.Types, tc.want.Schema.Target,
					"contentSchema reaches the registry as a type, not a raw blob")
			}
		})
	}
}

// TestContentVocabulary_KeepsTheBoundsWrittenBesideIt pins that hoisting a node
// for the content vocabulary does not cost the position its value constraints.
// Owning a node is what stops hoistDeclarationHome hoisting the alias that
// carries them, so the node the content vocabulary lands on has to carry them
// itself — otherwise adding contentEncoding to a bounded string deletes its
// bounds, which is a lossy lowering nothing reports (invariant 2).
func TestContentVocabulary_KeepsTheBoundsWrittenBesideIt(t *testing.T) {
	t.Parallel()
	five, nine := int64(5), int64(9)
	cases := []struct {
		name, schemas string
		at            ir.TypeID
		want          ir.Constraints
	}{
		{
			name:    "a component",
			schemas: "    A: {type: string, contentEncoding: base64, minLength: 5, maxLength: 9, pattern: '^[a-z]+$'}\n",
			at:      componentID("A"),
			want:    ir.Constraints{MinLength: &five, MaxLength: &nine, Pattern: "^[a-z]+$"},
		},
		{
			name:    "an inline position",
			schemas: "    A: {type: array, items: {type: string, contentMediaType: text/csv, minLength: 5}}\n",
			at:      "t/anon/components/schemas/A/items",
			want:    ir.Constraints{MinLength: &five},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			doc, diags := parseFull(t, openapitest.ComponentSpec(tc.schemas))
			openapitest.RequireNoErrorDiags(t, diags)

			sc, ok := doc.Types[tc.at].(*ir.Scalar)
			require.True(t, ok, "the content vocabulary hoists a Scalar at %s", tc.at)
			require.NotNil(t, sc.Encoding, "and lowers the keyword onto it")
			require.NotNil(t, sc.Constraints, "while keeping the bounds written beside it")
			assert.Empty(t, cmp.Diff(tc.want, *sc.Constraints))
		})
	}
}

// TestHoistedScalar_ReportsABoundItCannotConvert pins that every way a scalar
// hoists a node of its own (format: byte, a format the IR does not place, the
// content vocabulary) has a numeric bound that is no decimal reported, at the
// keyword that wrote it, and keeps no constraints in its place.
func TestHoistedScalar_ReportsABoundItCannotConvert(t *testing.T) {
	t.Parallel()
	cases := []struct{ name, schema string }{
		{"format byte", "{type: string, format: byte, minimum: .inf}"},
		{"an unplaced format", "{type: number, format: custom, minimum: .inf}"},
		{"the content vocabulary", "{type: string, contentEncoding: base64, minimum: .inf}"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			doc, diags := parseFull(t, openapitest.ComponentSpec("    A: "+tc.schema+"\n"))
			sc, ok := doc.Types[componentID("A")].(*ir.Scalar)
			require.True(t, ok, "the position hoists a Scalar of its own")
			require.NotNil(t, sc.Encoding, "carrying what it wrote")
			assert.True(t, openapitest.HasDiagCodeAt(diags, diag.NumericPrecision, "/components/schemas/A/minimum"))
			assert.Nil(t, sc.Constraints, "and no bound it could not read")
		})
	}
}

// TestContentVocabulary_KeptWhereNoEncodingHolds covers the other half of
// TestContentVocabulary_LowersToEncoding: a position that lowered to a shape
// with no Encoding field to fill keeps every content keyword verbatim,
// contentSchema included — the three share one home, so they are kept or
// lowered together — and a format that loses Encoding.Name to contentEncoding
// is kept beside it.
func TestContentVocabulary_KeptWhereNoEncodingHolds(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name, schemas, key, wantJSON string
		at                           ir.TypeID
	}{
		{
			name:    "array position has no Encoding",
			schemas: "    A: {type: array, items: {type: string}, contentMediaType: application/zip}\n",
			key:     "openapi:contentMediaType", wantJSON: `"application/zip"`, at: componentID("A"),
		},
		{
			name:    "object position has no Encoding for contentSchema either",
			schemas: "    A: {type: object, properties: {p: {type: string}}, contentSchema: {type: object}}\n",
			key:     "openapi:contentSchema", wantJSON: `{"type":"object"}`, at: componentID("A"),
		},
		{
			name:    "format loses Encoding.Name to contentEncoding",
			schemas: "    A: {type: string, format: iban, contentEncoding: base64}\n",
			key:     "openapi:format", wantJSON: `"iban"`, at: componentID("A"),
		},
		{
			name:    "an integer takes no content vocabulary",
			schemas: "    A: {type: integer, contentEncoding: base64}\n",
			key:     "openapi:contentEncoding", wantJSON: `"base64"`, at: componentID("A"),
		},
		{
			name:    "an integer's format keeps Encoding.Name",
			schemas: "    A: {type: integer, format: x-mine, contentMediaType: application/zip}\n",
			key:     "openapi:contentMediaType", wantJSON: `"application/zip"`, at: componentID("A"),
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			doc, diags := parseFull(t, openapitest.ComponentSpec(tc.schemas))
			openapitest.RequireNoErrorDiags(t, diags)

			td, ok := doc.Types[tc.at]
			require.True(t, ok)
			entry, ok := td.Common().Unmodeled[tc.key]
			require.True(t, ok, "%s must be kept verbatim under Unmodeled", tc.key)
			assert.JSONEq(t, tc.wantJSON, string(entry.Value))
			assert.Equal(t, ir.ReasonNoIRHome, entry.Reason)
			openapitest.AssertInfoDiagAt(t, diags, string(entry.Provenance.Pointer))
		})
	}
}

// TestContentVocabulary_ANumberTakesNone pins the content vocabulary beside a
// type it does not apply to, at every site: a number's Scalar hoisted for its
// format keeps the format as its encoding name and reads no content keyword,
// which is kept verbatim with one info, as one beside a bare number is.
func TestContentVocabulary_ANumberTakesNone(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, atEverySite("3.1.0", "{type: number, format: x-mine, contentEncoding: base64}"))
	openapitest.RequireNoErrorDiags(t, diags)
	for _, site := range sitesOf(t, doc) {
		sc, ok := site.node.(*ir.Scalar)
		require.True(t, ok, "%s: got %T", site.pointer, site.node)
		assert.Equal(t, &ir.Encoding{Name: "x-mine"}, sc.Encoding, site.pointer)
		assert.Equal(t, []string{"openapi:contentEncoding"}, unmodeledKeys(sc.Unmodeled), site.pointer)
		openapitest.AssertInfoDiagAt(t, diags, site.pointer+"/contentEncoding")
	}
	assert.Len(t, diags, 4)
}

// TestContentVocabulary_KeptOnACarrierWithNoNode covers the position whose
// schema owns no node at all: a property whose declared shape reduced to a shared
// primitive, where the ir.Property is the only home the keyword has.
func TestContentVocabulary_KeptOnACarrierWithNoNode(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, openapitest.ComponentSpec(
		"    A: {type: object, properties: {p: {contentMediaType: application/json}}}\n"))
	openapitest.RequireNoErrorDiags(t, diags)

	p := propertyOf(t, doc, "A", "p")
	assert.Equal(t, ir.TypeID("t/prim/any"), p.Type.Target, "an untyped schema stays schemaless")
	entry, ok := p.Unmodeled["openapi:contentMediaType"]
	require.True(t, ok, "the carrier is the only home when the schema hoisted no node")
	assert.Equal(t, ir.ReasonNoIRHome, entry.Reason)
	openapitest.AssertInfoDiagAt(t, diags, string(entry.Provenance.Pointer))
}

// TestRedactionFormat_SensitiveOnWhateverNodeThePositionOwns pins that
// `format: password` is an annotation rather than a shape: the position lowers
// as it would without it, and the node it owns, of any kind, is Sensitive. A
// component whose shape is a shared primitive owns an alias over it, as every
// component does, with no Encoding, since a redaction names none. Nothing is
// reported: the format reached its field everywhere (GitHub #579).
func TestRedactionFormat_SensitiveOnWhateverNodeThePositionOwns(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, schema string
		kind         ir.TypeKind
		base         ir.TypeID // the alias's base, for a Scalar
	}{
		{"string", "{type: string, format: password}", ir.KindScalar, "t/prim/string"},
		{"integer", "{type: integer, format: password}", ir.KindScalar, "t/prim/integer"},
		{"untyped", "{format: password}", ir.KindScalar, "t/prim/any"},
		{"nullable", "{type: [string, 'null'], format: password}", ir.KindScalar, "t/prim/string"},
		{"enum", "{type: string, enum: [a, b], format: password}", ir.KindEnum, ""},
		{"const", "{const: abc, format: password}", ir.KindLiteral, ""},
		{"object", "{type: object, properties: {a: {type: string}}, format: password}", ir.KindModel, ""},
		{"array", "{type: array, items: {type: string}, format: password}", ir.KindList, ""},
		{"tuple", "{type: array, prefixItems: [{type: string}], format: password}", ir.KindTuple, ""},
		{"multi-typed", "{type: [string, integer], format: password}", ir.KindUnion, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			doc, diags := parseFull(t, openapitest.ComponentSpec("    Secret: "+tc.schema+"\n"))
			assert.Empty(t, diags, "the format reached its field, so nothing is kept or reported")

			node := typeByName(doc, "Secret")
			require.NotNil(t, node)
			assert.Equal(t, tc.kind, node.Kind())
			assert.True(t, node.Common().Sensitive, "whole-type redaction")
			assert.NotContains(t, node.Common().Unmodeled, "openapi:format")
			sc, ok := node.(*ir.Scalar)
			if !ok {
				return
			}
			require.NotNil(t, sc.Base)
			assert.Equal(t, tc.base, sc.Base.Target, "the shape it would have had without the format")
			assert.Nil(t, sc.Encoding, "a redaction is no encoding")
		})
	}
}

// TestRedactionFormat_HoistsAnAliasWhereTheShapeOwnsNone pins the hoist at a
// position with neither a carrier nor a node of its own: array items whose
// shape is the shared primitive own an alias over it to hold the redaction,
// while items writing none keep the primitive.
func TestRedactionFormat_HoistsAnAliasWhereTheShapeOwnsNone(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, openapitest.ComponentSpec(
		"    Secrets: {type: array, items: {type: string, format: password}}\n"+
			"    Plain: {type: array, items: {type: string}}\n"))
	assert.Empty(t, diags)

	secrets, ok := typeByName(doc, "Secrets").(*ir.List)
	require.True(t, ok)
	item, ok := doc.Types[secrets.Elem.Target].(*ir.Scalar)
	require.True(t, ok, "the items own an alias")
	assert.True(t, item.Sensitive)
	require.NotNil(t, item.Base)
	assert.Equal(t, ir.TypeID("t/prim/string"), item.Base.Target)

	plain, ok := typeByName(doc, "Plain").(*ir.List)
	require.True(t, ok)
	assert.Equal(t, ir.TypeID("t/prim/string"), plain.Elem.Target, "no redaction, no alias")
}

// TestRedactionFormat_APropertyOwningNoNodeIsSecret pins the carrier half: a
// property whose schema keeps the shared primitive holds the redaction itself,
// as it holds the schema's docs, and no node is hoisted at its pointer. A
// property whose schema owns a node leaves it on the node, one home per
// declaration, as fillPropertyAnnotations draws the line.
func TestRedactionFormat_APropertyOwningNoNodeIsSecret(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, openapitest.ComponentSpec(
		"    Holder:\n      type: object\n      properties:\n"+
			"        token: {type: string, format: password, description: the token}\n"+
			"        choice: {type: string, enum: [a, b], format: password}\n"))
	assert.Empty(t, diags)

	token := propertyOf(t, doc, "Holder", "token")
	assert.True(t, token.Secret)
	assert.Equal(t, ir.TypeID("t/prim/string"), token.Type.Target, "the shape is the bare string's")
	assert.Equal(t, "the token", token.Docs.Description, "the docs stay on the property")
	assert.NotContains(t, doc.Types, ir.TypeID("t/anon/components/schemas/Holder/properties/token"),
		"no node is hoisted for the redaction at a carrier")

	choice := propertyOf(t, doc, "Holder", "choice")
	assert.False(t, choice.Secret, "the enum node holds it")
	node, ok := doc.Types[choice.Type.Target]
	require.True(t, ok)
	assert.True(t, node.Common().Sensitive)
}

// TestRedactionFormat_SharedPrimitiveStaysNotSensitive pins that the fact lands
// on the position's own node and never on t/prim/string, which every other
// plain string declaration resolves to: a shared node never carries one
// declaration's annotations (GitHub #116).
func TestRedactionFormat_SharedPrimitiveStaysNotSensitive(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, openapitest.ComponentSpec(
		"    Pw: {type: string, format: password}\n"+
			"    Plain: {type: string}\n"+
			"    Holder: {type: object, properties: {token: {type: string, format: password}}}\n"))
	openapitest.RequireNoErrorDiags(t, diags)

	assert.False(t, typeByName(doc, "Plain").Common().Sensitive)
	shared, ok := doc.Types[ir.TypeID("t/prim/string")]
	require.True(t, ok, "the shared string primitive is registered")
	assert.False(t, shared.Common().Sensitive, "the shared primitive is never sensitive")
}

// TestRedactionFormat_BesideContentEncoding pins that the two keywords no longer
// compete for Encoding.Name: contentEncoding names the encoding, and the
// redaction is Sensitive, so neither is kept verbatim or reported.
func TestRedactionFormat_BesideContentEncoding(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, openapitest.ComponentSpec(
		"    Encoded: {type: string, format: password, contentEncoding: base64, contentMediaType: text/plain}\n"))
	assert.Empty(t, diags)

	sc, ok := typeByName(doc, "Encoded").(*ir.Scalar)
	require.True(t, ok)
	assert.True(t, sc.Sensitive)
	require.NotNil(t, sc.Encoding)
	assert.Equal(t, "base64", sc.Encoding.Name)
	assert.Equal(t, "text/plain", sc.Encoding.MediaType)
	assert.NotContains(t, sc.Unmodeled, "openapi:format")
}
