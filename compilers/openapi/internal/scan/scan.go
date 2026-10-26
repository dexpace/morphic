package scan

import (
	"encoding/json/jsontext"
	"slices"
	"strconv"

	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/nodeview"
	"github.com/dexpace/morphic/compilers/openapi/internal/sourceindex"
	"github.com/dexpace/morphic/ir"
)

// Locator answers where a diagnostic's subject is. Given a node it returns the
// provenance to report the node at; given nil it returns the source alone, the
// answer for a finding about the document rather than a position in it.
//
// It is a parameter rather than a source index because a node's own line and
// column are not always the answer: a node an overlay grafted has none, and the
// caller is the one holding the attribution that can say where it came from
// (GitHub #476).
type Locator func(n *yaml.Node) ir.Provenance

// InSource is the Locator for a document nothing has patched: srcIndex, and the
// node's own line and column when there is a node. A node the parser did not
// place has neither, which is the zero Position: no position, rather than one
// at 0:0.
func InSource(srcIndex int) Locator {
	return func(n *yaml.Node) ir.Provenance {
		prov := ir.Provenance{Source: srcIndex}
		if n != nil {
			prov.Position = ir.Position{Line: n.Line, Column: n.Column}
		}
		return prov
	}
}

// InTree is InSource for a node of the tree pointers indexes (see Pointers),
// which also gives the node's JSON pointer there: a finding on a raw node is
// then placed where a lowering finding at the same position is, and a reader
// sees which declaration to edit (GitHub #803).
//
// The root's pointer is the empty one, which beside a position reads as none,
// so a finding on the document's top mapping is placed by position alone.
func InTree(srcIndex int, pointers func(*yaml.Node) (jsontext.Pointer, bool)) Locator {
	inSource := InSource(srcIndex)
	return func(n *yaml.Node) ir.Provenance {
		prov := inSource(n)
		prov.Pointer, _ = pointers(n)
		return prov
	}
}

// Pointers returns a node's JSON pointer in root's tree, its pairs keyed as
// the parser keys them (nodeview.KeyOf), and whether the tree holds the node.
// A scalar key shares its member's pointer, and an alias's target is placed
// where it is written. A nil node or root is held by none, nor is a node under
// a `<<` merge key, whose pair the merge's document holds at another pointer,
// nor a key that is no scalar, which no pointer reads inside.
//
// The tree is indexed on the first node asked about, once, so a load that
// reports nothing never indexes it.
func Pointers(root *yaml.Node) func(*yaml.Node) (jsontext.Pointer, bool) {
	var steps map[*yaml.Node]step
	return func(n *yaml.Node) (jsontext.Pointer, bool) {
		if n == nil || root == nil {
			return "", false
		}
		if steps == nil {
			// Never cut short for a tree the loader reads: each passed a
			// sourceindex walk under the same bound first.
			steps, _ = stepsIn(root, sourceindex.MaxIndexedNodes)
		}
		if _, held := steps[n]; !held {
			return "", false
		}
		return pointerOf(steps, n), true
	}
}

// step is how a node of a tree is reached from the node holding it: that node,
// and the token naming the member or element. The document's own content has
// no holder.
type step struct {
	holder *yaml.Node
	token  string
}

// stepsIn indexes every node of root's tree, its keys included, by the step
// that first reaches it, and reports whether limit nodes held them all. An
// alias is a leaf: what it names is indexed where it is written. A node
// indexed already is not entered again, so a node placed twice ends the walk
// too; one past limit has no pointer.
//
// A pointer is not stored per node: that is the sum of every node's depth,
// which a long key over a wide subtree, or a deep one, makes quadratic in the
// document's size.
func stepsIn(root *yaml.Node, limit int) (map[*yaml.Node]step, bool) {
	out := map[*yaml.Node]step{root: {}}
	top := nodeview.DocumentRoot(root)
	if top == nil {
		return out, true // a document node holding nothing
	}
	out[top] = step{}
	stack := []*yaml.Node{top}
	entered, cut := 1, false
	reach := func(n *yaml.Node, s step) {
		if _, indexed := out[n]; indexed {
			return
		}
		if entered >= limit {
			cut = true
			return
		}
		entered++
		out[n] = s
		stack = append(stack, n)
	}
	for len(stack) > 0 {
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
// key is n's but has no pointer of its own, and a key that is no scalar has
// none either, though its value is read under the empty key the parser gives
// it. A scalar holds nothing, and an alias's target is written elsewhere.
func eachChild(n *yaml.Node, visit func(c *yaml.Node, token string, held bool)) {
	switch n.Kind {
	case yaml.MappingNode:
		for i := 0; i+1 < len(n.Content); i += 2 {
			key, value := n.Content[i], n.Content[i+1]
			token, own := nodeview.KeyOf(key)
			k := nodeview.Deref(key)
			visit(key, token, own && k != nil && k.Kind == yaml.ScalarNode)
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
// ends at the top in as many steps as n is deep.
func pointerOf(steps map[*yaml.Node]step, n *yaml.Node) jsontext.Pointer {
	var tokens []string
	for at := n; ; {
		s, ok := steps[at]
		if !ok {
			return ""
		}
		if s.holder == nil {
			break
		}
		tokens = append(tokens, s.token)
		at = s.holder
	}
	var p jsontext.Pointer
	for _, token := range slices.Backward(tokens) {
		p = p.AppendToken(token)
	}
	return p
}

// recoverCycleScan runs scan behind diag.Catch and, on any panic from its
// walks, degrades to a diag.CycleScanFailed warning quoting it instead of
// propagating — a bug in the detector must not crash the compiler on a
// degenerate spec (GitHub #12). The compile still proceeds to the parser; only
// the pre-parse guarantee is flagged incomplete for this source.
func recoverCycleScan(locate Locator, scan func() []ir.Diagnostic) []ir.Diagnostic {
	var diags []ir.Diagnostic
	if text, panicked := diag.Catch(func() { diags = scan() }); panicked {
		return []ir.Diagnostic{diag.Newf(ir.SeverityWarning, diag.CycleScanFailed, locate(nil),
			"alias pre-scan aborted (%s); alias-cycle protection is incomplete for this source", text)}
	}
	return diags
}

// Aliases refuses, before parsing, a YAML document whose aliases expand
// without end or far past its own size (GitHub #12, #27): an anchor naming its
// own ancestor overflows the parser's stack, and an alias bomb exhausts its
// memory (docs/parser-library.md#alias-cycle,
// docs/parser-library.md#alias-expansion). It serves an overlay too
// (docs/parser-library.md#overlay-alias-clone). An incomplete scan yields a
// diag.CycleScanFailed warning.
//
// The index must cover the whole tree that will reach the parser: an alias to
// an ancestor is visible only from the root. surplus is the alias budget, zero
// for none; yaml.v3's own alias guard never fires on a yaml.Node decode
// (GitHub #479).
func Aliases(locate Locator, idx sourceindex.Index, surplus int64) []ir.Diagnostic {
	return recoverCycleScan(locate, func() []ir.Diagnostic {
		if d, ok := anchorCycle(locate, idx); ok {
			return []ir.Diagnostic{d}
		}
		if d, ok := aliasAmplification(locate, idx.Root(), idx.Nodes(), surplus); ok {
			return []ir.Diagnostic{d}
		}
		return nil
	})
}

// anchorCycle reports a recursive anchor the index found, if any, at the alias
// with the least pointer (Least): the parser cannot decode one
// (docs/parser-library.md#alias-cycle). It is read before anything weighs the
// document's aliases: a recursive anchor makes the expanded weight infinite,
// and having refused those is what makes the alias graph a DAG and the weigh
// walk provably terminating.
func anchorCycle(locate Locator, idx sourceindex.Index) (ir.Diagnostic, bool) {
	cycles := idx.AnchorCycles()
	if len(cycles) == 0 {
		return ir.Diagnostic{}, false
	}
	alias := Least(idx.Root(), cycles)
	return cyclicDiag(locate, alias,
		"recursive YAML anchor %q references an ancestor node", anchorName(alias)), true
}

// anchorName is the anchor label an alias points at, for the diagnostic message.
func anchorName(alias *yaml.Node) string {
	if alias.Alias != nil && alias.Alias.Anchor != "" {
		return alias.Alias.Anchor
	}
	return alias.Value
}

// cyclicDiag builds a diag.CyclicRef error diagnostic anchored where locate
// puts the node.
func cyclicDiag(locate Locator, n *yaml.Node, format string, args ...any) ir.Diagnostic {
	return diag.Newf(ir.SeverityError, diag.CyclicRef, locate(n), format, args...)
}
