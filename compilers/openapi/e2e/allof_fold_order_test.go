// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package e2e_test

import (
	"encoding/json/jsontext"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/ir"
)

// TestAllOfFold_ModelsReportInTheOrderTheirIDsSort pins the order a run's
// members settle in: by model ID, each after the models it composes, so a
// model holding another, each redeclaring a field, reports in one order
// whichever of the two is written first, and both orders report the same
// findings.
func TestAllOfFold_ModelsReportInTheOrderTheirIDsSort(t *testing.T) {
	t.Parallel()
	const (
		field  = "p: {type: integer}"
		nested = "q: {allOf: [{properties: {r: {type: string}}}, {properties: {r: {type: integer}}}]}"
		p      = jsontext.Pointer("/components/schemas/X/allOf/0/properties/p")
		r      = jsontext.Pointer("/components/schemas/X/allOf/1/properties/q/allOf/0/properties/r")
	)
	reported := map[string][]jsontext.Pointer{}
	for name, tc := range map[string]struct {
		properties string
		want       []jsontext.Pointer
	}{
		"the field first":      {properties: "{" + field + ", " + nested + "}", want: []jsontext.Pointer{r, p}},
		"the held model first": {properties: "{" + nested + ", " + field + "}", want: []jsontext.Pointer{r, p}},
	} {
		_, diags := parseFull(t, openapitest.ComponentSpec(
			"    X:\n      allOf:\n        - properties: {p: {type: string}}\n        - properties: "+tc.properties+"\n"))
		var got []jsontext.Pointer
		for _, diagnostic := range diags {
			if diagnostic.Code == diag.ConflictingRedecl {
				got = append(got, diagnostic.Provenance.Pointer)
			}
		}
		assert.Equal(t, tc.want, got, name)
		reported[name] = got
	}
	assert.ElementsMatch(t, reported["the field first"], reported["the held model first"],
		"the declaration order moves the reports, and changes none")
}

// TestAllOfFold_AnEncodedFormatIsJudgedAsByteIsBesideBinary pins how a field
// declared twice under allOf, once with a format whose value is written in a
// text of its own, folds, in both orders: as byte beside binary does, the
// encoded scalar wins over the bare primitive it is of, and no other pair
// folds two texts into one without a report
// (docs/compilers/openapi.md#allof-redeclared-property). An overlap or a
// conflict keeps the later declaration in either order and reports the other.
func TestAllOfFold_AnEncodedFormatIsJudgedAsByteIsBesideBinary(t *testing.T) {
	t.Parallel()
	const (
		bytesB64    = "bytes base64 string"
		bytesB64URL = "bytes base64url string"
		httpDate    = "datetime_offset http-date"
		seconds     = "datetime_offset unixTimestamp integer"
		secondsText = "datetime_offset unixTimestamp string"
	)
	for _, tc := range []struct {
		a, b   string
		code   string // "" where the fold narrows to one type, which wins
		winner string // the type the field takes where code is ""
	}{
		{a: "{type: string, format: binary}", b: "{type: string, format: byte}", winner: bytesB64},
		{a: "{type: string, format: date-time}", b: "{type: string, format: http-date}", winner: httpDate},
		{a: "{type: string}", b: "{type: string, format: http-date}", winner: httpDate},
		{a: "{type: string, format: date-time}", b: "{type: string, format: unixtime}", winner: secondsText},
		{a: "{type: string, format: binary}", b: "{type: string, format: base64url}", winner: bytesB64URL},
		{a: "{type: integer}", b: "{type: integer, format: unixtime}", winner: seconds},
		{a: "{type: number}", b: "{type: integer, format: unixtime}", winner: seconds},
		{a: "{type: integer, format: int64}", b: "{type: integer, format: unixtime}", code: diag.DegradedConstruct},
		{a: "{type: string, format: byte}", b: "{type: string, format: base64url}", code: diag.DegradedConstruct},
		{a: "{type: string, format: http-date}", b: "{type: string, format: unixtime}", code: diag.DegradedConstruct},
		{a: `{type: string, enum: ["Sun, 06 Nov 1994 08:49:37 GMT"]}`, b: "{type: string, format: http-date}", code: diag.DegradedConstruct},
		{a: "{type: string}", b: "{type: integer, format: unixtime}", code: diag.ConflictingRedecl},
		{a: "{type: string, format: date-time}", b: "{type: integer, format: unixtime}", code: diag.ConflictingRedecl},
		{a: "{type: string, format: date}", b: "{type: string, format: http-date}", code: diag.ConflictingRedecl},
		{a: "{type: string, format: time}", b: "{type: string, format: http-date}", code: diag.ConflictingRedecl},
		{a: "{type: string, format: uuid}", b: "{type: string, format: unixtime}", code: diag.ConflictingRedecl},
	} {
		t.Run(tc.a+" beside "+tc.b, func(t *testing.T) {
			t.Parallel()
			for _, order := range [][2]string{{tc.a, tc.b}, {tc.b, tc.a}} {
				field, codes := foldedField(t, order[0], order[1])
				if tc.code == "" {
					assert.Empty(t, codes, "%v: the narrower type wins with no report", order)
					assert.Equal(t, tc.winner, field, "%v: the field takes the narrower type", order)
					continue
				}
				assert.Equal(t, []string{tc.code}, codes, "%v: the first declaration is reported", order)
				later, _ := foldedField(t, order[1], order[1])
				assert.Equal(t, later, field, "%v: the field keeps the later declaration", order)
			}
		})
	}
}

// foldedField compiles a model whose two inline allOf entries declare its
// field b as first and second, and returns what the field's type is, as its
// primitive or a Scalar's base, encoding and wire type, and the codes reported
// at the first declaration.
func foldedField(t *testing.T, first, second string) (string, []string) {
	t.Helper()
	doc, diags := parseFull(t, openapitest.ComponentSpec("    Sub:\n      allOf:\n"+
		"        - {type: object, properties: {b: "+first+"}}\n"+
		"        - {type: object, properties: {b: "+second+"}}\n"))
	assertNoErrorDiags(t, diags)
	model, ok := doc.Types[componentID("Sub")].(*ir.Model)
	require.True(t, ok, "the allOf composes a model")
	property, ok := propertyByWire(model, "b")
	require.True(t, ok, "the field folds into one property")
	var codes []string
	for _, diagnostic := range diags {
		if diagnostic.Provenance.Pointer == "/components/schemas/Sub/allOf/0/properties/b" {
			codes = append(codes, diagnostic.Code)
		}
	}
	switch td := doc.Types[property.Type.Target].(type) {
	case *ir.Primitive:
		return string(td.PrimitiveKind), codes
	case *ir.Scalar:
		require.NotNil(t, td.Base)
		require.NotNil(t, td.Encoding)
		desc := fmt.Sprintf("%s %s", primitiveAt(t, doc, *td.Base), td.Encoding.Name)
		if td.Encoding.WireType != nil {
			desc += " " + string(primitiveAt(t, doc, *td.Encoding.WireType))
		}
		return desc, codes
	default:
		return string(td.Kind()), codes
	}
}

// primitiveAt returns the primitive ref names, which it must.
func primitiveAt(t *testing.T, doc *ir.Document, ref ir.TypeRef) ir.PrimitiveKind {
	t.Helper()
	primitive, ok := doc.Types[ref.Target].(*ir.Primitive)
	require.True(t, ok, "%s is a primitive", ref.Target)
	return primitive.PrimitiveKind
}
