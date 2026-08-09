package irverify_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/ir/irverify"
)

// primDoc interns one primitive of kind at the ID that kind derives, so the
// document is sound in every way except the kind itself — checkPrimIDs has
// nothing to say about it and only the new claim can fire.
func primDoc(kind ir.PrimKind) *ir.Document {
	id := ir.PrimTypeID(kind)
	return &ir.Document{Types: ir.TypeRegistry{id: &ir.Primitive{
		TypeCommon: ir.TypeCommon{ID: id, Provenance: ir.Provenance{Source: ir.NoSource}},
		Prim:       kind,
	}}}
}

// TestVerify_UndeclaredPrimKindIsAViolation is the case checkPrimIDs is blind
// to: the ID is derived from the kind, so it agrees with it — consistently, and
// wrongly. An emitter switching on PrimKind has no arm for any of these.
func TestVerify_UndeclaredPrimKindIsAViolation(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		kind ir.PrimKind
	}{
		{name: "an invented kind", kind: "flt"},
		{name: "a re-cased declared kind", kind: "Float64"},
		{name: "a near miss on a declared kind", kind: "datetimeOffset"},
		{name: "not an identifier at all", kind: "¡no such kind¡"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := irverify.Verify(primDoc(tc.kind))
			require.Len(t, got, 1, "the ID derives from the kind, so nothing else can be wrong")
			assert.Equal(t, "ir/unknown-prim-kind", got[0].Code)
			assert.Equal(t, "types["+string(ir.PrimTypeID(tc.kind))+"]", got[0].Path)
			assert.Contains(t, got[0].Message, string(tc.kind))
		})
	}
}

// TestVerify_DeclaredPrimKindIsClean is the other half of the proof: a check
// that cannot stay silent is no better than one that cannot fire. The documents
// differ from the ones above only in the kind, so nothing but the kind can be
// what the check reads.
func TestVerify_DeclaredPrimKindIsClean(t *testing.T) {
	t.Parallel()
	for _, kind := range []ir.PrimKind{ir.PrimString, ir.PrimFloat64, ir.PrimDatetimeOffset, ir.PrimAny} {
		t.Run(string(kind), func(t *testing.T) {
			t.Parallel()
			assert.Empty(t, irverify.Verify(primDoc(kind)))
		})
	}
}

// authDoc interns one auth scheme naming kind, keyed by and carrying a
// well-formed ID and a full naming, so nothing but the mechanism is open to
// question.
func authDoc(kind ir.AuthKind) *ir.Document {
	const id ir.AuthID = "auth/openapi/components/securitySchemes/token"
	return &ir.Document{Auth: map[ir.AuthID]ir.AuthScheme{id: {
		ID: id, Name: named("token"), Kind: kind,
		Provenance: ir.Provenance{Source: ir.NoSource},
	}}}
}

// TestVerify_UndeclaredAuthKindIsAViolation covers the class the compiler-side
// refusal closed one instance of. The empty kind is the shape that reached the
// IR: a scheme interned naming no mechanism at all, indistinguishable from a
// sound one to every check that reads only a key and an ID.
func TestVerify_UndeclaredAuthKindIsAViolation(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		kind ir.AuthKind
	}{
		{name: "no mechanism at all", kind: ""},
		{name: "a misspelled mechanism", kind: "api_key"},
		{name: "an invented mechanism", kind: "mtls-but-not-really"},
		{name: "a re-cased mechanism", kind: "OAuth2"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := irverify.Verify(authDoc(tc.kind))
			require.Len(t, got, 1)
			assert.Equal(t, "ir/unknown-auth-kind", got[0].Code)
			assert.Equal(t, "auth[auth/openapi/components/securitySchemes/token]", got[0].Path)
		})
	}
}

// TestVerify_DeclaredAuthKindIsClean holds the silent half: the same document
// with a mechanism ir declares must yield nothing.
func TestVerify_DeclaredAuthKindIsClean(t *testing.T) {
	t.Parallel()
	for _, kind := range []ir.AuthKind{ir.AuthKindOAuth2, ir.AuthKindAPIKey, ir.AuthKindX509, ir.AuthKindCustom} {
		t.Run(string(kind), func(t *testing.T) {
			t.Parallel()
			assert.Empty(t, irverify.Verify(authDoc(kind)))
		})
	}
}
