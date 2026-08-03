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
// and is held to no agreement with one. It is held to the ID its kind derives
// instead, which the cases below cover; requiring a pointer agreement it cannot
// have would make every document violate.
func TestVerify_PointerlessIDIsClean(t *testing.T) {
	t.Parallel()
	p := &ir.Primitive{TypeCommon: ir.TypeCommon{ID: "t/prim/string"}, Prim: ir.PrimString}
	doc := &ir.Document{Types: ir.TypeRegistry{p.ID: p}}
	assert.Empty(t, irverify.Verify(doc))
}

// TestVerify_PrimitiveAwayFromItsSharedIDIsAViolation plants the primitive IDs
// nothing else in Verify has an opinion about. Each is well-shaped, keyed by its
// own node ID, and records no pointer to disagree with — so before this check
// every one of them passed clean (GitHub #73).
//
// The rows are the two ways the agreement breaks. The first two put a shared
// leaf in a format's own space, so the same type lowered from two formats stops
// being the same type — and the second is the shape that looks right, a private
// space merely spelled "prim". The rest keep the shared space but carry a path
// that is not the node's kind: another kind, no path at all, or the kind
// re-cased. Those contradict themselves, and no consumer switching on either the
// ID or the kind can resolve which one to believe.
func TestVerify_PrimitiveAwayFromItsSharedIDIsAViolation(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		id   ir.TypeID
		kind ir.PrimKind
	}{
		{name: "a per-position ID", id: "t/openapi/components/schemas/Name", kind: ir.PrimString},
		{name: "a compiler's private prim space", id: "t/graphql/prim/string", kind: ir.PrimString},
		{name: "the right space, the wrong kind", id: "t/prim/int32", kind: ir.PrimString},
		{name: "the space alone", id: "t/prim", kind: ir.PrimString},
		{name: "a re-cased kind", id: "t/prim/dateTimeOffset", kind: ir.PrimDatetimeOffset},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			p := &ir.Primitive{TypeCommon: ir.TypeCommon{ID: tc.id}, Prim: tc.kind}
			doc := &ir.Document{Types: ir.TypeRegistry{tc.id: p}}

			got := irverify.Verify(doc)
			assert.Contains(t, violationCodes(got), "ir/prim-id-not-derived")
			assert.NotContains(t, violationCodes(got), "ir/id-malformed",
				"the point of these cases is that shape alone cannot tell: each is well-shaped")
		})
	}
}

// TestVerify_KindlessPrimitiveIsReportedOnItsOwnTerms pins the one case with no
// destination to name. ir.PrimTypeID derives t/prim/ from the zero-value kind,
// which is not an ID at all, so the message must not offer it as the place the
// node belongs — a reader sent there fixes the wrong end, and the check would be
// telling them to write an ID checkIDs reports as malformed.
func TestVerify_KindlessPrimitiveIsReportedOnItsOwnTerms(t *testing.T) {
	t.Parallel()
	const id ir.TypeID = "t/openapi/components/schemas/Name"
	doc := &ir.Document{Types: ir.TypeRegistry{
		id: &ir.Primitive{TypeCommon: ir.TypeCommon{ID: id}},
	}}

	got := irverify.Verify(doc)
	require.Len(t, got, 1)
	assert.Equal(t, "ir/prim-id-not-derived", got[0].Code)
	assert.Contains(t, got[0].Message, "carries no kind")
	assert.NotContains(t, got[0].Message, string(ir.PrimTypeID("")),
		"t/prim/ is not an ID; naming it as the destination sends the reader to the wrong end")
}

// TestVerify_NonPrimitiveInThePrimSpaceIsAViolation covers the other direction.
// The space is reserved rather than conventional: a node there either collides
// with the primitive of that kind outright, or squats a name the next PrimKind
// takes — and either way which node is reached depends on what was interned
// first, which is invariant 3's corollary.
func TestVerify_NonPrimitiveInThePrimSpaceIsAViolation(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		id   ir.TypeID
	}{
		{name: "squatting an existing kind", id: "t/prim/string"},
		{name: "squatting a name no kind uses yet", id: "t/prim/instant"},
		{name: "the space alone", id: "t/prim"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			m := &ir.Model{TypeCommon: ir.TypeCommon{
				ID: tc.id, Name: ir.Naming{Source: "M", Canonical: "m"},
			}}
			doc := &ir.Document{Types: ir.TypeRegistry{tc.id: m}}
			assert.Contains(t, violationCodes(irverify.Verify(doc)), "ir/prim-space-reserved")
		})
	}
}

// TestVerify_PrimIDChecksAreScopedToTheSpaceAndTheKind is the control for both
// checks above: a document holding every primitive at the ID its kind derives,
// beside ordinary types in a format's own space, reports nothing. Without it a
// check that fired on everything would pass both tables and read as proof.
//
// The two models are chosen against the implementation that would be wrong in
// the easy way. "prim" appears in one as a path segment and in the other inside
// a name, and neither is in the reserved space — which only reading the space
// segment can tell. Matching the ID as a substring passes both tables above and
// fails here.
func TestVerify_PrimIDChecksAreScopedToTheSpaceAndTheKind(t *testing.T) {
	t.Parallel()
	doc := &ir.Document{Types: ir.TypeRegistry{}}
	for _, kind := range []ir.PrimKind{ir.PrimString, ir.PrimInt32, ir.PrimDatetimeOffset, ir.PrimAny} {
		id := ir.PrimTypeID(kind)
		doc.Types[id] = &ir.Primitive{TypeCommon: ir.TypeCommon{ID: id}, Prim: kind}
	}
	for _, m := range []struct {
		id      ir.TypeID
		pointer string
		source  string
	}{
		{id: "t/openapi/prim/string", pointer: "/prim/string", source: "String"},
		{id: "t/openapi/components/schemas/primitive", pointer: "/components/schemas/primitive", source: "primitive"},
	} {
		doc.Types[m.id] = &ir.Model{TypeCommon: ir.TypeCommon{
			ID:         m.id,
			Name:       ir.Naming{Source: m.source, Canonical: ir.CanonicalWords(m.source)},
			Provenance: ir.Provenance{Pointer: m.pointer},
		}}
	}

	assert.Empty(t, irverify.Verify(doc),
		"an ID carrying \"prim\" outside the space segment is not in the reserved space")
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
