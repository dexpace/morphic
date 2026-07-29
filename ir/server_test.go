package ir_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
)

// TestServer_JSONContract pins that Name and Description carry no omitempty
// (every server has both), and Auth carries no omitempty because an empty
// non-nil slice ("explicitly public") must stay distinguishable from nil ("no
// server-scoped override") — the same reasoning as Operation.Auth and
// Service.Auth. It also pins that a fully populated Server round-trips.
func TestServer_JSONContract(t *testing.T) {
	t.Parallel()
	assertJSONContract(t, ir.Server{}, `{"name":{},"description":{},"auth":null}`, ir.Server{
		Name:        ir.Naming{Source: "production", Canonical: "production"},
		URLTemplate: "https://{region}.example.com/v1",
		Description: populatedDocs(),
		Variables: []ir.ServerVariable{
			{Name: "region", Default: "us-east-1", Enum: []string{"us-east-1", "eu-west-1"}, Docs: populatedDocs()},
			{Name: "stage", Default: "prod"},
		},
		Protocol:        "kafka",
		ProtocolVersion: "3.5",
		Tags:            []string{"internal", "beta"},
		Auth: []ir.AuthRequirement{
			{Schemes: []ir.SchemeUse{{Scheme: "auth/apiKey", Scopes: []string{"read"}}}},
		},
		Bindings: map[string]ir.RawConfig{
			"kafka": {"groupId": ir.RawValue(`"g1"`)},
			"amqp":  {"exchange": ir.RawValue(`"e1"`)},
		},
		Unmodeled: populatedPreserved(),
	})
}

// TestServer_AuthEmptyNonNilRoundTrips mirrors TestOperation_AuthEmptyNonNilRoundTrips
// (document_test.go) for Server.Auth: an empty non-nil slice must serialize as
// [] and must not collapse to nil on decode, since Server is the other place
// besides Operation and Service where the source comment makes this promise.
func TestServer_AuthEmptyNonNilRoundTrips(t *testing.T) {
	t.Parallel()
	srv := ir.Server{Auth: []ir.AuthRequirement{}}
	raw, err := json.Marshal(srv)
	require.NoError(t, err)
	assert.Contains(t, string(raw), `"auth":[]`, "empty non-nil Auth serializes as []")

	var back ir.Server
	require.NoError(t, json.Unmarshal(raw, &back))
	require.NotNil(t, back.Auth, "empty Auth must not deserialize to nil")
	assert.Empty(t, back.Auth)

	var nilSrv ir.Server
	rawNil, err := json.Marshal(nilSrv)
	require.NoError(t, err)
	assert.Contains(t, string(rawNil), `"auth":null`, "nil Auth serializes as null")
}

// TestServer_BindingsDeterministic pins Class C for Server's map field:
// Bindings must marshal with keys in sorted order on every run.
func TestServer_BindingsDeterministic(t *testing.T) {
	t.Parallel()
	srv := ir.Server{Bindings: outOfOrderProtocolBindings()}
	got := assertDeterministicMarshal(t, srv)
	assert.Contains(t, got, sortedProtocolBindingsJSON)
}

// TestServerVariable_JSONContract pins ServerVariable's contract that Docs
// carries no omitempty, since every variable has a (possibly empty)
// documentation object, while Name/Default/Enum/Unmodeled stay optional, and
// that a fully populated ServerVariable round-trips.
func TestServerVariable_JSONContract(t *testing.T) {
	t.Parallel()
	assertJSONContract(t, ir.ServerVariable{}, `{"docs":{}}`, ir.ServerVariable{
		Name:      "region",
		Default:   "us-east-1",
		Enum:      []string{"us-east-1", "eu-west-1", "ap-south-1"},
		Docs:      populatedDocs(),
		Unmodeled: populatedPreserved(),
	})
}
