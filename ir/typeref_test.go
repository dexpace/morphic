package ir_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
)

// TestTypeRef_ZeroValueShape pins the half of invariant #8 that lives on
// TypeRef: Nullable must serialize even when false, because "not nullable" is
// a declared fact distinct from "unspecified" — there is no third state. If
// `,omitempty` were added to Nullable, a non-nullable reference would
// serialize identically to one that never set the field at all, and
// TestTypeRef_NullableNeverOmitted below would catch it directly.
func TestTypeRef_ZeroValueShape(t *testing.T) {
	t.Parallel()
	assertZeroValueShape(t, ir.TypeRef{}, `{"target":"","nullable":false}`)
}

// TestTypeRef_PopulatedRoundTrip pins that a populated TypeRef round-trips.
func TestTypeRef_PopulatedRoundTrip(t *testing.T) {
	t.Parallel()
	assertRoundTrip(t, populatedTypeRef())
}

// TestTypeRef_NullableNeverOmitted is the TypeRef half of the invariant #8
// four-state test (the Property half lives in property_test.go as
// TestProperty_RequiredNullableFourStates). Nullability lives on the
// reference, not the target type, because the same type is nullable in one
// position and not another (ir-design §3.3); collapsing "false" and "absent"
// into one wire representation would make that per-usage distinction
// unrecoverable from a decoded document.
func TestTypeRef_NullableNeverOmitted(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		nullable bool
		want     string
	}{
		{name: "not nullable", nullable: false, want: `{"target":"t/x","nullable":false}`},
		{name: "nullable", nullable: true, want: `{"target":"t/x","nullable":true}`},
	}

	// The distinctness check needs every row's encoding in hand, so it is
	// computed synchronously up front. Subtests below re-derive and assert on
	// each row independently and may run in parallel; note that t.Parallel()
	// pauses a subtest until this enclosing function returns, so an assertion
	// that depended on the subtests' side effects would silently run against
	// an empty result — precisely the bug this two-phase structure avoids.
	seen := make(map[string]bool, len(tests))
	for _, tt := range tests {
		ref := ir.TypeRef{Target: "t/x", Nullable: tt.nullable}
		raw, err := json.Marshal(ref)
		require.NoError(t, err)
		seen[string(raw)] = true
	}
	assert.Len(t, seen, len(tests), "the two nullable states must produce distinct JSON encodings")

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ref := ir.TypeRef{Target: "t/x", Nullable: tt.nullable}

			raw, err := json.Marshal(ref)
			require.NoError(t, err)
			got := string(raw)
			assert.Equal(t, tt.want, got)
			assert.Contains(t, got, `"nullable":`, "invariant #8: nullable must appear literally regardless of its value")

			var back ir.TypeRef
			require.NoError(t, json.Unmarshal(raw, &back))
			assert.Equal(t, ref, back)
		})
	}
}
