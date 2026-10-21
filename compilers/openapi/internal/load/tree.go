package load

import (
	oas3 "github.com/speakeasy-api/openapi/jsonschema/oas3"
	yaml "gopkg.in/yaml.v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/sourceindex"
)

// schemaRef is a schema of the parser's model, as each position holds one.
type schemaRef = oas3.JSONSchema[oas3.Referenceable]

// oasDiscriminator is a schema's discriminator, as the parser's model holds
// one.
type oasDiscriminator = oas3.Discriminator

// anyNode reports whether any node in the tree under root satisfies match. It
// walks each node once, never through an alias, so its cost is the node count
// the source budget already bounds; the loader refuses a tree past the bound.
func anyNode(root *yaml.Node, match func(*yaml.Node) bool) bool {
	stack := []*yaml.Node{root}
	for visited := 0; len(stack) > 0 && visited < sourceindex.MaxIndexedNodes; visited++ {
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if n == nil || n.Kind == yaml.AliasNode {
			continue
		}
		if match(n) {
			return true
		}
		stack = append(stack, n.Content...)
	}
	return false
}
