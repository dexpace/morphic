package merge

import (
	"encoding/json/jsontext"
	"errors"
	"fmt"
	"slices"
	"testing"

	"github.com/google/go-cmp/cmp"
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
	byWire := NewPropIndex()

	g.MergeProperty(m, byWire, ir.Property{WireName: "id", Required: true, Provenance: ptrAt("/p")}, unread(t))
	g.MergeProperty(m, byWire, ir.Property{WireName: "name", Provenance: ptrAt("/p")}, unread(t))

	require.Len(t, m.Properties, 2, "two distinct wire names are two properties")
	assert.Equal(t, 0, byWire.byWire["id"].pos)
	assert.Equal(t, 1, byWire.byWire["name"].pos)

	g.MergeProperty(m, byWire, ir.Property{WireName: "id", Secret: true, Provenance: ptrAt("/other")}, unread(t))

	require.Len(t, m.Properties, 2, "a redeclaration folds rather than appending")
	assert.True(t, m.Properties[0].Required, "required is kept from the first declaration")
	assert.True(t, m.Properties[0].Secret, "and OR-ed with the redeclaration's")
	assert.Empty(t, *recorded, "agreeing declarations are not a conflict")
}

// TestReconcileProperty_DifferingDescriptionsKeepTheFirst pins a merge outcome
// that is reported without being a conflict: two branches describing the same
// field differently cannot both be kept, so the first wins, the loss is
// announced rather than silent, and the losing declaration is kept whole.
func TestReconcileProperty_DifferingDescriptionsKeepTheFirst(t *testing.T) {
	t.Parallel()
	g, recorded := stubMerger(nil)
	dst := ir.Property{WireName: "id", Docs: ir.Docs{Description: "first"}}

	g.reconcileProperty(&dst, new(origins), ir.Property{WireName: "id", Docs: ir.Docs{Description: "second"}, Provenance: ptrAt("/other")},
		written(`{"description":"second"}`))

	assert.Equal(t, "first", dst.Docs.Description)
	require.Len(t, *recorded, 1)
	assert.Equal(t, ir.SeverityInfo, (*recorded)[0].Severity)
	assert.Equal(t, diag.DegradedConstruct, (*recorded)[0].Code)
	assert.Contains(t, (*recorded)[0].Message, `"id"`)
	assert.Contains(t, (*recorded)[0].Message, "description")
	assert.Equal(t, ir.RawValue(`{"description":"second"}`), dst.Unmodeled[losingDeclarationKey+"/other"].Value)
}

// TestReconcileProperty_AnIdenticalDescriptionIsNotADisagreement pins the other
// side of that check: repeating the same description in both branches is one
// construct written twice, not two that disagree.
func TestReconcileProperty_AnIdenticalDescriptionIsNotADisagreement(t *testing.T) {
	t.Parallel()
	g, recorded := stubMerger(nil)
	dst := ir.Property{WireName: "id", Docs: ir.Docs{Description: "same"}}

	g.reconcileProperty(&dst, new(origins), ir.Property{WireName: "id", Docs: ir.Docs{Description: "same"}, Provenance: ptrAt("/other")}, unread(t))

	assert.Equal(t, "same", dst.Docs.Description)
	assert.Empty(t, *recorded)
}

// TestReconcileProperty_TitleAndExternalDocsFoldLikeDescription pins that a
// summary and external links fold as a description does: an empty side adopts
// the other, equal sides are one construct written twice, and differing sides
// keep the first and the redeclaration whole, in either declaration order.
func TestReconcileProperty_TitleAndExternalDocsFoldLikeDescription(t *testing.T) {
	t.Parallel()
	linkA := []ir.Link{{URL: "https://a.example"}}
	linkB := []ir.Link{{URL: "https://b.example"}}
	summary := func(s string) ir.Docs { return ir.Docs{Summary: s} }
	links := func(l []ir.Link) ir.Docs { return ir.Docs{ExternalDocs: l} }

	tests := []struct {
		name     string
		dst, src ir.Docs
		want     ir.Docs
		detail   string // empty when nothing is lost
	}{
		{"summary, dst empty", summary(""), summary("B"), summary("B"), ""},
		{"summary, src empty", summary("A"), summary(""), summary("A"), ""},
		{"summary, equal", summary("A"), summary("A"), summary("A"), ""},
		{"summary, different", summary("A"), summary("B"), summary("A"), "title"},
		{"summary, different swapped", summary("B"), summary("A"), summary("B"), "title"},
		{"external docs, dst empty", links(nil), links(linkB), links(linkB), ""},
		{"external docs, src empty", links(linkA), links(nil), links(linkA), ""},
		{"external docs, equal", links(linkA), links(linkA), links(linkA), ""},
		{"external docs, different", links(linkA), links(linkB), links(linkA), "external docs"},
		{"external docs, different swapped", links(linkB), links(linkA), links(linkB), "external docs"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			g, recorded := stubMerger(nil)
			dst := ir.Property{WireName: "id", Docs: tt.dst}
			src := ir.Property{WireName: "id", Docs: tt.src, Provenance: ptrAt("/other")}
			source := unread(t)
			if tt.detail != "" {
				source = written(`{"x":1}`)
			}

			g.reconcileProperty(&dst, new(origins), src, source)

			assert.Empty(t, cmp.Diff(tt.want, dst.Docs))
			if tt.detail == "" {
				assert.Empty(t, *recorded)
				assert.Empty(t, dst.Unmodeled)
				return
			}
			require.Len(t, *recorded, 1)
			assert.Equal(t, ir.SeverityInfo, (*recorded)[0].Severity)
			assert.Equal(t, diag.DegradedConstruct, (*recorded)[0].Code)
			assert.Equal(t, jsontext.Pointer("/other"), (*recorded)[0].Provenance.Pointer)
			assert.Contains(t, (*recorded)[0].Message, `"id"`)
			assert.Contains(t, (*recorded)[0].Message, tt.detail)
			require.Len(t, dst.Unmodeled, 1)
			entry, ok := dst.Unmodeled[losingDeclarationKey+"/other"]
			require.True(t, ok)
			assert.Equal(t, ir.ReasonDegradedLowering, entry.Reason)
			assert.Equal(t, ir.RawValue(`{"x":1}`), entry.Value)
		})
	}
}

// TestReconcileProperty_EveryDocsDisagreementIsReported pins that foldDocs
// judges each docs field rather than stopping at the first that differs: a
// redeclaration giving all three differently is reported three times, and
// kept once.
func TestReconcileProperty_EveryDocsDisagreementIsReported(t *testing.T) {
	t.Parallel()
	g, recorded := stubMerger(nil)
	first := ir.Docs{Summary: "A", Description: "dA", ExternalDocs: []ir.Link{{URL: "https://a.example"}}}
	dst := ir.Property{WireName: "id", Docs: first}
	src := ir.Property{WireName: "id", Provenance: ptrAt("/other"),
		Docs: ir.Docs{Summary: "B", Description: "dB", ExternalDocs: []ir.Link{{URL: "https://b.example"}}}}

	g.reconcileProperty(&dst, new(origins), src, written(`{"x":1}`))

	assert.Empty(t, cmp.Diff(first, dst.Docs))
	require.Len(t, *recorded, 3)
	for i, detail := range []string{"title", "description", "external docs"} {
		assert.Contains(t, (*recorded)[i].Message, detail)
	}
	require.Len(t, dst.Unmodeled, 1)
	assert.Equal(t, ir.RawValue(`{"x":1}`), dst.Unmodeled[losingDeclarationKey+"/other"].Value)
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
// unrestricted, not restricted-to-nothing (ir-design §5.2), so each side must
// be read as the set it admits before intersecting.
//
// The first three cases cannot catch a plain append, since concatenating an
// empty Only is a no-op. The five below them do: appending duplicates when both
// branches agree, unions where allOf intersects, and cannot spell the empty
// set.
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

	g.reconcileProperty(&dst, new(origins), ir.Property{WireName: "id", Visibility: readOnlyVisibility, Provenance: ptrAt("/other")}, unread(t))

	assert.Equal(t, readOnlyVisibility, dst.Visibility)
}

// TestReconcileProperty_DisjointVisibilityIsARestrictionNotAConflict pins both
// halves of the choice for an allOf pairing that admits no lifecycle at all
// (readOnly against writeOnly on the same field). mergeVisibility represents it
// exactly as None, so it is recorded rather than routed through
// recordRedeclarationConflict — nothing is arbitrarily discarded, as it would
// be for an incompatible-type redeclaration. But exact is not unremarkable: a
// field neither a request nor a response can carry is a composition that cannot
// take effect, so it is warned about under its own code.
func TestReconcileProperty_DisjointVisibilityIsARestrictionNotAConflict(t *testing.T) {
	t.Parallel()
	g, recorded := stubMerger(nil)
	dst := ir.Property{WireName: "id", Visibility: readOnlyVisibility}

	g.reconcileProperty(&dst, new(origins), ir.Property{WireName: "id", Visibility: writeOnlyVisibility, Provenance: ptrAt("/other")}, unread(t))

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

			g.reconcileProperty(&dst, new(origins), ir.Property{WireName: "id", Visibility: tc.src, Provenance: ptrAt("/other")}, unread(t))

			assert.Equal(t, ir.Visibility{None: true}, dst.Visibility)
			assert.Empty(t, *recorded, "None was already the answer; this merge announced nothing new")
		})
	}
}

// TestRecordRedeclarationConflict_ConstraintDisagreementIsReported pins the
// second of the two conflict routes. Two branches that agree on type but pin one
// keyword to different values produce a merge that keeps dst's bound, so the
// stricter one is discarded and must be announced.
func TestRecordRedeclarationConflict_ConstraintDisagreementIsReported(t *testing.T) {
	t.Parallel()
	g, recorded := stubMerger(nil)
	dst := ir.Property{
		WireName:    "age",
		Provenance:  ir.Provenance{Pointer: "/components/schemas/A/properties/age"},
		Constraints: &ir.Constraints{Min: bigVal("10")},
	}
	src := ir.Property{
		WireName:    "age",
		Provenance:  ptrAt("/components/schemas/B/properties/age"),
		Constraints: &ir.Constraints{Min: bigVal("20")},
	}

	g.recordRedeclarationConflict(&dst, &src)

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
		"t/enum": &ir.Enum{ID: "t/enum", ValueType: ir.PrimString},
		"t/alias": &ir.Scalar{
			ID:   "t/alias",
			Base: &ir.TypeRef{Target: "t/enum"},
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
		"t/model":  &ir.Model{ID: "t/model"},
		"t/other":  &ir.Model{ID: "t/other"},
		"t/union":  &ir.Union{ID: "t/union"},
		"t/opaque": &ir.Scalar{ID: "t/opaque"},
	})

	assert.False(t, g.differentTypeKind(ir.TypeRef{Target: "t/model"}, ir.TypeRef{Target: "t/other"}),
		"two models are the same kind, so not provably contradictory")
	assert.True(t, g.differentTypeKind(ir.TypeRef{Target: "t/model"}, ir.TypeRef{Target: "t/union"}),
		"a model and a union are different kinds")
	assert.Equal(t, verdictConflict, g.typesConflict(ir.TypeRef{Target: "t/opaque"}, ir.TypeRef{Target: "t/union"}),
		"and typesConflict reaches that comparison when neither side is a primitive")
}

// TestKeepLosingDeclaration_RecordsTheDiscardedDeclaration pins what the fix for
// GitHub #424 adds: the redeclaration whose type loses is kept verbatim beside
// the winner, so a consumer reading the document — not the diagnostic stream —
// still sees what the second declaration said.
//
// The reference is asserted whole rather than by its target alone: Nullable is
// half of what a redeclaration says about a field, and an entry that kept only
// the ID would lose it while still looking like a preservation.
func TestKeepLosingDeclaration_RecordsTheDiscardedDeclaration(t *testing.T) {
	t.Parallel()
	g, recorded := stubMerger(map[ir.TypeID]ir.TypeDef{
		"t/str": &ir.Primitive{ID: "t/str", Prim: ir.PrimString},
		"t/int": &ir.Primitive{ID: "t/int", Prim: ir.PrimInt32},
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

	g.reconcileProperty(&dst, new(origins), src, written(`{"type":"integer","nullable":true}`))

	require.Len(t, *recorded, 1, "the conflict is still diagnosed")
	assert.Equal(t, ir.TypeID("t/str"), dst.Type.Target, "the first declaration still wins the shape")

	key := "openapi:conflicting-redeclaration/components/schemas/S/allOf/1/properties/id"
	entry, ok := dst.Unmodeled[key]
	require.True(t, ok, "the losing declaration is kept under a pointer-namespaced key; got %v", dst.Unmodeled)
	assert.Equal(t, ir.ReasonDegradedLowering, entry.Reason)
	assert.JSONEq(t, `{"type":"integer","nullable":true}`, string(entry.Value),
		"the declaration is kept as the document wrote it, nullability included")
	assert.Equal(t, ir.Provenance{Source: 2, Pointer: "/components/schemas/S/allOf/1/properties/id"},
		entry.Provenance, "the entry locates the losing declaration, not the merged property")
}

// TestKeepLosingDeclaration_EveryLoserSurvivesItsSiblings pins the key's namespacing.
// A field three branches type three incompatible ways discards two
// declarations, and a fixed key would leave only whichever branch ran last —
// the same silent overwrite the entry exists to prevent.
func TestKeepLosingDeclaration_EveryLoserSurvivesItsSiblings(t *testing.T) {
	t.Parallel()
	g, _ := stubMerger(map[ir.TypeID]ir.TypeDef{
		"t/str":  &ir.Primitive{ID: "t/str", Prim: ir.PrimString},
		"t/int":  &ir.Primitive{ID: "t/int", Prim: ir.PrimInt32},
		"t/bool": &ir.Primitive{ID: "t/bool", Prim: ir.PrimBool},
	})
	m := &ir.Model{}
	byWire := NewPropIndex()

	g.MergeProperty(m, byWire, ir.Property{WireName: "id", Type: ir.TypeRef{Target: "t/str"}, Provenance: ptrAt("/a")}, unread(t))
	g.MergeProperty(m, byWire, ir.Property{WireName: "id", Type: ir.TypeRef{Target: "t/int"}, Provenance: ptrAt("/b")}, written(`{"type":"integer"}`))
	g.MergeProperty(m, byWire, ir.Property{WireName: "id", Type: ir.TypeRef{Target: "t/bool"}, Provenance: ptrAt("/c")}, written(`{"type":"boolean"}`))

	require.Len(t, m.Properties, 1, "three declarations still reconcile to one property")
	assert.Equal(t, ir.Unmodeled{
		"openapi:conflicting-redeclaration/b": {
			Reason:     ir.ReasonDegradedLowering,
			Value:      ir.RawValue(`{"type":"integer"}`),
			Provenance: ir.Provenance{Pointer: "/b"},
		},
		"openapi:conflicting-redeclaration/c": {
			Reason:     ir.ReasonDegradedLowering,
			Value:      ir.RawValue(`{"type":"boolean"}`),
			Provenance: ir.Provenance{Pointer: "/c"},
		},
	}, m.Properties[0].Unmodeled)
}

// TestKeepLosingDeclaration_AgreeingDeclarationsKeepNothing pins the side the
// entry must not appear on: agreeing declarations discard nothing, so an entry
// would be noise. A constraint conflict is the other side — the merge keeps the
// first declaration's keyword and drops the redeclaration's, which is a loss
// like any other until the bounds are intersected instead (GitHub #10), and
// the entry records what the merge dropped rather than deciding what it should
// have kept.
func TestKeepLosingDeclaration_AgreeingDeclarationsKeepNothing(t *testing.T) {
	t.Parallel()
	g, recorded := stubMerger(map[ir.TypeID]ir.TypeDef{
		"t/str": &ir.Primitive{ID: "t/str", Prim: ir.PrimString},
	})
	ten, twenty := int64(10), int64(20)
	agreeing := ir.Property{WireName: "id", Type: ir.TypeRef{Target: "t/str"}}

	g.reconcileProperty(&agreeing, new(origins), ir.Property{WireName: "id", Type: ir.TypeRef{Target: "t/str"}, Provenance: ptrAt("/b")}, unread(t))

	assert.Empty(t, *recorded, "agreeing declarations are not a conflict")
	assert.Empty(t, agreeing.Unmodeled, "and nothing was discarded to keep")

	bounded := ir.Property{
		WireName: "code", Type: ir.TypeRef{Target: "t/str"},
		Constraints: &ir.Constraints{MaxLength: &ten},
	}
	g.reconcileProperty(&bounded, new(origins), ir.Property{
		WireName: "code", Type: ir.TypeRef{Target: "t/str"},
		Constraints: &ir.Constraints{MaxLength: &twenty},
		Provenance:  ptrAt("/b"),
	}, written(`{"type":"string","maxLength":20}`))

	require.Len(t, *recorded, 1, "the constraint conflict is still diagnosed")
	assert.Equal(t, diag.ConflictingRedecl, (*recorded)[0].Code)
	assert.Equal(t, ir.Unmodeled{
		"openapi:conflicting-redeclaration/b": {
			Reason:     ir.ReasonDegradedLowering,
			Value:      ir.RawValue(`{"type":"string","maxLength":20}`),
			Provenance: ir.Provenance{Pointer: "/b"},
		},
	}, bounded.Unmodeled, "the keyword the merge dropped is kept with the declaration that wrote it")
}

// TestReconcileProperty_AConflictingTypeTakesNothingWithIt pins that a
// discarded declaration is discarded whole. The type conflict is diagnosed and
// the loser's type kept under Unmodeled, but the fold below it used to run to
// completion regardless, adopting the loser's default, constraints and examples
// onto the winner. That leaves the document asserting two contradictory things
// about one field — an integer carrying a string default — and nothing in
// pass/validate or irverify compares a Value's kind to its property's type, so
// an emitter renders it into non-compiling code.
func TestReconcileProperty_AConflictingTypeTakesNothingWithIt(t *testing.T) {
	t.Parallel()
	g, recorded := stubMerger(map[ir.TypeID]ir.TypeDef{
		"t/str": &ir.Primitive{ID: "t/str", Prim: ir.PrimString},
		"t/int": &ir.Primitive{ID: "t/int", Prim: ir.PrimInt32},
	})
	dst := ir.Property{WireName: "id", Type: ir.TypeRef{Target: "t/int"}}
	src := ir.Property{
		WireName:    "id",
		Type:        ir.TypeRef{Target: "t/str"},
		Default:     &ir.Value{Kind: ir.ValueString, Str: "abc"},
		Constraints: &ir.Constraints{MaxLength: new(int64(10))},
		Examples:    []ir.Example{{Value: &ir.Value{Kind: ir.ValueString, Str: "abc"}}},
	}

	g.reconcileProperty(&dst, new(origins), src, written(`{"type":"string"}`))

	require.Len(t, *recorded, 1, "the conflict is diagnosed")
	assert.Equal(t, ir.TypeID("t/int"), dst.Type.Target, "the first declaration wins the shape")
	assert.Nil(t, dst.Default, "a string default does not belong to an integer field")
	assert.Nil(t, dst.Constraints, "maxLength constrains the string that was discarded")
	assert.Empty(t, dst.Examples, "the examples are of the discarded shape")
}

// TestReconcileProperty_NullabilityIntersectsWhenTargetsAgree pins the case
// typesConflict returns on before it reads Nullable: one branch admits null and
// the other does not, so the intersection forbids it. Neither reconciled nor
// preserved before, and the answer flipped with branch order — the same source
// compiled to nullable: true or false depending on which branch was written
// first. foldNullVerdicts already states the conjunction rule for the
// single-schema case; this is the same rule across a redeclaration.
func TestReconcileProperty_NullabilityIntersectsWhenTargetsAgree(t *testing.T) {
	t.Parallel()
	reg := map[ir.TypeID]ir.TypeDef{
		"t/str": &ir.Primitive{ID: "t/str", Prim: ir.PrimString},
	}
	cases := []struct {
		name                   string
		dstNull, srcNull, want bool
	}{
		{"nullable then not", true, false, false},
		{"not then nullable", false, true, false},
		{"both nullable", true, true, true},
		{"neither nullable", false, false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			g, recorded := stubMerger(reg)
			dst := ir.Property{WireName: "x", Type: ir.TypeRef{Target: "t/str", Nullable: tc.dstNull}}
			src := ir.Property{WireName: "x", Type: ir.TypeRef{Target: "t/str", Nullable: tc.srcNull}}

			g.reconcileProperty(&dst, new(origins), src, unread(t))

			assert.Equal(t, tc.want, dst.Type.Nullable, "the merged field admits null only where both branches do")
			assert.Empty(t, *recorded, "an intersection the IR can express is not a conflict")
		})
	}
}

// TestReconcileProperty_ADroppedTypeIsKeptEvenWhenItDoesNotConflict pins the
// gap between what the merge drops and what it used to record. typesConflict
// deliberately answers false for two composites of one kind and for the top type
// against anything — it does not guess — but reconcileProperty keeps dst.Type
// regardless, so in every one of those the redeclaration vanished with neither a
// diagnostic nor an entry. That is #424's own failure: a consumer diffing two
// versions sees no change. Preservation is owed wherever a type is dropped;
// the diagnostic stays with the conflict, which is a narrower claim.
func TestReconcileProperty_ADroppedTypeIsKeptEvenWhenItDoesNotConflict(t *testing.T) {
	t.Parallel()
	reg := map[ir.TypeID]ir.TypeDef{
		"t/A":   &ir.Model{ID: "t/A"},
		"t/B":   &ir.Model{ID: "t/B"},
		"t/any": &ir.Any{ID: "t/any"},
		"t/str": &ir.Primitive{ID: "t/str", Prim: ir.PrimString},
		"t/opq": &ir.Scalar{ID: "t/opq"},
	}
	cases := []struct {
		name                 string
		dstTarget, srcTarget ir.TypeID
	}{
		{"two models of one kind", "t/A", "t/B"},
		{"an opaque scalar keeps the position", "t/opq", "t/str"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			g, recorded := stubMerger(reg)
			dst := ir.Property{WireName: "id", Type: ir.TypeRef{Target: tc.dstTarget}}
			src := ir.Property{
				WireName:   "id",
				Type:       ir.TypeRef{Target: tc.srcTarget},
				Provenance: ir.Provenance{Source: 1, Pointer: "/p/allOf/1/properties/id"},
			}

			g.reconcileProperty(&dst, new(origins), src, written(`{"type":"object"}`))

			assert.Equal(t, tc.dstTarget, dst.Type.Target, "the first declaration still wins")
			assert.Empty(t, *recorded, "a drop the predicate does not call a conflict is not diagnosed")
			entry, ok := dst.Unmodeled[losingDeclarationKey+"/p/allOf/1/properties/id"]
			require.True(t, ok, "the dropped declaration is kept; got %v", dst.Unmodeled)
			assert.Equal(t, ir.ReasonDegradedLowering, entry.Reason)
		})
	}
}

// TestReconcileProperty_ADroppedTypeTakesNothingWithItEvenUnjudged is
// TestReconcileProperty_AConflictingTypeTakesNothingWithIt for the drops
// typesConflict declines to judge. The skip was keyed on the conflict
// diagnostic rather than on the drop, so two Models — never called a conflict —
// still folded the loser's default and constraints onto a winner whose type
// could not hold them, beside an entry saying that type was dropped: the
// contradiction the skip exists to prevent, on the one path it did not cover.
func TestReconcileProperty_ADroppedTypeTakesNothingWithItEvenUnjudged(t *testing.T) {
	t.Parallel()
	g, recorded := stubMerger(map[ir.TypeID]ir.TypeDef{
		"t/A": &ir.Model{ID: "t/A"},
		"t/B": &ir.Model{ID: "t/B"},
	})
	dst := ir.Property{WireName: "x", Type: ir.TypeRef{Target: "t/A"}}
	src := ir.Property{
		WireName:    "x",
		Type:        ir.TypeRef{Target: "t/B"},
		Default:     &ir.Value{Kind: ir.ValueObject},
		Constraints: &ir.Constraints{MinProps: new(int64(1))},
		Examples:    []ir.Example{{Value: &ir.Value{Kind: ir.ValueObject}}},
		Provenance:  ptrAt("/allOf/1/properties/x"),
	}

	g.reconcileProperty(&dst, new(origins), src, written(`{"type":"object","default":{},"minProperties":1}`))

	assert.Empty(t, *recorded, "two models of one kind are still not called a conflict")
	assert.Nil(t, dst.Default, "the default describes the shape that was dropped")
	assert.Nil(t, dst.Constraints, "so does minProperties")
	assert.Empty(t, dst.Examples, "and so do the examples")
	assert.Contains(t, dst.Unmodeled, losingDeclarationKey+"/allOf/1/properties/x", "the whole declaration is kept instead")
}

// TestReconcileProperty_ALosingDetailIsKeptAndReported sweeps the fold's
// adopt-if-absent fields. Each of them took the first declaration's value and
// dropped a present, different value from the redeclaration with nothing said
// and nothing kept — a consumer diffing two revisions that change the second
// branch's default saw no change. Now the merge says which detail disagreed
// and keeps the losing declaration verbatim, under the same key a losing type
// is kept under, since it is the same event: a redeclaration the merge could
// not fold whole.
func TestReconcileProperty_ALosingDetailIsKeptAndReported(t *testing.T) {
	t.Parallel()
	one, two := &ir.Value{Kind: ir.ValueNumber, Num: "1"}, &ir.Value{Kind: ir.ValueNumber, Num: "2"}
	cases := []struct {
		name     string
		dst, src ir.Property
		detail   string
	}{
		{"default", ir.Property{Default: one}, ir.Property{Default: two}, "default"},
		{"examples", ir.Property{Examples: []ir.Example{{Value: one}}}, ir.Property{Examples: []ir.Example{{Value: two}}}, "examples"},
		{"deprecation", ir.Property{Deprecation: &ir.Deprecation{Since: "1"}}, ir.Property{Deprecation: &ir.Deprecation{Since: "2"}}, "deprecation"},
		{"xml", ir.Property{XML: &ir.XMLHints{Name: "a"}}, ir.Property{XML: &ir.XMLHints{Name: "b"}}, "xml"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			g, recorded := stubMerger(map[ir.TypeID]ir.TypeDef{
				"t/int": &ir.Primitive{ID: "t/int", Prim: ir.PrimInt32},
			})
			dst, src := tc.dst, tc.src
			dst.WireName, src.WireName = "id", "id"
			dst.Type, src.Type = ir.TypeRef{Target: "t/int"}, ir.TypeRef{Target: "t/int"}
			src.Provenance = ir.Provenance{Source: 1, Pointer: "/allOf/1/properties/id"}
			want := tc.dst

			g.reconcileProperty(&dst, new(origins), src, written(`{"type":"integer","x":2}`))

			assert.Equal(t, want.Default, dst.Default, "the first declaration keeps its default")
			assert.Equal(t, want.Examples, dst.Examples, "and its examples")
			assert.Equal(t, want.Deprecation, dst.Deprecation, "and its deprecation")
			assert.Equal(t, want.XML, dst.XML, "and its xml hints")
			require.Len(t, *recorded, 1, "one detail disagreed, one diagnostic; got %v", *recorded)
			d := (*recorded)[0]
			assert.Equal(t, ir.SeverityInfo, d.Severity)
			assert.Equal(t, diag.DegradedConstruct, d.Code)
			assert.Equal(t, jsontext.Pointer("/allOf/1/properties/id"), d.Provenance.Pointer)
			assert.Contains(t, d.Message, `"id"`)
			assert.Contains(t, d.Message, tc.detail, "the message names the detail that disagreed")
			assert.Equal(t, ir.Unmodeled{
				losingDeclarationKey + "/allOf/1/properties/id": {
					Reason:     ir.ReasonDegradedLowering,
					Value:      ir.RawValue(`{"type":"integer","x":2}`),
					Provenance: ir.Provenance{Source: 1, Pointer: "/allOf/1/properties/id"},
				},
			}, dst.Unmodeled)
		})
	}
}

// TestReconcileProperty_AnIdenticalDetailIsNotADisagreement is the other half:
// two branches writing the same default have not disagreed, and reporting or
// keeping one would be noise. The source is unread, which is the proof.
func TestReconcileProperty_AnIdenticalDetailIsNotADisagreement(t *testing.T) {
	t.Parallel()
	g, recorded := stubMerger(map[ir.TypeID]ir.TypeDef{
		"t/int": &ir.Primitive{ID: "t/int", Prim: ir.PrimInt32},
	})
	detail := func() ir.Property {
		return ir.Property{
			WireName:    "id",
			Type:        ir.TypeRef{Target: "t/int"},
			Default:     &ir.Value{Kind: ir.ValueNumber, Num: "1"},
			Examples:    []ir.Example{{Value: &ir.Value{Kind: ir.ValueNumber, Num: "1"}}},
			Deprecation: &ir.Deprecation{Since: "1"},
			XML:         &ir.XMLHints{Name: "a"},
			Provenance:  ptrAt("/allOf/1/properties/id"),
		}
	}
	dst := detail()

	g.reconcileProperty(&dst, new(origins), detail(), unread(t))

	assert.Empty(t, *recorded)
	assert.Empty(t, dst.Unmodeled)
}

// TestReconcileProperty_AnUnrenderableLoserIsAnError pins the one way keeping
// the declaration can fail. The merge has already said the redeclaration is
// kept, so a source that will not render is reported as an error — the
// construct reached the IR in no form — rather than left to the announcement.
func TestReconcileProperty_AnUnrenderableLoserIsAnError(t *testing.T) {
	t.Parallel()
	g, recorded := stubMerger(map[ir.TypeID]ir.TypeDef{
		"t/str": &ir.Primitive{ID: "t/str", Prim: ir.PrimString},
		"t/int": &ir.Primitive{ID: "t/int", Prim: ir.PrimInt32},
	})
	dst := ir.Property{WireName: "id", Type: ir.TypeRef{Target: "t/str"}}
	src := ir.Property{WireName: "id", Type: ir.TypeRef{Target: "t/int"}, Provenance: ptrAt("/b")}

	g.reconcileProperty(&dst, new(origins), src, func() (ir.RawValue, error) { return nil, errors.New("no such node") })

	require.Len(t, *recorded, 2, "the conflict, and then the failure to keep it; got %v", *recorded)
	assert.Equal(t, diag.ConflictingRedecl, (*recorded)[0].Code)
	assert.Equal(t, diag.UnpreservableConstruct, (*recorded)[1].Code)
	assert.Equal(t, ir.SeverityError, (*recorded)[1].Severity)
	assert.Empty(t, dst.Unmodeled, "nothing claims a preservation that did not happen")
}

// narrowingRegistry holds one bare primitive per kind the narrowing tests pair.
func narrowingRegistry() map[ir.TypeID]ir.TypeDef {
	reg := map[ir.TypeID]ir.TypeDef{}
	for _, k := range []ir.PrimKind{
		ir.PrimNumber, ir.PrimInteger, ir.PrimInt32, ir.PrimInt64, ir.PrimString, ir.PrimDatetimeOffset,
		ir.PrimURL, ir.PrimFloat64, ir.PrimUUID, ir.PrimUint32, ir.PrimBytes,
		ir.PrimUint8, ir.PrimInt16, ir.PrimAny, ir.PrimBool, ir.PrimFloat32, ir.PrimTime,
	} {
		id := ir.TypeID("t/prim/" + string(k))
		reg[id] = &ir.Primitive{ID: id, Prim: k}
	}
	return reg
}

func primRef(k ir.PrimKind) ir.TypeRef { return ir.TypeRef{Target: ir.TypeID("t/prim/" + string(k))} }

// TestReconcileProperty_ANarrowerPrimitiveWinsInEitherOrder pins the refinement
// rule: a redeclaration that narrows a bare primitive loses nothing, so the
// narrower target wins whichever branch is written first, with no diagnostic
// and no kept declaration.
func TestReconcileProperty_ANarrowerPrimitiveWinsInEitherOrder(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name          string
		broad, narrow ir.PrimKind
	}{
		{"number and integer", ir.PrimNumber, ir.PrimInteger},
		{"integer and int32", ir.PrimInteger, ir.PrimInt32},
		{"string and datetime_offset", ir.PrimString, ir.PrimDatetimeOffset},
		{"int32 narrows int64", ir.PrimInt64, ir.PrimInt32},
		{"url and string", ir.PrimString, ir.PrimURL},
		{"float64 and number", ir.PrimNumber, ir.PrimFloat64},
		{"uuid and string", ir.PrimString, ir.PrimUUID},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var merged [2]ir.TypeRef
			for i, order := range [][2]ir.PrimKind{{tc.broad, tc.narrow}, {tc.narrow, tc.broad}} {
				g, recorded := stubMerger(narrowingRegistry())
				dst := ir.Property{WireName: "x", Type: primRef(order[0])}
				src := ir.Property{WireName: "x", Type: primRef(order[1]), Provenance: ptrAt("/allOf/1/properties/x")}

				g.reconcileProperty(&dst, new(origins), src, unread(t))

				assert.Equal(t, primRef(tc.narrow).Target, dst.Type.Target, "the narrower target wins (order %d)", i)
				assert.Empty(t, *recorded, "a refinement is not a conflict (order %d)", i)
				assert.Empty(t, dst.Unmodeled, "nothing is dropped, so nothing is kept (order %d)", i)
				merged[i] = dst.Type
			}
			assert.Empty(t, cmp.Diff(merged[0], merged[1]), "the merged type does not depend on branch order")
		})
	}
}

// TestReconcileProperty_AUint8NarrowsInt16InEitherOrder pins a containment the
// old chain omitted: every uint8 is an int16, so int16 declared beside uint8
// keeps uint8 whichever branch is written first, with nothing reported or kept.
func TestReconcileProperty_AUint8NarrowsInt16InEitherOrder(t *testing.T) {
	t.Parallel()
	var merged [2]ir.TypeRef
	for i, order := range [][2]ir.PrimKind{{ir.PrimInt16, ir.PrimUint8}, {ir.PrimUint8, ir.PrimInt16}} {
		g, recorded := stubMerger(narrowingRegistry())
		dst := ir.Property{WireName: "x", Type: primRef(order[0])}
		src := ir.Property{WireName: "x", Type: primRef(order[1]), Provenance: ptrAt("/allOf/1/properties/x")}

		g.reconcileProperty(&dst, new(origins), src, unread(t))

		assert.Equal(t, primRef(ir.PrimUint8).Target, dst.Type.Target, "uint8 wins (order %d)", i)
		assert.Empty(t, *recorded, "a containment is not a conflict (order %d)", i)
		assert.Empty(t, dst.Unmodeled, "nothing is dropped (order %d)", i)
		merged[i] = dst.Type
	}
	assert.Empty(t, cmp.Diff(merged[0], merged[1]), "the merged type does not depend on branch order")
}

// TestReconcileProperty_ANarrowingIntersectsNullability pins that the swap
// keeps the conjunction the equal-target case already states: over every pair
// of null admissions, in both orders, the merged field admits null only where
// both branches do.
func TestReconcileProperty_ANarrowingIntersectsNullability(t *testing.T) {
	t.Parallel()
	for _, broadFirst := range []bool{true, false} {
		for _, broadNull := range []bool{true, false} {
			for _, narrowNull := range []bool{true, false} {
				name := fmt.Sprintf("broadFirst=%v/broadNullable=%v/narrowNullable=%v", broadFirst, broadNull, narrowNull)
				t.Run(name, func(t *testing.T) {
					t.Parallel()
					g, recorded := stubMerger(narrowingRegistry())
					broad := primRef(ir.PrimNumber)
					broad.Nullable = broadNull
					narrow := primRef(ir.PrimInteger)
					narrow.Nullable = narrowNull
					dst, src := broad, narrow
					if !broadFirst {
						dst, src = narrow, broad
					}
					p := ir.Property{WireName: "x", Type: dst}

					g.reconcileProperty(&p, new(origins), ir.Property{WireName: "x", Type: src, Provenance: ptrAt("/p")}, unread(t))

					assert.Equal(t, narrow.Target, p.Type.Target)
					assert.Equal(t, broadNull && narrowNull, p.Type.Nullable,
						"null is admitted only where both branches admit it")
					assert.Empty(t, *recorded)
				})
			}
		}
	}
}

// TestReconcileProperty_ADroppedTargetKeepsTheFirstNullAdmission pins the drop
// arm of foldTypeTargets: an unrelated redeclaration's type is lost whole, so
// the merged field keeps the first declaration's null admission, not the
// conjunction a same-referent or narrowing pair takes.
func TestReconcileProperty_ADroppedTargetKeepsTheFirstNullAdmission(t *testing.T) {
	t.Parallel()
	for _, firstNull := range []bool{true, false} {
		t.Run(fmt.Sprintf("firstNullable=%v", firstNull), func(t *testing.T) {
			t.Parallel()
			g, recorded := stubMerger(narrowingRegistry())
			dst := ir.Property{WireName: "x", Type: primRef(ir.PrimString)}
			dst.Type.Nullable = firstNull
			src := ir.Property{WireName: "x", Type: primRef(ir.PrimInteger), Provenance: ptrAt("/allOf/1/properties/x")}
			src.Type.Nullable = !firstNull

			g.reconcileProperty(&dst, new(origins), src, written(`{"type":"integer"}`))

			assert.Equal(t, primRef(ir.PrimString).Target, dst.Type.Target, "the first declaration keeps the shape")
			assert.Equal(t, firstNull, dst.Type.Nullable, "a dropped type takes its null admission with it")
			require.Len(t, *recorded, 1)
			assert.Equal(t, diag.ConflictingRedecl, (*recorded)[0].Code)
		})
	}
}

// TestReconcileProperty_ANarrowingAdoptsTheBroaderDeclarationsDetail pins that
// nothing is dropped, so the broader declaration's constraints and default are
// folded as for any agreeing pair.
func TestReconcileProperty_ANarrowingAdoptsTheBroaderDeclarationsDetail(t *testing.T) {
	t.Parallel()
	for _, broadFirst := range []bool{true, false} {
		t.Run(fmt.Sprintf("broadFirst=%v", broadFirst), func(t *testing.T) {
			t.Parallel()
			g, recorded := stubMerger(narrowingRegistry())
			broad := ir.Property{
				WireName:    "x",
				Type:        primRef(ir.PrimNumber),
				Default:     &ir.Value{Kind: ir.ValueNumber, Num: "1"},
				Constraints: &ir.Constraints{Min: bigVal("0")},
				Provenance:  ptrAt("/allOf/0/properties/x"),
			}
			narrow := ir.Property{WireName: "x", Type: primRef(ir.PrimInteger), Provenance: ptrAt("/allOf/1/properties/x")}
			dst, src := broad, narrow
			if !broadFirst {
				dst, src = narrow, broad
			}

			g.reconcileProperty(&dst, new(origins), src, unread(t))

			assert.Equal(t, primRef(ir.PrimInteger).Target, dst.Type.Target)
			require.NotNil(t, dst.Constraints)
			assert.Equal(t, bigVal("0"), dst.Constraints.Min, "the broader declaration's bound lands on the merged field")
			require.NotNil(t, dst.Default)
			assert.Equal(t, ir.BigVal("1"), dst.Default.Num, "and so does its default")
			assert.Empty(t, *recorded)
		})
	}
}

// TestTypesConflict_UnrelatedPrimitivesStillConflict pins the pairs the
// relation calls disjoint, and the enum against a bare kind of another family.
func TestTypesConflict_UnrelatedPrimitivesStillConflict(t *testing.T) {
	t.Parallel()
	reg := narrowingRegistry()
	reg["t/enm"] = &ir.Enum{ID: "t/enm", ValueType: ir.PrimString}
	g, _ := stubMerger(reg)

	assert.Equal(t, verdictConflict, g.typesConflict(primRef(ir.PrimUUID), primRef(ir.PrimDatetimeOffset)))
	assert.Equal(t, verdictConflict, g.typesConflict(ir.TypeRef{Target: "t/enm"}, primRef(ir.PrimInteger)))
	assert.Equal(t, verdictOverlap, g.typesConflict(primRef(ir.PrimInt32), primRef(ir.PrimUint32)),
		"overlapping kinds are no conflict")
}

// TestFoldTypeTargets_ANarrowingIsNoDrop pins that a bare narrowing settles on
// one target in either order and is no drop, so the redeclaration is neither
// classified nor reported nor kept.
func TestFoldTypeTargets_ANarrowingIsNoDrop(t *testing.T) {
	t.Parallel()
	for i, order := range [][2]ir.PrimKind{{ir.PrimInteger, ir.PrimNumber}, {ir.PrimNumber, ir.PrimInteger}} {
		g, recorded := stubMerger(narrowingRegistry())
		dst := ir.Property{WireName: "x", Type: primRef(order[0])}
		src := ir.Property{WireName: "x", Type: primRef(order[1]), Provenance: ptrAt("/allOf/1/properties/x")}

		rel, _ := g.foldTypeTargets(&dst, &src)
		assert.False(t, rel.dropped(), "a narrowing is no drop (order %d)", i)
		assert.Equal(t, primRef(ir.PrimInteger).Target, dst.Type.Target, "the narrower target wins (order %d)", i)

		// Build the pair afresh: in order 1 the fold above has already made
		// the targets equal, and the classifier must meet a narrowing in both orders.
		dst2 := ir.Property{WireName: "x", Type: primRef(order[0])}
		src2 := ir.Property{WireName: "x", Type: primRef(order[1]), Provenance: ptrAt("/allOf/1/properties/x")}
		require.NotEqual(t, dst2.Type.Target, src2.Type.Target, "the pair is a narrowing (order %d)", i)

		dropped, lost, _ := g.recordRedeclarationConflict(&dst2, &src2)

		assert.False(t, dropped, "order %d", i)
		assert.False(t, lost, "order %d", i)
		assert.Empty(t, *recorded, "a narrowing draws no diagnostic (order %d)", i)
	}
}

// TestReconcileProperty_TheJudgmentsVerdictIsWhatIsReported pins the three
// paths of the fold: a disjoint pair warns, an overlapping pair draws an info,
// and one target declared twice judges null alone and adopts a default.
func TestReconcileProperty_TheJudgmentsVerdictIsWhatIsReported(t *testing.T) {
	t.Parallel()
	d := str("x")
	for _, tc := range []struct {
		name      string
		a, b      ir.TypeID
		dflt      *ir.Value
		severity  ir.Severity
		code      string
		keptEntry bool
	}{
		{"a literal against a kind it is disjoint from", "t/lit-x", "t/prim/integer", nil,
			ir.SeverityWarning, diag.ConflictingRedecl, true},
		{"lists whose elements overlap", "t/list-int", "t/list-i32-null", nil,
			ir.SeverityInfo, diag.DegradedConstruct, true},
		{"one target declared twice", "t/prim/integer", "t/prim/integer", &d, "", "", false},
	} {
		for i, order := range [][2]ir.TypeID{{tc.a, tc.b}, {tc.b, tc.a}} {
			g, recorded := stubMerger(judgeRegistry())
			dst := ir.Property{WireName: "x", Type: ref(order[0]), Provenance: ptrAt("/allOf/0/properties/x")}
			src := ir.Property{WireName: "x", Type: ref(order[1]), Default: tc.dflt,
				Provenance: ptrAt("/allOf/1/properties/x")}
			if i == 1 {
				dst.Default, src.Default = src.Default, nil
			}

			g.reconcileProperty(&dst, new(origins), src, written(`{"type":"x"}`))

			if tc.keptEntry {
				require.Len(t, *recorded, 1, "%s (order %d)", tc.name, i)
				assert.Equal(t, tc.severity, (*recorded)[0].Severity, "%s (order %d)", tc.name, i)
				assert.Equal(t, tc.code, (*recorded)[0].Code, "%s (order %d)", tc.name, i)
				assert.Contains(t, dst.Unmodeled, losingDeclarationKey+"/allOf/1/properties/x",
					"%s (order %d)", tc.name, i)
				continue
			}
			assert.Empty(t, *recorded, "%s (order %d)", tc.name, i)
			assert.Equal(t, tc.dflt, dst.Default, "the default is adopted (%s, order %d)", tc.name, i)
			assert.Empty(t, dst.Unmodeled, "%s (order %d)", tc.name, i)
		}
	}
}

// reconcileBothOrders reconciles kinds a and b in both declaration orders and
// returns what each order recorded and merged.
func reconcileBothOrders(t *testing.T, a, b ir.PrimKind) (dsts [2]ir.Property, diags [2][]ir.Diagnostic) {
	t.Helper()
	for i, order := range [][2]ir.PrimKind{{a, b}, {b, a}} {
		g, recorded := stubMerger(narrowingRegistry())
		dst := ir.Property{WireName: "x", Type: primRef(order[0]), Provenance: ptrAt("/allOf/0/properties/x")}
		src := ir.Property{WireName: "x", Type: primRef(order[1]), Provenance: ptrAt("/allOf/1/properties/x")}

		g.reconcileProperty(&dst, new(origins), src, written(`{"type":"x"}`))

		dsts[i], diags[i] = dst, *recorded
	}
	return dsts, diags
}

// TestReconcileProperty_OverlappingKindsAreKeptAndReportedAsInfo pins that
// kinds sharing values with neither holding the other keep the first
// declaration's kind, draw an info, and keep the redeclaration, in both orders.
func TestReconcileProperty_OverlappingKindsAreKeptAndReportedAsInfo(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		a, b ir.PrimKind
	}{
		{"int32 and uint32", ir.PrimInt32, ir.PrimUint32},
		{"float32 and integer", ir.PrimFloat32, ir.PrimInteger},
		{"int64 and float64", ir.PrimInt64, ir.PrimFloat64},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dsts, diags := reconcileBothOrders(t, tc.a, tc.b)
			for i, first := range []ir.PrimKind{tc.a, tc.b} {
				assert.Equal(t, primRef(first).Target, dsts[i].Type.Target, "the first declaration's kind stays (order %d)", i)
				require.Len(t, diags[i], 1, "exactly one diagnostic (order %d)", i)
				assert.Equal(t, diag.DegradedConstruct, diags[i][0].Code, "order %d", i)
				assert.Equal(t, ir.SeverityInfo, diags[i][0].Severity, "order %d", i)
				assert.Contains(t, dsts[i].Unmodeled, losingDeclarationKey+"/allOf/1/properties/x",
					"the redeclaration is kept (order %d)", i)
			}
		})
	}
}

// TestReconcileProperty_DisjointKindsStillWarn pins that kinds no value shares
// keep the conflicting-redeclaration warning, in both orders.
func TestReconcileProperty_DisjointKindsStillWarn(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		a, b ir.PrimKind
	}{
		{"string and integer", ir.PrimString, ir.PrimInteger},
		{"bool and number", ir.PrimBool, ir.PrimNumber},
		{"uuid and date-time", ir.PrimUUID, ir.PrimDatetimeOffset},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dsts, diags := reconcileBothOrders(t, tc.a, tc.b)
			for i, first := range []ir.PrimKind{tc.a, tc.b} {
				assert.Equal(t, primRef(first).Target, dsts[i].Type.Target, "the first declaration still wins (order %d)", i)
				require.Len(t, diags[i], 1, "exactly one diagnostic (order %d)", i)
				assert.Equal(t, diag.ConflictingRedecl, diags[i][0].Code, "order %d", i)
				assert.Equal(t, ir.SeverityWarning, diags[i][0].Severity, "order %d", i)
				assert.Contains(t, dsts[i].Unmodeled, losingDeclarationKey+"/allOf/1/properties/x", "order %d", i)
			}
		})
	}
}

// enumRegistry adds two integer enums and two string enums to narrowingRegistry,
// each with a member the pairs below judge, and the uuid and bytes Scalar nodes
// that stand for a kind the same way a bare reference does.
func enumRegistry() map[ir.TypeID]ir.TypeDef {
	reg := narrowingRegistry()
	reg["t/enum/a"] = &ir.Enum{ID: "t/enum/a", ValueType: ir.PrimString, Members: []ir.EnumMember{{Value: str("a")}}}
	reg["t/enum/b"] = &ir.Enum{ID: "t/enum/b", ValueType: ir.PrimString, Members: []ir.EnumMember{{Value: str("b")}}}
	reg["t/enum/big"] = &ir.Enum{ID: "t/enum/big", ValueType: ir.PrimInteger, Members: []ir.EnumMember{{Value: num("5000000000")}}}
	reg["t/enum/small"] = &ir.Enum{ID: "t/enum/small", ValueType: ir.PrimInteger, Members: []ir.EnumMember{{Value: num("1")}}}
	uuid, bytes := primRef(ir.PrimUUID), primRef(ir.PrimBytes)
	reg["t/scalar/uuid"] = &ir.Scalar{ID: "t/scalar/uuid", Base: &uuid}
	reg["t/scalar/bytes"] = &ir.Scalar{ID: "t/scalar/bytes", Base: &bytes}
	return reg
}

// TestReconcileProperty_EnumPairsAreJudgedByKind pins, in both orders, what an
// enum side draws: an enum over a bare kind or a Scalar node it equals or
// narrows wins; a bare kind or a Scalar node that admits one of its members
// overlaps it, which is info and keeps the first declaration, as does a kind
// with no judge; a kind that admits none of them warns, as does another
// family; and two enums of one kind, each with members, overlap.
func TestReconcileProperty_EnumPairsAreJudgedByKind(t *testing.T) {
	t.Parallel()
	enum := func(id ir.TypeID) ir.TypeRef { return ir.TypeRef{Target: id} }
	cases := []struct {
		name   string
		a, b   ir.TypeRef
		want   ir.Severity // "" = none
		winner ir.TypeID   // "" = the first declaration's
	}{
		{"enum of string and uuid", enum("t/enum/a"), primRef(ir.PrimUUID), ir.SeverityWarning, ""},
		{"enum of integer and int32", enum("t/enum/big"), primRef(ir.PrimInt32), ir.SeverityWarning, ""},
		{"enum admitted by int32", enum("t/enum/small"), primRef(ir.PrimInt32), ir.SeverityInfo, ""},
		{"enum of string and bytes", enum("t/enum/a"), primRef(ir.PrimBytes), ir.SeverityInfo, ""},
		{"enum of string and a uuid scalar", enum("t/enum/a"), enum("t/scalar/uuid"), ir.SeverityWarning, ""},
		{"enum of string and a bytes scalar", enum("t/enum/a"), enum("t/scalar/bytes"), ir.SeverityInfo, ""},
		{"enum of string and integer", enum("t/enum/a"), primRef(ir.PrimInteger), ir.SeverityWarning, ""},
		{"enum of string and another enum", enum("t/enum/a"), enum("t/enum/b"), ir.SeverityInfo, ""},
		{"enum of string and bare string", enum("t/enum/a"), primRef(ir.PrimString), "", "t/enum/a"},
		{"enum of integer and number", enum("t/enum/big"), primRef(ir.PrimNumber), "", "t/enum/big"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			for i, order := range [][2]ir.TypeRef{{tc.a, tc.b}, {tc.b, tc.a}} {
				g, recorded := stubMerger(enumRegistry())
				dst := ir.Property{WireName: "x", Type: order[0], Provenance: ptrAt("/allOf/0/properties/x")}
				src := ir.Property{WireName: "x", Type: order[1], Provenance: ptrAt("/allOf/1/properties/x")}

				g.reconcileProperty(&dst, new(origins), src, written(`{"type":"x"}`))

				wantTarget := order[0].Target
				if tc.winner != "" {
					wantTarget = tc.winner
				}
				assert.Equal(t, wantTarget, dst.Type.Target, "the winning target (order %d)", i)
				if tc.want == "" {
					assert.Empty(t, *recorded, "no diagnostic (order %d)", i)
					continue
				}
				require.Len(t, *recorded, 1, "exactly one diagnostic (order %d)", i)
				assert.Equal(t, tc.want, (*recorded)[0].Severity, "order %d", i)
				assert.Contains(t, dst.Unmodeled, losingDeclarationKey+"/allOf/1/properties/x",
					"the redeclaration is kept (order %d)", i)
			}
		})
	}
}

// TestReconcileProperty_AnInadmissibleValueIsKeptNotAdopted pins that no
// declaration of a fold lands a default or example the merged field does not
// admit: the field carries no such value, and one info and one Unmodeled entry
// sit at each declaration that wrote it, identical whichever comes first. The
// table holds the broad side's kind refusals (a fractional default, an
// out-of-range default, a non-UUID example) and a null the merged field no
// longer admits; the helpers add every writer, late writers, and a null on
// either side of a fold.
func TestReconcileProperty_AnInadmissibleValueIsKeptNotAdopted(t *testing.T) {
	t.Parallel()
	const broadPtr, narrowPtr = "/allOf/0/properties/p", "/allOf/1/properties/p"
	half, big, hello := num("1.5"), num("5000000000"), str("hello")
	badURL, commaTime, offsetDate := str("mailto:a b"), str("23:59:59,5Z"), str("2024-01-01T00:00:00+24:00")
	null := ir.Value{Kind: ir.ValueNull}
	for _, tc := range []struct {
		name          string
		broad, narrow ir.PrimKind
		decorate      func(*ir.Property)
		keyword       string
		raw           string
	}{
		{"fractional default", ir.PrimNumber, ir.PrimInteger,
			func(p *ir.Property) { p.Default = &half; p.Examples = []ir.Example{{Value: &half}} }, "default",
			`{"type":"number","default":1.5,"examples":[1.5]}`},
		{"out of range default", ir.PrimInt64, ir.PrimInt32,
			func(p *ir.Property) { p.Default = &big }, "default", `{"type":"integer","format":"int64","default":5000000000}`},
		{"non uuid example", ir.PrimString, ir.PrimUUID,
			func(p *ir.Property) { p.Examples = []ir.Example{{Value: &hello}} }, "examples", `{"type":"string","examples":["hello"]}`},
		{"non url default", ir.PrimString, ir.PrimURL,
			func(p *ir.Property) { p.Default = &badURL }, "default", `{"type":"string","default":"mailto:a b"}`},
		{"comma fraction time example", ir.PrimString, ir.PrimTime,
			func(p *ir.Property) { p.Examples = []ir.Example{{Value: &commaTime}} }, "examples",
			`{"type":"string","examples":["23:59:59,5Z"]}`},
		{"offset hour 24 date time example", ir.PrimString, ir.PrimDatetimeOffset,
			func(p *ir.Property) { p.Examples = []ir.Example{{Value: &offsetDate}} }, "examples",
			`{"type":"string","examples":["2024-01-01T00:00:00+24:00"]}`},
		{"null default", ir.PrimNumber, ir.PrimInteger,
			func(p *ir.Property) { p.Default = &null; p.Type.Nullable = true }, "default", `{"type":["number","null"],"default":null}`},
	} {
		var merged [2]ir.Property
		var diags [2][]ir.Diagnostic
		for i, broadFirst := range []bool{true, false} {
			g, recorded := stubMerger(narrowingRegistry())
			broad := ir.Property{WireName: "p", Type: primRef(tc.broad), Provenance: ptrAt(broadPtr)}
			tc.decorate(&broad)
			narrow := ir.Property{WireName: "p", Type: primRef(tc.narrow), Provenance: ptrAt(narrowPtr)}
			m := &ir.Model{}
			idx := NewPropIndex()
			first, second := broad, narrow
			firstSrc, secondSrc := written(tc.raw), unread(t)
			if !broadFirst {
				first, second = narrow, broad
				firstSrc, secondSrc = unread(t), written(tc.raw)
				first.Provenance, second.Provenance = ptrAt(narrowPtr), ptrAt(broadPtr)
			}
			g.MergeProperty(m, idx, first, firstSrc)
			g.MergeProperty(m, idx, second, secondSrc)

			require.Len(t, m.Properties, 1, "%s", tc.name)
			merged[i], diags[i] = m.Properties[0], *recorded
		}
		for i := range merged {
			got := merged[i]
			assert.Equal(t, primRef(tc.narrow).Target, got.Type.Target, "%s (order %d): the narrow type wins", tc.name, i)
			assert.Nil(t, got.Default, "%s (order %d): no inadmissible default lands", tc.name, i)
			assert.Empty(t, got.Examples, "%s (order %d): and no inadmissible example", tc.name, i)
			require.Len(t, diags[i], 1, "%s (order %d): one info", tc.name, i)
			assert.Equal(t, ir.SeverityInfo, diags[i][0].Severity)
			assert.Equal(t, diag.DegradedConstruct, diags[i][0].Code)
			assert.Equal(t, jsontext.Pointer(broadPtr), diags[i][0].Provenance.Pointer, "%s: at the broad declaration", tc.name)
			assert.Contains(t, diags[i][0].Message, tc.keyword)
			assert.Equal(t, ir.Unmodeled{"openapi:conflicting-redeclaration" + broadPtr: {
				Reason: ir.ReasonDegradedLowering, Value: ir.RawValue(tc.raw), Provenance: ptrAt(broadPtr),
			}}, got.Unmodeled, "%s (order %d): the broad declaration is kept verbatim", tc.name, i)
		}
		assert.Equal(t, diags[0], diags[1], "%s: the diagnostics do not depend on order", tc.name)
		assert.Equal(t, merged[0].Unmodeled, merged[1].Unmodeled, "%s: nor does Unmodeled", tc.name)
		assert.Equal(t, merged[0].Type.Nullable, merged[1].Type.Nullable, "%s: nor the nullability", tc.name)
	}
	assertThreeDeclarationsKeepTheWritingOne(t)
	assertEveryWriterOfARefusedValueIsKept(t)
	assertRefusedValuesAreKeptAtTheirOwnDeclarations(t)
	assertANullTheMergedFieldRefusesIsKeptAtItsWriter(t)
	assertTheTopTypeKeepsANull(t)
}

// assertANullTheMergedFieldRefusesIsKeptAtItsWriter holds that a null default
// is judged on the declaration that wrote it, whichever side of the fold that
// is and whether the fold narrowed or kept one target: the field ends
// non-nullable with no default, and one info and one entry sit at the writer
// alone, in every order of the declarations.
func assertANullTheMergedFieldRefusesIsKeptAtItsWriter(t *testing.T) {
	t.Helper()
	null := ir.Value{Kind: ir.ValueNull}
	nullInt := decl{kind: ir.PrimInteger, nullable: true, def: &null, raw: `{"type":["integer","null"],"default":null}`}
	nullNum := decl{kind: ir.PrimNumber, nullable: true, def: &null, raw: `{"type":["number","null"],"default":null}`}
	plainInt := decl{kind: ir.PrimInteger, raw: `{"type":"integer"}`}
	plainNum := decl{kind: ir.PrimNumber, raw: `{"type":"number"}`}
	nullIntEx := decl{kind: ir.PrimInteger, nullable: true, examples: []ir.Value{null}, raw: `{"type":["integer","null"],"examples":[null]}`}
	for _, tc := range []struct {
		name  string
		decls []decl // the null writer is first
	}{
		{"a null example, narrow against a number", []decl{nullIntEx, plainNum}},
		{"a null example, the same target", []decl{nullIntEx, plainInt}},
		{"a narrow nullable integer against a number", []decl{nullInt, plainNum}},
		{"a broad nullable number against an integer", []decl{nullNum, plainInt}},
		{"the same target, nullable then not", []decl{nullInt, plainInt}},
		{"a nullable integer among a number and an integer", []decl{nullInt, plainNum, plainInt}},
	} {
		for _, order := range permutations(len(tc.decls)) {
			placed := make([]decl, len(order))
			var writer decl
			for pos, which := range order {
				placed[pos] = tc.decls[which]
				placed[pos].ptr = jsontext.Pointer("/allOf/" + string(rune('0'+pos)))
				if which == 0 {
					writer = placed[pos]
				}
			}
			label := fmt.Sprintf("%s %v", tc.name, order)
			got, diags := mergeDecls(t, placed...)
			assert.Equal(t, primRef(ir.PrimInteger).Target, got.Type.Target, label)
			assert.False(t, got.Type.Nullable, label)
			assert.Nil(t, got.Default, label)
			assert.Empty(t, got.Examples, label)
			assert.Equal(t, entry(writer), got.Unmodeled, "%s: kept at the null writer alone", label)
			require.Len(t, diags, 1, "%s: one info", label)
			assert.Equal(t, ir.SeverityInfo, diags[0].Severity, label)
			assert.Equal(t, writer.ptr, diags[0].Provenance.Pointer, "%s: at the writer", label)
		}
	}
}

// assertTheTopTypeKeepsANull holds that a fold of two top-type declarations
// keeps a null default or example: the top type admits every instance, so
// nothing is refused, reported or kept aside, in either order.
func assertTheTopTypeKeepsANull(t *testing.T) {
	t.Helper()
	null := ir.Value{Kind: ir.ValueNull}
	plain := decl{kind: ir.PrimAny, raw: `{"description":"x"}`}
	for _, writer := range []decl{
		{kind: ir.PrimAny, def: &null, raw: `{"default":null}`},
		{kind: ir.PrimAny, examples: []ir.Value{null}, raw: `{"examples":[null]}`},
	} {
		for _, order := range [][]decl{{writer, plain}, {plain, writer}} {
			label := fmt.Sprintf("%s first", order[0].raw)
			order[0].ptr, order[1].ptr = "/allOf/0", "/allOf/1"
			got, diags := mergeDecls(t, order...)
			assert.Equal(t, writer.def, got.Default, label)
			assert.Len(t, got.Examples, len(writer.examples), label)
			assert.Empty(t, got.Unmodeled, label)
			assert.Empty(t, diags, label)
		}
	}
}

// permutations returns every ordering of 0..n-1.
func permutations(n int) [][]int {
	all := [][]int{{}}
	for k := range n {
		next := make([][]int, 0, len(all)*(k+1))
		for _, rest := range all {
			for at := 0; at <= len(rest); at++ {
				next = append(next, slices.Insert(slices.Clone(rest), at, k))
			}
		}
		all = next
	}
	return all
}

// decl is one declaration of the field "p": its kind, optional default and
// examples, and the JSON it was written as.
type decl struct {
	ptr      jsontext.Pointer
	kind     ir.PrimKind
	nullable bool
	def      *ir.Value
	examples []ir.Value
	raw      string
}

// mergeDecls merges decls in order into one Model and returns its only
// property and the diagnostics recorded.
func mergeDecls(t *testing.T, decls ...decl) (ir.Property, []ir.Diagnostic) {
	t.Helper()
	g, recorded := stubMerger(narrowingRegistry())
	m := &ir.Model{}
	idx := NewPropIndex()
	for _, d := range decls {
		p := ir.Property{WireName: "p", Type: primRef(d.kind), Default: d.def, Provenance: ptrAt(d.ptr)}
		p.Type.Nullable = d.nullable
		for i := range d.examples {
			p.Examples = append(p.Examples, ir.Example{Value: &d.examples[i]})
		}
		g.MergeProperty(m, idx, p, written(d.raw))
	}
	require.Len(t, m.Properties, 1)
	return m.Properties[0], *recorded
}

// entry is the Unmodeled entry a kept declaration lands as.
func entry(d decl) ir.Unmodeled {
	return ir.Unmodeled{losingDeclarationKey + string(d.ptr): {
		Reason: ir.ReasonDegradedLowering, Value: ir.RawValue(d.raw), Provenance: ptrAt(d.ptr),
	}}
}

// assertThreeDeclarationsKeepTheWritingOne holds that a value one of three
// declarations wrote is kept at that declaration whichever position the
// narrowing one takes: the value never lands, and no entry sits at a
// declaration that does not hold it.
func assertThreeDeclarationsKeepTheWritingOne(t *testing.T) {
	t.Helper()
	half, two := num("1.5"), num("2.5")
	wide := decl{ptr: "/allOf/0", kind: ir.PrimNumber, raw: `{"type":"number"}`}
	narrow := decl{ptr: "/allOf/2", kind: ir.PrimInteger, raw: `{"type":"integer"}`}
	for _, tc := range []struct {
		name    string
		writer  decl
		keyword string
	}{
		{"default", decl{ptr: "/allOf/1", kind: ir.PrimNumber, def: &half,
			raw: `{"type":"number","default":1.5}`}, "default"},
		{"examples", decl{ptr: "/allOf/1", kind: ir.PrimNumber, examples: []ir.Value{two},
			raw: `{"type":"number","examples":[2.5]}`}, "examples"},
	} {
		for name, order := range map[string][]decl{
			"abc": {wide, tc.writer, narrow}, "cab": {narrow, wide, tc.writer}, "bca": {tc.writer, narrow, wide},
		} {
			got, diags := mergeDecls(t, order...)
			label := tc.name + " " + name
			assert.Equal(t, primRef(ir.PrimInteger).Target, got.Type.Target, label)
			assert.Nil(t, got.Default, label)
			assert.Empty(t, got.Examples, label)
			assert.Equal(t, entry(tc.writer), got.Unmodeled, "%s: kept at the declaration that wrote it", label)
			require.Len(t, diags, 1, label)
			assert.Equal(t, tc.writer.ptr, diags[0].Provenance.Pointer, "%s: the info names it", label)
			assert.Contains(t, diags[0].Message, tc.keyword, label)
		}
	}
}

// assertEveryWriterOfARefusedValueIsKept holds that two declarations giving a
// refused value equal are both reported and kept in all six orders, the
// narrowing one never, and that each pointer is the one its position gives.
func assertEveryWriterOfARefusedValueIsKept(t *testing.T) {
	t.Helper()
	half, two := num("1.5"), num("2.5")
	for _, kw := range []string{"default", "examples"} {
		writer := func(name string) decl {
			d := decl{kind: ir.PrimNumber, raw: `{"writer":"` + name + `"}`}
			if kw == "default" {
				d.def = &half
			} else {
				d.examples = []ir.Value{two}
			}
			return d
		}
		decls := map[string]decl{"a": writer("a"), "b": writer("b"), "c": {kind: ir.PrimInteger, raw: `{"type":"integer"}`}}
		for _, order := range []string{"abc", "acb", "bac", "bca", "cab", "cba"} {
			placed := make([]decl, 0, len(order))
			want := ir.Unmodeled{}
			var wantPtrs []jsontext.Pointer
			for i, name := range order {
				d := decls[string(name)]
				d.ptr = jsontext.Pointer("/allOf/" + string(rune('0'+i)))
				placed = append(placed, d)
				if name != 'c' {
					want[losingDeclarationKey+string(d.ptr)] = entry(d)[losingDeclarationKey+string(d.ptr)]
					wantPtrs = append(wantPtrs, d.ptr)
				}
			}
			label := kw + " " + order
			got, diags := mergeDecls(t, placed...)
			assert.Equal(t, primRef(ir.PrimInteger).Target, got.Type.Target, label)
			assert.Nil(t, got.Default, label)
			assert.Empty(t, got.Examples, label)
			assert.Equal(t, want, got.Unmodeled, "%s: both writers are kept, and only they", label)
			gotPtrs := make([]jsontext.Pointer, 0, len(diags))
			for _, d := range diags {
				gotPtrs = append(gotPtrs, d.Provenance.Pointer)
				assert.Contains(t, d.Message, kw, label)
			}
			assert.ElementsMatch(t, wantPtrs, gotPtrs, "%s: and both are reported", label)
		}
	}
}

// assertRefusedValuesAreKeptAtTheirOwnDeclarations holds the grouping: one
// info and one entry per declaration that wrote a refused value, a value
// adopted from the narrow side's own declaration after a refusal moves the
// origin to that declaration.
func assertRefusedValuesAreKeptAtTheirOwnDeclarations(t *testing.T) {
	t.Helper()
	half, two, big := num("1.5"), num("2.5"), num("5000000000")
	defaulted := decl{ptr: "/allOf/0", kind: ir.PrimNumber, def: &half, raw: `{"default":1.5}`}
	exampled := decl{ptr: "/allOf/1", kind: ir.PrimNumber, examples: []ir.Value{two}, raw: `{"examples":[2.5]}`}
	both := decl{ptr: "/allOf/1", kind: ir.PrimNumber, def: &half, examples: []ir.Value{two}, raw: `{"both":1}`}
	narrow := decl{ptr: "/allOf/2", kind: ir.PrimInteger, raw: `{"type":"integer"}`}

	got, diags := mergeDecls(t, defaulted, exampled, narrow)
	assert.Equal(t, ir.Unmodeled{
		losingDeclarationKey + "/allOf/0": entry(defaulted)[losingDeclarationKey+"/allOf/0"],
		losingDeclarationKey + "/allOf/1": entry(exampled)[losingDeclarationKey+"/allOf/1"],
	}, got.Unmodeled, "a default and examples from different declarations are two entries")
	require.Len(t, diags, 2, "and two infos")
	assert.Equal(t, jsontext.Pointer("/allOf/0"), diags[0].Provenance.Pointer)
	assert.Contains(t, diags[0].Message, "default")
	assert.NotContains(t, diags[0].Message, "examples")
	assert.Equal(t, jsontext.Pointer("/allOf/1"), diags[1].Provenance.Pointer)
	assert.Contains(t, diags[1].Message, "examples")

	wide := decl{ptr: "/allOf/0", kind: ir.PrimNumber, raw: `{"type":"number"}`}
	got, diags = mergeDecls(t, wide, both, narrow)
	assert.Equal(t, entry(both), got.Unmodeled, "a default and examples from one declaration are one entry")
	require.Len(t, diags, 1, "and one info naming both")
	assert.Contains(t, diags[0].Message, "default and examples")

	late := decl{ptr: "/allOf/1", kind: ir.PrimInteger, def: &big, raw: `{"late":5000000000}`}
	last := decl{ptr: "/allOf/2", kind: ir.PrimInt32, raw: `{"last":1}`}
	got, diags = mergeDecls(t, defaulted, late, last)
	assert.Equal(t, ir.Unmodeled{
		losingDeclarationKey + "/allOf/0": entry(defaulted)[losingDeclarationKey+"/allOf/0"],
		losingDeclarationKey + "/allOf/1": entry(late)[losingDeclarationKey+"/allOf/1"],
	}, got.Unmodeled, "a value adopted after a refusal is kept at the declaration that wrote it")
	require.Len(t, diags, 2)
	assert.Equal(t, jsontext.Pointer("/allOf/0"), diags[0].Provenance.Pointer)
	assert.Equal(t, jsontext.Pointer("/allOf/1"), diags[1].Provenance.Pointer)
}

// TestReconcileProperty_ALowerCaseDateTimeIsAdopted pins that RFC 3339 admits
// a lower-case T and Z, so a string default of that spelling lands on a
// datetime_offset field rather than being refused.
func TestReconcileProperty_ALowerCaseDateTimeIsAdopted(t *testing.T) {
	t.Parallel()
	lower := str("2024-01-01t00:00:00z")
	got, diags := mergeDecls(t,
		decl{ptr: "/a", kind: ir.PrimString, def: &lower, raw: `{}`},
		decl{ptr: "/b", kind: ir.PrimDatetimeOffset, raw: `{}`})
	require.NotNil(t, got.Default)
	assert.Equal(t, lower, *got.Default)
	assert.Empty(t, diags)
	assert.Empty(t, got.Unmodeled)
}

// TestReconcileProperty_ARefusedValueFromAnIndexlessDeclarationIsReported pins
// that a broader first declaration with no recorded source is reported
// unkeepable rather than dropped silently.
func TestReconcileProperty_ARefusedValueFromAnIndexlessDeclarationIsReported(t *testing.T) {
	t.Parallel()
	g, recorded := stubMerger(narrowingRegistry())
	half := num("1.5")
	m := &ir.Model{Properties: []ir.Property{{WireName: "p", Type: primRef(ir.PrimNumber), Default: &half, Provenance: ptrAt("/a")}}}
	at := origin{prov: ptrAt("/a")}
	idx := &PropIndex{byWire: map[string]*indexed{"p": {pos: 0, origins: origins{def: []origin{at}, examples: []origin{at}}}}}

	g.MergeProperty(m, idx, ir.Property{WireName: "p", Type: primRef(ir.PrimInteger), Provenance: ptrAt("/b")}, unread(t))

	require.Len(t, *recorded, 2)
	assert.Equal(t, diag.DegradedConstruct, (*recorded)[0].Code)
	assert.Equal(t, diag.UnpreservableConstruct, (*recorded)[1].Code)
	assert.Contains(t, (*recorded)[1].Message, errNoSource.Error())
	assert.Empty(t, m.Properties[0].Unmodeled)
}

// TestReconcileProperty_AScalarOverANarrowerKindWins pins that a Scalar over a
// kind the bare side equals or contains wins in either order, with no
// diagnostic and no kept declaration.
func TestReconcileProperty_AScalarOverANarrowerKindWins(t *testing.T) {
	t.Parallel()
	reg := judgeRegistry()
	for i, order := range [][2]ir.TypeRef{
		{ref("t/scalar-int"), primRef(ir.PrimNumber)},
		{primRef(ir.PrimNumber), ref("t/scalar-int")},
	} {
		g, recorded := stubMerger(reg)
		dst := ir.Property{WireName: "x", Type: order[0], Provenance: ptrAt("/allOf/0/properties/x")}
		src := ir.Property{WireName: "x", Type: order[1], Provenance: ptrAt("/allOf/1/properties/x")}

		g.reconcileProperty(&dst, new(origins), src, unread(t))

		assert.Equal(t, ir.TypeID("t/scalar-int"), dst.Type.Target, "order %d", i)
		assert.Empty(t, *recorded, "order %d", i)
		assert.Empty(t, dst.Unmodeled, "order %d", i)
	}
}

// TestReconcileProperty_TheTopTypeTakesTheOtherSidesNullAdmission pins that a
// narrower type over the top type keeps its own null admission rather than the
// conjunction with the top type's, since the top type admits null yet lowers
// non-nullable.
func TestReconcileProperty_TheTopTypeTakesTheOtherSidesNullAdmission(t *testing.T) {
	t.Parallel()
	nullable := func(r ir.TypeRef) ir.TypeRef { r.Nullable = true; return r }
	for i, order := range [][2]ir.TypeRef{
		{primRef(ir.PrimAny), nullable(primRef(ir.PrimInteger))},
		{nullable(primRef(ir.PrimInteger)), primRef(ir.PrimAny)},
	} {
		g, recorded := stubMerger(judgeRegistry())
		dst := ir.Property{WireName: "x", Type: order[0], Provenance: ptrAt("/allOf/0/properties/x")}
		src := ir.Property{WireName: "x", Type: order[1], Provenance: ptrAt("/allOf/1/properties/x")}

		g.reconcileProperty(&dst, new(origins), src, unread(t))

		assert.Equal(t, nullable(primRef(ir.PrimInteger)), dst.Type, "nullable integer (order %d)", i)
		assert.Empty(t, *recorded, "order %d", i)
	}
}

// twinRegistry holds, for each of Enum, Literal and Scalar, two hoisted nodes
// that differ only in ID and member IDs, and the variants that differ in a
// field. t/enum/x-named and t/enum/x-named2 are named copies of t/enum/x1.
func twinRegistry() map[ir.TypeID]ir.TypeDef {
	reg := narrowingRegistry()
	members := func(prefix string, vs ...string) []ir.EnumMember {
		out := make([]ir.EnumMember, 0, len(vs))
		for _, v := range vs {
			out = append(out, ir.EnumMember{ID: ir.EnumMemberID(prefix + v), Value: str(v)})
		}
		return out
	}
	enum := func(id ir.TypeID, ms []ir.EnumMember, desc string) *ir.Enum {
		e := &ir.Enum{ID: id, ValueType: ir.PrimString, Members: ms}
		e.Docs.Description = desc
		return e
	}
	reg["t/enum/x1"] = enum("t/enum/x1", members("x1/", "a", "b"), "")
	reg["t/enum/x2"] = enum("t/enum/x2", members("x2/", "a", "b"), "")
	reg["t/enum/x-described"] = enum("t/enum/x-described", members("xd/", "a", "b"), "some")
	reg["t/enum/x-reordered"] = enum("t/enum/x-reordered", members("xr/", "b", "a"), "")
	for _, id := range []ir.TypeID{"t/enum/x-ext1", "t/enum/x-ext2"} {
		e := enum(id, members(string(id)+"/", "a", "b"), "")
		e.Unmodeled = ir.Unmodeled{"openapi:x-foo": {
			Reason:     ir.ReasonVendorExtension,
			Value:      ir.RawValue("1"),
			Provenance: ptrAt(jsontext.Pointer("/at/" + string(id) + "/x-foo")),
		}}
		reg[id] = e
	}
	valued := enum("t/enum/x-ext-valued", members("xv/", "a", "b"), "")
	valued.Unmodeled = ir.Unmodeled{"openapi:x-foo": {
		Reason:     ir.ReasonVendorExtension,
		Value:      ir.RawValue("2"),
		Provenance: ptrAt("/at/t/enum/x-ext1/x-foo"),
	}}
	reg["t/enum/x-ext-valued"] = valued
	reg["t/lit/1"] = &ir.Literal{ID: "t/lit/1", Value: str("a")}
	reg["t/lit/2"] = &ir.Literal{ID: "t/lit/2", Value: str("a")}
	str := primRef(ir.PrimString)
	minLen := int64(2)
	reg["t/hex/1"] = &ir.Scalar{ID: "t/hex/1", Base: &str}
	reg["t/hex/2"] = &ir.Scalar{ID: "t/hex/2", Base: &str}
	reg["t/hex/min"] = &ir.Scalar{ID: "t/hex/min", Base: &str, Constraints: &ir.Constraints{MinLength: &minLen}}
	for _, td := range reg {
		switch td.(type) {
		case *ir.Enum, *ir.Literal, *ir.Scalar:
			td.Common().Anonymous = true
		}
	}
	reg["t/enum/x-named"] = enum("t/enum/x-named", members("xn/", "a", "b"), "")
	reg["t/enum/x-named2"] = enum("t/enum/x-named2", members("xn2/", "a", "b"), "")
	return reg
}

// TestReconcileProperty_IdenticalNodesAreOneType pins, in both orders, that two
// hoisted Enum, Literal or Scalar nodes equal but for what names their site are
// one type: no diagnostic and nothing kept aside. A named node wins over a
// hoisted copy of itself, and two named ones, or a pair that differs in a
// field, member order included, are an overlap reported as info.
func TestReconcileProperty_IdenticalNodesAreOneType(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		a, b ir.TypeID
		same bool
		win  ir.TypeID // the type that stands in both orders, or "" for the first
	}{
		{"two identical enums", "t/enum/x1", "t/enum/x2", true, ""},
		{"two identical literals", "t/lit/1", "t/lit/2", true, ""},
		{"two identical scalars", "t/hex/1", "t/hex/2", true, ""},
		{"enums that differ in the provenance of an extension", "t/enum/x-ext1", "t/enum/x-ext2", true, ""},
		{"a named enum against a hoisted copy", "t/enum/x-named", "t/enum/x1", true, "t/enum/x-named"},
		{"two named enums declared alike", "t/enum/x-named", "t/enum/x-named2", false, ""},
		{"enums that differ in an extension's value", "t/enum/x-ext1", "t/enum/x-ext-valued", false, ""},
		{"enums that differ in a description", "t/enum/x1", "t/enum/x-described", false, ""},
		{"enums that differ in member order", "t/enum/x1", "t/enum/x-reordered", false, ""},
		{"scalars that differ in a constraint", "t/hex/1", "t/hex/min", false, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			for i, order := range [][2]ir.TypeID{{tc.a, tc.b}, {tc.b, tc.a}} {
				g, recorded := stubMerger(twinRegistry())
				dst := ir.Property{WireName: "x", Type: ir.TypeRef{Target: order[0]}, Provenance: ptrAt("/allOf/0/properties/x")}
				src := ir.Property{WireName: "x", Type: ir.TypeRef{Target: order[1]}, Provenance: ptrAt("/allOf/1/properties/x")}

				g.reconcileProperty(&dst, new(origins), src, written(`{"type":"x"}`))

				want := tc.win
				if want == "" {
					want = order[0]
				}
				assert.Equal(t, want, dst.Type.Target, "the type that stands (order %d)", i)
				if tc.same {
					assert.Empty(t, *recorded, "no diagnostic (order %d)", i)
					assert.Empty(t, dst.Unmodeled, "nothing is kept (order %d)", i)
					continue
				}
				require.Len(t, *recorded, 1, "exactly one diagnostic (order %d)", i)
				assert.Equal(t, ir.SeverityInfo, (*recorded)[0].Severity, "order %d", i)
				assert.Len(t, dst.Unmodeled, 1, "the redeclaration is kept (order %d)", i)
			}
		})
	}
}
