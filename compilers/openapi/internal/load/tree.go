// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package load

import (
	"context"

	"github.com/speakeasy-api/openapi/jsonschema/oas3"
	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/nodeview"
)

// schemaRef is a schema of the parser's model, as each position holds one.
type schemaRef = oas3.JSONSchema[oas3.Referenceable]

// anyNode reports whether any node but an alias in the tree under root
// satisfies match. It walks each node once, never through an alias, so its
// cost is the node count the source budget already bounds.
func anyNode(ctx context.Context, root *yaml.Node, match func(*yaml.Node) bool) bool {
	return nodeview.Walk{Nodes: maxTreeNodes, Visit: func(node *yaml.Node, _ int) nodeview.Step {
		if node.Kind != yaml.AliasNode && match(node) {
			return nodeview.Stop
		}
		return nodeview.Descend
	}}.Run(ctx, root).Stopped
}
