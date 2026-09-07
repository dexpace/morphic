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

// readOnlyVisibility and writeOnlyVisibility are the two non-empty shapes
// EffectiveVisibility ever produces, reused across the visibility-merge
// cases below rather than re-spelling the lifecycle lists in each one.
var (
	readOnlyVisibility  = ir.Visibility{Only: []ir.Lifecycle{ir.LifecycleRead, ir.LifecycleDelete, ir.LifecycleQuery}}
	writeOnlyVisibility = ir.Visibility{Only: []ir.Lifecycle{ir.LifecycleCreate, ir.LifecycleUpdate}}
)

// TestMergeVisibility_IntersectsUnderAllOfSemantics pins the shapes an allOf
// redeclaration's Visibility pairing can take. An empty Only means
// unrestricted rather than restricted-to-nothing (ir-design §5.2), so the
// merge has to read each side as the set it admits before intersecting.
//
// A plain append is the tempting wrong answer, and the first three cases here
// do not catch it: concatenating an empty Only is a no-op, so every pairing
// where at most one side restricts comes out right by accident. It is the five
// below them that carry the argument — appending duplicates when both branches
// agree, unions where allOf intersects, and cannot spell the empty set at all.
func TestMergeVisibility_IntersectsUnderAllOfSemantics(t *testing.T) {
	t.Parallel()
	unrestricted := ir.Visibility{}
	invisible := ir.Visibility{None: true}

	tests := []struct {
		name     string
		dst, src ir.Visibility
		want     ir.Visibility
	}{
		{
			name: "neither branch restricts visibility",
			dst:  unrestricted, src: unrestricted,
			want: unrestricted,
		},
		{
			// The issue's own reproduction: a first branch that leaves the
			// field unrestricted must not shadow a later branch's readOnly.
			name: "only the redeclaration restricts visibility",
			dst:  unrestricted, src: readOnlyVisibility,
			want: readOnlyVisibility,
		},
		{
			name: "only the first declaration restricts visibility",
			dst:  readOnlyVisibility, src: unrestricted,
			want: readOnlyVisibility,
		},
		{
			name: "both branches agree on the same restriction",
			dst:  readOnlyVisibility, src: readOnlyVisibility,
			want: readOnlyVisibility,
		},
		{
			// readOnly admits {read, delete, query}; writeOnly admits
			// {create, update} — disjoint sets whose intersection is empty,
			// meaning the field can satisfy no lifecycle both branches
			// admit. None is the IR's existing shape for "visible in no
			// lifecycle" (TypeSpec @invisible), so the empty intersection is
			// recorded exactly rather than read as unrestricted.
			name: "disjoint restrictions intersect to invisible",
			dst:  readOnlyVisibility, src: writeOnlyVisibility,
			want: invisible,
		},
		{
			// Neither side is unrestricted and neither subsumes the other, so
			// the answer is a proper subset of both — the one shape the fast
			// paths above cannot produce, and the only case that pins the
			// intersection itself rather than which operand survives whole.
			// EffectiveVisibility restricts to two fixed sets that are either
			// identical or disjoint, so no OpenAPI document reaches this pairing
			// today; it is the helper's contract a second visibility source
			// would inherit.
			name: "partially overlapping restrictions keep only the shared lifecycles",
			dst:  readOnlyVisibility, src: ir.Visibility{Only: []ir.Lifecycle{ir.LifecycleRead, ir.LifecycleQuery}},
			want: ir.Visibility{Only: []ir.Lifecycle{ir.LifecycleRead, ir.LifecycleQuery}},
		},
		{
			name: "None on the first declaration dominates",
			dst:  invisible, src: readOnlyVisibility,
			want: invisible,
		},
		{
			name: "None on the redeclaration dominates",
			dst:  readOnlyVisibility, src: invisible,
			want: invisible,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, mergeVisibility(tc.dst, tc.src),
				"mergeVisibility(dst, src) for %q", tc.name)
			assert.Equal(t, tc.want, mergeVisibility(tc.src, tc.dst),
				"mergeVisibility(src, dst) must agree — allOf branch order is not semantic, for %q", tc.name)
		})
	}
}

// TestMergeVisibility_ReorderedBranchesAgree pins the one way two branches can
// disagree that the table above cannot express: they admit lifecycles that
// overlap, but list them in different orders. Every case in the table names its
// operands in one order, so a merge that read the surviving order off dst alone
// would satisfy all of them and still answer two different slices here.
//
// The exact order the merge settles on is not pinned — a set has no preferred
// spelling — only that both branch orders reach the same one, which is what
// keeps the composition's spelling out of the IR.
func TestMergeVisibility_ReorderedBranchesAgree(t *testing.T) {
	t.Parallel()
	first := readOnlyVisibility
	second := ir.Visibility{Only: []ir.Lifecycle{ir.LifecycleQuery, ir.LifecycleRead}}

	forward := mergeVisibility(first, second)
	reversed := mergeVisibility(second, first)

	assert.Equal(t, forward, reversed, "allOf branch order must not reach the merged Only")
	assert.ElementsMatch(t, []ir.Lifecycle{ir.LifecycleRead, ir.LifecycleQuery}, forward.Only,
		"the merge is still the intersection, whichever order it is spelled in")
	assert.False(t, forward.None, "a non-empty intersection is a restriction, not invisibility")
}

// TestReconcileProperty_VisibilityAdoptedFromRedeclaration pins the issue's
// own reproduction at the reconcile layer (GitHub #34): a property left
// unrestricted by its first declaration must adopt a later branch's readOnly
// rather than silently keep the first, empty Visibility.
func TestReconcileProperty_VisibilityAdoptedFromRedeclaration(t *testing.T) {
	t.Parallel()
	g, _ := stubMerger(nil)
	dst := ir.Property{WireName: "id"}

	g.reconcileProperty(&dst, ir.Property{WireName: "id", Visibility: readOnlyVisibility}, "/other")

	assert.Equal(t, readOnlyVisibility, dst.Visibility)
}

// TestReconcileProperty_DisjointVisibilityIsARestrictionNotAConflict pins both
// halves of the choice for an allOf pairing that admits no lifecycle at all
// (readOnly against writeOnly on the same field). mergeVisibility represents it
// exactly as None, so it is recorded rather than routed through
// diagnoseRedeclarationConflict — nothing is arbitrarily discarded, as it would
// be for an incompatible-type redeclaration. But exact is not unremarkable: a
// field neither a request nor a response can carry is a composition that cannot
// take effect, so it is warned about under its own code.
func TestReconcileProperty_DisjointVisibilityIsARestrictionNotAConflict(t *testing.T) {
	t.Parallel()
	g, recorded := stubMerger(nil)
	dst := ir.Property{WireName: "id", Visibility: readOnlyVisibility}

	g.reconcileProperty(&dst, ir.Property{WireName: "id", Visibility: writeOnlyVisibility}, "/other")

	assert.Equal(t, ir.Visibility{None: true}, dst.Visibility)
	require.Len(t, *recorded, 1, "an emptied visibility set is reported, not passed over in silence")
	assert.Equal(t, diag.DisjointVisibility, (*recorded)[0].Code,
		"reported under its own code, not as a conflicting redeclaration")
	assert.Equal(t, ir.SeverityWarning, (*recorded)[0].Severity)
}

// TestReconcileProperty_AlreadyInvisibleVisibilityIsNotReAnnounced pins the half
// of the report guard the disjoint case above cannot reach. The warning is about
// the transition — two restricted branches whose intersection empties — not
// about the state of being invisible, so a side that was already None merges
// quietly. Drop that half of the condition and every later redeclaration of an
// invisible field re-announces a fact the document has already been told, which
// no assertion in the disjoint case would notice.
func TestReconcileProperty_AlreadyInvisibleVisibilityIsNotReAnnounced(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		dst, src ir.Visibility
	}{
		{name: "first declaration was already invisible", dst: ir.Visibility{None: true}, src: readOnlyVisibility},
		{name: "redeclaration is already invisible", dst: readOnlyVisibility, src: ir.Visibility{None: true}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			g, recorded := stubMerger(nil)
			dst := ir.Property{WireName: "id", Visibility: tc.dst}

			g.reconcileProperty(&dst, ir.Property{WireName: "id", Visibility: tc.src}, "/other")

			assert.Equal(t, ir.Visibility{None: true}, dst.Visibility)
			assert.Empty(t, *recorded, "None was already the answer; this merge announced nothing new")
		})
	}
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

// TestBigValConflictDetail_ComparesByMagnitude pins the comparison every
// arbitrary-precision keyword goes through. Equal magnitudes spelled
// differently must not read as a conflict — 10 and 10.0 are one value, and
// reporting them would invent a disagreement the source never wrote — while
// differing magnitudes must, since the merge keeps one and drops the other.
//
// The keyword is a parameter, so the name in the message is the one the caller
// passed: the four bounds and multipleOf share this helper, and a hard-coded
// name would report every one of them as the same keyword.
func TestBigValConflictDetail_ComparesByMagnitude(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		keyword string
		a, b    *ir.BigVal
		want    string
	}{
		{name: "only one side bounds", keyword: "minimum", a: bigVal("10")},
		{name: "neither side bounds", keyword: "minimum"},
		{
			name:    "the same magnitude, spelled differently",
			keyword: "minimum", a: bigVal("10"), b: bigVal("10.0"),
		},
		{
			name: "differing magnitudes", keyword: "minimum", a: bigVal("10"), b: bigVal("20"),
			want: "conflicting minimum (10 and 20)",
		},
		{
			name:    "the exclusive bound reports under its own keyword",
			keyword: "exclusiveMinimum", a: bigVal("10"), b: bigVal("20"),
			want: "conflicting exclusiveMinimum (10 and 20)",
		},
		{
			name:    "multipleOf shares the comparison",
			keyword: "multipleOf", a: bigVal("3"), b: bigVal("5"),
			want: "conflicting multipleOf (3 and 5)",
		},
		{
			name:    "unparseable operands compare exactly",
			keyword: "minimum", a: bigVal("nan"), b: bigVal("other"),
			want: "conflicting minimum (nan and other)",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			detail, ok := bigValConflictDetail(tc.keyword, tc.a, tc.b)
			assert.Equal(t, tc.want != "", ok)
			assert.Equal(t, tc.want, detail)
		})
	}
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

// TestKeepLosingType_RecordsTheDiscardedDeclaration pins what the fix for
// GitHub #424 adds: the redeclaration whose type loses is kept verbatim beside
// the winner, so a consumer reading the document — not the diagnostic stream —
// still sees what the second declaration said.
//
// The reference is asserted whole rather than by its target alone: Nullable is
// half of what a redeclaration says about a field, and an entry that kept only
// the ID would lose it while still looking like a preservation.
func TestKeepLosingType_RecordsTheDiscardedDeclaration(t *testing.T) {
	t.Parallel()
	g, recorded := stubMerger(map[ir.TypeID]ir.TypeDef{
		"t/str": &ir.Primitive{TypeCommon: ir.TypeCommon{ID: "t/str"}, Prim: ir.PrimString},
		"t/int": &ir.Primitive{TypeCommon: ir.TypeCommon{ID: "t/int"}, Prim: ir.PrimInt32},
	})
	dst := ir.Property{
		WireName:   "id",
		Type:       ir.TypeRef{Target: "t/str"},
		Provenance: ir.Provenance{Source: 2, Pointer: "/components/schemas/S/allOf/0/properties/id"},
	}
	src := ir.Property{
		WireName:   "id",
		Type:       ir.TypeRef{Target: "t/int", Nullable: true},
		Provenance: ir.Provenance{Source: 2, Pointer: "/components/schemas/S/allOf/1/properties/id"},
	}

	g.reconcileProperty(&dst, src, "/components/schemas/S/allOf/1/properties/id")

	require.Len(t, *recorded, 1, "the conflict is still diagnosed")
	assert.Equal(t, ir.TypeID("t/str"), dst.Type.Target, "the first declaration still wins the shape")

	key := "openapi:conflicting-redeclaration/components/schemas/S/allOf/1/properties/id"
	entry, ok := dst.Unmodeled[key]
	require.True(t, ok, "the losing declaration is kept under a pointer-namespaced key; got %v", dst.Unmodeled)
	assert.Equal(t, ir.ReasonDegradedLowering, entry.Reason)
	assert.JSONEq(t, `{"target":"t/int","nullable":true}`, string(entry.Value),
		"the whole reference is kept, nullability included")
	assert.Equal(t, ir.Provenance{Source: 2, Pointer: "/components/schemas/S/allOf/1/properties/id"},
		entry.Provenance, "the entry locates the losing declaration, not the merged property")
}

// TestKeepLosingType_EveryLoserSurvivesItsSiblings pins the key's namespacing.
// A field three branches type three incompatible ways discards two
// declarations, and a fixed key would leave only whichever branch ran last —
// the same silent overwrite the entry exists to prevent.
func TestKeepLosingType_EveryLoserSurvivesItsSiblings(t *testing.T) {
	t.Parallel()
	g, _ := stubMerger(map[ir.TypeID]ir.TypeDef{
		"t/str":  &ir.Primitive{TypeCommon: ir.TypeCommon{ID: "t/str"}, Prim: ir.PrimString},
		"t/int":  &ir.Primitive{TypeCommon: ir.TypeCommon{ID: "t/int"}, Prim: ir.PrimInt32},
		"t/bool": &ir.Primitive{TypeCommon: ir.TypeCommon{ID: "t/bool"}, Prim: ir.PrimBool},
	})
	m := &ir.Model{}
	byWire := WireNameIndex(m.Properties)

	g.MergeProperty(m, byWire, ir.Property{WireName: "id", Type: ir.TypeRef{Target: "t/str"}}, "/a")
	g.MergeProperty(m, byWire, ir.Property{WireName: "id", Type: ir.TypeRef{Target: "t/int"}}, "/b")
	g.MergeProperty(m, byWire, ir.Property{WireName: "id", Type: ir.TypeRef{Target: "t/bool"}}, "/c")

	require.Len(t, m.Properties, 1, "three declarations still reconcile to one property")
	assert.Equal(t, ir.Unmodeled{
		"openapi:conflicting-redeclaration/b": {
			Reason:     ir.ReasonDegradedLowering,
			Value:      ir.RawValue(`{"target":"t/int","nullable":false}`),
			Provenance: ir.Provenance{Pointer: "/b"},
		},
		"openapi:conflicting-redeclaration/c": {
			Reason:     ir.ReasonDegradedLowering,
			Value:      ir.RawValue(`{"target":"t/bool","nullable":false}`),
			Provenance: ir.Provenance{Pointer: "/c"},
		},
	}, m.Properties[0].Unmodeled)
}

// TestKeepLosingType_LeavesAgreeingAndConstraintOnlyConflictsAlone pins the two
// sides the entry must not appear on. Agreeing declarations discard nothing, so
// an entry would be noise; a constraint conflict does discard the
// redeclaration's keyword, but the recorded direction there is to intersect the
// bounds rather than preserve the loser (GitHub #10), so preserving it here
// would settle a decision that already has one.
func TestKeepLosingType_LeavesAgreeingAndConstraintOnlyConflictsAlone(t *testing.T) {
	t.Parallel()
	g, recorded := stubMerger(map[ir.TypeID]ir.TypeDef{
		"t/str": &ir.Primitive{TypeCommon: ir.TypeCommon{ID: "t/str"}, Prim: ir.PrimString},
	})
	ten, twenty := int64(10), int64(20)
	agreeing := ir.Property{WireName: "id", Type: ir.TypeRef{Target: "t/str"}}

	g.reconcileProperty(&agreeing, ir.Property{WireName: "id", Type: ir.TypeRef{Target: "t/str"}}, "/b")

	assert.Empty(t, *recorded, "agreeing declarations are not a conflict")
	assert.Empty(t, agreeing.Unmodeled, "and nothing was discarded to keep")

	bounded := ir.Property{
		WireName: "code", Type: ir.TypeRef{Target: "t/str"},
		Constraints: &ir.Constraints{MaxLength: &ten},
	}
	g.reconcileProperty(&bounded, ir.Property{
		WireName: "code", Type: ir.TypeRef{Target: "t/str"},
		Constraints: &ir.Constraints{MaxLength: &twenty},
	}, "/b")

	require.Len(t, *recorded, 1, "the constraint conflict is still diagnosed")
	assert.Equal(t, diag.ConflictingRedecl, (*recorded)[0].Code)
	assert.Empty(t, bounded.Unmodeled, "a constraint conflict keeps no type entry")
}
