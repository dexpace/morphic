// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package nodeview

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	yaml "go.yaml.in/yaml/v3"
)

// ComponentsDoc builds `{components: {schemas: {S0..Sn-1: {type: object}}}}`,
// the shape every internal $ref in an OpenAPI document points into.
func ComponentsDoc(n int) *yaml.Node {
	schemas := &yaml.Node{Kind: yaml.MappingNode}
	for i := range n {
		schemas.Content = append(schemas.Content,
			&yaml.Node{Kind: yaml.ScalarNode, Value: fmt.Sprintf("S%d", i)},
			&yaml.Node{Kind: yaml.MappingNode, Content: []*yaml.Node{
				{Kind: yaml.ScalarNode, Value: "type"},
				{Kind: yaml.ScalarNode, Value: "object"},
			}})
	}
	components := &yaml.Node{Kind: yaml.MappingNode, Content: []*yaml.Node{
		{Kind: yaml.ScalarNode, Value: "schemas"}, schemas,
	}}
	return &yaml.Node{Kind: yaml.MappingNode, Content: []*yaml.Node{
		{Kind: yaml.ScalarNode, Value: "components"}, components,
	}}
}

// YAMLDoc parses src and returns its document root as the view sees it.
func YAMLDoc(t *testing.T, src string) *yaml.Node {
	t.Helper()
	var doc yaml.Node
	require.NoError(t, yaml.Unmarshal([]byte(src), &doc))
	return DocumentRoot(&doc)
}
