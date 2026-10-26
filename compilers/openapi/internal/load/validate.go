package load

import (
	"context"
	"encoding/json/jsontext"
	"fmt"

	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/budget"
	"github.com/dexpace/morphic/compilers/openapi/internal/nodeview"
	"github.com/dexpace/morphic/compilers/openapi/internal/oas"
	"github.com/dexpace/morphic/compilers/openapi/internal/scan"
	"github.com/dexpace/morphic/ir"
)

// objectKey identifies an object the table has judged: a mapping, read as a
// kind. A mapping a $ref reads as another kind is not the same object.
type objectKey struct {
	node *yaml.Node
	kind oas.Kind
}

// judgment is what the table found in the source: its diagnostics, and the
// objects it read, so the validation of what a reference reaches skips them.
type judgment struct {
	diags []ir.Diagnostic
	seen  map[objectKey]bool
}

// declaredVersion returns the version the document at root writes under
// "openapi", or "" when it writes none as a scalar. It reads the tree rather
// than the model, which is built from a respelled copy.
func declaredVersion(root *yaml.Node) string {
	for _, p := range nodeview.Pairs(nodeview.DocumentRoot(root)) {
		if v := nodeview.Deref(p.Val); p.Key == "openapi" && v != nil && v.Kind == yaml.ScalarNode && v.Tag != "!!null" {
			return v.Value
		}
	}
	return ""
}

// judgeSource holds the source to the table of the version it declares. root
// is the tree as the source writes it, an applied overlay's patch included, and
// tree places each finding at the node it is about, naming where it was read
// when an alias or a merge key mounts that node there. within bounds what the
// check reads (see nodesAdmitted).
func judgeSource(ctx context.Context, root *yaml.Node, tree sourceTree, within budget.Limit) judgment {
	j := judgment{seen: map[objectKey]bool{}}
	version := declaredVersion(root)
	opts := oas.Options{
		Budget: within,
		Locate: tree.locatorIn(root),
		Seen: func(kind oas.Kind, node *yaml.Node) {
			j.seen[objectKey{node: nodeview.Deref(node), kind: kind}] = true
		},
		ReadAt: true,
	}
	j.diags = append(oas.NewChecker().Check(ctx, root, version, opts),
		oas.CheckFormat(ctx, root, version, formatOptions(opts))...)
	return j
}

// formatOptions returns opts for oas.CheckFormat, which reads each node of a
// tree once: bounded by the size of a tree the load phase holds rather than
// by the work budget a check of its objects spends, so a tree the node budget
// admitted is read whole.
func formatOptions(opts oas.Options) oas.Options {
	opts.Budget, opts.Seen = budget.AtMost(maxTreeNodes), nil
	return opts
}

// nodesAdmitted is how many nodes a document opts admits may hold with its
// aliases expanded: the node budget plus the alias budget, unbounded where
// either is. A check of the table reads no more objects than that, so a
// budget of it stops no check of an admitted document.
func nodesAdmitted(opts Options) budget.Limit {
	return opts.MaxSourceNodes.Plus(opts.MaxAliasSurplus)
}

// locatorIn returns the Locator for the pointers of the object written at
// node, which places each as the source's own findings are (see walkedFrom).
func (t sourceTree) locatorIn(node *yaml.Node) oas.Locator { return walkedFrom(node, t.locate) }

// walkedFrom returns the Locator for the pointers of the object written at
// node: each is walked from node to the node it names, which locate places. A
// pointer that leads nowhere is placed at the last node it reached.
func walkedFrom(node *yaml.Node, locate scan.Locator) oas.Locator {
	view := nodeview.New()
	root := nodeview.DocumentRoot(node)
	return func(pointer jsontext.Pointer) ir.Provenance {
		path, _ := view.DocumentPath(root, pointer)
		if len(path) == 0 {
			return locate(nil)
		}
		return locate(path[len(path)-1])
	}
}

// foreign marks the provenance a Locator gives a finding no document entry
// holds, until the finding is placed at the reference that reached it: it is
// no source index any list holds.
const foreign = -1

// reachedLocator returns the Locator for the object written at node, which a
// reference reaches. A finding about a node the source's tree holds is placed
// there, as the source's own are; any other is marked foreign, its position
// that of the node in the document it is in, which no entry lists.
func (t sourceTree) reachedLocator(node *yaml.Node) oas.Locator {
	elsewhere := scan.InSource(foreign)
	return walkedFrom(node, func(n *yaml.Node) ir.Provenance {
		if t.has(n) {
			return t.locate(n)
		}
		return elsewhere(n)
	})
}

// holds reports whether d is placed in a document, rather than marked foreign.
func holds(d ir.Diagnostic) bool { return d.Provenance.Source != foreign }

// placeAt puts a finding about an object a reference reaches where it is
// reported: one placed in a document stays there; one marked foreign, in a
// document no entry lists, goes to the reference, site, its message naming
// where in the document place it lies, as no Provenance can (GitHub #74).
func placeAt(d ir.Diagnostic, site ir.Provenance, place string) ir.Diagnostic {
	if holds(d) {
		return d
	}
	at := d.Provenance.Position
	d.Provenance = site
	d.Message = fmt.Sprintf("%s, at %d:%d of %s", d.Message, at.Line, at.Column, place)
	return d
}

// identity is what tells a finding from another about the same node however
// many references reach it: the finding as placed in its document, whichever
// position read it (oas.AsPlaced), or, for one marked foreign, the finding with
// the place it names, whichever reference it is placed at.
func identity(d ir.Diagnostic, place string) ir.Diagnostic {
	d = oas.AsPlaced(d)
	if !holds(d) {
		d.Message += " of " + place
	}
	return d
}
