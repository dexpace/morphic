// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package schema_test

import (
	"encoding/json/jsontext"
	"testing"

	"github.com/google/go-cmp/cmp"
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
		{"past a binary float's range", "1e2000000000", "1e2000000000"},
		{"at the edge of the exponent range", "1e999999999999999999", "1e999999999999999999"},
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

// TestConstraints_ABoundPastTheExponentRangeIsRefused pins the far side of the
// range the rows above reach the edge of: a bound whose leading digit carries a
// power of ten past ir.MaxDecimalExp is reported once, as the numeric-precision
// error every unreadable bound draws, and is dropped rather than recorded.
func TestConstraints_ABoundPastTheExponentRangeIsRefused(t *testing.T) {
	t.Parallel()
	spec := openapitest.ComponentSpec("    S:\n      type: object\n      properties:\n        n: {type: number, minimum: 1e1000000000000000000}\n")
	doc, diags := schema.LowerSpec(t, spec)

	assert.Equal(t, 1, openapitest.CountDiagsAt(diags, diag.NumericPrecision, ir.SeverityError),
		"one numeric-precision error, got: %+v", diags)
	m, ok := typeByName(doc, "S").(*ir.Model)
	require.True(t, ok)
	require.Len(t, m.Properties, 1)
	if c := m.Properties[0].Constraints; c != nil {
		assert.Nil(t, c.Min, "a refused bound is dropped, not recorded")
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

// boundedRef and boundedNullable are the two spellings of a position that
// lowers to a reference, each writing all three collection bounds.
const (
	boundedRef      = "{$ref: '#/components/schemas/Tags', minItems: 1, maxItems: 3, uniqueItems: true}"
	boundedNullable = "{oneOf: [{$ref: '#/components/schemas/Tags'}, {type: 'null'}], " +
		"minItems: 1, maxItems: 3, uniqueItems: true}"
	tagsSchema = "    Tags: {type: array, items: {type: string}}\n"
)

// allBounds is what boundedRef and boundedNullable state.
func allBounds() *ir.Constraints {
	return &ir.Constraints{MinItems: new(int64(1)), MaxItems: new(int64(3)), UniqueItems: true}
}

// TestCollectionBound_BesideAReferenceReachesTheAlias pins
// docs/compilers/openapi.md#constraints at every position that hoists an alias
// over a reference: the bounds land on it, none is also kept, and none is
// reported as having no home.
func TestCollectionBound_BesideAReferenceReachesTheAlias(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, schema string
		at           ir.TypeID
		pointer      string
	}{
		{"component", boundedRef, componentID("S"), "/components/schemas/S"},
		{"nullable component", boundedNullable, componentID("S"), "/components/schemas/S"},
		{"items", "{type: array, items: " + boundedRef + "}",
			"t/anon/components/schemas/S/items", "/components/schemas/S/items"},
		{"nullable items", "{type: array, items: " + boundedNullable + "}",
			"t/anon/components/schemas/S/items", "/components/schemas/S/items"},
		{"prefixItems", "{type: array, prefixItems: [" + boundedRef + "]}",
			"t/anon/components/schemas/S/prefixItems/0", "/components/schemas/S/prefixItems/0"},
		{"additionalProperties", "{type: object, additionalProperties: " + boundedRef + "}",
			"t/anon/components/schemas/S/additionalProperties", "/components/schemas/S/additionalProperties"},
		{"union branch", "{oneOf: [" + boundedRef + ", {type: string}]}",
			"t/anon/components/schemas/S/oneOf/0", "/components/schemas/S/oneOf/0"},
		{"allOf branch", "{allOf: [" + boundedRef + "], properties: {f: {type: string}}}",
			"t/anon/components/schemas/S/allOf/0", "/components/schemas/S/allOf/0"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			doc, diags := parseFull(t, openapitest.ComponentSpec(tagsSchema+"    S: "+tc.schema+"\n"))
			openapitest.RequireNoErrorDiags(t, diags)

			alias, ok := doc.Types[tc.at].(*ir.Scalar)
			require.True(t, ok, "the position owns an alias: %#v", doc.Types[tc.at])
			assert.Equal(t, allBounds(), alias.Constraints)
			assert.Empty(t, alias.Unmodeled, "a bound the alias holds is not also kept")
			assert.False(t, openapitest.HasDiagCodeAt(diags, diag.DegradedConstruct, tc.pointer),
				"a bound the alias holds is not reported as homeless: %+v", diags)
		})
	}
}

// TestCollectionBound_BesideAReferenceReachesTheCarrier pins the same rule at a
// property, which hoists no alias, for both spellings of a reference.
func TestCollectionBound_BesideAReferenceReachesTheCarrier(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, openapitest.ComponentSpec(tagsSchema+
		"    Holder:\n      type: object\n      properties:\n"+
		"        ref: "+boundedRef+"\n"+
		"        nullable: "+boundedNullable+"\n"))
	openapitest.RequireNoErrorDiags(t, diags)

	for _, wire := range []string{"ref", "nullable"} {
		p := propertyOf(t, doc, "Holder", wire)
		assert.Equal(t, allBounds(), p.Constraints, wire)
		assert.Empty(t, p.Unmodeled, "%s: a bound the carrier holds is not also kept", wire)
		assert.False(t, openapitest.HasDiagCodeAt(diags, diag.DegradedConstruct,
			"/components/schemas/Holder/properties/"+wire), "%s: %+v", wire, diags)
	}
}

// TestCollectionBound_ReadsThePositionNotItsTarget pins that the rule asks what
// the position wrote and never what the $ref names, as for a value bound: a
// bound conjoins with whatever the target admits, and reading the target would
// let declaration order decide. The string and the object are the targets it
// binds nothing on, and the tuple is one it binds.
func TestCollectionBound_ReadsThePositionNotItsTarget(t *testing.T) {
	t.Parallel()
	targets := "    Str: {type: string}\n    Obj: {type: object, properties: {f: {type: string}}}\n" +
		"    Tup: {type: array, prefixItems: [{type: string}]}\n"
	for _, target := range []string{"Str", "Obj", "Tup"} {
		t.Run(target, func(t *testing.T) {
			t.Parallel()
			written := "{$ref: '#/components/schemas/" + target + "', minItems: 1}"
			doc, diags := parseFull(t, openapitest.ComponentSpec(targets+
				"    S: "+written+"\n    Holder:\n      type: object\n      properties:\n        f: "+written+"\n"))
			openapitest.RequireNoErrorDiags(t, diags)

			want := &ir.Constraints{MinItems: new(int64(1))}
			alias, ok := typeByName(doc, "S").(*ir.Scalar)
			require.True(t, ok)
			assert.Equal(t, want, alias.Constraints)
			assert.Equal(t, want, propertyOf(t, doc, "Holder", "f").Constraints)
		})
	}
}

// TestCollectionBound_BesideAnExpandedDynamicRefReachesWhatThePositionOwns pins
// the third position that lowers to a reference. Its census asks what the reader
// filled, as a $ref site's does, so a bound the reader cannot state is kept
// verbatim and reported rather than read as a false 0 or a stated false.
func TestCollectionBound_BesideAnExpandedDynamicRefReachesWhatThePositionOwns(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, openapitest.ComponentSpec(`    Node:
      $dynamicAnchor: node
      type: array
      items: {type: string}
    Holder:
      type: object
      properties:
        carried: {$dynamicRef: '#node', maxItems: 3}
        unread: {$dynamicRef: '#node', maxItems: 1e400, uniqueItems: false}
    Element: {type: array, items: {$dynamicRef: '#node', maxItems: 4}}
`))
	openapitest.RequireNoErrorDiags(t, diags)

	carried := propertyOf(t, doc, "Holder", "carried")
	assert.Equal(t, &ir.Constraints{MaxItems: new(int64(3))}, carried.Constraints)
	assert.Empty(t, carried.Unmodeled, "a bound the carrier holds is not also kept")
	unread := propertyOf(t, doc, "Holder", "unread")
	assert.Nil(t, unread.Constraints)
	assert.Equal(t, []string{"openapi:maxItems", "openapi:uniqueItems"}, unmodeledKeys(unread.Unmodeled))
	assert.True(t, openapitest.HasDiagCodeAt(diags, diag.DegradedConstruct, "/components/schemas/Holder/properties/unread"))
	alias, ok := doc.Types["t/anon/components/schemas/Element/items"].(*ir.Scalar)
	require.True(t, ok)
	assert.Equal(t, &ir.Constraints{MaxItems: new(int64(4))}, alias.Constraints)
	assert.Empty(t, alias.Unmodeled, "a bound the alias holds is not also kept")
}

// TestCollectionBound_BesideARefIn30IsReadAsIn31 pins the dialect the rule
// docs/compilers/openapi.md#ref-use-site reads alike: the bound lands the same
// way, under the warning that says OpenAPI 3.0 ignores it.
func TestCollectionBound_BesideARefIn30IsReadAsIn31(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, openapitest.ComponentSpecVer("3.0.3", tagsSchema+
		"    Holder:\n      type: object\n      properties:\n"+
		"        f: {$ref: '#/components/schemas/Tags', minItems: 1}\n"))
	openapitest.RequireNoErrorDiags(t, diags)

	assert.Equal(t, &ir.Constraints{MinItems: new(int64(1))}, propertyOf(t, doc, "Holder", "f").Constraints)
	assert.True(t, openapitest.HasDiagCodeAt(diags, diag.LaterVersionConstruct, "/components/schemas/Holder/properties/f/minItems"))
}

// TestCollectionBound_ARedeclaredFieldFoldsIt pins the merge over a bound the
// carrier holds (docs/compilers/openapi.md#allof-redeclared-property): as a
// value bound does, it joins the intersection of the declarations, the larger
// minItems and the smaller maxItems in either order, and is not also kept.
func TestCollectionBound_ARedeclaredFieldFoldsIt(t *testing.T) {
	t.Parallel()
	ref := "{$ref: '#/components/schemas/Tags', "
	for _, tc := range []struct {
		name, a, b string
		want       *ir.Constraints
	}{
		{"agreeing", ref + "minItems: 1}", ref + "minItems: 1}", &ir.Constraints{MinItems: new(int64(1))}},
		{"each its own", ref + "minItems: 1}", ref + "maxItems: 2}",
			&ir.Constraints{MinItems: new(int64(1)), MaxItems: new(int64(2))}},
		{"the larger minItems second", ref + "minItems: 1}", ref + "minItems: 2}", &ir.Constraints{MinItems: new(int64(2))}},
		{"the larger minItems first", ref + "minItems: 2}", ref + "minItems: 1}", &ir.Constraints{MinItems: new(int64(2))}},
		{"the smaller maxItems", ref + "maxItems: 5}", ref + "maxItems: 3}", &ir.Constraints{MaxItems: new(int64(3))}},
		{"uniqueItems from either", ref + "minItems: 1}", ref + "uniqueItems: true}",
			&ir.Constraints{MinItems: new(int64(1)), UniqueItems: true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			doc, diags := settledSpec(t, tagsSchema, tc.a, tc.b)

			p := propertyOf(t, doc, "T", "v")
			assert.Equal(t, tc.want, p.Constraints)
			assert.Empty(t, p.Unmodeled, "the fold reads the typed field")
			assert.Empty(t, diags)
		})
	}
}

// TestCollectionBound_ADefaultIsJudgedAgainstTheAliasHoldingIt pins the merge's
// admission reading what the alias now carries: a default the element's bound
// refuses is not taken, and one it admits is.
func TestCollectionBound_ADefaultIsJudgedAgainstTheAliasHoldingIt(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, bound string
		taken       bool
	}{{"refused", "maxItems: 1", false}, {"admitted", "maxItems: 2", true}} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			doc, diags := settledSpec(t, tagsSchema,
				"{type: array, items: {$ref: '#/components/schemas/Tags', "+tc.bound+"}}",
				"{type: array, items: {$ref: '#/components/schemas/Tags'}, default: [[a, b]]}")
			openapitest.RequireNoErrorDiags(t, diags)

			assert.Equal(t, tc.taken, propertyOf(t, doc, "T", "v").Default != nil)
		})
	}
}

// TestCollectionBound_BesideAValueBoundReachesOneConstraints pins the join: a
// position writing both kinds holds both, on one ir.Constraints.
func TestCollectionBound_BesideAValueBoundReachesOneConstraints(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, openapitest.ComponentSpec(tagsSchema+
		"    S: {$ref: '#/components/schemas/Tags', minItems: 1, maxLength: 9}\n"))
	openapitest.RequireNoErrorDiags(t, diags)

	alias, ok := typeByName(doc, "S").(*ir.Scalar)
	require.True(t, ok)
	assert.Equal(t, &ir.Constraints{MinItems: new(int64(1)), MaxLength: new(int64(9))}, alias.Constraints)
}

// TestCollectionBound_ALiteralReadExactlyReachesConstraints pins the spellings
// of a count the parser's model reads as the number they state.
func TestCollectionBound_ALiteralReadExactlyReachesConstraints(t *testing.T) {
	t.Parallel()
	for literal, want := range map[string]int64{
		"0": 0, "1.0": 1, "1e3": 1000, "1e18": 1e18, "0x10": 16, "0o17": 15, "1_000": 1000,
		"9223372036854775807": 9223372036854775807,
	} {
		t.Run(literal, func(t *testing.T) {
			t.Parallel()
			doc, diags := parseFull(t, openapitest.ComponentSpec(tagsSchema+
				"    S: {$ref: '#/components/schemas/Tags', minItems: "+literal+"}\n"))
			openapitest.RequireNoErrorDiags(t, diags)

			alias, ok := typeByName(doc, "S").(*ir.Scalar)
			require.True(t, ok)
			assert.Equal(t, &ir.Constraints{MinItems: &want}, alias.Constraints)
			assert.Empty(t, alias.Unmodeled)
		})
	}
}

// TestCollectionBound_ASpellingOfYAMLReachesConstraints pins the raw-node half of
// the read against the model's: an alias and a merge key state the bound as a
// plain literal does, so neither is left verbatim.
func TestCollectionBound_ASpellingOfYAMLReachesConstraints(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, openapitest.ComponentSpec(tagsSchema+
		"    Anchored: {type: array, items: {type: string}, minItems: &n 2}\n"+
		"    Holder:\n      type: object\n      properties:\n"+
		"        alias: {$ref: '#/components/schemas/Tags', minItems: *n}\n"+
		"        merged: {<<: {minItems: 2}, $ref: '#/components/schemas/Tags'}\n"))
	openapitest.RequireNoErrorDiags(t, diags)

	for _, wire := range []string{"alias", "merged"} {
		p := propertyOf(t, doc, "Holder", wire)
		assert.Equal(t, &ir.Constraints{MinItems: new(int64(2))}, p.Constraints, wire)
		assert.Empty(t, p.Unmodeled, wire)
	}
}

// TestCollectionBound_AReferenceKeepsWhatConstraintsCannotState pins the census
// to what the reader filled, at an alias and at a carrier alike. A count past
// int64 reads as 0 (GitHub #1050), so calling it homed would write a false
// bound over the verbatim one, and a false uniqueItems has no spelling a plain
// bool can tell from unset.
func TestCollectionBound_AReferenceKeepsWhatConstraintsCannotState(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, keywords string
		kept           []string
		held           *ir.Constraints
	}{
		{"uniqueItems false", "uniqueItems: false", []string{"openapi:uniqueItems"}, nil},
		{"integer past int64", "minItems: 99999999999999999999", []string{"openapi:minItems"}, nil},
		{"one past int64", "minItems: 9223372036854775808", []string{"openapi:minItems"}, nil},
		{"exponent past int64", "minItems: 1e19", []string{"openapi:minItems"}, nil},
		{"float past float64", "maxItems: 1e400", []string{"openapi:maxItems"}, nil},
		{"only the unreadable one", "minItems: 1, maxItems: 1e400", []string{"openapi:maxItems"},
			&ir.Constraints{MinItems: new(int64(1))}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			written := "{$ref: '#/components/schemas/Tags', " + tc.keywords + "}"
			doc, diags := parseFull(t, openapitest.ComponentSpec(tagsSchema+
				"    S: "+written+"\n    Holder:\n      type: object\n      properties:\n        f: "+written+"\n"))
			openapitest.RequireNoErrorDiags(t, diags)

			alias, ok := typeByName(doc, "S").(*ir.Scalar)
			require.True(t, ok)
			assert.Equal(t, tc.held, alias.Constraints)
			assert.Equal(t, tc.kept, unmodeledKeys(alias.Unmodeled))
			assert.True(t, openapitest.HasDiagCodeAt(diags, diag.DegradedConstruct, "/components/schemas/S"))

			p := propertyOf(t, doc, "Holder", "f")
			assert.Equal(t, tc.held, p.Constraints)
			assert.Equal(t, tc.kept, unmodeledKeys(p.Unmodeled))
			assert.True(t, openapitest.HasDiagCodeAt(diags, diag.DegradedConstruct, "/components/schemas/Holder/properties/f"))
		})
	}
}

// TestCollectionBound_DecidesWhetherAnAliasDecorates pins ir.Decorated to the
// line the census draws: a bound the alias holds says something of the value,
// and a false uniqueItems, kept verbatim, says nothing.
func TestCollectionBound_DecidesWhetherAnAliasDecorates(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		keyword   string
		decorates bool
	}{{"minItems: 1", false}, {"uniqueItems: true", false}, {"uniqueItems: false", true}} {
		t.Run(tc.keyword, func(t *testing.T) {
			t.Parallel()
			doc, diags := parseFull(t, openapitest.ComponentSpec(tagsSchema+
				"    S: {type: array, items: {$ref: '#/components/schemas/Tags', "+tc.keyword+"}}\n"))
			openapitest.RequireNoErrorDiags(t, diags)

			alias := doc.Types["t/anon/components/schemas/S/items"]
			require.NotNil(t, alias)
			_, decorates := ir.Decorated(alias)
			assert.Equal(t, tc.decorates, decorates)
		})
	}
}

// TestCollectionBound_ACarrierLeavesItToTheNodeItsPositionOwns pins one home per
// declaration: a position that lowered to a collection node holds its bounds
// there, so the carrier restates none.
func TestCollectionBound_ACarrierLeavesItToTheNodeItsPositionOwns(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, openapitest.ComponentSpec(
		"    Holder:\n      type: object\n      properties:\n"+
			"        list: {type: array, items: {type: string}, minItems: 1}\n"+
			"        tuple: {type: array, prefixItems: [{type: string}], maxItems: 3}\n"))
	openapitest.RequireNoErrorDiags(t, diags)

	list, ok := doc.Types["t/anon/components/schemas/Holder/properties/list"].(*ir.List)
	require.True(t, ok)
	assert.Equal(t, &ir.Constraints{MinItems: new(int64(1))}, list.Constraints)
	assert.Nil(t, propertyOf(t, doc, "Holder", "list").Constraints)

	tuple, ok := doc.Types["t/anon/components/schemas/Holder/properties/tuple"].(*ir.Tuple)
	require.True(t, ok)
	assert.Equal(t, &ir.Constraints{MaxItems: new(int64(3))}, tuple.Constraints)
	assert.Nil(t, propertyOf(t, doc, "Holder", "tuple").Constraints)
}

// nodeConstraints returns the Constraints of a node kind with the field, and nil
// for one without.
func nodeConstraints(td ir.TypeDef) *ir.Constraints {
	switch n := td.(type) {
	case *ir.Scalar:
		return n.Constraints
	case *ir.Model:
		return n.Constraints
	case *ir.MapT:
		return n.Constraints
	default:
		return nil
	}
}

// TestCollectionBound_ANodeOfAnotherShapeKeepsItVerbatim pins the positions the
// reference rule does not reach (docs/compilers/openapi.md#shape-applicator): a
// node the position lowered to that holds no collection takes no bound in its
// Constraints, and neither does the carrier beside it.
func TestCollectionBound_ANodeOfAnotherShapeKeepsItVerbatim(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, schema string
		kind         ir.TypeKind
	}{
		{"object", "{type: object, properties: {f: {type: string}}, minItems: 1}", ir.KindModel},
		{"map", "{type: object, additionalProperties: {type: string}, minItems: 1}", ir.KindMap},
		{"string", "{type: string, minItems: 1}", ir.KindScalar},
		{"formatted string", "{type: string, format: date, minItems: 1}", ir.KindScalar},
		{"no type", "{minItems: 1}", ir.KindScalar},
		{"enum", "{enum: [a, b], minItems: 1}", ir.KindEnum},
		{"null-only union", "{oneOf: [{type: 'null'}], minItems: 1}", ir.KindScalar},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			doc, diags := parseFull(t, openapitest.ComponentSpec(
				"    S: "+tc.schema+"\n    Holder:\n      type: object\n      properties:\n        f: "+tc.schema+"\n"))
			openapitest.RequireNoErrorDiags(t, diags)

			for _, td := range []ir.TypeDef{typeByName(doc, "S"), doc.Types["t/anon/components/schemas/Holder/properties/f"]} {
				require.NotNil(t, td)
				assert.Equal(t, tc.kind, td.Kind())
				assert.Contains(t, td.Common().Unmodeled, "openapi:minItems")
				assert.Nil(t, nodeConstraints(td), "the node holds no bound it also keeps")
			}
			assert.Nil(t, propertyOf(t, doc, "Holder", "f").Constraints)
		})
	}
}

// TestCollectionBound_ARefNamingThePositionIsOrderInvariant pins the alias a
// $ref hoists at a property's pointer. It is a node of its own, so it holds the
// bound as the property does, whichever declaration lowered first, and the
// property is not left reading it as its own node.
func TestCollectionBound_ARefNamingThePositionIsOrderInvariant(t *testing.T) {
	t.Parallel()
	holder := "    Holder:\n      type: object\n      properties:\n        beside: " + boundedRef + "\n"
	named := "    Named: {$ref: '#/components/schemas/Holder/properties/beside'}\n"
	docs := make([]*ir.Document, 0, 2)
	for _, schemas := range []string{tagsSchema + holder + named, named + holder + tagsSchema} {
		doc, diags := parseFull(t, openapitest.ComponentSpec(schemas))
		openapitest.RequireNoErrorDiags(t, diags)

		assert.Equal(t, allBounds(), propertyOf(t, doc, "Holder", "beside").Constraints)
		alias, ok := doc.Types["t/anon/components/schemas/Holder/properties/beside"].(*ir.Scalar)
		require.True(t, ok, "the $ref hoists an alias at the property's pointer")
		assert.Equal(t, allBounds(), alias.Constraints)
		docs = append(docs, doc)
	}
	assert.Empty(t, cmp.Diff(docs[0].Types, docs[1].Types, orderInvariantIR()...))
}
