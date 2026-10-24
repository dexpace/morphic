package merge

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/ir"
)

// TestFold_VisibilityAdoptedFromRedeclaration pins the issue's
// own reproduction at the reconcile layer (GitHub #34): a property left
// unrestricted by its first declaration must adopt a later branch's readOnly
// rather than silently keep the first, empty Visibility.
func TestFold_VisibilityAdoptedFromRedeclaration(t *testing.T) {
	t.Parallel()
	g, _ := stubMerger(nil)
	dst := ir.Property{WireName: "id"}

	dst = foldTwo(g, dst, ir.Property{WireName: "id", Visibility: readOnlyVisibility, Provenance: ptrAt("/other")}, unread(t))

	assert.Equal(t, readOnlyVisibility, dst.Visibility)
}

// TestFold_DisjointVisibilityIsARestrictionNotAConflict pins both
// halves of the choice for an allOf pairing that admits no lifecycle at all
// (readOnly against writeOnly on the same field). mergeVisibility represents it
// exactly as None, so it is recorded rather than dropped and kept — nothing is
// arbitrarily discarded, as it would be for an incompatible-type
// redeclaration. But exact is not unremarkable: a
// field neither a request nor a response can carry is a composition that cannot
// take effect, so it is warned about under its own code.
func TestFold_DisjointVisibilityIsARestrictionNotAConflict(t *testing.T) {
	t.Parallel()
	g, recorded := stubMerger(nil)
	dst := ir.Property{WireName: "id", Visibility: readOnlyVisibility}

	dst = foldTwo(g, dst, ir.Property{WireName: "id", Visibility: writeOnlyVisibility, Provenance: ptrAt("/other")}, unread(t))

	assert.Equal(t, ir.Visibility{None: true}, dst.Visibility)
	require.Len(t, *recorded, 1, "an emptied visibility set is reported, not passed over in silence")
	assert.Equal(t, diag.DisjointVisibility, (*recorded)[0].Code,
		"reported under its own code, not as a conflicting redeclaration")
	assert.Equal(t, ir.SeverityWarning, (*recorded)[0].Severity)
}

// TestFold_AlreadyInvisibleVisibilityIsNotReAnnounced pins the half
// of the report guard the disjoint case above cannot reach. The warning is about
// the transition — two restricted branches whose intersection empties — not
// about the state of being invisible, so a side that was already None merges
// quietly. Drop that half of the condition and every later redeclaration of an
// invisible field re-announces a fact the document has already been told, which
// no assertion in the disjoint case would notice.
func TestFold_AlreadyInvisibleVisibilityIsNotReAnnounced(t *testing.T) {
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

			dst = foldTwo(g, dst, ir.Property{WireName: "id", Visibility: tc.src, Provenance: ptrAt("/other")}, unread(t))

			assert.Equal(t, ir.Visibility{None: true}, dst.Visibility)
			assert.Empty(t, *recorded, "None was already the answer; this merge announced nothing new")
		})
	}
}
