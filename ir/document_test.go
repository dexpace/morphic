package ir_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
)

func TestDocument_ConstructRepresentative(t *testing.T) {
	t.Parallel()
	userID := ir.TypeID("t/openapi/components/schemas/User")
	doc := ir.Document{
		IRVersion: "0.1.0",
		Name:      "Petstore",
		Version:   "1.0.0",
		Types: ir.TypeRegistry{
			userID: &ir.Model{
				TypeCommon: ir.TypeCommon{
					ID:   userID,
					Name: ir.Naming{Source: "User", Canonical: "user"},
				},
				Properties: []ir.Property{{
					ID:       ir.PropID("p/openapi/components/schemas/User/properties/id"),
					Name:     ir.Naming{Source: "id", Canonical: "id"},
					WireName: "id",
					Type:     ir.TypeRef{Target: ir.TypeID("t/openapi/prim/string")},
					Required: true,
				}},
			},
		},
		Services: []ir.Service{{
			ID:   ir.ServiceID("s/openapi/petstore"),
			Name: ir.Naming{Source: "Petstore", Canonical: "petstore"},
			Groups: []ir.OperationGroup{{
				Name: ir.Naming{Source: "users", Canonical: "users"},
				Operations: []ir.Operation{{
					ID:   ir.OpID("op/openapi/paths/~1users/get"),
					Name: ir.Naming{Source: "listUsers", Canonical: "list_users"},
					Responses: []ir.Response{{
						Conditions: ir.ResponseConditions{
							StatusCodes: []ir.StatusRange{{From: 200, To: 200}},
						},
						Payload: &ir.Payload{Contents: []ir.Content{{
							MediaType: "application/json",
							Type:      ir.TypeRef{Target: userID},
						}}},
					}},
					Bindings: ir.OpBindings{HTTP: []ir.HTTPBinding{{
						Method:      "GET",
						URITemplate: "/users",
					}}},
				}},
			}},
		}},
	}

	require.Len(t, doc.Services, 1)
	got, ok := doc.Types[userID]
	require.True(t, ok)
	model, ok := got.(*ir.Model)
	require.True(t, ok, "expected *ir.Model, got %T", got)
	assert.Equal(t, ir.KindModel, model.Kind())
	assert.True(t, model.Properties[0].Required)
	assert.False(t, model.Properties[0].Type.Nullable)
}

// TestDocument_ChannelsDeterministic pins Class C for Document's map-keyed
// registry fields: Channels must marshal with keys in sorted order on every
// run.
func TestDocument_ChannelsDeterministic(t *testing.T) {
	t.Parallel()
	doc := ir.Document{
		Channels: map[ir.ChannelID]ir.Channel{"z/c": {}, "m/c": {}, "a/c": {}},
	}
	got := assertDeterministicMarshal(t, doc)
	assert.Contains(t, got,
		`"channels":{"a/c":{"name":{},"docs":{},"provenance":{"source":0}},`+
			`"m/c":{"name":{},"docs":{},"provenance":{"source":0}},`+
			`"z/c":{"name":{},"docs":{},"provenance":{"source":0}}}`)
}

// TestDocument_MessagesDeterministic pins Class C for Document's map-keyed
// registry fields: Messages must marshal with keys in sorted order on every
// run.
func TestDocument_MessagesDeterministic(t *testing.T) {
	t.Parallel()
	doc := ir.Document{
		Messages: map[ir.MessageID]ir.Message{"z/m": {}, "m/m": {}, "a/m": {}},
	}
	got := assertDeterministicMarshal(t, doc)
	assert.Contains(t, got,
		`"messages":{"a/m":{"name":{},"payload":{},"docs":{},"provenance":{"source":0}},`+
			`"m/m":{"name":{},"payload":{},"docs":{},"provenance":{"source":0}},`+
			`"z/m":{"name":{},"payload":{},"docs":{},"provenance":{"source":0}}}`)
}

// TestDocument_AuthDeterministic pins Class C for Document's map-keyed
// registry fields: Auth must marshal with keys in sorted order on every run.
func TestDocument_AuthDeterministic(t *testing.T) {
	t.Parallel()
	doc := ir.Document{
		Auth: map[ir.AuthID]ir.AuthScheme{"z/a": {}, "m/a": {}, "a/a": {}},
	}
	got := assertDeterministicMarshal(t, doc)
	assert.Contains(t, got,
		`"auth":{"a/a":{"name":{},"docs":{},"provenance":{"source":0}},`+
			`"m/a":{"name":{},"docs":{},"provenance":{"source":0}},`+
			`"z/a":{"name":{},"docs":{},"provenance":{"source":0}}}`)
}
