package ir_test

import (
	"testing"

	"github.com/dexpace/morphic/ir"
)

// TestAvailability_ZeroValueShape pins Availability's omitempty contract:
// formats without versioning leave the whole field nil on the owning entity,
// and *Availability itself has every member optional, so its zero value must
// marshal to an empty object rather than six empty-slice/empty-string fields.
func TestAvailability_ZeroValueShape(t *testing.T) {
	t.Parallel()
	assertZeroValueShape(t, ir.Availability{}, `{}`)
}

// TestAvailability_PopulatedRoundTrip pins that a fully populated Availability
// — add/remove/re-add cycles, a deprecation version, renames, type changes,
// and required-flips — survives a JSON round trip (ir-design §11).
func TestAvailability_PopulatedRoundTrip(t *testing.T) {
	t.Parallel()
	assertRoundTrip(t, *populatedAvailability())
}

// TestVersionedName_ZeroValueShape pins VersionedName's omitempty contract:
// both fields are optional.
func TestVersionedName_ZeroValueShape(t *testing.T) {
	t.Parallel()
	assertZeroValueShape(t, ir.VersionedName{}, `{}`)
}

// TestVersionedName_PopulatedRoundTrip pins that a populated VersionedName
// round-trips.
func TestVersionedName_PopulatedRoundTrip(t *testing.T) {
	t.Parallel()
	assertRoundTrip(t, ir.VersionedName{Version: "v1", Name: "oldName"})
}

// TestVersionedType_ZeroValueShape pins VersionedType's contract that Type
// carries no omitempty — a prior type is always a concrete TypeRef, never
// implicitly absent, even though the surrounding TypeChangedFrom slice can be
// nil.
func TestVersionedType_ZeroValueShape(t *testing.T) {
	t.Parallel()
	assertZeroValueShape(t, ir.VersionedType{}, `{"type":{"target":"","nullable":false}}`)
}

// TestVersionedType_PopulatedRoundTrip pins that a populated VersionedType
// round-trips.
func TestVersionedType_PopulatedRoundTrip(t *testing.T) {
	t.Parallel()
	assertRoundTrip(t, ir.VersionedType{Version: "v1", Type: populatedTypeRef()})
}

// TestVersionedBool_ZeroValueShape pins VersionedBool's contract that
// WasRequired carries no omitempty: "was required: false" at a given version
// is a distinct, meaningful fact from the field being absent, exactly the
// same reasoning invariant #8 applies to Property.Required.
func TestVersionedBool_ZeroValueShape(t *testing.T) {
	t.Parallel()
	assertZeroValueShape(t, ir.VersionedBool{}, `{"wasRequired":false}`)
}

// TestVersionedBool_PopulatedRoundTrip pins that a populated VersionedBool
// round-trips, and that both boolean states are distinguishable on the wire.
func TestVersionedBool_PopulatedRoundTrip(t *testing.T) {
	t.Parallel()
	tests := []ir.VersionedBool{
		{Version: "v1", WasRequired: true},
		{Version: "v2", WasRequired: false},
	}
	for _, want := range tests {
		t.Run(want.Version, func(t *testing.T) {
			t.Parallel()
			assertRoundTrip(t, want)
		})
	}
}
