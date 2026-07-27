package openapi_test

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi"
	"github.com/dexpace/morphic/ir"
)

// parsePetstore compiles the shared golden petstore spec (goldenPetstore, in
// fuzz_test.go) through the public pipeline. Reading the on-disk spec rather
// than a hand-copied literal keeps this test coupled to the same document the
// golden snapshot is compared against, so an edit to the fixture cannot leave
// this test asserting against a stale copy.
func parsePetstore(t *testing.T) (*ir.Document, []ir.Diagnostic) {
	t.Helper()
	data, err := os.ReadFile(goldenPetstore)
	require.NoError(t, err)
	doc, diags, err := openapi.New().Compile(context.Background(),
		[]compilers.Source{{Path: "petstore.yaml", Data: data}}, compilers.Options{})
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
	require.NoError(t, reg.Register(openapi.New()))
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
	data, err := os.ReadFile(goldenPetstore)
	require.NoError(t, err)
	_, _, err = openapi.New().Compile(context.Background(),
		[]compilers.Source{
			{Path: "a.yaml", Data: data},
			{Path: "b.yaml", Data: data},
		}, compilers.Options{})
	require.Error(t, err)
}
