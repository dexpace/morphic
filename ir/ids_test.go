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

// TestPrimTypeID_IsTheSharedScheme pins the spelling every compiler must reach
// for a primitive. These IDs are written into every golden IR snapshot and are
// the one identity two documents lowered from different formats have to agree
// on, so a change here is a silent breaking change across formats.
func TestPrimTypeID_IsTheSharedScheme(t *testing.T) {
	t.Parallel()
	assert.Equal(t, ir.TypeID("t/prim/string"), ir.PrimTypeID(ir.PrimString))
	assert.Equal(t, ir.TypeID("t/prim/datetime_offset"), ir.PrimTypeID(ir.PrimDatetimeOffset),
		"a multi-word kind is spelled as its constant, not re-cased")
}

// TestPrimTypeID_IsWellFormedAndInjective walks the whole primitive vocabulary
// rather than a sample of it. Two properties have to hold for every kind, and a
// sample cannot show either: the ID is one the grammar produces — irverify
// rejects every document otherwise — and distinct kinds do not collide, since
// two kinds sharing an ID would make the node reached for one of them depend on
// which was interned first.
func TestPrimTypeID_IsWellFormedAndInjective(t *testing.T) {
	t.Parallel()
	byID := make(map[ir.TypeID]ir.PrimKind, len(primKindSpellings))
	for kind := range primKindSpellings {
		id := ir.PrimTypeID(kind)
		require.True(t, ir.WellFormedID(ir.IDKindType, string(id)),
			"%q is not an ID the grammar produces", id)

		space, ok := ir.IDSpace(ir.IDKindType, string(id))
		require.True(t, ok)
		assert.Equal(t, ir.IDSpacePrim, space, "%q is addressed outside the shared space", id)

		path, has := ir.IDPath(ir.IDKindType, string(id))
		require.True(t, has)
		assert.Equal(t, string(kind), path, "the path is the kind and nothing else")

		if other, clash := byID[id]; clash {
			t.Errorf("kinds %q and %q share the ID %q", kind, other, id)
		}
		byID[id] = kind
	}
	assert.Len(t, byID, len(primKindSpellings), "every kind reaches an ID of its own")
}

// TestIDSpace_Extraction pins what an ID's space is, including the answers that
// are not one: a malformed ID has no space to report, and neither has an ID
// carrying another kind's prefix.
func TestIDSpace_Extraction(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		kind   string
		id     string
		want   string
		wantOK bool
	}{
		{
			name: "the segment before the path", kind: ir.IDKindType,
			id: "t/openapi/components/schemas/User", want: "openapi", wantOK: true,
		},
		{name: "a space-only ID is all space", kind: ir.IDKindType, id: "t/prim", want: "prim", wantOK: true},
		{name: "a path with its own separators", kind: ir.IDKindProp, id: "p/openapi/a/b/c", want: "openapi", wantOK: true},
		{name: "an empty space reports none", kind: ir.IDKindType, id: "t//x"},
		{name: "an empty path reports none", kind: ir.IDKindType, id: "t/openapi/"},
		{name: "a wrong-kind ID reports none", kind: ir.IDKindType, id: "op/openapi/x"},
		{name: "an empty ID reports none", kind: ir.IDKindType, id: ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, ok := ir.IDSpace(tc.kind, tc.id)
			if got != tc.want || ok != tc.wantOK {
				t.Errorf("IDSpace(%q, %q) = (%q, %v), want (%q, %v)",
					tc.kind, tc.id, got, ok, tc.want, tc.wantOK)
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
