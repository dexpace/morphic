package irverify_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/ir/irverify"
)

// TestVerify_MalformedTypeIDIsAViolation plants IDs the grammar could not have
// produced. Each is a way a derivation can go wrong while the document still
// looks well-formed everywhere else: the registry key matches the node, every
// reference resolves, and nothing else in Verify has an opinion.
func TestVerify_MalformedTypeIDIsAViolation(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		id   ir.TypeID
	}{
		{name: "no kind prefix", id: "x/space/path"},
		{name: "kind alone", id: "t"},
		{name: "kind with a trailing separator and no space", id: "t/"},
		{name: "empty space", id: "t//path"},
		{name: "empty path", id: "t/space/"},
		{name: "another kind's prefix", id: "op/space/path"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			m := &ir.Model{TypeCommon: ir.TypeCommon{
				ID: tc.id, Name: ir.Naming{Source: "M", Canonical: "m"},
			}}
			doc := &ir.Document{Types: ir.TypeRegistry{tc.id: m}}
			assert.Contains(t, violationCodes(irverify.Verify(doc)), "ir/id-malformed",
				"%q is not an ID the grammar produces", tc.id)
		})
	}
}

// TestVerify_IDDisagreeingWithItsPointerIsAViolation is the half that catches
// GitHub #141's actual instance. "t/anonaddr" lost the separator between its
// space and its path, so by shape it reads as a legitimate ID in a space named
// "anonaddr" — indistinguishable from a real one. What gives it away is the
// source pointer recorded beside it, which the path no longer matches.
func TestVerify_IDDisagreeingWithItsPointerIsAViolation(t *testing.T) {
	t.Parallel()
	m := &ir.Model{TypeCommon: ir.TypeCommon{
		ID:         "t/anonaddr",
		Name:       ir.Naming{Source: "Addr", Canonical: "addr"},
		Provenance: ir.Provenance{Pointer: "addr"},
	}}
	doc := &ir.Document{Types: ir.TypeRegistry{m.ID: m}}

	got := irverify.Verify(doc)
	assert.Contains(t, violationCodes(got), "ir/id-provenance-disagreement")
	assert.NotContains(t, violationCodes(got), "ir/id-malformed",
		"the point of this case is that shape alone cannot tell: it is well-shaped")
}

// TestVerify_WrongPointerIsAViolation covers the other direction — a
// well-shaped ID whose recorded pointer names a different coordinate than the
// one it was built from. That is the defect a signature change introduces when
// it passes the wrong `at`, which nothing else in the repository can see.
func TestVerify_WrongPointerIsAViolation(t *testing.T) {
	t.Parallel()
	m := &ir.Model{TypeCommon: ir.TypeCommon{
		ID:         "t/openapi/components/schemas/Child",
		Name:       ir.Naming{Source: "Child", Canonical: "child"},
		Provenance: ir.Provenance{Pointer: "/components/schemas/Parent"},
	}}
	doc := &ir.Document{Types: ir.TypeRegistry{m.ID: m}}
	assert.Contains(t, violationCodes(irverify.Verify(doc)), "ir/id-provenance-disagreement")
}

// TestVerify_PointerlessIDIsClean pins the exclusion: a primitive is shared
// across every source position and derives from none, so it records no pointer
// and is held to shape alone. Holding it to an agreement it cannot have would
// make every document violate.
func TestVerify_PointerlessIDIsClean(t *testing.T) {
	t.Parallel()
	p := &ir.Primitive{TypeCommon: ir.TypeCommon{ID: "t/prim/string"}, Prim: ir.PrimString}
	doc := &ir.Document{Types: ir.TypeRegistry{p.ID: p}}
	assert.Empty(t, irverify.Verify(doc))
}

// TestVerify_AuthIDIsHeldToTheSameRule pins that the check is about the grammar
// rather than about one registry: an auth scheme's ID is derived the same way and
// is held to the same agreement.
func TestVerify_AuthIDIsHeldToTheSameRule(t *testing.T) {
	t.Parallel()
	scheme := ir.AuthScheme{
		ID:         "auth/openapi/components/securitySchemes/Other",
		Name:       ir.Naming{Source: "apiKey", Canonical: "api_key"},
		Provenance: ir.Provenance{Pointer: "/components/securitySchemes/apiKey"},
	}
	doc := &ir.Document{Auth: map[ir.AuthID]ir.AuthScheme{scheme.ID: scheme}}
	assert.Contains(t, violationCodes(irverify.Verify(doc)), "ir/id-provenance-disagreement")
}

// TestVerify_DerivedIDsAreClean is the control for every case above: an ID whose
// path is exactly the pointer it records reports nothing, at both the
// pointer-derived spaces a compiler addresses and a minted one.
func TestVerify_DerivedIDsAreClean(t *testing.T) {
	t.Parallel()
	doc := &ir.Document{Types: ir.TypeRegistry{}}
	for _, id := range []ir.TypeID{
		"t/openapi/components/schemas/User",
		"t/anon/paths/~1pets/get/responses/200/content/application~1json/schema",
		"t/composed/components/schemas/Combo/oneOf/0",
	} {
		path, ok := ir.IDPath(ir.IDKindType, string(id))
		require.True(t, ok, "%s carries a path", id)
		doc.Types[id] = &ir.Model{TypeCommon: ir.TypeCommon{
			ID:         id,
			Name:       ir.Naming{Source: "N", Canonical: "n"},
			Provenance: ir.Provenance{Pointer: "/" + path},
		}}
	}
	assert.Empty(t, irverify.Verify(doc))
}

// violationCodes returns the codes of vs, for set-membership assertions that do
// not depend on violation order or message wording.
func violationCodes(vs []irverify.Violation) []string {
	out := make([]string, 0, len(vs))
	for _, v := range vs {
		out = append(out, v.Code)
	}
	return out
}
