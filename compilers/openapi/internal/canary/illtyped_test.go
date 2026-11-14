// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package canary_test

import (
	"strings"
	"testing"

	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	yaml "go.yaml.in/yaml/v3"
)

// nilPopulatePanic pins nil-populate-panic: building the model of a Reference
// Object whose $ref is null, or of an additionalOperations entry that is no
// mapping, panics on the caller's goroutine.
func nilPopulatePanic(t *testing.T) {
	tests := map[string]string{
		"a null $ref": head31 + "paths:\n  /a:\n    get:\n      parameters:\n        - $ref:\n" +
			"      responses: {'200': {description: ok}}\n",
		"an additionalOperations entry that is no mapping": "openapi: 3.2.0\ninfo: {title: t, version: '1'}\n" +
			"paths:\n  /a:\n    additionalOperations:\n      PURGE: ~\n",
	}
	for name, input := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			r := probe(t, "unmarshal", input)
			require.Equal(t, returned, r.outcome, revisit(t, r))
			assert.True(t, strings.HasPrefix(r.report, recoveredMark), revisit(t, r))
			assert.Contains(t, r.report, "nil pointer dereference", revisit(t, r))
		})
	}
}

// stringAsBoolean pins string-as-boolean: a boolean field decodes true from the
// string scalars "yes" and on, and a false that is no nil from any other string.
func stringAsBoolean(t *testing.T) {
	doc, _, err := soa.Unmarshal(t.Context(), strings.NewReader(head31+
		"paths:\n  /a:\n    get:\n      parameters:\n"+
		"        - {name: p, in: query, required: on, schema: {type: string}}\n"+
		"        - {name: q, in: query, required: \"yes\", schema: {type: string}}\n"+
		"        - {name: r, in: query, explode: \"true\", schema: {type: string}}\n"+
		"      responses: {'200': {description: ok}}\n"))
	require.NoError(t, err)
	pi, ok := doc.Paths.Get("/a")
	require.True(t, ok)
	params := pi.GetObject().Get().GetParameters()
	require.Len(t, params, 3)

	for i, name := range []string{"on", `"yes"`} {
		p := params[i].GetObject()
		require.NotNil(t, p.Required, name)
		assert.True(t, *p.Required, "%s: %s", name, revisit(t, result{}))
	}
	explode := params[2].GetObject().Explode
	require.NotNil(t, explode, revisit(t, result{report: `"true" decodes as nil`}))
	assert.False(t, *explode, revisit(t, result{report: `"true" decodes as true`}))
}

// aliasFieldOnScalar pins alias-field-on-scalar: the decoder and the encoder
// read a scalar's Alias field nowhere, so a null whose field holds another node
// decodes into a map and a struct, and encodes, as the null it is.
func aliasFieldOnScalar(t *testing.T) {
	var doc yaml.Node
	require.NoError(t, yaml.Unmarshal([]byte("a: ~\nb: keep\n"), &doc))
	root := doc.Content[0]
	root.Content[1].Alias = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map", Content: []*yaml.Node{
		{Kind: yaml.ScalarNode, Tag: "!!str", Value: "x"}, {Kind: yaml.ScalarNode, Tag: "!!str", Value: "y"},
	}}

	out, err := yaml.Marshal(root)
	require.NoError(t, err)
	assert.Equal(t, "a: ~\nb: keep\n", string(out), revisit(t, result{report: "the encoder"}))
	var asMap map[string]any
	require.NoError(t, root.Decode(&asMap))
	assert.Equal(t, map[string]any{"a": nil, "b": "keep"}, asMap, revisit(t, result{report: "a map"}))
	var asStruct struct {
		A *map[string]string `yaml:"a"`
	}
	require.NoError(t, root.Decode(&asStruct))
	assert.Nil(t, asStruct.A, revisit(t, result{report: "a struct"}))
}

// head31 opens a 3.1 document with no paths written.
const head31 = "openapi: 3.1.0\ninfo: {title: t, version: '1'}\n"
