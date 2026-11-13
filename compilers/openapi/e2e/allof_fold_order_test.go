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

// TestAllOfFold_AModelReportsWhereItsFirstRedeclarationIsWritten pins when a
// model's folds are queued: at its first redeclared field, as the walk meets
// it. A model holding another, each redeclaring a field, reports before or
// after the one it holds as the two are written, whichever order that is, and
// both orders report the same findings. Queued once a model's lowering
// returned instead, the model it holds would report first in both orders.
func TestAllOfFold_AModelReportsWhereItsFirstRedeclarationIsWritten(t *testing.T) {
	t.Parallel()
	const (
		field  = "p: {type: integer}"
		nested = "q: {allOf: [{properties: {r: {type: string}}}, {properties: {r: {type: integer}}}]}"
		p      = jsontext.Pointer("/components/schemas/X/allOf/1/properties/p")
		r      = jsontext.Pointer("/components/schemas/X/allOf/1/properties/q/allOf/1/properties/r")
	)
	reported := map[string][]jsontext.Pointer{}
	for name, tc := range map[string]struct {
		props string
		want  []jsontext.Pointer
	}{
		"the field first":      {props: "{" + field + ", " + nested + "}", want: []jsontext.Pointer{p, r}},
		"the held model first": {props: "{" + nested + ", " + field + "}", want: []jsontext.Pointer{r, p}},
	} {
		_, diags := parseFull(t, openapitest.ComponentSpec(
			"    X:\n      allOf:\n        - properties: {p: {type: string}}\n        - properties: "+tc.props+"\n"))
		var got []jsontext.Pointer
		for _, d := range diags {
			if d.Code == diag.ConflictingRedecl {
				got = append(got, d.Provenance.Pointer)
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
// conflict keeps the first declaration in either order and reports the other.
func TestAllOfFold_AnEncodedFormatIsJudgedAsByteIsBesideBinary(t *testing.T) {
	t.Parallel()
	const (
		bytesB64    = "bytes base64 string"
		bytesB64URL = "bytes base64url string"
		httpDate    = "datetime_offset http-date"
		seconds     = "datetime_offset unixTimestamp integer"
	)
	for _, tc := range []struct {
		a, b   string
		code   string // "" where the fold narrows to one type, which wins
		winner string // the type the field takes where code is ""
	}{
		{a: "{type: string, format: binary}", b: "{type: string, format: byte}", winner: bytesB64},
		{a: "{type: string, format: date-time}", b: "{type: string, format: http-date}", winner: httpDate},
		{a: "{type: string}", b: "{type: string, format: http-date}", winner: httpDate},
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
				assert.Equal(t, []string{tc.code}, codes, "%v: the second declaration is reported", order)
				first, _ := foldedField(t, order[0], order[0])
				assert.Equal(t, first, field, "%v: the field keeps the first declaration", order)
			}
		})
	}
}

// foldedField compiles a model whose two inline allOf branches declare its
// field b as first and second, and returns what the field's type is, as its
// primitive or a Scalar's base, encoding and wire type, and the codes reported
// at the second declaration.
func foldedField(t *testing.T, first, second string) (string, []string) {
	t.Helper()
	doc, diags := parseFull(t, openapitest.ComponentSpec("    Sub:\n      allOf:\n"+
		"        - {type: object, properties: {b: "+first+"}}\n"+
		"        - {type: object, properties: {b: "+second+"}}\n"))
	assertNoErrorDiags(t, diags)
	m, ok := doc.Types[componentID("Sub")].(*ir.Model)
	require.True(t, ok, "the allOf composes a model")
	p, ok := propByWire(m, "b")
	require.True(t, ok, "the field folds into one property")
	var codes []string
	for _, d := range diags {
		if d.Provenance.Pointer == "/components/schemas/Sub/allOf/1/properties/b" {
			codes = append(codes, d.Code)
		}
	}
	switch td := doc.Types[p.Type.Target].(type) {
	case *ir.Primitive:
		return string(td.Prim), codes
	case *ir.Scalar:
		require.NotNil(t, td.Base)
		require.NotNil(t, td.Encoding)
		desc := fmt.Sprintf("%s %s", primAt(t, doc, *td.Base), td.Encoding.Name)
		if td.Encoding.WireType != nil {
			desc += " " + string(primAt(t, doc, *td.Encoding.WireType))
		}
		return desc, codes
	default:
		return string(td.Kind()), codes
	}
}

// primAt returns the primitive ref names, which it must.
func primAt(t *testing.T, doc *ir.Document, ref ir.TypeRef) ir.PrimKind {
	t.Helper()
	prim, ok := doc.Types[ref.Target].(*ir.Primitive)
	require.True(t, ok, "%s is a primitive", ref.Target)
	return prim.Prim
}
