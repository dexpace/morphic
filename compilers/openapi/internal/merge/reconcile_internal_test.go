package merge

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/ir"
)

// bigVal returns a pointer to v, which the optional constraint bounds take.
func bigVal(v string) *ir.BigVal {
	b := ir.BigVal(v)
	return &b
}

// TestWireNameIndex_KeysEveryPropertyByItsWireName pins the index a
// redeclaration is found through. A property missing from it would be appended a
// second time rather than reconciled, leaving one wire name declared twice in
// one model.
func TestWireNameIndex_KeysEveryPropertyByItsWireName(t *testing.T) {
	t.Parallel()
	got := WireNameIndex([]ir.Property{{WireName: "id"}, {WireName: "name"}, {WireName: "age"}})

	assert.Equal(t, map[string]int{"id": 0, "name": 1, "age": 2}, got)
	assert.Empty(t, WireNameIndex(nil), "no properties index to nothing")
}

// TestMergeProperty_AppendsThenFolds pins the two outcomes the entry point
// chooses between: a wire name not yet present is appended and indexed, and one
// already present is folded into the property that holds it rather than
// appended beside it.
func TestMergeProperty_AppendsThenFolds(t *testing.T) {
	t.Parallel()
	g, recorded := stubMerger(nil)
	m := &ir.Model{}
	byWire := WireNameIndex(m.Properties)

	g.MergeProperty(m, byWire, ir.Property{WireName: "id", Required: true}, "/p")
	g.MergeProperty(m, byWire, ir.Property{WireName: "name"}, "/p")

	require.Len(t, m.Properties, 2, "two distinct wire names are two properties")
	assert.Equal(t, map[string]int{"id": 0, "name": 1}, byWire)

	g.MergeProperty(m, byWire, ir.Property{WireName: "id", Secret: true}, "/other")

	require.Len(t, m.Properties, 2, "a redeclaration folds rather than appending")
	assert.True(t, m.Properties[0].Required, "required is kept from the first declaration")
	assert.True(t, m.Properties[0].Secret, "and OR-ed with the redeclaration's")
	assert.Empty(t, *recorded, "agreeing declarations are not a conflict")
}

// TestReconcileProperty_DifferingDescriptionsKeepTheFirst pins the one merge
// outcome that is reported without being a conflict: two branches describing the
// same field differently cannot both be kept, so the first wins and the loss is
// announced rather than silent.
func TestReconcileProperty_DifferingDescriptionsKeepTheFirst(t *testing.T) {
	t.Parallel()
	g, recorded := stubMerger(nil)
	dst := ir.Property{WireName: "id", Docs: ir.Docs{Description: "first"}}

	g.reconcileProperty(&dst, ir.Property{WireName: "id", Docs: ir.Docs{Description: "second"}}, "/other")

	assert.Equal(t, "first", dst.Docs.Description)
	require.Len(t, *recorded, 1)
	assert.Equal(t, ir.SeverityInfo, (*recorded)[0].Severity)
	assert.Equal(t, diag.DegradedConstruct, (*recorded)[0].Code)
	assert.Contains(t, (*recorded)[0].Message, `"id"`)
}

// TestReconcileProperty_AnIdenticalDescriptionIsNotADisagreement pins the other
// side of that check: repeating the same description in both branches is one
// construct written twice, not two that disagree.
func TestReconcileProperty_AnIdenticalDescriptionIsNotADisagreement(t *testing.T) {
	t.Parallel()
	g, recorded := stubMerger(nil)
	dst := ir.Property{WireName: "id", Docs: ir.Docs{Description: "same"}}

	g.reconcileProperty(&dst, ir.Property{WireName: "id", Docs: ir.Docs{Description: "same"}}, "/other")

	assert.Equal(t, "same", dst.Docs.Description)
	assert.Empty(t, *recorded)
}

// TestDiagnoseRedeclarationConflict_ConstraintDisagreementIsReported pins the
// second of the two conflict routes. Two branches that agree on type but pin one
// keyword to different values produce a merge that keeps dst's bound, so the
// stricter one is discarded and must be announced.
func TestDiagnoseRedeclarationConflict_ConstraintDisagreementIsReported(t *testing.T) {
	t.Parallel()
	g, recorded := stubMerger(nil)
	dst := ir.Property{
		WireName:    "age",
		Provenance:  ir.Provenance{Pointer: "/components/schemas/A/properties/age"},
		Constraints: &ir.Constraints{Min: bigVal("10")},
	}
	src := ir.Property{WireName: "age", Constraints: &ir.Constraints{Min: bigVal("20")}}

	g.diagnoseRedeclarationConflict(&dst, &src, "/components/schemas/B/properties/age")

	require.Len(t, *recorded, 1)
	assert.Equal(t, ir.SeverityWarning, (*recorded)[0].Severity)
	assert.Equal(t, diag.ConflictingRedecl, (*recorded)[0].Code)
	assert.Contains(t, (*recorded)[0].Message, "conflicting minimum (10 and 20)")
	assert.Contains(t, (*recorded)[0].Message, "/components/schemas/A/properties/age",
		"the kept declaration is named")
	assert.Contains(t, (*recorded)[0].Message, "/components/schemas/B/properties/age",
		"and so is the discarded one")
}

// TestBoundConflictDetail_ComparesMagnitudeAndSense pins both halves of a bound
// comparison. Equal magnitudes spelled differently must not read as a conflict,
// while the same magnitude under a differing exclusivity flag must — "> 10" and
// ">= 10" are different bounds, and the merge can only keep one.
func TestBoundConflictDetail_ComparesMagnitudeAndSense(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name         string
		a, b         *ir.BigVal
		exclA, exclB bool
		want         string
	}{
		{name: "only one side bounds", a: bigVal("10"), exclA: true},
		{name: "neither side bounds"},
		{name: "the same magnitude, spelled differently", a: bigVal("10"), b: bigVal("10.0")},
		{
			name: "differing magnitudes", a: bigVal("10"), b: bigVal("20"),
			want: "conflicting minimum (10 and 20)",
		},
		{
			name: "the same magnitude, differing sense", a: bigVal("10"), b: bigVal("10"), exclB: true,
			want: "conflicting minimum (10 and exclusive 10)",
		},
		{
			name: "unparseable operands compare exactly", a: bigVal("nan"), b: bigVal("other"),
			want: "conflicting minimum (nan and other)",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			detail, ok := boundConflictDetail("minimum", tc.a, tc.b, tc.exclA, tc.exclB)
			assert.Equal(t, tc.want != "", ok)
			assert.Equal(t, tc.want, detail)
		})
	}
}

// TestMultipleOfConflictDetail_ComparesByMagnitude pins the plain numeric
// comparison multipleOf goes through: 10 and 10.0 are one value, so reporting
// them as a conflict would invent a disagreement the source never wrote.
func TestMultipleOfConflictDetail_ComparesByMagnitude(t *testing.T) {
	t.Parallel()
	_, ok := multipleOfConflictDetail(bigVal("1e1"), bigVal("10"))
	assert.False(t, ok, "equal magnitudes spelled differently do not conflict")

	_, ok = multipleOfConflictDetail(nil, bigVal("10"))
	assert.False(t, ok, "a keyword only one branch sets is adopted, not a conflict")

	detail, ok := multipleOfConflictDetail(bigVal("3"), bigVal("5"))
	assert.True(t, ok)
	assert.Equal(t, "conflicting multipleOf (3 and 5)", detail)
}

// TestResolvePrimKind_EnumResolvesThroughItsValueType pins the enum case of the
// resolution walk. An enum member can itself be a scalar of the kind being
// redeclared, so it must answer with its value type rather than staying
// unresolved and falling through to the structural comparison.
func TestResolvePrimKind_EnumResolvesThroughItsValueType(t *testing.T) {
	t.Parallel()
	g, _ := stubMerger(map[ir.TypeID]ir.TypeDef{
		"t/enum": &ir.Enum{TypeCommon: ir.TypeCommon{ID: "t/enum"}, ValueType: ir.PrimString},
		"t/alias": &ir.Scalar{
			TypeCommon: ir.TypeCommon{ID: "t/alias"},
			Base:       &ir.TypeRef{Target: "t/enum"},
		},
	})

	kind, ok := g.resolvePrimKind(ir.TypeRef{Target: "t/enum"})
	require.True(t, ok)
	assert.Equal(t, ir.PrimString, kind)

	kind, ok = g.resolvePrimKind(ir.TypeRef{Target: "t/alias"})
	require.True(t, ok, "a scalar's Base chain is followed to the enum behind it")
	assert.Equal(t, ir.PrimString, kind)
}

// TestDifferentTypeKind_ComparesResolvedKinds pins the last-resort comparison
// typesConflict falls back to when neither side resolves a primitive. Two
// composites of the same kind are not provably contradictory; two of different
// kinds are.
func TestDifferentTypeKind_ComparesResolvedKinds(t *testing.T) {
	t.Parallel()
	g, _ := stubMerger(map[ir.TypeID]ir.TypeDef{
		"t/model":  &ir.Model{TypeCommon: ir.TypeCommon{ID: "t/model"}},
		"t/other":  &ir.Model{TypeCommon: ir.TypeCommon{ID: "t/other"}},
		"t/union":  &ir.Union{TypeCommon: ir.TypeCommon{ID: "t/union"}},
		"t/opaque": &ir.Scalar{TypeCommon: ir.TypeCommon{ID: "t/opaque"}},
	})

	assert.False(t, g.differentTypeKind(ir.TypeRef{Target: "t/model"}, ir.TypeRef{Target: "t/other"}),
		"two models are the same kind, so not provably contradictory")
	assert.True(t, g.differentTypeKind(ir.TypeRef{Target: "t/model"}, ir.TypeRef{Target: "t/union"}),
		"a model and a union are different kinds")
	assert.True(t, g.typesConflict(ir.TypeRef{Target: "t/opaque"}, ir.TypeRef{Target: "t/union"}),
		"and typesConflict reaches that comparison when neither side is a primitive")
}
