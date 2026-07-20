package openapi_test // external test package — exercises only the public API

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/frontend"
	"github.com/dexpace/morphic/frontend/openapi"
	"github.com/dexpace/morphic/ir"
)

const petstore = `openapi: 3.1.0
info:
  title: Petstore
  version: "1.0.0"
  termsOfService: https://example.com/terms
  contact: {name: API Team, email: api@example.com}
  license: {name: MIT}
servers:
  - url: https://{env}.example.com/v1
    description: Primary server
    variables:
      env:
        default: api
        enum: [api, staging]
        description: Deployment environment
security:
  - petstore_auth: [read:pets]
tags:
  - {name: pets, description: Pet operations}
paths:
  /pets:
    get:
      operationId: listPets
      tags: [pets]
      parameters:
        - {name: limit, in: query, schema: {type: integer}}
      responses:
        "200":
          description: A list of pets
          content:
            application/json:
              schema: {type: array, items: {$ref: "#/components/schemas/Pet"}}
        default:
          description: Unexpected error
          content:
            application/json:
              schema: {$ref: "#/components/schemas/Error"}
    post:
      operationId: createPet
      tags: [pets]
      security:
        - {}
      requestBody:
        required: true
        content:
          application/json:
            schema: {$ref: "#/components/schemas/Pet"}
      responses:
        "200":
          description: Created
          content:
            application/json:
              schema: {$ref: "#/components/schemas/Pet"}
        "404":
          description: Not found
          content:
            application/json:
              schema: {$ref: "#/components/schemas/Error"}
  /pets/{petId}:
    get:
      operationId: getPet
      tags: [pets]
      parameters:
        - {name: petId, in: path, required: true, schema: {type: string}}
      responses:
        "200":
          description: A pet
          content:
            application/json:
              schema: {$ref: "#/components/schemas/Pet"}
components:
  securitySchemes:
    petstore_auth:
      type: oauth2
      flows:
        implicit:
          authorizationUrl: https://example.com/auth
          scopes: {"read:pets": read your pets}
  schemas:
    Pet:
      type: object
      required: [id, name]
      properties:
        id: {type: integer}
        name: {type: string}
        category: {$ref: "#/components/schemas/Category"}
        status:
          oneOf:
            - {type: string}
            - {type: integer}
        meta:
          type: object
          properties:
            tag: {type: string}
    Category:
      type: object
      properties:
        name: {type: string}
    Error:
      type: object
      properties:
        code: {type: integer}
        message: {type: string}
`

func parsePetstore(t *testing.T) (*ir.Document, []ir.Diagnostic) {
	t.Helper()
	doc, diags, err := openapi.New().Parse(context.Background(),
		[]frontend.Source{{Path: "petstore.yaml", Data: []byte(petstore)}}, frontend.Options{})
	require.NoError(t, err)
	require.NotNil(t, doc)
	return doc, diags
}

func TestParse_EndToEnd(t *testing.T) {
	t.Parallel()
	doc, diags := parsePetstore(t)
	for _, d := range diags {
		assert.NotEqual(t, ir.SeverityError, d.Severity, "diag: %+v", d)
	}
	assert.Equal(t, ir.IRVersion, doc.IRVersion)
	assert.Equal(t, "Petstore", doc.Name)
	assert.Equal(t, "1.0.0", doc.Version)
	require.NotNil(t, doc.Contact)
	assert.Equal(t, "API Team", doc.Contact.Name)
	require.NotNil(t, doc.License)
	assert.Equal(t, "MIT", doc.License.Name)
	require.Len(t, doc.Services, 1)
	require.Len(t, doc.Sources, 1)
	assert.Len(t, doc.Sources[0].Hash, 64)

	// One server with a templated variable survived.
	require.Len(t, doc.Servers, 1)
	assert.Equal(t, "https://{env}.example.com/v1", doc.Servers[0].URLTemplate)
	require.Len(t, doc.Servers[0].Variables, 1)
	assert.Equal(t, "env", doc.Servers[0].Variables[0].Name)
	assert.Equal(t, []string{"api", "staging"}, doc.Servers[0].Variables[0].Enum)

	// Document-level security lowered to the service default (OR-of-ANDs).
	require.Len(t, doc.Services[0].Auth, 1)
	require.Len(t, doc.Services[0].Auth[0].Schemes, 1)
	assert.Equal(t, []string{"read:pets"}, doc.Services[0].Auth[0].Schemes[0].Scopes)

	// Auth scheme registry: the oauth2 scheme lowered with its implicit flow.
	require.Len(t, doc.Auth, 1)
	var oauth ir.AuthScheme
	for _, s := range doc.Auth {
		oauth = s
	}
	assert.Equal(t, ir.AuthKindOAuth2, oauth.Kind)
	require.Len(t, oauth.Flows, 1)
	assert.Equal(t, "implicit", oauth.Flows[0].Kind)

	// Spot-checks over the type registry: the named schema is present under its
	// pointer ID, at least one anonymous type was hoisted, and the oneOf
	// survived as a Union node rather than being collapsed.
	var pet ir.TypeDef
	var sawAnon, sawUnion bool
	for _, td := range doc.Types {
		if td.Common().Name.Source == "Pet" {
			pet = td
		}
		if td.Common().Anonymous {
			sawAnon = true
		}
		if td.Kind() == ir.KindUnion {
			sawUnion = true
		}
	}
	require.NotNil(t, pet, "named schema Pet present in the type registry")
	assert.Equal(t, ir.KindModel, pet.Kind())
	assert.True(t, sawAnon, "the inline meta object was hoisted as an anonymous type")
	assert.True(t, sawUnion, "the oneOf survived as a Union node")
}

func TestParse_RegistersInRegistry(t *testing.T) {
	t.Parallel()
	reg := frontend.NewRegistry()
	require.NoError(t, reg.Register(openapi.New()))
	got, ok := reg.Lookup(frontend.SourceFormat{Name: "openapi", Version: "3.1"})
	require.True(t, ok)
	assert.NotNil(t, got)
}

func TestParse_JSONRoundTrip(t *testing.T) {
	t.Parallel()
	doc, _ := parsePetstore(t)
	raw, err := json.Marshal(doc)
	require.NoError(t, err)
	var back ir.Document
	require.NoError(t, json.Unmarshal(raw, &back))
	if diff := cmp.Diff(doc, &back); diff != "" {
		t.Errorf("round-trip mismatch (-want +got):\n%s", diff)
	}
	again, err := json.Marshal(&back)
	require.NoError(t, err)
	assert.Equal(t, string(raw), string(again), "marshal must be deterministic")
}

func TestParse_RejectsMultipleSources(t *testing.T) {
	t.Parallel()
	_, _, err := openapi.New().Parse(context.Background(),
		[]frontend.Source{
			{Path: "a.yaml", Data: []byte(petstore)},
			{Path: "b.yaml", Data: []byte(petstore)},
		}, frontend.Options{})
	require.Error(t, err)
}
