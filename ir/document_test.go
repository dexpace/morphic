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
