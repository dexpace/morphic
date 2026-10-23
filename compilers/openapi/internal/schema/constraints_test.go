package schema_test

import (
	"encoding/json/jsontext"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/ir"
)

// TestCoDeclaredBound_BothKeywordsReachTheCarriersConstraints pins the two
// carriers this package owns for a 2020-12 side that declares both of its bound
// keywords. The two keywords are independent and both apply, and ir.Constraints
// has a field for each, so both reach the constraints the carrier holds and
// neither is kept beside them.
//
// The rows are pairs that swap which of the two is the tighter while leaving the
// same magnitudes on the side. One bound slot answered both rows of a pair
// identically, which is what made a revision that moved only the looser keyword
// read as no change at all (GitHub #425).
func TestCoDeclaredBound_BothKeywordsReachTheCarriersConstraints(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, openapitest.ComponentSpec(
		"    Alias: {type: integer, minimum: 10, exclusiveMinimum: 0}\n"+
			"    Tight: {type: integer, minimum: 0, exclusiveMinimum: 10}\n"+
			"    Holder:\n      type: object\n      properties:\n"+
			"        low: {type: integer, maximum: 100, exclusiveMaximum: 5}\n"+
			"        high: {type: integer, maximum: 5, exclusiveMaximum: 100}\n"))
	openapitest.RequireNoErrorDiags(t, diags)

	tests := []struct {
		name     string
		unmod    ir.Unmodeled
		bound    *ir.Constraints
		read     func(*ir.Constraints) (incl, excl *ir.BigVal)
		wantIncl *ir.BigVal
		wantExcl *ir.BigVal
	}{
		{
			name:  "alias node where the minimum is the tighter",
			unmod: typeByName(doc, "Alias").Common().Unmodeled,
			bound: typeByName(doc, "Alias").(*ir.Scalar).Constraints,
			read:  minSide, wantIncl: bigValOf("10"), wantExcl: bigValOf("0"),
		},
		{
			name:  "alias node where the exclusive minimum is the tighter",
			unmod: typeByName(doc, "Tight").Common().Unmodeled,
			bound: typeByName(doc, "Tight").(*ir.Scalar).Constraints,
			read:  minSide, wantIncl: bigValOf("0"), wantExcl: bigValOf("10"),
		},
		{
			name:  "property where the exclusive maximum is the tighter",
			unmod: propertyOf(t, doc, "Holder", "low").Unmodeled,
			bound: propertyOf(t, doc, "Holder", "low").Constraints,
			read:  maxSide, wantIncl: bigValOf("100"), wantExcl: bigValOf("5"),
		},
		{
			name:  "property where the maximum is the tighter",
			unmod: propertyOf(t, doc, "Holder", "high").Unmodeled,
			bound: propertyOf(t, doc, "Holder", "high").Constraints,
			read:  maxSide, wantIncl: bigValOf("5"), wantExcl: bigValOf("100"),
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			require.NotNil(t, tc.bound, "both bounds reach ir.Constraints")
			incl, excl := tc.read(tc.bound)
			assert.Equal(t, tc.wantIncl, incl, "the inclusive keyword as written")
			assert.Equal(t, tc.wantExcl, excl, "the exclusive keyword as written, beside it")
			assert.Empty(t, tc.unmod, "with a field apiece there is nothing left to keep")
		})
	}
}

// TestExclusiveModifier_WithNoBoundIsKeptOnTheCarrierThatReadIt pins the one
// bound keyword that still reaches no field, at the two carriers this package
// owns. A 3.0 exclusiveMinimum is a modifier of the minimum beside it, so one
// written without a minimum modifies nothing — draft-4 forbids that schema, and
// the loader hands the keyword here unchecked. Dropping it would lose a declared
// keyword silently, so it is kept verbatim beside the constraints it did not
// reach.
func TestExclusiveModifier_WithNoBoundIsKeptOnTheCarrierThatReadIt(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, openapitest.ComponentSpecVer("3.0.3",
		"    Alias: {type: integer, exclusiveMinimum: true}\n"+
			"    Holder:\n      type: object\n      properties:\n"+
			"        low: {type: integer, exclusiveMaximum: true}\n"))

	tests := []struct {
		name     string
		unmod    ir.Unmodeled
		wantKept string
		at       string
		carrier  string
	}{
		{
			name:     "alias node",
			unmod:    typeByName(doc, "Alias").Common().Unmodeled,
			wantKept: "openapi:exclusiveMinimum",
			at:       "/components/schemas/Alias/exclusiveMinimum",
			carrier:  "/components/schemas/Alias",
		},
		{
			name:     "property",
			unmod:    propertyOf(t, doc, "Holder", "low").Unmodeled,
			wantKept: "openapi:exclusiveMaximum",
			at:       "/components/schemas/Holder/properties/low/exclusiveMaximum",
			carrier:  "/components/schemas/Holder/properties/low",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			entry, ok := tc.unmod[tc.wantKept]
			require.True(t, ok, "%s is kept on the carrier that read it; got %v", tc.wantKept, tc.unmod)
			assert.Equal(t, ir.ReasonDegradedLowering, entry.Reason)
			assert.JSONEq(t, "true", string(entry.Value))
			assert.Equal(t, jsontext.Pointer(tc.at), entry.Provenance.Pointer, "located at the keyword itself")
			assert.Len(t, diagsAtPointer(diags, diag.DegradedConstruct, tc.carrier), 1,
				"and reported once, at the schema that read it: %+v", diags)
		})
	}
}

// TestCoDeclaredBound_ASingleKeywordKeepsNothing is the other half of
// TestCoDeclaredBound_BothKeywordsReachTheCarriersConstraints: a side writing
// one keyword has it in a field, so an entry restating it
// would give one bound two homes and make the two source shapes indistinguishable
// in the opposite direction.
func TestCoDeclaredBound_ASingleKeywordKeepsNothing(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, openapitest.ComponentSpec(
		"    Alias: {type: integer, minimum: 10}\n"+
			"    Holder: {type: object, properties: {low: {type: integer, exclusiveMinimum: 0}}}\n"))
	openapitest.RequireNoErrorDiags(t, diags)

	assert.Empty(t, typeByName(doc, "Alias").Common().Unmodeled)
	assert.Empty(t, propertyOf(t, doc, "Holder", "low").Unmodeled)
}

// minSide and maxSide read one side's pair of bounds off a Constraints, so one
// table can drive both sides through the same assertion.
func minSide(c *ir.Constraints) (incl, excl *ir.BigVal) { return c.Min, c.ExclusiveMin }

func maxSide(c *ir.Constraints) (incl, excl *ir.BigVal) { return c.Max, c.ExclusiveMax }

// bigValOf is the *ir.BigVal a bound assertion compares against.
func bigValOf(v string) *ir.BigVal {
	b := ir.BigVal(v)
	return &b
}
