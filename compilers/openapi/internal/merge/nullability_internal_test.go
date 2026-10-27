package merge

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dexpace/morphic/ir"
)

// TestIntersectNull_TheMarkerFollowsTheBit pins what the intersection does with
// TypeRef.Inferred. The declared reading admits null only where both sides
// declare it, so the result's null rests on a heuristic wherever either side's
// did: it carries every marker either names, once each, and none once it admits
// no null, where a marker would qualify nothing.
func TestIntersectNull_TheMarkerFollowsTheBit(t *testing.T) {
	t.Parallel()
	ref := func(nullable bool, inferred ir.Inferred) ir.TypeRef {
		return ir.TypeRef{Target: "t/str", Nullable: nullable, Inferred: inferred}
	}
	cases := []struct {
		name           string
		dst, src, want ir.TypeRef
	}{
		{"declared on both sides", ref(true, ""), ref(true, ""), ref(true, "")},
		{"inferred on the redeclaration", ref(true, ""), ref(true, "h"), ref(true, "h")},
		{"inferred on the first", ref(true, "h"), ref(true, ""), ref(true, "h")},
		{"one marker on both", ref(true, "h"), ref(true, "h"), ref(true, "h")},
		{"two markers, the first's first", ref(true, "a,b"), ref(true, "b,c"), ref(true, "a,b,c")},
		{"a side admitting none drops the marker", ref(true, "h"), ref(false, ""), ref(false, "")},
		{"so does the other order", ref(false, ""), ref(true, "h"), ref(false, "")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, intersectNull(tc.dst, tc.src))
		})
	}
}
