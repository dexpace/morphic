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
