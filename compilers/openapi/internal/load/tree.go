package load

import (
	oas3 "github.com/speakeasy-api/openapi/jsonschema/oas3"
	yaml "gopkg.in/yaml.v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/nodeview"
)

// schemaRef is a schema of the parser's model, as each position holds one.
type schemaRef = oas3.JSONSchema[oas3.Referenceable]

// oasDiscriminator is a schema's discriminator, as the parser's model holds
// one.
type oasDiscriminator = oas3.Discriminator

// anyNode reports whether any node but an alias in the tree under root
// satisfies match. It walks each node once, never through an alias, so its
// cost is the node count the source budget already bounds.
func anyNode(root *yaml.Node, match func(*yaml.Node) bool) bool {
	return nodeview.Walk{Nodes: maxTreeNodes, Visit: func(n *yaml.Node, _ int) nodeview.Step {
		if n.Kind != yaml.AliasNode && match(n) {
			return nodeview.Stop
		}
		return nodeview.Descend
	}}.Run(root).Stopped
}
