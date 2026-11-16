// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package scan

import (
	"context"
	"encoding/json/jsontext"
	"slices"
	"strconv"
	"strings"

	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/budget"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/nodeview"
	"github.com/dexpace/morphic/compilers/openapi/internal/provenance"
	"github.com/dexpace/morphic/compilers/openapi/internal/sourceindex"
	"github.com/dexpace/morphic/ir"
)

// Pointers returns a node's JSON pointer in root's tree, its pairs keyed as
// the parser keys them (nodeview.KeyOf), and whether the tree holds it. A
// scalar key shares its member's pointer, and a shared node is placed by
// rehome. A nil node or root is held by none, nor is a node under
// a `<<` merge key, whose pair the merge's document holds elsewhere, nor a key
// that is no scalar, which no pointer reads inside.
//
// The tree is indexed under ctx on the first node asked about, once, so a load
// that reports nothing never indexes it.
func Pointers(ctx context.Context, root *yaml.Node) func(*yaml.Node) (jsontext.Pointer, bool) {
	var steps map[*yaml.Node]step
	return func(n *yaml.Node) (jsontext.Pointer, bool) {
		if n == nil || root == nil {
			return "", false
		}
		if steps == nil {
			// Never cut short by the bound for a tree the loader reads: each
			// passed a sourceindex walk under the same bound first.
			steps = placesIn(ctx, root)
		}
		if _, held := steps[n]; !held {
			return "", false
		}
		return pointerOf(steps, n), true
	}
}

// placesIn indexes root's tree by stepsIn under the compiler's node bound,
// each node aliases share placed at the least of its places (rehome).
func placesIn(ctx context.Context, root *yaml.Node) map[*yaml.Node]step {
	steps, _ := stepsIn(ctx, root, sourceindex.MaxIndexedNodes)
	rehome(steps)
	return steps
}

// step is how a node of a tree is reached from the node holding it: that node,
// and the token naming the member or element. The document's own content has
// no holder.
type step struct {
	holder *yaml.Node
	token  string
}

// stepsIn indexes every node of root's tree, its keys included, by the step
// that first reaches it, and reports whether limit nodes held them all and ctx
// let it finish. An alias is a leaf: what it names is indexed where it is
// written. A node indexed already is not entered again, so a node placed twice
// ends the walk too; one past limit has no pointer.
//
// A pointer is not stored per node: that is the sum of every node's depth,
// which a long key over a wide subtree, or a deep one, makes quadratic in the
// document's size.
func stepsIn(ctx context.Context, root *yaml.Node, limit int) (map[*yaml.Node]step, bool) {
	out := map[*yaml.Node]step{root: {}}
	top := nodeview.DocumentRoot(root)
	if top == nil {
		return out, true // a document node holding nothing
	}
	out[top] = step{}
	stack := []*yaml.Node{top}
	entered, cut := budget.New(budget.AtMost(limit)), false
	entered.Spend(1)
	reach := func(n *yaml.Node, s step) {
		if _, indexed := out[n]; indexed {
			return
		}
		if !entered.Spend(1) {
			cut = true
			return
		}
		out[n] = s
		stack = append(stack, n)
	}
	for popped := 0; len(stack) > 0; popped++ {
		if popped%nodeview.AskEvery == 0 && ctx.Err() != nil {
			return out, false
		}
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		reachChildren(n, reach)
	}
	return out, !cut
}

// reachChildren hands reach each child of n a pointer reaches, with the step
// that leads to it.
func reachChildren(n *yaml.Node, reach func(*yaml.Node, step)) {
	eachChild(n, func(c *yaml.Node, token string, held bool) {
		if held {
			reach(c, step{holder: n, token: token})
		}
	})
}

// eachChild hands visit each child of a mapping or sequence n, with the token
// naming it and whether a pointer reaches it there: a pair under a `<<` merge
// key is n's but has no pointer of its own, and an entry whose key is no
// scalar, which every reader leaves out, has none either. A scalar holds
// nothing, and an alias's target is written elsewhere.
func eachChild(n *yaml.Node, visit func(c *yaml.Node, token string, held bool)) {
	switch n.Kind {
	case yaml.MappingNode:
		for i := 0; i+1 < len(n.Content); i += 2 {
			key, value := n.Content[i], n.Content[i+1]
			token, own := nodeview.KeyOf(key)
			visit(key, token, own)
			visit(value, token, own)
		}
	case yaml.SequenceNode:
		for i, item := range n.Content {
			visit(item, strconv.Itoa(i), true)
		}
	default:
	}
}

// pointerOf returns n's pointer in the tree steps indexes, or "" when the tree
// does not hold n. A holder is indexed before what it holds, so the walk up
// ends at the top in as many steps as n is deep. The pointer is spelled into
// one buffer: appending token by token to a pointer copies it at each, half
// n's depth in copies of the whole.
func pointerOf(steps map[*yaml.Node]step, n *yaml.Node) jsontext.Pointer {
	var tokens []string
	size := 0
	for at := n; ; {
		s, ok := steps[at]
		if !ok {
			return ""
		}
		if s.holder == nil {
			break
		}
		tokens = append(tokens, s.token)
		size += len(s.token) + 1
		at = s.holder
	}
	escaped := strings.NewReplacer("~", "~0", "/", "~1") // RFC 6901, section 3
	spelled := make([]byte, 0, size)
	for _, token := range slices.Backward(tokens) {
		spelled = append(append(spelled, '/'), escaped.Replace(token)...)
	}
	return jsontext.Pointer(spelled)
}

// scanRecovered runs scan behind diag.Catch and, on any panic from its
// walks, degrades to a diag.CycleScanFailed warning quoting it instead of
// propagating — a bug in the detector must not crash the compiler on a
// degenerate spec (GitHub #12). The compile still proceeds to the parser; only
// the pre-parse guarantee is flagged incomplete for this source, at whole.
func scanRecovered(whole ir.Provenance, scan func() []ir.Diagnostic) []ir.Diagnostic {
	var diags []ir.Diagnostic
	if text, panicked := diag.Catch(func() { diags = scan() }); panicked {
		return []ir.Diagnostic{diag.Newf(ir.SeverityWarning, diag.CycleScanFailed, whole,
			"alias pre-scan aborted (%s); alias-cycle protection is incomplete for this source", text)}
	}
	return diags
}

// Aliases refuses, before parsing, a YAML document whose aliases expand
// without end or far past its own size (GitHub #12, #27): an anchor naming its
// own ancestor overflows the parser's stack, and an alias bomb exhausts its
// memory (docs/parser-library.md#alias-cycle,
// docs/parser-library.md#alias-expansion), or add past maxBytes of scalar
// text (GitHub #1429). tree places a refusal at its node; an incomplete scan
// warns at whole.
//
// The index must cover the whole tree that reaches the parser: an alias to an
// ancestor shows only from the root. surplus is the alias budget, which no
// guard of yaml.v3's enforces on a node decode (GitHub #479).
func Aliases(ctx context.Context, tree provenance.Tree, whole ir.Provenance, idx sourceindex.Index,
	surplus, maxBytes budget.Limit,
) []ir.Diagnostic {
	return scanRecovered(whole, func() []ir.Diagnostic {
		if d, ok := anchorCycle(ctx, tree, idx); ok {
			return []ir.Diagnostic{d}
		}
		if d, ok := aliasAmplification(ctx, tree, idx.Root(), idx.Nodes(), surplus); ok {
			return []ir.Diagnostic{d}
		}
		if added := idx.AliasText(); !maxBytes.Admits(added) {
			return []ir.Diagnostic{diag.Newf(ir.SeverityError, diag.BudgetExceeded, whole,
				"YAML aliases add %d bytes of scalar text to the document, past the %d-byte budget", added, maxBytes.Cap())}
		}
		return nil
	})
}

// anchorCycle reports a recursive anchor the index found, if any, at the alias
// with the least pointer (Least): the parser cannot decode one
// (docs/parser-library.md#alias-cycle). It is read before anything weighs the
// document's aliases: a recursive anchor makes the expanded weight infinite,
// and having refused those makes the alias graph a DAG above the index's
// tracked depth. One below it is the weigh walk's to end, by its cut, so the
// walk terminates on its own account.
func anchorCycle(ctx context.Context, tree provenance.Tree, idx sourceindex.Index) (ir.Diagnostic, bool) {
	cycles := idx.AnchorCycles()
	if len(cycles) == 0 {
		return ir.Diagnostic{}, false
	}
	alias := Least(ctx, idx.Root(), cycles)
	return cyclicDiag(tree, alias,
		"recursive YAML anchor %q references an ancestor node", anchorName(alias)), true
}

// anchorName is the anchor label an alias points at, for the diagnostic message.
func anchorName(alias *yaml.Node) string {
	if alias.Kind == yaml.AliasNode && alias.Alias != nil && alias.Alias.Anchor != "" {
		return alias.Alias.Anchor
	}
	return alias.Value
}

// cyclicDiag builds a diag.CyclicRef error diagnostic anchored where tree
// places the node.
func cyclicDiag(tree provenance.Tree, n *yaml.Node, format string, args ...any) ir.Diagnostic {
	return diag.Newf(ir.SeverityError, diag.CyclicRef, tree.Node(n), format, args...)
}
