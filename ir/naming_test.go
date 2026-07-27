package ir_test

import (
	"testing"

	"github.com/dexpace/morphic/ir"
)

// TestNaming_JSONContract pins that every ir.Naming field is optional — an
// anonymous hoisted type has empty Source, and a named entity may have no
// aliases — so the zero value marshals to an empty object, and that a fully
// populated Naming survives a JSON round trip. Naming is the identity vehicle
// threaded through every named entity in the IR (ir-design §3.2).
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
