package typed_test

import (
	"encoding/json/jsontext"
	"strings"
	"testing"

	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/bind"
	"github.com/dexpace/morphic/compilers/openapi/internal/bind/typed"
)

const spec = `openapi: 3.1.0
info: {title: t, version: "1"}
paths: {}
x-lib:
  S: {type: string}
components:
  schemas:
    A: {type: object, properties: {p: {type: integer}}}
`

// bridge decodes spec and returns a Bridge over it and the parsed document.
func bridge(t *testing.T) (*typed.Bridge, *soa.OpenAPI) {
	t.Helper()
	doc, _, err := soa.Unmarshal(t.Context(), strings.NewReader(spec))
	require.NoError(t, err)
	return typed.New(doc, doc.GetRootNode()), doc
}

// TestSchema_AnswersTheModelsOwnSchema pins a Schema position: the very object
// the parser's model holds there, which the lowering already reads.
func TestSchema_AnswersTheModelsOwnSchema(t *testing.T) {
	t.Parallel()
	b, doc := bridge(t)
	a, ok := doc.Components.Schemas.Get("A")
	require.True(t, ok)
	p, ok := a.GetSchema().GetProperties().Get("p")
	require.True(t, ok)

	for pointer, want := range map[jsontext.Pointer]any{"/components/schemas/A": a, "/components/schemas/A/properties/p": p} {
		got, err := b.Schema(t.Context(), pointer, bind.Schema)
		require.NoError(t, err, pointer)
		assert.Same(t, want, got, pointer)
	}
}

// TestSchema_BuildsRawYAMLOnce pins a Raw position: a schema built from the
// YAML written there, the same object however often it is asked for.
func TestSchema_BuildsRawYAMLOnce(t *testing.T) {
	t.Parallel()
	b, _ := bridge(t)
	first, err := b.Schema(t.Context(), "/x-lib/S", bind.Raw)
	require.NoError(t, err)
	assert.Equal(t, "string", string(first.GetSchema().GetType()[0]))
	again, err := b.Schema(t.Context(), "/x-lib/S", bind.Raw)
	require.NoError(t, err)
	assert.Same(t, first, again)
}

// TestSchema_RefusesWhatHoldsNoSchema pins every way a position fails to hold
// one: a role no schema has, a pointer the model refuses or reads nothing at,
// raw YAML at a schema position, and nothing written.
func TestSchema_RefusesWhatHoldsNoSchema(t *testing.T) {
	t.Parallel()
	b, _ := bridge(t)
	for _, tc := range []struct {
		pointer jsontext.Pointer
		role    bind.Role
		want    string
	}{
		{"/info", bind.Object, "holds no schema (object)"},
		{"/components/schemas/A/~2", bind.Schema, "not one the model reads"},
		{"/components/schemas/Z", bind.Schema, "reading /components/schemas/Z"},
		{"/components/schemas/A/type", bind.Schema, "the model holds no schema"},
		{"/x-lib/S", bind.Schema, "the model holds no schema"},
		{"/x-lib/Z", bind.Raw, "the document holds nothing"},
	} {
		_, err := b.Schema(t.Context(), tc.pointer, tc.role)
		require.Error(t, err, tc.pointer)
		assert.Contains(t, err.Error(), tc.want, tc.pointer)
	}
}
