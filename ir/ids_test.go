package ir_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
)

// TestIDTypes_MarshalAsPlainJSONStrings pins ids.go's whole contract: every ID
// type is a bare string alias, so it must marshal as a plain JSON string, not
// as an object or array. If any of these were ever changed to a struct (to
// carry, say, a cached hash alongside the pointer), every consumer that treats
// an ID as a map key or does string equality would silently break, and this is
// the test that would catch the change at the wire-format boundary (ir-design
// §3.1: IDs are opaque but deterministic strings).
func TestIDTypes_MarshalAsPlainJSONStrings(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		zero any
		full any
		want string
	}{
		{name: "TypeID", zero: ir.TypeID(""), full: ir.TypeID("t/openapi/components/schemas/User"), want: `"t/openapi/components/schemas/User"`},
		{name: "OpID", zero: ir.OpID(""), full: ir.OpID("op/openapi/paths/~1users/get"), want: `"op/openapi/paths/~1users/get"`},
		{name: "ServiceID", zero: ir.ServiceID(""), full: ir.ServiceID("s/openapi/petstore"), want: `"s/openapi/petstore"`},
		{name: "ChannelID", zero: ir.ChannelID(""), full: ir.ChannelID("c/asyncapi/user-signup"), want: `"c/asyncapi/user-signup"`},
		{name: "MessageID", zero: ir.MessageID(""), full: ir.MessageID("m/asyncapi/UserSignedUp"), want: `"m/asyncapi/UserSignedUp"`},
		{name: "AuthID", zero: ir.AuthID(""), full: ir.AuthID("auth/openapi/apiKey"), want: `"auth/openapi/apiKey"`},
		{name: "PropID", zero: ir.PropID(""), full: ir.PropID("p/openapi/components/schemas/User/properties/id"), want: `"p/openapi/components/schemas/User/properties/id"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			zeroRaw, err := json.Marshal(tt.zero)
			require.NoError(t, err)
			assert.Equal(t, `""`, string(zeroRaw), "zero-value %s must marshal as an empty JSON string", tt.name)

			fullRaw, err := json.Marshal(tt.full)
			require.NoError(t, err)
			assert.Equal(t, tt.want, string(fullRaw))
		})
	}
}

// TestTypeID_JSONRoundTrip pins that TypeID survives marshal/unmarshal, since
// it is the key type of Document.Types (TypeRegistry) and the target of every
// TypeRef; a lossy round-trip here would silently dangle every reference in
// the document.
func TestTypeID_JSONRoundTrip(t *testing.T) {
	t.Parallel()
	want := ir.TypeID("t/openapi/components/schemas/User")
	raw, err := json.Marshal(want)
	require.NoError(t, err)
	var got ir.TypeID
	require.NoError(t, json.Unmarshal(raw, &got))
	assert.Equal(t, want, got)
}

// TestPropID_UsableAsMapKey pins that PropID (and by construction every ID
// type sharing its underlying string representation) can be used as a JSON
// object key, since Property lookups and PropPath segments depend on that.
func TestPropID_UsableAsMapKey(t *testing.T) {
	t.Parallel()
	m := map[ir.PropID]int{
		"p/z": 1,
		"p/a": 2,
	}
	raw, err := json.Marshal(m)
	require.NoError(t, err)
	assert.Equal(t, `{"p/a":2,"p/z":1}`, string(raw), "map keys sort lexically, matching invariant #7")
}

// TestWellFormedID_Shape pins the shape every synthetic ID must take. The rows
// are the ways a derivation can go wrong: a missing kind, a missing space, a
// missing separator, an empty segment.
//
// "t/anonaddr" is in the well-formed set deliberately. It is the malformed ID
// GitHub #141 reported — the separator between space and path went missing — and
// it is well-formed by shape, because a space with no path is legal and
// "anonaddr" is a space like any other. Recording it here states the limit
// rather than leaving a reader to assume shape catches everything; what catches
// it is the ID agreeing with the pointer it was derived from, which irverify
// checks.
func TestWellFormedID_Shape(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		kind string
		id   string
		want bool
	}{
		{name: "kind, space and path", kind: ir.IDKindType, id: "t/openapi/components/schemas/User", want: true},
		{name: "space alone names one node", kind: ir.IDKindType, id: "t/prim", want: true},
		{name: "a path with its own separators", kind: ir.IDKindProp, id: "p/openapi/a/b/c", want: true},
		{name: "a lost separator still reads as a space", kind: ir.IDKindType, id: "t/anonaddr", want: true},
		{name: "multi-character kind", kind: ir.IDKindOp, id: "op/openapi/paths/~1x/get", want: true},

		{name: "empty", kind: ir.IDKindType, id: "", want: false},
		{name: "kind alone", kind: ir.IDKindType, id: "t", want: false},
		{name: "kind and separator, no space", kind: ir.IDKindType, id: "t/", want: false},
		{name: "empty space", kind: ir.IDKindType, id: "t//path", want: false},
		{name: "empty path", kind: ir.IDKindType, id: "t/openapi/", want: false},
		{name: "another kind's prefix", kind: ir.IDKindType, id: "op/openapi/x", want: false},
		{name: "kind as a prefix of a longer word", kind: ir.IDKindType, id: "types/openapi/x", want: false},
		{name: "no separator after the kind", kind: ir.IDKindOp, id: "openapi/x", want: false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := ir.WellFormedID(tc.kind, tc.id); got != tc.want {
				t.Errorf("WellFormedID(%q, %q) = %v, want %v", tc.kind, tc.id, got, tc.want)
			}
		})
	}
}

// TestIDPath_Extraction pins what an ID's path is, including the two answers
// that are not a path: a malformed ID has none to report, and a space-only ID
// carries none by construction.
func TestIDPath_Extraction(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		kind    string
		id      string
		want    string
		wantHas bool
	}{
		{
			name: "everything after the space", kind: ir.IDKindType,
			id: "t/openapi/components/schemas/User", want: "components/schemas/User", wantHas: true,
		},
		{name: "a space-only ID carries no path", kind: ir.IDKindType, id: "t/prim"},
		{name: "a malformed ID reports none", kind: ir.IDKindType, id: "t//x"},
		{name: "a wrong-kind ID reports none", kind: ir.IDKindType, id: "op/openapi/x"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, has := ir.IDPath(tc.kind, tc.id)
			if got != tc.want || has != tc.wantHas {
				t.Errorf("IDPath(%q, %q) = (%q, %v), want (%q, %v)",
					tc.kind, tc.id, got, has, tc.want, tc.wantHas)
			}
		})
	}
}
