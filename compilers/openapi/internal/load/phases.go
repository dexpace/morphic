// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package load

import (
	"context"
	"fmt"
	"slices"

	soa "github.com/speakeasy-api/openapi/openapi"
	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/overlay"
	"github.com/dexpace/morphic/compilers/openapi/internal/provenance"
	refscope "github.com/dexpace/morphic/compilers/openapi/internal/resolve"
	"github.com/dexpace/morphic/ir"
)

// phase names one step of a load. Each is entered through Options.enter, which
// asks the context first, so a cancel is seen before any phase starts.
type phase string

// The phases of a load, in the order it enters them.
const (
	phaseParse    phase = "parse"    // the source decoded, or detection's parse taken
	phaseRefuse   phase = "refuse"   // the pre-parse refusals
	phasePatch    phase = "patch"    // the overlay read and applied
	phaseMeasure  phase = "measure"  // the node budget
	phaseJudge    phase = "judge"    // the rule table's judgment of the tree
	phaseModel    phase = "model"    // the parser's model built, and its version read
	phaseSiblings phase = "siblings" // the fields beside path item $refs, and the media types, modelled
	phaseBind     phase = "bind"     // every $ref bound in the compiler
	phaseReach    phase = "reach"    // what references reach, validated
	phaseCheck    phase = "check"    // the source checks
)

// enter is the check a load makes before each phase: the test seam is told of
// p, then ctx is asked.
func (o Options) enter(ctx context.Context, p phase) error {
	if o.entered != nil {
		o.entered(p)
	}
	return ctx.Err()
}

// step is one phase of build, run over the load so far. refused reports a
// refusal to lower, carried in early.
type step struct {
	phase phase
	run   func(l *loading, ctx context.Context) (refused bool, err error)
}

// steps returns the phases build runs after the parse, in order.
func steps() []step {
	return []step{
		{phaseRefuse, (*loading).preParse},
		{phasePatch, (*loading).patchOverlay},
		{phaseMeasure, (*loading).measure},
		{phaseJudge, (*loading).judge},
		{phaseModel, (*loading).buildModel},
		{phaseSiblings, (*loading).findSiblings},
		{phaseBind, (*loading).bindRefs},
		{phaseReach, (*loading).reach},
		{phaseCheck, (*loading).checkSource},
	}
}

// loading is one source's load as its phases build it, each reading what the
// ones before it left.
type loading struct {
	srcIndex int
	src      compilers.Source
	opts     Options
	root     *yaml.Node
	// places is where the files the compile names lie, one answer for each
	// directory however often a read asks.
	places *refscope.Places
	// early is what the phases up to the model report: a refusal among them
	// ends the load with these alone.
	early  []ir.Diagnostic
	origin overlay.Origin
	// place places a pointer of the source, and tree a node of its patched
	// tree, as the lowering places them: in the source, the overlay named as
	// the writer of a position it wrote.
	place  provenance.Doc
	tree   provenance.Tree
	judged judgment
	doc    *soa.OpenAPI
	minor  string
	// dialects reads the source and each document a reference reaches at its
	// minor, off the table judged gives, so a compile builds that table once.
	dialects *dialects
	sibs     Siblings
	media    MediaTypes
	// sibFaults is what modelling the fields beside a path item $ref, or a
	// components/mediaTypes entry, faulted on.
	sibFaults []ir.Diagnostic
	self      sourceDocument
	reader    *external
	bound     bound
	// named is the rows each schema resource root naming its dialect governs.
	named Dialects
	// sites places a finding at a reference site, or a position of the
	// bindings, in whichever document writes it.
	sites places
	// resolved is what binding and validating what references reach report,
	// and checked what the source checks report.
	resolved, checked []ir.Diagnostic
}

// preParse runs the pre-parse refusals, so the parser is never handed a tree
// it faults on. A non-fatal finding, the scan's report that it could not
// finish, is carried forward.
func (l *loading) preParse(ctx context.Context) (bool, error) {
	tree := treeOf(ctx, provenance.Doc{Source: l.srcIndex}, l.root)
	l.early = refusals(ctx, tree, tree.Node(nil), l.root, l.opts)
	return diag.HasError(l.early), nil
}

// patchOverlay reads and applies the caller's overlay, refusing to lower a
// half-patched tree, and places the source's positions in the tree it leaves.
func (l *loading) patchOverlay(ctx context.Context) (bool, error) {
	origin, diags, err := patch(ctx, l.srcIndex, l.root, l.opts)
	if err != nil {
		return false, err
	}
	l.origin, l.early = origin, append(l.early, diags...)
	l.place = provenance.Doc{Source: l.srcIndex, Overlay: origin.Attribution()}
	l.tree = treeOf(ctx, l.place, l.root)
	return diag.HasError(l.early), nil
}

// measure holds the tree to the node budget. It is measured after the overlay
// for the reason patch re-runs the pre-parse refusals: the tree the model is
// built from is no longer the bytes that were measured, and an overlay action
// can graft nodes onto it. Building that model and binding its references is
// what the budget bounds, and both are still ahead.
func (l *loading) measure(ctx context.Context) (bool, error) {
	nodes := nodeCount(ctx, l.root)
	if l.opts.MaxSourceNodes.Admits(nodes) {
		return false, nil
	}
	l.early = append(l.early, budgetRefusal(l.srcIndex,
		"source document parses to %d nodes, past the %d-node budget", nodes, l.opts.MaxSourceNodes))
	return true, nil
}

// judge holds the tree to the rule table as the source writes it.
func (l *loading) judge(ctx context.Context) (bool, error) {
	l.judged = judgeSource(ctx, l.root, l.tree, nodesAdmitted(l.opts))
	return false, nil
}

// buildModel builds the parser's model of the tree, its anchors released, and
// refuses a version the compiler does not support.
func (l *loading) buildModel(ctx context.Context) (bool, error) {
	readyForModel(ctx, l.root)
	// The model build reads no context, so a cancel that cut the walk above
	// short is seen before it starts, and never waited out on a tree half
	// prepared for it.
	if err := ctx.Err(); err != nil {
		return false, err
	}
	doc, err := unmarshal(ctx, l.src.Data, l.root)
	if err != nil {
		return false, fmt.Errorf("openapi: unmarshal source %d: %w", l.srcIndex, err)
	}
	minor, ok := supportedMinor(doc.OpenAPI)
	if !ok {
		l.early = append(l.early, diag.Newf(ir.SeverityError, diag.UnsupportedVersion,
			ir.Provenance{Source: l.srcIndex},
			"unsupported OpenAPI version %q; want 3.0, 3.1, or 3.2", doc.OpenAPI))
		return true, nil
	}
	l.doc, l.minor = doc, minor
	l.dialects = &dialects{spec: l.judged.checker.Spec(), version: doc.OpenAPI, minor: minor,
		dialect: doc.GetJSONSchemaDialect()}
	return false, nil
}

// findSiblings models the fields the source writes beside its path items'
// $refs, and the components/mediaTypes entries it declares.
func (l *loading) findSiblings(ctx context.Context) (bool, error) {
	l.sibs, l.sibFaults = modelSiblings(ctx, l.doc, l.tree)
	var mediaFaults []ir.Diagnostic
	l.media, mediaFaults = modelMediaTypes(ctx, l.doc, l.tree)
	l.sibFaults = append(l.sibFaults, mediaFaults...)
	l.self = newSourceDocument(l.src, l.places, l.root, l.judged, l.tree)
	return false, nil
}

// bindRefs binds every $ref in the compiler (bindAll), reading other
// documents through a preparing reader when the caller supplied one, and
// places what it found where the binding places each document.
func (l *loading) bindRefs(ctx context.Context) (bool, error) {
	if l.opts.Reader != nil {
		l.reader = newExternal(l.opts, newExternalReads(l.self, l.opts.MaxExternalDocuments))
	}
	l.bound = bindAll(ctx, l.doc, l.sibs, l.media, l.self, l.reader, refusedByName(l.opts, l.self.path, l.places), l.dialects)
	table := l.bound.bindings.Table()
	if l.reader != nil {
		first := l.srcIndex + 1
		if l.origin.Applied() {
			first = l.opts.OverlaySrcIndex + 1
		}
		l.bound.others = newOthers(ctx, l.reader.read, first, l.dialects, unreachedKeys(table))
	}
	l.sites = places{source: l.place, others: l.bound.others, table: table}
	var unread []ir.Diagnostic
	l.named, unread = readDialects(table, l.bound.others, l.dialects, l.sites)
	l.judged = judgeSchemas(ctx, l.judged, l.named)
	l.self.judged = l.judged
	l.resolved = slices.Concat(unread, l.bound.faults.diags(l.sites, table), bindingDiags(l.sites, table),
		buildFailures(l.sites, l.bound.built, l.bound.objects))
	if l.reader != nil {
		l.resolved = append(l.resolved, l.reader.read.exhausted(l.place)...)
	}
	return false, nil
}

// reach validates what the references reach (validateReached).
func (l *loading) reach(ctx context.Context) (bool, error) {
	l.resolved = append(l.resolved, validateReached(ctx, l.sites, l.bound, l.doc, l.opts, l.self, l.named)...)
	return false, nil
}

// checkSource runs the source checks over the source (sourceChecks).
func (l *loading) checkSource(ctx context.Context) (bool, error) {
	l.checked = sourceChecks(ctx, l.place, l.doc, l.sibs, l.media, l.bound.bindings)
	return false, nil
}

// document assembles the loaded Document from the phases, and what they
// reported in the order the load lists it.
func (l *loading) document(hash string) (*Document, []ir.Diagnostic) {
	diags := slices.Concat(l.early, l.sibFaults, l.judged.diags, l.resolved, l.checked)
	return &Document{
		Doc:      l.doc,
		Location: l.self.path,
		Places:   l.places,
		Bindings: l.bound.bindings,
		Source: ir.SourceInfo{
			Format:  "openapi@" + l.minor,
			Path:    l.src.Path,
			Hash:    hash,
			SelfURI: l.doc.GetSelf(),
		},
		Overlay:    l.origin,
		Siblings:   l.sibs,
		MediaTypes: l.media,
		Others:     l.bound.others,
		Dialects:   l.named,
		Rows:       l.dialects.own(),
	}, diags
}
