package load

import (
	oas3 "github.com/speakeasy-api/openapi/jsonschema/oas3"
	yaml "gopkg.in/yaml.v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/scan"
	"github.com/dexpace/morphic/compilers/openapi/internal/sourceindex"
	"github.com/dexpace/morphic/ir"
)

// schemaRef is the one schema type the resolver resolves.
type schemaRef = oas3.JSONSchema[oas3.Referenceable]

// recoverChains runs walk behind the barrier the scans have: a panic from the
// library reading its own model becomes a warning that the protection is
// incomplete, never a crash of the compile it guards.
func recoverChains(locate scan.Locator, walk func() (ir.Diagnostic, bool)) (d ir.Diagnostic, found bool) {
	defer func() {
		if r := recover(); r != nil {
			d = diag.Newf(ir.SeverityWarning, diag.CycleScanFailed, locate(nil),
				"reference-chain scan aborted (%v); reference-cycle protection is incomplete for this source", r)
			found = true
		}
	}()
	return walk()
}

// declaresRegistryKeys reports whether any mapping in the tree under root has a
// $anchor or $id key. The library registers a schema under nothing else, so a
// tree without either leaves every registry empty and no lookup can succeed:
// the chains then hold only pointers, which the pre-parse scan has already
// refused a cycle of. It walks each node once, never through an alias, so its
// cost is the node count the budget already bounds.
func declaresRegistryKeys(root *yaml.Node) bool {
	stack := []*yaml.Node{root}
	for visited := 0; len(stack) > 0 && visited < sourceindex.MaxIndexedNodes; visited++ {
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if n == nil || n.Kind == yaml.AliasNode {
			continue
		}
		if n.Kind == yaml.MappingNode && hasRegistryKey(n) {
			return true
		}
		stack = append(stack, n.Content...)
	}
	return false
}

func hasRegistryKey(n *yaml.Node) bool {
	for i := 0; i+1 < len(n.Content); i += 2 {
		if k := n.Content[i]; k.Kind == yaml.ScalarNode && (k.Value == "$anchor" || k.Value == "$id") {
			return true
		}
	}
	return false
}
