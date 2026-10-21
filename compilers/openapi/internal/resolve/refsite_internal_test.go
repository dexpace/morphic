package resolve

import (
	"strings"
	"testing"

	oas3 "github.com/speakeasy-api/openapi/jsonschema/oas3"
	"github.com/speakeasy-api/openapi/marshaller"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// schemaFromYAML unmarshals body as a JSONSchema, keeping the reference form a
// bare *oas3.Schema cannot represent.
func schemaFromYAML(t *testing.T, body string) *oas3.JSONSchema[oas3.Referenceable] {
	t.Helper()
	js := &oas3.JSONSchema[oas3.Referenceable]{}
	valErrs, err := marshaller.Unmarshal(t.Context(), strings.NewReader(body), js)
	require.NoError(t, err)
	require.Empty(t, valErrs, "the fixture parses cleanly")
	return js
}

// TestIsRefSite_IncludesTheDegenerateRef pins the deliberately broad test. A
// position carrying a $ref field is $ref-shaped even when the ref is empty:
// callers here ask "is there a $ref-carrying body", not "is there a followable
// reference", and reading the narrower question would treat {$ref: ""} as an
// ordinary schema body.
func TestIsRefSite_IncludesTheDegenerateRef(t *testing.T) {
	t.Parallel()
	ref := schemaFromYAML(t, "{$ref: '#/$defs/T'}\n")
	assert.True(t, IsRefSite(ref, ref.GetSchema()))

	empty := schemaFromYAML(t, "{$ref: ''}\n")
	assert.True(t, IsRefSite(empty, empty.GetSchema()),
		"a present but empty $ref still carries a Ref field")

	plain := schemaFromYAML(t, "type: string\n")
	assert.False(t, IsRefSite(plain, plain.GetSchema()))
	assert.False(t, IsRefSite(plain, nil), "a boolean schema carries no body to inspect")
}
