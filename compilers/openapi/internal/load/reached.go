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

	"github.com/dexpace/morphic/compilers/openapi/internal/budget"
	"github.com/dexpace/morphic/compilers/openapi/internal/dialect"
	"github.com/dexpace/morphic/compilers/openapi/internal/nodeview"
	"github.com/dexpace/morphic/compilers/openapi/internal/provenance"
	"github.com/dexpace/morphic/compilers/openapi/internal/reftable"
	"github.com/dexpace/morphic/compilers/openapi/internal/specrules"
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
	checker specrules.Checker
	// version is the source's, as the model reads it.
	version string
	// limit bounds what each check reads.
	limit budget.Limit
	// tree places a finding about a node of the source (see reachedLocator),
	// and others one about another document's (see locatorFor).
	tree   provenance.Tree
	others Others
	// dialects gives the rows each schema checked is read with.
	dialects dialect.SchemaDialectRows
	// seen holds the objects read as their kind, by the source's own check
	// and by each check of a target since.
	seen map[objectKey]bool
	// reported holds every finding already reported, the source's own included.
	reported map[ir.DiagnosticKey]bool
	// formatted holds the pointers of each other document whose keys a check
	// has read (specrules.CheckFormat), by the document's name.
	formatted map[string]map[jsontext.Pointer]bool
	// judged is what the checks refused, accepted and read as Reference
	// Objects, and judging the test seam told each object read (Options).
	judged  judgment
	judging func(specrules.Kind, *yaml.Node)
	// cut is the context's error where it ended the checks before the last.
	cut error
}

// reachedTarget is an object the source's references reach: the least
// position among the $refs reaching it, the kind they expect it to be, the
// node it is written as, the position it is at, and the name of the document
// holding it, all as the bindings' table reads them.
type reachedTarget struct {
	site, at reftable.DocumentPointer
	kind     specrules.Kind
	node     *yaml.Node
	doc      string
}

// inSource reports whether t's object is in the source.
func (t reachedTarget) inSource() bool { return t.at.Doc == reftable.Source }

// judgeReached judges, before anything is built for it, every object a
// reference reaches that the source's own check did not read as that kind:
// each object landing of table and each schema landing, raw YAML of the source
// or a node of another document (GitHub #545, #759, #872, #1928). Each finding
// is reported once, where it is placed (see locatorFor), and none the source's
// own check made. It returns the findings and what it judged, the acceptances
// of the source's check counted, for settle to read the tree by, and the
// context's error where it cut the checks short.
func judgeReached(ctx context.Context, sites places, table *reftable.ReferenceTargetTable, doc *soa.OpenAPI, opts Options,
	self sourceDocument, others Others, dialects dialect.SchemaDialectRows,
) ([]ir.Diagnostic, judgment, error) {
	checks := newReached(doc, opts, self.tree, self.judged)
	checks.others, checks.dialects = others, dialects
	view := nodeview.New()
	written := func(documentPointer reftable.DocumentPointer) *yaml.Node {
		return writtenIn(view, table, documentPointer)
	}
	targets := landingTargets(table.Landings(), written)
	targets = append(targets, objectTargets(table.ObjectLandings(), written)...)
	found := checkReached(ctx, sites, targets, reftable.PositionOrder(table), checks)
	checks.judged.seen = checks.seen
	return found, checks.judged, checks.cut
}

// objectTargets returns the targets the table's object landings make, each
// placed at the least position whose chain reaches it, in whichever document.
// One no position reaches, which only a budget cutting the reading leaves, is
// no target, and nor is one written as no mapping, which no reference resolves
// to.
func objectTargets(landings []reftable.ObjectLanding, written func(reftable.DocumentPointer) *yaml.Node) []reachedTarget {
	var out []reachedTarget
	for _, objectLanding := range landings {
		kind, known := objectKinds[objectLanding.Kind]
		node := written(objectLanding.At)
		if !objectLanding.Reached || !known || node == nil {
			continue
		}
		out = append(out, reachedTarget{site: objectLanding.Site, at: objectLanding.At, kind: kind, node: node, doc: objectLanding.Name})
	}
	return out
}

// objectKinds is the rule table's kind of each kind of object a reference can
// stand for.
var objectKinds = map[reftable.RefKind]specrules.Kind{
	reftable.RefPathItem: specrules.PathItem, reftable.RefParameter: specrules.Parameter, reftable.RefHeader: specrules.Header,
	reftable.RefRequestBody: specrules.RequestBody, reftable.RefResponse: specrules.Response, reftable.RefCallback: specrules.Callback,
	reftable.RefExample: specrules.Example, reftable.RefLink: specrules.Link, reftable.RefSecurityScheme: specrules.SecurityScheme,
	reftable.RefMediaType: specrules.MediaType,
}

// landingTargets returns the targets the table's schema landings make, each
// placed at the least position whose chain reaches it, in whichever document,
// and read as written there. One no position reaches, which only a budget
// cutting the reading leaves, is none, nor is one where nothing is written,
// nor a boolean schema, which holds nothing to check. A value that is no
// schema, such as a number, is one, which the check reports, but a tuple's
// rest that is no mapping is judged by its own row where the schema writing
// it is read.
func landingTargets(landings []reftable.Landing, written func(reftable.DocumentPointer) *yaml.Node) []reachedTarget {
	var out []reachedTarget
	for _, landing := range landings {
		node := written(landing.At)
		if !landing.Reached || node == nil || isBoolean(node) || isTupleRest(landing) && node.Kind != yaml.MappingNode {
			continue
		}
		out = append(out, reachedTarget{site: landing.Site, at: landing.At, kind: specrules.Schema, node: node, doc: landing.Name})
	}
	return out
}

// writtenIn returns the node the documents of table write at documentPointer, read through
// view, and nil where none is there.
func writtenIn(view *nodeview.View, table *reftable.ReferenceTargetTable, documentPointer reftable.DocumentPointer) *yaml.Node {
	path, complete := view.DocumentPath(table.Document(documentPointer.Doc).Root, documentPointer.Pointer)
	if !complete || len(path) == 0 {
		return nil
	}
	return nodeview.Deref(path[len(path)-1])
}

// isTupleRest reports whether landing is the `additionalItems` of the tuple its site
// writes (reftable.Landing), the one landing a schema holds as its own field.
func isTupleRest(landing reftable.Landing) bool {
	return landing.At.Doc == landing.Site.Doc && landing.At.Pointer == landing.Site.Pointer.AppendToken("additionalItems")
}

// isBoolean reports whether n is a boolean scalar, a schema JSON Schema reads
// as true or false.
func isBoolean(n *yaml.Node) bool {
	return n.Kind == yaml.ScalarNode && n.Tag == "!!bool"
}

// checkReached checks targets, each object once, at the least site reaching
// it as compare orders sites, less those already read as the kind expected.
func checkReached(ctx context.Context, sites places, targets []reachedTarget, compare func(documentPointer, reftableDocumentPointer reftable.DocumentPointer) int,
	checks *reached,
) []ir.Diagnostic {
	least := map[objectKey]reachedTarget{}
	for _, reachedTarget := range targets {
		key := objectKey{node: reachedTarget.node, kind: reachedTarget.kind}
		if prev, seen := least[key]; !seen || compare(reachedTarget.site, prev.site) < 0 {
			least[key] = reachedTarget
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
	for _, reachedTarget := range targets {
		if reachedTarget.inSource() {
			continue
		}
		if at[reachedTarget.doc] == nil {
			at[reachedTarget.doc] = map[jsontext.Pointer]bool{}
		}
		at[reachedTarget.doc][reachedTarget.at.Pointer] = true
	}
	out := map[string]map[jsontext.Pointer]bool{}
	for doc, pointers := range at {
		out[doc] = map[jsontext.Pointer]bool{}
		for pointer := range pointers {
			out[doc][pointer] = !heldBy(pointer, pointers)
		}
	}
	return out
}

// heldBy reports whether a pointer of pointers other than pointer is a token prefix
// of pointer.
func heldBy(pointer jsontext.Pointer, pointers map[jsontext.Pointer]bool) bool {
	for up := pointer; up != ""; {
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
	reported := make(map[ir.DiagnosticKey]bool, len(judged.diags))
	for _, diagnostic := range judged.diags {
		reported[specrules.AsPlaced(diagnostic).Key()] = true
	}
	seen := make(map[objectKey]bool, len(judged.seen))
	maps.Copy(seen, judged.seen)
	accepted := make(map[*yaml.Node]bool, len(judged.accepted))
	maps.Copy(accepted, judged.accepted)
	return &reached{
		checker:  judged.checker,
		tree:     tree,
		version:  doc.OpenAPI,
		limit:    nodesAdmitted(opts),
		seen:     seen,
		reported: reported,
		judged:   judgment{refused: map[*yaml.Node]bool{}, accepted: accepted},
		judging:  opts.judging,
	}
}

// checkAll checks each target in the order of its site, and reports its
// findings. A finding already reported was reported for a lesser $ref, so one
// placed at a $ref lands at the least whose object's check draws it, whatever
// the declaration order. An object already read as the kind a target expects,
// inside a container checked before it or by the source's own check, is not
// read again; an extension's content, which no container reads, is.
func (v *reached) checkAll(ctx context.Context, sites places, targets map[objectKey]reachedTarget,
	compare func(documentPointer, reftableDocumentPointer reftable.DocumentPointer) int,
) []ir.Diagnostic {
	order := slices.SortedFunc(maps.Values(targets), func(a, b reachedTarget) int {
		if c := compare(a.site, b.site); c != 0 {
			return c
		}
		return compare(a.at, b.at)
	})
	diags := make([]ir.Diagnostic, 0, len(order))
	for _, reachedTarget := range order {
		if err := ctx.Err(); err != nil {
			v.cut = err
			break
		}
		if v.seen[objectKey{node: reachedTarget.node, kind: reachedTarget.kind}] {
			continue
		}
		diags = append(diags, v.check(ctx, sites.position(reachedTarget.site), reachedTarget)...)
	}
	return diags
}

// check judges reachedTargetValue's object and reports each finding where locatorFor places it,
// naming the pointer it was read at where that is elsewhere, as the source's
// own check does (specrules.Options.ReadAt). One already reported is not reported
// again, as the source reports a finding once however many references share
// its node; one about a node of a document no entry lists goes to site, the
// $ref that reached it.
func (v *reached) check(ctx context.Context, site ir.Provenance, reachedTargetValue reachedTarget) []ir.Diagnostic {
	opts := specrules.Options{Start: reachedTargetValue.kind, Budget: v.limit, Locate: v.locatorFor(reachedTargetValue), ReadAt: true, Root: reachedTargetValue.at.Pointer,
		Rows: v.dialects.At(v.positionOf(reachedTargetValue, reachedTargetValue.at.Pointer)),
		RootRows: func(at jsontext.Pointer) (specrules.SchemaRules, bool) {
			return v.dialects.RootAt(v.positionOf(reachedTargetValue, at))
		},
		Seen: v.see,
		Skip: func(kind specrules.Kind, node *yaml.Node) bool {
			return v.seen[objectKey{node: nodeview.Deref(node), kind: kind}]
		},
		Judged: v.judged.note, Shared: true,
		Referenced: func(_ specrules.Kind, node *yaml.Node) { v.judged.referenced = append(v.judged.referenced, node) },
	}
	version := v.versionOf(reachedTargetValue)
	found := v.checker.Check(ctx, reachedTargetValue.node, version, opts)
	if !reachedTargetValue.inSource() && v.formatted[reachedTargetValue.doc][reachedTargetValue.at.Pointer] {
		found = append(found, specrules.CheckFormat(ctx, reachedTargetValue.node, version, formatOptions(opts))...)
	}
	var diags []ir.Diagnostic
	for _, diagnostic := range found {
		if key := identity(diagnostic, reachedTargetValue.doc).Key(); !v.reported[key] {
			v.reported[key] = true
			diags = append(diags, placeAt(diagnostic, site, reachedTargetValue.doc))
		}
	}
	return diags
}

// see records that a check read node as kind, and tells the test seam.
func (v *reached) see(kind specrules.Kind, node *yaml.Node) {
	v.seen[objectKey{node: nodeview.Deref(node), kind: kind}] = true
	if v.judging != nil {
		v.judging(kind, node)
	}
}

// positionOf returns where pointer of the document holding reachedTargetValue's object is in
// the bindings' table.
func (v *reached) positionOf(reachedTargetValue reachedTarget, pointer jsontext.Pointer) reftable.DocumentPointer {
	return reftable.DocumentPointer{Doc: reachedTargetValue.at.Doc, Pointer: pointer}
}

// versionOf returns the version reachedTargetValue's object is held to: that of the document
// holding it, read at the minor it declares (Other), as locatorFor places a
// finding there, and the source's for an object in the source or in a
// document no entry lists.
func (v *reached) versionOf(reachedTargetValue reachedTarget) string {
	if other, listed := v.others.Named(reachedTargetValue.doc); listed && !reachedTargetValue.inSource() {
		return other.version
	}
	return v.version
}

// locatorFor returns where a finding about reachedTargetValue's object is placed: where the
// source's tree holds the node it is about; else in the LoadedOpenAPIDocument.Sources entry
// of the document holding the object, at the node's pointer there (GitHub
// #1030); and, for a document no entry lists, nowhere yet, marked foreign for
// placeAt to put at the $ref.
func (v *reached) locatorFor(reachedTargetValue reachedTarget) specrules.Locator {
	if tree, listed := v.others.tree(reachedTargetValue.doc); listed && !reachedTargetValue.inSource() {
		return walkedFrom(reachedTargetValue.node, tree.Node)
	}
	return reachedLocator(v.tree, reachedTargetValue.node)
}
