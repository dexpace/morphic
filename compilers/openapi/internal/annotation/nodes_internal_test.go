// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package annotation

import (
	"strings"
	"testing"

	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	yaml "go.yaml.in/yaml/v3"
)

// TestRawPropertyNode_NilSchemaReadsNothing pins the nil guard on the schema
// side of the same reader, which every caller relies on to ask about a position
// that may have no body written at it.
func TestRawPropertyNode_NilSchemaReadsNothing(t *testing.T) {
	t.Parallel()
	assert.Nil(t, RawPropertyNode(nil, "type"))

	s := schemaFromYAML(t, "type: string\n")
	require.NotNil(t, RawPropertyNode(s, "type"))
	assert.Nil(t, RawPropertyNode(s, "title"), "a keyword the schema did not write")
}

// TestRawPropertyNode_ReadsANullKeywordAsLeftOut pins the one reading a null
// keyword has, which every keeper goes through, and the three keywords whose
// value is an instance, where a null is that value.
func TestRawPropertyNode_ReadsANullKeywordAsLeftOut(t *testing.T) {
	t.Parallel()
	s := schemaFromYAML(t, "{maxLength: null, format: ~, $id: null, const: null, default: ~, example: null, title: t}\n")
	for _, keyword := range []string{"maxLength", "format", "$id"} {
		assert.Nil(t, RawPropertyNode(s, keyword), keyword)
	}
	for _, keyword := range []string{"const", "default", "example"} {
		node := RawPropertyNode(s, keyword)
		require.NotNil(t, node, keyword)
		assert.True(t, IsNullNode(node), keyword)
	}
	assert.NotNil(t, RawPropertyNode(s, "title"))
}

// TestRawPropertyNode_ReadsAKeywordAsTheParserDoes pins the raw keyword reader
// every verbatim keyword and every bound goes through to the model it sits
// beside: a keyword merged in through `<<` and one written through an alias
// key are read (GitHub #395, #384), a keyword written twice at its last copy,
// a value written as an alias as what it names, and a schema whose whole body
// is an alias, which the model is built from as the alias node, through it.
func TestRawPropertyNode_ReadsAKeywordAsTheParserDoes(t *testing.T) {
	t.Parallel()
	doc, _, err := soa.Unmarshal(t.Context(), strings.NewReader(`openapi: 3.1.0
info: {title: t, version: "1"}
paths: {}
x-keys: [&max maximum]
x-base: &base {maximum: 9}
components:
  schemas:
    Merged: {type: integer, <<: *base}
    AliasKey: {type: integer, *max : 7}
    Repeated: {type: integer, maximum: 1, maximum: 2}
    Anchored: &anchored {type: integer, maximum: &n 5}
    Aliased: *anchored
    AliasValue: {type: integer, maximum: *n}
`))
	require.NoError(t, err)
	for name, want := range map[string]string{
		"Merged": "9", "AliasKey": "7", "Repeated": "2", "AliasValue": "5", "Aliased": "5",
	} {
		js, ok := doc.Components.Schemas.Get(name)
		require.True(t, ok, name)
		node := RawPropertyNode(js.GetSchema(), "maximum")
		require.NotNil(t, node, "%s writes maximum as the parser reads it", name)
		assert.Equal(t, yaml.ScalarNode, node.Kind, "%s: the value's alias is followed", name)
		assert.Equal(t, want, node.Value, name)
	}
}

// TestDeclaresAny_AsksTheRawNodes pins the gate's agreement with the recorders it
// gates: it must answer from the same raw nodes they read, so a keyword one sees
// and the other does not cannot arise.
func TestDeclaresAny_AsksTheRawNodes(t *testing.T) {
	t.Parallel()
	assert.True(t, DeclaresAny(schemaFromYAML(t, "$id: 'urn:a'\ntype: string\n"), DialectKeywords))
	assert.False(t, DeclaresAny(schemaFromYAML(t, "type: string\n"), DialectKeywords))
	assert.False(t, DeclaresAny(schemaFromYAML(t, "type: string\n"), nil))
}
