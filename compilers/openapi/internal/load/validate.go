// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package load

import (
	"context"
	"encoding/json/jsontext"
	"fmt"
	"slices"

	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/bind"
	"github.com/dexpace/morphic/compilers/openapi/internal/budget"
	"github.com/dexpace/morphic/compilers/openapi/internal/nodeview"
	"github.com/dexpace/morphic/compilers/openapi/internal/oas"
	"github.com/dexpace/morphic/compilers/openapi/internal/provenance"
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
// checker is the table it judged against, built once a load, which judges
// what the references reach and gives each document's Schema Object rows.
// pending is each Schema Object it handed back unread, in a document that may
// name a dialect, which is judged under opts once the bindings say which
// dialect each is read in (judgeSchemas).
type judgment struct {
	checker oas.Checker
	diags   []ir.Diagnostic
	seen    map[objectKey]bool
	pending []oas.Pending
	version string
	opts    oas.Options
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

// declaredDialect returns the jsonSchemaDialect the OpenAPI document root
// declares, or "" for one declaring none, or no version, as a document no
// OpenAPI Object roots does not.
func declaredDialect(root *yaml.Node) string {
	if declaredVersion(root) == "" {
		return ""
	}
	for _, p := range nodeview.Pairs(nodeview.DocumentRoot(root)) {
		if v := nodeview.Deref(p.Val); p.Key == "jsonSchemaDialect" && v != nil && v.Kind == yaml.ScalarNode && v.Tag != "!!null" {
			return v.Value
		}
	}
	return ""
}

// judgeSource holds the source to the table of the version it declares. root
// is the tree as the source writes it, an applied overlay's patch included, and
// tree places each finding at the node it is about, naming where it was read
// when an alias or a merge key mounts that node there. within bounds what the
// check reads (see nodesAdmitted). A document that may name a dialect
// (namesDialect) has its Schema Objects handed back unread, since the dialect
// each is read in is known only once the bindings are read (judgeSchemas).
func judgeSource(ctx context.Context, root *yaml.Node, tree provenance.Tree, within budget.Limit) judgment {
	j := judgment{checker: oas.NewChecker(), seen: map[objectKey]bool{}, version: declaredVersion(root)}
	j.opts = oas.Options{
		Budget: within,
		Locate: walkedFrom(root, tree.Node),
		Seen: func(kind oas.Kind, node *yaml.Node) {
			j.seen[objectKey{node: nodeview.Deref(node), kind: kind}] = true
		},
		ReadAt: true,
	}
	opts := j.opts
	if namesDialect(ctx, root, j.version) {
		opts.Later = func(p oas.Pending) { j.pending = append(j.pending, p) }
	}
	j.diags = append(j.checker.Check(ctx, root, j.version, opts),
		oas.CheckFormat(ctx, root, j.version, formatOptions(opts))...)
	return j
}

// namesDialect reports whether a schema of the document at root, declaring
// version, may be read in another dialect than its minor's own: the document
// is 3.1 or 3.2 and writes jsonSchemaDialect, or a mapping of its tree writes
// the key `$schema`, in a schema or not, which is all a dialect root can name
// one by. It reads the tree, so it holds for a document whose model is not yet
// built, and a walk the tree's bound or ctx cuts short says it may.
func namesDialect(ctx context.Context, root *yaml.Node, version string) bool {
	if m, ok := oas.ParseMinor(version); !ok || m == oas.V30 {
		return false
	}
	if declaredDialect(root) != "" {
		return true
	}
	walk := nodeview.Walk{Nodes: maxTreeNodes, Visit: func(n *yaml.Node, _ int) nodeview.Step {
		if n.Kind == yaml.MappingNode && slices.ContainsFunc(nodeview.Pairs(n), func(p nodeview.Pair) bool {
			return p.Key == "$schema"
		}) {
			return nodeview.Stop
		}
		return nodeview.Descend
	}}.Run(ctx, root)
	return walk.Stopped || walk.Cut
}

// judgeSchemas returns j with the findings about the Schema Objects the
// source's check handed back unread (judgeSource), each read with the rows of
// the dialect it is read in, which the bindings, once read, give (Dialects):
// those of a dialect root it is or is held by, else the source's.
func judgeSchemas(ctx context.Context, j judgment, d Dialects) judgment {
	if len(j.pending) == 0 {
		return j
	}
	opts := j.opts
	opts.Rows = d.rowsAt(bind.Position{Doc: bind.Source})
	opts.RootRows = func(at jsontext.Pointer) (oas.SchemaRules, bool) {
		return d.root(bind.Position{Doc: bind.Source, Pointer: at})
	}
	j.diags = append(j.diags, j.checker.CheckPending(ctx, j.pending, j.version, opts)...)
	j.pending = nil
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

// walkedFrom returns the Locator for the pointers of the object written at
// node: each is walked from node to the node it names, which place places. A
// pointer that leads nowhere is placed at the last node it reached.
func walkedFrom(node *yaml.Node, place func(*yaml.Node) ir.Provenance) oas.Locator {
	view := nodeview.New()
	root := nodeview.DocumentRoot(node)
	return func(pointer jsontext.Pointer) ir.Provenance {
		path, _ := view.DocumentPath(root, pointer)
		if len(path) == 0 {
			return place(nil)
		}
		return place(path[len(path)-1])
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
func reachedLocator(tree provenance.Tree, node *yaml.Node) oas.Locator {
	elsewhere := provenance.Tree{Doc: provenance.Doc{Source: foreign}}
	return walkedFrom(node, func(n *yaml.Node) ir.Provenance {
		if treeHolds(tree, n) {
			return tree.Node(n)
		}
		return elsewhere.Node(n)
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
