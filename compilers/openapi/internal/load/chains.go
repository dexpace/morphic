package load

import (
	"context"

	oas3 "github.com/speakeasy-api/openapi/jsonschema/oas3"
	soa "github.com/speakeasy-api/openapi/openapi"
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

// chains runs the reference-chain cycle refusal: chainCycle, or the check a
// test put in its place.
func (o Options) chains(ctx context.Context, locate scan.Locator, root *yaml.Node, doc *soa.OpenAPI) (ir.Diagnostic, bool) {
	if o.chainCheck != nil {
		return o.chainCheck(ctx, locate, root, doc)
	}
	return chainCycle(ctx, locate, root, doc)
}

// chainCycle runs the reach check when the tree can hold a lookup the
// pre-parse scan does not model: a $anchor or $id the registries hold, or a
// /$defs/ pointer resolved against something other than the root. Without
// either, every chain is root pointers, which that scan has already refused a
// cycle of.
func chainCycle(ctx context.Context, locate scan.Locator, root *yaml.Node, doc *soa.OpenAPI) (ir.Diagnostic, bool) {
	if !needsChainModel(root) {
		return ir.Diagnostic{}, false
	}
	return reachCycle(ctx, locate, root, doc)
}

// needsChainModel reports whether any scalar in the tree spells $anchor or $id,
// the only keys the library registers a schema under, or a reference the
// resolver reads as a /$defs/ pointer.
//
// It reads scalars rather than keys. A merge key or an alias can supply a key
// where it is not written, but the word is still written somewhere, so a tree
// without it leaves every registry empty and no lookup can succeed.
func needsChainModel(root *yaml.Node) bool {
	return anyScalar(root, func(v string) bool { return v == "$anchor" || v == "$id" || isDefsRef(v) })
}

// anyScalar reports whether any scalar in the tree under root satisfies match.
// It walks each node once, never through an alias, so its cost is the node
// count the source budget already bounds.
func anyScalar(root *yaml.Node, match func(string) bool) bool {
	stack := []*yaml.Node{root}
	for visited := 0; len(stack) > 0 && visited < sourceindex.MaxIndexedNodes; visited++ {
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if n == nil || n.Kind == yaml.AliasNode {
			continue
		}
		if n.Kind == yaml.ScalarNode && match(n.Value) {
			return true
		}
		stack = append(stack, n.Content...)
	}
	return false
}
