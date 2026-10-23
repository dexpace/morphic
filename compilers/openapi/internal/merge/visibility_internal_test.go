package merge

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dexpace/morphic/ir"
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
