// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package ir_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dexpace/morphic/ir"
)

// TestNaming_JSONContract pins that every ir.Naming field is optional — an
// anonymous hoisted type has empty Source, and a named entity may have no
// aliases — so the zero value marshals to an empty object, and that a fully
// populated Naming survives a JSON round trip. Naming is the identity vehicle
// threaded through every named entity in the IR (docs/ir/naming.md).
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

// TestNamingFor_KeepsTheSpellingAndTheWords pins the pairing that makes a Naming
// neutral: the source spelling is preserved verbatim for anyone who needs the
// original, and the words beside it carry no casing an emitter should own.
func TestNamingFor_KeepsTheSpellingAndTheWords(t *testing.T) {
	t.Parallel()
	assert.Equal(t, ir.Naming{Source: "com.example.User", Canonical: "com_example_user"},
		ir.NamingFor("com.example.User"))
	assert.Equal(t, ir.Naming{Source: "***", Canonical: ""}, ir.NamingFor("***"),
		"a spelling with no words still keeps its spelling")
}

// TestNamingFor_EmptySourceIsMintedAName covers the input that used to produce a
// Naming with nothing in any channel — a legal enum value, property key, tag or
// component name in OpenAPI, and an entity an emitter had no way to name
// (GitHub #251). There is no spelling to keep, so the minted name goes in the
// hint channel rather than being fabricated as a Source the spec never wrote.
func TestNamingFor_EmptySourceIsMintedAName(t *testing.T) {
	t.Parallel()
	got := ir.NamingFor("")
	assert.Equal(t, ir.Naming{Hint: "empty"}, got)
	assert.Empty(t, got.Source, "nothing is fabricated into the source channel")
}
