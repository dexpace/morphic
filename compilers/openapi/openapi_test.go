package openapi

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
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
	doc, diags, err := New().Compile(context.Background(),
		[]compilers.Source{{Path: "petstore.yaml", Data: []byte(petstore)}}, compilers.Options{})
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
	reg := compilers.NewRegistry()
	require.NoError(t, reg.Register(New()))
	got, ok := reg.Lookup(compilers.SourceFormat{Name: "openapi", Version: "3.1"})
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
	_, _, err := New().Compile(context.Background(),
		[]compilers.Source{
			{Path: "a.yaml", Data: []byte(petstore)},
			{Path: "b.yaml", Data: []byte(petstore)},
		}, compilers.Options{})
	require.Error(t, err)
}

func TestParse_UnsupportedVersion(t *testing.T) {
	t.Parallel()
	spec := "openapi: 2.0.0\ninfo: {title: T, version: \"1\"}\npaths: {}\n"
	doc, diags, err := New().Compile(context.Background(), []compilers.Source{sourceOf(spec)}, compilers.Options{})
	require.NoError(t, err)
	assert.Nil(t, doc, "unsupported version refuses to lower")
	var sawUnsupported bool
	for _, d := range diags {
		if d.Code == codeUnsupportedVersion {
			sawUnsupported = true
		}
	}
	assert.True(t, sawUnsupported)
}

func TestParse_UnmarshalError(t *testing.T) {
	t.Parallel()
	_, _, err := New().Compile(context.Background(),
		[]compilers.Source{sourceOf("\t\t: : : not valid : yaml\n\x00")}, compilers.Options{})
	require.Error(t, err)
}

// ghostRefsSpec references non-existent components everywhere so every
// resolve-or-skip path (unresolved GetObject → nil) and resolution-error branch
// is exercised without a panic.
const ghostRefsSpec = `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /a:
    parameters:
      - {$ref: '#/components/parameters/GhostParam'}
    get:
      operationId: getA
      callbacks:
        good:
          '{$url}': {$ref: '#/components/pathItems/GhostInner'}
        bad: {$ref: '#/components/callbacks/GhostCb'}
      requestBody: {$ref: '#/components/requestBodies/GhostBody'}
      responses:
        "200": {$ref: '#/components/responses/GhostResp'}
        "201":
          description: ok
          headers:
            X-H: {$ref: '#/components/headers/GhostHeader'}
          content:
            application/json:
              schema: {type: string}
              examples:
                one: {$ref: '#/components/examples/GhostEx'}
  /ref: {$ref: '#/components/pathItems/GhostItem'}
webhooks:
  hook: {$ref: '#/components/pathItems/GhostHook'}
`

func TestGhostRefs_AllResolversDegradeGracefully(t *testing.T) {
	t.Parallel()
	// Uses the internal lowerer directly so resolution errors surface as
	// diagnostics without failing the parse; the point is no panic and coverage
	// of every resolve-or-skip branch.
	loadedDoc, diags, err := load(t.Context(), 0, sourceOf(ghostRefsSpec), Options{}.withDefaults())
	require.NoError(t, err)
	require.NotNil(t, loadedDoc)
	l := newLowerer(0, loadedDoc, Options{}.withDefaults())
	out := l.run()
	require.NotNil(t, out)
	var sawUnresolved bool
	for _, d := range append(diags, l.diags...) {
		if d.Code == codeUnresolvedRef {
			sawUnresolved = true
		}
	}
	assert.True(t, sawUnresolved, "unresolved refs reported")
}

func TestResolvers_NilInputs(t *testing.T) {
	t.Parallel()
	assert.Nil(t, resolvePathItem(nil))
	assert.Nil(t, resolveResponse(nil))
	assert.Nil(t, resolveHeader(nil))
	assert.Nil(t, resolveCallback(nil))
	assert.Nil(t, resolveParameter(nil))
	assert.Nil(t, resolveRequestBody(nil))
	assert.Nil(t, resolveExample(nil))
	assert.Nil(t, resolveSecurityScheme(nil))
	_, ok := paramKey(nil)
	assert.False(t, ok)
}
