package ir_test

import (
	"testing"

	"github.com/dexpace/morphic/ir"
)

// TestNaming_JSONContract pins the omitempty contract of ir.Naming — every
// field is optional (an anonymous hoisted type has empty Source, and a named
// entity with no aliases has a nil Aliases slice), so the zero value must
// marshal to an empty object. Losing an omitempty tag here would litter every
// anonymous type's JSON with empty-string noise; gaining one where a field
// must always appear (none do, today) would silently drop data. It also pins
// that a fully populated Naming — source, canonical, hint, and multiple
// aliases — survives a JSON round trip unchanged, since Naming is the
// identity vehicle threaded through every named entity in the IR
// (ir-design §3.2).
func TestNaming_JSONContract(t *testing.T) {
	t.Parallel()
	assertJSONContract(t, ir.Naming{}, `{}`, populatedNaming())
}

// TestNaming_AliasesDeterministicOrder pins that Aliases — a slice, never a
// map — round-trips in source order rather than being silently reordered.
// Aliases are used for schema-resolution matching, so reordering would be
// invisible in most tests but would change which alias is "first" for any
// consumer that cares.
func TestNaming_AliasesDeterministicOrder(t *testing.T) {
	t.Parallel()
	want := ir.Naming{Source: "id", Aliases: []string{"z_alias", "a_alias", "m_alias"}}
	assertRoundTrip(t, want)
}
