package schema_test

import (
	"encoding/json/jsontext"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/compilers/openapi/internal/schema"
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
			read:  minSide, wantIncl: new(ir.BigVal("10")), wantExcl: new(ir.BigVal("0")),
		},
		{
			name:  "alias node where the exclusive minimum is the tighter",
			unmod: typeByName(doc, "Tight").Common().Unmodeled,
			bound: typeByName(doc, "Tight").(*ir.Scalar).Constraints,
			read:  minSide, wantIncl: new(ir.BigVal("0")), wantExcl: new(ir.BigVal("10")),
		},
		{
			name:  "property where the exclusive maximum is the tighter",
			unmod: propertyOf(t, doc, "Holder", "low").Unmodeled,
			bound: propertyOf(t, doc, "Holder", "low").Constraints,
			read:  maxSide, wantIncl: new(ir.BigVal("100")), wantExcl: new(ir.BigVal("5")),
		},
		{
			name:  "property where the maximum is the tighter",
			unmod: propertyOf(t, doc, "Holder", "high").Unmodeled,
			bound: propertyOf(t, doc, "Holder", "high").Constraints,
			read:  maxSide, wantIncl: new(ir.BigVal("5")), wantExcl: new(ir.BigVal("100")),
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

func TestConstraints_ExclusiveBoolean30(t *testing.T) {
	t.Parallel()
	spec := openapitest.ComponentSpecVer("3.0.3", `    S:
      type: object
      properties:
        n:
          type: integer
          minimum: 5
          exclusiveMinimum: true
          maximum: 10
          exclusiveMaximum: true
`)
	// The library models exclusiveMinimum/Maximum as numbers and flags the valid
	// 3.0 boolean form with a type-mismatch; because those keywords are Morphic's
	// to own, load suppresses that false positive, so a valid 3.0 boolean exclusive
	// bound lowers cleanly with the flag set and no error diagnostic.
	doc, diags := schema.LowerSpec(t, spec)
	openapitest.RequireNoErrorDiags(t, diags)
	c := propConstraints(t, doc, "S", "n")
	// The modifier is a spelling of the exclusive bound, so the literal it
	// modified lands in the exclusive field and the inclusive one is left empty
	// — the same constraints the 2020-12 spelling of "x > 5, x < 10" produces.
	assert.Nil(t, c.Min, "the modified minimum does not also stay inclusive")
	assert.Nil(t, c.Max)
	require.NotNil(t, c.ExclusiveMin)
	require.NotNil(t, c.ExclusiveMax)
	assert.Equal(t, ir.BigVal("5"), *c.ExclusiveMin)
	assert.Equal(t, ir.BigVal("10"), *c.ExclusiveMax)
}

func TestConstraints_ExclusiveNumeric31(t *testing.T) {
	t.Parallel()
	spec := openapitest.ComponentSpec(`    S:
      type: object
      properties:
        n:
          type: number
          exclusiveMinimum: 1.5
          exclusiveMaximum: 9.5
`)
	doc, diags := schema.LowerSpec(t, spec)
	openapitest.RequireNoErrorDiags(t, diags)
	c := propConstraints(t, doc, "S", "n")
	assert.Nil(t, c.Min)
	assert.Nil(t, c.Max)
	require.NotNil(t, c.ExclusiveMin)
	require.NotNil(t, c.ExclusiveMax)
	assert.Equal(t, ir.BigVal("1.5"), *c.ExclusiveMin)
	assert.Equal(t, ir.BigVal("9.5"), *c.ExclusiveMax)
}

func TestConstraints_MalformedNumericLiterals(t *testing.T) {
	t.Parallel()
	spec := openapitest.ComponentSpec(`    S:
      type: object
      properties:
        a: {type: number, minimum: .inf}
        b: {type: number, exclusiveMinimum: .inf}
`)
	doc, diags := schema.LowerSpec(t, spec)
	require.NotNil(t, doc)
	var count int
	for _, d := range diags {
		if d.Code == diag.NumericPrecision {
			count++
			assert.Equal(t, ir.SeverityError, d.Severity)
		}
	}
	assert.GreaterOrEqual(t, count, 2, "both malformed literals error")
}

func TestConstraints_NumericPrecisionSurvives(t *testing.T) {
	t.Parallel()
	spec := openapitest.ComponentSpec(`    S:
      type: object
      properties:
        ratio:
          type: number
          minimum: 0.30000000000000004
          maximum: 9007199254740993
          multipleOf: 0.1
`)
	doc, diags := schema.LowerSpec(t, spec)
	openapitest.RequireNoErrorDiags(t, diags)
	m := doc.Types[componentID("S")].(*ir.Model)
	c := m.Properties[0].Constraints
	require.NotNil(t, c)
	// Exact decimal strings — a float64 path would corrupt all three.
	assert.Equal(t, ir.BigVal("0.30000000000000004"), *c.Min)
	assert.Equal(t, ir.BigVal("9007199254740993"), *c.Max)
	assert.Equal(t, ir.BigVal("0.1"), *c.MultipleOf)
}

func TestConstraints_LosslessNumericLiterals(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		literal string
		want    ir.BigVal
	}{
		{"beyond float64 range", "1.8e308", "1.8e308"},
		{"far beyond float64 range", "1e400", "1e400"},
		{"leading dot spelling", ".5", "0.5"},
		{"trailing dot spelling", "5.", "5"},
		{"huge integer beyond int64", "123456789012345678901234567890", "123456789012345678901234567890"},
		{"high-precision decimal", "0.12345678901234567890123456789", "0.12345678901234567890123456789"},
		{"exponential notation", "6.022e23", "6.022e23"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			spec := openapitest.ComponentSpec("    S:\n      type: object\n      properties:\n        n: {type: number, minimum: " + tc.literal + "}\n")
			doc, diags := schema.LowerSpec(t, spec)
			// A valid number, however spelled, is accepted with no error: the
			// library's float64/JSON complaint is not surfaced.
			openapitest.RequireNoErrorDiags(t, diags)
			c := propConstraints(t, doc, "S", "n")
			require.NotNil(t, c.Min)
			assert.Equal(t, tc.want, *c.Min)
		})
	}
}

// TestConstraints_HoistedSubSchemaBadBoundSingleError pins that a malformed bound
// on a component-property sub-schema reached by a $ref is reported exactly once,
// even though the sub-schema's constraints are read from two positions (the owning
// property and the $ref hoist).
//
// What holds it is identity de-duplication in compile.Diags. Both reads stamp
// this same pointer, so the two diagnostics agree in severity, code, message and
// provenance, and the second is dropped as a repeat. Removing that dedup is what
// reddens this test — it then reports the identical error twice.
func TestConstraints_HoistedSubSchemaBadBoundSingleError(t *testing.T) {
	t.Parallel()
	spec := openapitest.ComponentSpec("    Foo:\n      type: object\n      properties:\n        bar: {type: number, minimum: hello}\n" +
		"    User:\n      type: object\n      properties:\n        b: {$ref: '#/components/schemas/Foo/properties/bar'}\n")
	_, diags := schema.LowerSpec(t, spec)
	assert.Equal(t, 1, openapitest.CountDiagsAt(diags, diag.NumericPrecision, ir.SeverityError),
		"one error for the shared bad bound, got: %+v", diags)
}

// TestConstraints_ExclusiveWrongDialectForm pins that an exclusiveMinimum/Maximum
// written in the wrong form for the document's dialect is reported as a single
// error, not silently accepted as a degenerate constraint: a boolean under the
// 2020-12 dialect (3.1/3.2) and a number under 3.0.
func TestConstraints_ExclusiveWrongDialectForm(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name, version, value string
	}{
		{"boolean under 3.1", "3.1.0", "true"},
		{"boolean under 3.2", "3.2.0", "false"},
		{"number under 3.0", "3.0.3", "5"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			spec := openapitest.ComponentSpecVer(tc.version,
				"    S:\n      type: object\n      properties:\n        n: {type: number, exclusiveMinimum: "+tc.value+"}\n")
			doc, diags := schema.LowerSpec(t, spec)
			require.NotNil(t, doc)
			assert.Equal(t, 1, openapitest.CountDiagsAt(diags, diag.ExclusiveBoundForm, ir.SeverityError),
				"one dialect-form error, got: %+v", diags)
			// The degenerate bound is dropped, not recorded.
			m, ok := typeByName(doc, "S").(*ir.Model)
			require.True(t, ok)
			for _, p := range m.Properties {
				if p.WireName == "n" && p.Constraints != nil {
					assert.Nil(t, p.Constraints.ExclusiveMin, "wrong-form exclusive bound is not set")
				}
			}
		})
	}
}

func TestConstraints_TypeWrongBoundYieldsSingleError(t *testing.T) {
	t.Parallel()
	spec := openapitest.ComponentSpec("    S:\n      type: object\n      properties:\n        n: {type: number, minimum: hello}\n")
	_, diags := schema.LowerSpec(t, spec)
	// Exactly one diagnostic: Morphic's error with the schema's own provenance.
	// The library emits two redundant float64 type-mismatch findings on the same
	// keyword; load suppresses both because Morphic owns numeric-bound keywords.
	require.Len(t, diags, 1, "one diagnostic for a type-wrong bound, got: %+v", diags)
	assert.Equal(t, diag.NumericPrecision, diags[0].Code)
	assert.Equal(t, ir.SeverityError, diags[0].Severity)
	assert.NotEmpty(t, diags[0].Provenance.Pointer)
}

func TestConstraints_NonNumericMinimumErrors(t *testing.T) {
	t.Parallel()
	spec := openapitest.ComponentSpec("    S:\n      type: object\n      properties:\n        n: {type: number, minimum: hello}\n")
	doc, diags := schema.LowerSpec(t, spec)
	require.NotNil(t, doc)
	// A genuinely non-numeric bound is never dropped silently: Morphic owns the
	// keyword and reports it as an error with the property's exact provenance.
	var reported bool
	for _, d := range diags {
		if d.Code == diag.NumericPrecision {
			reported = true
			assert.Equal(t, ir.SeverityError, d.Severity)
		}
	}
	assert.True(t, reported, "a non-numeric minimum yields an error diagnostic")
}

// propConstraints returns the constraints of a named model's property.
func propConstraints(t *testing.T, doc *ir.Document, model, wire string) *ir.Constraints {
	t.Helper()
	m, ok := typeByName(doc, model).(*ir.Model)
	require.True(t, ok)
	for _, p := range m.Properties {
		if p.WireName == wire {
			require.NotNil(t, p.Constraints, "property %s has constraints", wire)
			return p.Constraints
		}
	}
	t.Fatalf("property %s not found", wire)
	return nil
}

// TestComponentConstraints_DiagnosticProvenance covers the diagnostic-stamping
// loop: a scalar component whose numeric bound is not a valid number surfaces a
// numeric-precision error stamped with the component's own pointer.
func TestComponentConstraints_DiagnosticProvenance(t *testing.T) {
	t.Parallel()
	spec := openapitest.ComponentSpec("    BadN: {type: number, minimum: hello}\n")
	_, diags := schema.LowerSpec(t, spec)
	var found bool
	for _, d := range diags {
		if d.Code == diag.NumericPrecision {
			found = true
			assert.NotEmpty(t, d.Provenance.Pointer, "component numeric diagnostic carries its pointer")
			assert.Equal(t, ir.SeverityError, d.Severity, "a non-numeric bound is an error")
		}
	}
	assert.True(t, found, "a non-numeric component bound is reported")
}
