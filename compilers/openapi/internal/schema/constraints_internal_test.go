// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package schema

import (
	"testing"

	"github.com/speakeasy-api/openapi/jsonschema/oas3"
	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/speakeasy-api/openapi/references"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/compile"
	"github.com/dexpace/morphic/compilers/openapi/internal/annotation"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/ir"
)

// TestConstraintsOf_NonSchemaInputs covers annotation.SchemaOf's nil and
// boolean early return, plus a well-formed reference besides: none of the three
// leaves a body with constraint keywords on it, so ConstraintsOf reports none
// either way.
func TestConstraintsOf_NonSchemaInputs(t *testing.T) {
	t.Parallel()
	l := NewRawLowerer(&soa.OpenAPI{})
	for _, js := range []*oas3.Schema{
		annotation.SchemaOf(nil),
		annotation.SchemaOf(oas3.NewJSONSchemaFromBool(true)),
		annotation.SchemaOf(oas3.NewJSONSchemaFromReference("#/components/schemas/Other")),
	} {
		var kept ir.Unmodeled
		cons, diags := ConstraintsOf(l.Ctx, &kept, js, nil, "/p")
		assert.Nil(t, cons)
		assert.Empty(t, diags)
		assert.Empty(t, kept)
	}
}

// TestReferenceConstraintsOf_NilSchema pins that a position with no schema reads
// as ConstraintsOf reads it, holding nothing.
func TestReferenceConstraintsOf_NilSchema(t *testing.T) {
	t.Parallel()
	l := NewRawLowerer(&soa.OpenAPI{})
	var kept ir.Unmodeled
	cons, diags := referenceConstraintsOf(l.Ctx, compile.NewTypes(), &kept, nil, nil, ir.TypeRef{}, "/p")
	assert.Nil(t, cons)
	assert.Empty(t, diags)
	assert.Empty(t, kept)
}

// TestReferenceHolds_NoCollectionBoundIsHeld pins the switch's default, which
// keywordHome's arm never reaches: a keyword that is no collection bound.
func TestReferenceHolds_NoCollectionBoundIsHeld(t *testing.T) {
	t.Parallel()
	assert.False(t, referenceHolds(&oas3.Schema{}, "pattern"))
}

// TestConstraintsOf_EmptyRefSchema covers a $ref pointer that is present but
// empty: annotation.At reads it as a declaration, since IsReference is false
// for it, and SchemaOf returns its body like any other, so ConstraintsOf meets
// a body that carries only Ref and finds no constraint keywords. The parser
// never emits this shape, so it is built by hand.
func TestConstraintsOf_EmptyRefSchema(t *testing.T) {
	t.Parallel()
	l := NewRawLowerer(&soa.OpenAPI{})
	js := oas3.NewJSONSchemaFromSchema[oas3.Referenceable](&oas3.Schema{Ref: new(references.Reference(""))})
	var kept ir.Unmodeled
	cons, diags := ConstraintsOf(l.Ctx, &kept, annotation.SchemaOf(js), nil, "/p")
	assert.Nil(t, cons)
	assert.Empty(t, diags)
	assert.Empty(t, kept)
}

// TestStampConstraintDiags_RelocatesEveryDiagnosticToTheReadingPointer pins what
// makes two reads of one sub-schema dedup rather than report twice: the
// diagnostics a constraint read produces are located at the position that read
// it, not at the position that declared it.
func TestStampConstraintDiags_RelocatesEveryDiagnosticToTheReadingPointer(t *testing.T) {
	t.Parallel()
	l := NewRawLowerer(&soa.OpenAPI{})
	in := []ir.Diagnostic{
		{Code: diag.DegradedConstruct, Provenance: ir.Provenance{Pointer: "/elsewhere"}},
		{Code: diag.NumericPrecision, Provenance: ir.Provenance{Source: 9, Pointer: "/other"}},
	}

	got := stampConstraintDiags(l.Ctx, in, "/components/schemas/S")

	require.Len(t, got, 2)
	for _, d := range got {
		assert.Equal(t, ir.Provenance{Source: 0, Pointer: "/components/schemas/S"}, d.Provenance,
			"every diagnostic is relocated, not just the first")
	}
	assert.Equal(t, diag.NumericPrecision, got[1].Code, "only the provenance changes")
}

// TestCompareCounts_OrdersLiteralsExactly pins compareCounts on spellings of
// one value and on powers of ten past what an ir.Decimal holds, where reading
// either through a float64 or an int64 would tie or wrap, a carry crossing such
// a power's digits among them, and on an exponent no count is written with,
// read as 0.
func TestCompareCounts_OrdersLiteralsExactly(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		a, b string
		want int
	}{
		{"99999999999999999999", "88888888888888888888", 1},
		{"100000000000000000000", "1e20", 0},
		{"1E20", "0.0001e24", 0},
		{"+00012.30e19", "123e18", 0},
		{"100000000000000000001", "1e20", 1},
		{"1e1000000000000000000", "2e1000000000000000000", -1},
		{"1e99999999999999999999", "9e1000000000000000000", 1},
		{"1e400", "99999999999999999999", 1},
		{"1e1000000000000000000", "10e999999999999999999", 0},
		{"1e1000000000000000000", "1000e999999999999999998", -1},
		{"1e1000000000000000000", "0.001e1000000000000000003", 0},
		{"0.0001e1000000000000000003", "1e999999999999999999", 0},
		{"123e1000000000000000000", "0.00124e1000000000000000005", -1},
		{"1e10000000000000000000", "9e9999999999999999999", 1},
		{"1ex", "1", 0},
		{"1e-10000000000000000000", "1", 0},
		{"1e-1000000000000000000", "1", 0},
	} {
		assert.Equal(t, tc.want, compareCounts(tc.a, tc.b), "%s against %s", tc.a, tc.b)
		assert.Equal(t, -tc.want, compareCounts(tc.b, tc.a), "%s against %s", tc.b, tc.a)
	}
}
