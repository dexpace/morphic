// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package load

import (
	"context"
	"encoding/json/jsontext"
	"maps"
	"slices"
	"strings"

	soa "github.com/speakeasy-api/openapi/openapi"
	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/bind"
	"github.com/dexpace/morphic/compilers/openapi/internal/bind/typed"
	"github.com/dexpace/morphic/compilers/openapi/internal/budget"
	"github.com/dexpace/morphic/compilers/openapi/internal/nodeview"
	"github.com/dexpace/morphic/compilers/openapi/internal/oas"
	"github.com/dexpace/morphic/compilers/openapi/internal/provenance"
	"github.com/dexpace/morphic/ir"
)

// reached holds to the table what a reference reaches that the source's own
// check never read as that kind: an object in another document (GitHub #545),
// or one in the source outside its model, such as under an x-* key (GitHub
// #759, #872). The reference names the kind it expects, and the version is
// that of the document holding it (versionOf).
//
// Only what a reference reaches is checked, so a shared file's unread content
// fails no compile. Each object is read once per kind it is read as, and each
// finding reported once (see checkAll) (GitHub #571).
type reached struct {
	checker oas.Checker
	// version is the source's, as the model reads it.
	version string
	// limit bounds what each check reads.
	limit budget.Limit
	// tree places a finding about a node of the source (see reachedLocator),
	// and others one about another document's (see locatorFor).
	tree   provenance.Tree
	others Others
	// dialects gives the rows each schema checked is read with.
	dialects Dialects
	// seen holds the objects read as their kind, by the source's own check
	// and by each check of a target since.
	seen map[objectKey]bool
	// reported holds every finding already reported, the source's own included.
	reported map[ir.Diagnostic]bool
	// formatted holds the pointers of each other document whose keys a check
	// has read (oas.CheckFormat), by the document's name.
	formatted map[string]map[jsontext.Pointer]bool
}

// reachedTarget is an object the source's references reach: the least
// position among the $refs reaching it, the kind they expect it to be, the
// node it is written as, the position it is at, and the name of the document
// holding it, all as the bindings' table reads them.
type reachedTarget struct {
	site, at bind.Position
	kind     oas.Kind
	node     *yaml.Node
	doc      string
}

// inSource reports whether t's object is in the source.
func (t reachedTarget) inSource() bool { return t.at.Doc == bind.Source }

// validateReached checks every object a reference reaches that the source's
// own check did not read as that kind: each object landing of b's table, raw
// YAML of the source or a node of another document (GitHub #545, #759, #872),
// and each schema built for a landing. Each finding is reported once, where it
// is placed (see locatorFor), and none the source's own check made.
func validateReached(ctx context.Context, sites places, b bound, doc *soa.OpenAPI, opts Options,
	self sourceDocument, dialects Dialects,
) []ir.Diagnostic {
	checks := newReached(doc, opts, self.tree, self.judged)
	checks.others, checks.dialects = b.others, dialects
	table := b.bindings.Table()
	view := nodeview.New()
	name := func(id bind.DocID) string { return table.Document(id).Name }
	written := func(p bind.Position) *yaml.Node { return writtenIn(view, table, p) }
	targets := builtTargets(b.built, name, written)
	targets = append(targets, objectTargets(table.ObjectLandings(), written)...)
	return checkReached(ctx, sites, targets, bind.PositionOrder(table), checks)
}

// objectTargets returns the targets the table's object landings make, each
// placed at the least position whose chain reaches it, in whichever document.
// One no position reaches, which only a budget cutting the reading leaves, is
// no target, and nor is one written as no mapping, which no reference resolves
// to.
func objectTargets(landings []bind.ObjectLanding, written func(bind.Position) *yaml.Node) []reachedTarget {
	var out []reachedTarget
	for _, l := range landings {
		kind, known := objectKinds[l.Kind]
		node := written(l.At)
		if !l.Reached || !known || node == nil {
			continue
		}
		out = append(out, reachedTarget{site: l.Site, at: l.At, kind: kind, node: node, doc: l.Name})
	}
	return out
}

// objectKinds is the rule table's kind of each kind of object a reference can
// stand for.
var objectKinds = map[bind.RefKind]oas.Kind{
	bind.RefPathItem: oas.PathItem, bind.RefParameter: oas.Parameter, bind.RefHeader: oas.Header,
	bind.RefRequestBody: oas.RequestBody, bind.RefResponse: oas.Response, bind.RefCallback: oas.Callback,
	bind.RefExample: oas.Example, bind.RefLink: oas.Link, bind.RefSecurityScheme: oas.SecurityScheme,
	bind.RefMediaType: oas.MediaType,
}

// builtTargets returns the targets the schemas built for landings make, each
// placed at the least position whose chain reaches it, in whichever document,
// as name names it. One no position reaches, which only a budget cutting the
// reading leaves, is no target, nor is a boolean schema, which holds nothing to
// check. A tuple's entry the parser read as false though what written returns
// there is no schema, such as a number, is one, so the check reports it as a
// prefixItems entry; its rest is judged by its own row where the check reads
// the schema writing it.
func builtTargets(built []typed.Built, name func(bind.DocID) string, written func(bind.Position) *yaml.Node,
) []reachedTarget {
	var out []reachedTarget
	for _, b := range built {
		if b.Schema == nil || !b.Landing.Reached {
			continue
		}
		at := b.Landing.At
		var node *yaml.Node
		if s := b.Schema.GetSchema(); s != nil {
			node = nodeview.Deref(s.GetRootNode())
		} else if node = written(at); node == nil || isBoolean(node) || isTupleRest(b.Landing) {
			continue
		}
		out = append(out, reachedTarget{site: b.Landing.Site, at: at, kind: oas.Schema, node: node, doc: name(at.Doc)})
	}
	return out
}

// writtenIn returns the node the documents of table write at p, read through
// view, and nil where none is there.
func writtenIn(view *nodeview.View, table *bind.Table, p bind.Position) *yaml.Node {
	path, complete := view.DocumentPath(table.Document(p.Doc).Root, p.Pointer)
	if !complete || len(path) == 0 {
		return nil
	}
	return nodeview.Deref(path[len(path)-1])
}

// isTupleRest reports whether l is the `additionalItems` of the tuple its site
// writes (bind.Landing), the one landing a schema holds as its own field.
func isTupleRest(l bind.Landing) bool {
	return l.At.Doc == l.Site.Doc && l.At.Pointer == l.Site.Pointer.AppendToken("additionalItems")
}

// isBoolean reports whether n is a boolean scalar, a schema JSON Schema reads
// as true or false.
func isBoolean(n *yaml.Node) bool {
	return n.Kind == yaml.ScalarNode && n.Tag == "!!bool"
}

// checkReached checks targets, each object once, at the least site reaching
// it as compare orders sites, less those already read as the kind expected.
func checkReached(ctx context.Context, sites places, targets []reachedTarget, compare func(p, q bind.Position) int,
	checks *reached,
) []ir.Diagnostic {
	least := map[objectKey]reachedTarget{}
	for _, t := range targets {
		key := objectKey{node: t.node, kind: t.kind}
		if prev, seen := least[key]; !seen || compare(t.site, prev.site) < 0 {
			least[key] = t
		}
	}
	checks.formatted = outermost(least)
	return checks.checkAll(ctx, sites, least, compare)
}

// outermost returns, for each document but the source, the pointers of the
// targets in it no other target holds: one whose pointer has another's as a
// token prefix lies inside it. The keys of each are read once, and the source's
// all were read by its own check.
func outermost(targets map[objectKey]reachedTarget) map[string]map[jsontext.Pointer]bool {
	at := map[string]map[jsontext.Pointer]bool{}
	for _, t := range targets {
		if t.inSource() {
			continue
		}
		if at[t.doc] == nil {
			at[t.doc] = map[jsontext.Pointer]bool{}
		}
		at[t.doc][t.at.Pointer] = true
	}
	out := map[string]map[jsontext.Pointer]bool{}
	for doc, pointers := range at {
		out[doc] = map[jsontext.Pointer]bool{}
		for p := range pointers {
			out[doc][p] = !heldBy(p, pointers)
		}
	}
	return out
}

// heldBy reports whether a pointer of pointers other than p is a token prefix
// of p.
func heldBy(p jsontext.Pointer, pointers map[jsontext.Pointer]bool) bool {
	for up := p; up != ""; {
		up = up[:strings.LastIndexByte(string(up), '/')]
		if pointers[up] {
			return true
		}
	}
	return false
}

// newReached returns the checker for the objects doc's references reach, under
// opts' node and alias budgets; either one unbounded leaves the charge
// unbounded. It holds one in the source to the version doc declares, and one in
// another document to that document's (versionOf). tree is the source's,
// and judged what the table found in it, none of which is reported again.
func newReached(doc *soa.OpenAPI, opts Options, tree provenance.Tree, judged judgment) *reached {
	reported := make(map[ir.Diagnostic]bool, len(judged.diags))
	for _, d := range judged.diags {
		reported[oas.AsPlaced(d)] = true
	}
	seen := make(map[objectKey]bool, len(judged.seen))
	maps.Copy(seen, judged.seen)
	return &reached{
		checker:  judged.checker,
		tree:     tree,
		version:  doc.OpenAPI,
		limit:    nodesAdmitted(opts),
		seen:     seen,
		reported: reported,
	}
}

// checkAll checks each target in the order of its site, and reports its
// findings. A finding already reported was reported for a lesser $ref, so one
// placed at a $ref lands at the least whose object's check draws it, whatever
// the declaration order. An object already read as the kind a target expects,
// inside a container checked before it or by the source's own check, is not
// read again; an extension's content, which no container reads, is.
func (v *reached) checkAll(ctx context.Context, sites places, targets map[objectKey]reachedTarget,
	compare func(p, q bind.Position) int,
) []ir.Diagnostic {
	order := slices.SortedFunc(maps.Values(targets), func(a, b reachedTarget) int {
		if c := compare(a.site, b.site); c != 0 {
			return c
		}
		return compare(a.at, b.at)
	})
	diags := make([]ir.Diagnostic, 0, len(order))
	for _, t := range order {
		if ctx.Err() != nil {
			break
		}
		if v.seen[objectKey{node: t.node, kind: t.kind}] {
			continue
		}
		diags = append(diags, v.check(ctx, sites.position(t.site), t)...)
	}
	return diags
}

// check judges t's object and reports each finding where locatorFor places it,
// naming the pointer it was read at where that is elsewhere, as the source's
// own check does (oas.Options.ReadAt). One already reported is not reported
// again, as the source reports a finding once however many references share
// its node; one about a node of a document no entry lists goes to site, the
// $ref that reached it.
func (v *reached) check(ctx context.Context, site ir.Provenance, t reachedTarget) []ir.Diagnostic {
	opts := oas.Options{Start: t.kind, Budget: v.limit, Locate: v.locatorFor(t), ReadAt: true, Root: t.at.Pointer,
		Rows:     v.dialects.rowsAt(v.positionOf(t, t.at.Pointer)),
		RootRows: func(at jsontext.Pointer) (oas.SchemaRules, bool) { return v.dialects.root(v.positionOf(t, at)) },
		Seen:     func(kind oas.Kind, node *yaml.Node) { v.seen[objectKey{node: nodeview.Deref(node), kind: kind}] = true },
		Skip: func(kind oas.Kind, node *yaml.Node) bool {
			return v.seen[objectKey{node: nodeview.Deref(node), kind: kind}]
		},
	}
	version := v.versionOf(t)
	found := v.checker.Check(ctx, t.node, version, opts)
	if !t.inSource() && v.formatted[t.doc][t.at.Pointer] {
		found = append(found, oas.CheckFormat(ctx, t.node, version, formatOptions(opts))...)
	}
	var diags []ir.Diagnostic
	for _, d := range found {
		if key := identity(d, t.doc); !v.reported[key] {
			v.reported[key] = true
			diags = append(diags, placeAt(d, site, t.doc))
		}
	}
	return diags
}

// positionOf returns where pointer of the document holding t's object is in
// the bindings' table.
func (v *reached) positionOf(t reachedTarget, pointer jsontext.Pointer) bind.Position {
	return bind.Position{Doc: t.at.Doc, Pointer: pointer}
}

// versionOf returns the version t's object is held to: that of the document
// holding it, read at the minor it declares (Other), as locatorFor places a
// finding there, and the source's for an object in the source or in a
// document no entry lists.
func (v *reached) versionOf(t reachedTarget) string {
	if o, listed := v.others.Named(t.doc); listed && !t.inSource() {
		return o.version
	}
	return v.version
}

// locatorFor returns where a finding about t's object is placed: where the
// source's tree holds the node it is about; else in the Document.Sources entry
// of the document holding the object, at the node's pointer there (GitHub
// #1030); and, for a document no entry lists, nowhere yet, marked foreign for
// placeAt to put at the $ref.
func (v *reached) locatorFor(t reachedTarget) oas.Locator {
	if tree, listed := v.others.tree(t.doc); listed && !t.inSource() {
		return walkedFrom(t.node, tree.Node)
	}
	return reachedLocator(v.tree, t.node)
}
