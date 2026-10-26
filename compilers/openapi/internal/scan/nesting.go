package scan

import (
	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/nodeview"
	"github.com/dexpace/morphic/compilers/openapi/internal/sourceindex"
	"github.com/dexpace/morphic/ir"
)

// MaxNesting bounds how deeply a document may nest, as
// sourceindex.Index.Nesting counts it. What the parser pays per node grows
// with the node's depth (docs/parser-library.md#nesting-cost), so a schema
// thousands of levels deep cost seconds and gigabytes before the schema walk's
// own bound refused it (GitHub #905).
//
// Real specs nest far less: the deepest of the 1,839 OpenAPI 3 documents the
// real-world sweep read in October 2026 nests 21 levels. Anything the lowering
// lowers in full fits beneath it (TestMaxNesting_AdmitsWhatTheLoweringLowersInFull).
// It bounds levels, not their keys' bytes, which cost the parser per node too
// (GitHub #931). It is a limit on a shape, not a budget.
const MaxNesting = 1024

// Nesting refuses a document nested past MaxNesting before the parser builds
// it (docs/parser-library.md#nesting-cost), as a diag.SourceTooDeep error
// anchored at the least of the sites deepSites finds, or reports nothing for
// one within it. The index must cover the tree the parser will be handed,
// untruncated.
//
// A document holding a recursive anchor is left to the anchor-cycle refusal:
// it nests without end, and the index's count of one turn round it is no
// fact to refuse on or to find a site for.
func Nesting(locate Locator, idx sourceindex.Index) (ir.Diagnostic, bool) {
	return nestingWithin(locate, idx, MaxNesting)
}

// nestingWithin is Nesting under a bound of limit levels, so a test can cross
// it without a document a thousand levels deep.
func nestingWithin(locate Locator, idx sourceindex.Index, limit int64) (ir.Diagnostic, bool) {
	if idx.Nesting() <= limit || len(idx.AnchorCycles()) > 0 {
		return ir.Diagnostic{}, false
	}
	root := idx.Root()
	return diag.Newf(ir.SeverityError, diag.SourceTooDeep, locate(Least(root, deepSites(root, limit))),
		"document nests %d levels deep with its aliases expanded, past the %d-level bound", idx.Nesting(), limit), true
}

// placed is one node of the walk deepSites takes, and the levels from the top
// of the tree down to it, its own included.
type placed struct {
	n     *yaml.Node
	level int64
}

// deepSites returns where the tree under root first nests past limit, one site
// per way down, or none for a tree within it: a mapping or sequence one level
// past it; an alias whose target carries the nesting past it, at the alias,
// since its target is no part of it; or a node whose part under a `<<` merge
// key or a non-scalar key does, the deepest position there with a pointer.
//
// It descends only into what passes the bound, so a refused document costs
// one measure of the tree and one walk of part of it.
func deepSites(root *yaml.Node, limit int64) []*yaml.Node {
	top := nodeview.DocumentRoot(root)
	if sourceindex.Level(top) > limit {
		return []*yaml.Node{top}
	}
	measure := sourceindex.NewMeasure(sourceindex.MaxIndexedNodes)
	if measure.Of(top) <= limit {
		return nil
	}
	var sites []*yaml.Node
	stack := []placed{{n: top, level: sourceindex.Level(top)}}
	for len(stack) > 0 {
		at := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		passed := false
		eachChild(at.n, func(c *yaml.Node, _ string, held bool) {
			if c == nil || at.level+measure.Of(c) <= limit {
				return
			}
			passed = true
			switch {
			case !held:
				sites = append(sites, at.n)
			case at.level == limit:
				sites = append(sites, c)
			default:
				stack = append(stack, placed{n: c, level: at.level + sourceindex.Level(c)})
			}
		})
		if !passed {
			sites = append(sites, at.n) // an alias, or a node the parser makes no child of: no part of it passes
		}
	}
	return sites
}
