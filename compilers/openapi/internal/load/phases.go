// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package load

import (
	"context"
	"encoding/json/jsontext"
	"fmt"
	"slices"

	soa "github.com/speakeasy-api/openapi/openapi"
	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers"
	refscope "github.com/dexpace/morphic/compilers/openapi/internal/componentlookup"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/dialect"
	"github.com/dexpace/morphic/compilers/openapi/internal/doctree"
	"github.com/dexpace/morphic/compilers/openapi/internal/overlay"
	"github.com/dexpace/morphic/compilers/openapi/internal/provenance"
	"github.com/dexpace/morphic/compilers/openapi/internal/reftable"
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
	phaseJudge    phase = "judge"    // the rule table's judgment of the tree
	phaseSettle   phase = "settle"   // the tree made to read as the table judged it
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
		{phaseJudge, (*loading).judge},
		{phaseSettle, (*loading).settle},
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
	// read is the source as doctree read it, and root its tree.
	read doctree.ParsedYAMLDocument
	root *yaml.Node
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
	// aside holds what the load set aside for the model build (settle), and
	// anchors each anchor the model build cleared (readyForModel).
	aside   aside
	anchors []anchor
	doc     *soa.OpenAPI
	minor   string
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
	// named is the rows each schema of the documents is read with.
	named dialect.SchemaDialectRows
	// sites places a finding at a reference site, or a position of the
	// bindings, in whichever document writes it.
	sites places
	// resolved is what binding and judging what references reach report,
	// reached what that judging found, and checked what the source checks
	// report.
	resolved, reached, checked []ir.Diagnostic
}

// preParse runs the pre-parse refusals, so the parser is never handed a tree
// it faults on. A non-fatal finding, the scan's report that it could not
// finish, is carried forward.
func (l *loading) preParse(ctx context.Context) (bool, error) {
	tree := treeOf(ctx, provenance.Doc{Source: l.srcIndex}, l.root)
	l.early = append(versionDiagnostics(tree, l.read), refusals(ctx, tree, tree.Node(nil), l.root, l.opts, true)...)
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

// judge holds the tree to the rule table as the source writes it.
func (l *loading) judge(ctx context.Context) (bool, error) {
	l.judged = judgeSource(ctx, l.root, l.tree, nodesAdmitted(l.opts))
	return false, nil
}

// settle makes the tree read as the table judged it (settle).
func (l *loading) settle(context.Context) (bool, error) {
	settle(l.judged, &l.aside)
	return false, nil
}

// buildModel builds the parser's model of the tree, its anchors released, and
// refuses a version the compiler does not support.
func (l *loading) buildModel(ctx context.Context) (bool, error) {
	l.anchors = readyForModel(ctx, l.root, &l.aside)
	if refused, err := l.model(ctx); refused || err != nil {
		return refused, err
	}
	l.dialects = &dialects{spec: l.judged.checker.Spec(), version: l.doc.OpenAPI, minor: l.minor,
		dialect: l.doc.GetJSONSchemaDialect()}
	return false, nil
}

// model builds the parser's model of the tree (decode), and refuses a version
// the compiler does not support.
func (l *loading) model(ctx context.Context) (bool, error) {
	doc, err := l.decode(ctx)
	if err != nil {
		return false, err
	}
	minor, ok := supportedMinor(doc.OpenAPI)
	if !ok {
		l.early = append(l.early, diag.Newf(ir.SeverityError, diag.UnsupportedVersion,
			ir.Provenance{Source: l.srcIndex},
			"unsupported OpenAPI version %q; want 3.0, 3.1, or 3.2", doc.OpenAPI))
		return true, nil
	}
	l.doc, l.minor = doc, minor
	return false, nil
}

// decode builds the parser's model of the tree as it now reads.
func (l *loading) decode(ctx context.Context) (*soa.OpenAPI, error) {
	// The model build reads no context, so a cancel that cut a walk before it
	// short is seen before it starts, and never waited out on a tree half
	// prepared for it.
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	doc, err := unmarshal(ctx, l.src.Data, l.root)
	if err != nil {
		return nil, fmt.Errorf("openapi: unmarshal source %d: %w", l.srcIndex, err)
	}
	return doc, nil
}

// findSiblings models the fields the source writes beside its path items'
// $refs, and the components/mediaTypes entries it declares.
func (l *loading) findSiblings(ctx context.Context) (bool, error) {
	l.modelBeside(ctx)
	l.self = newSourceDocument(l.src, l.places, l.root, l.judged, l.tree)
	l.self.mappings = l.opts.Mappings
	return false, nil
}

// modelBeside models what the parser's model leaves out: the fields beside
// path item $refs, and the components/mediaTypes entries.
func (l *loading) modelBeside(ctx context.Context) {
	l.sibs, l.sibFaults = modelSiblings(ctx, l.doc, l.tree)
	var mediaFaults []ir.Diagnostic
	l.media, mediaFaults = modelMediaTypes(ctx, l.doc, l.tree)
	l.sibFaults = append(l.sibFaults, mediaFaults...)
}

// bindRefs binds every $ref in the compiler (bindTable), reading other
// documents through a preparing reader when the caller supplied one, judges
// the Schema Objects the table gives a dialect (judgeSchemas) and what each
// landing names (judgeReached), and builds it (bindAll), placing what it found
// where the binding places each document. A value a judgment leaves refused is
// turned before any landing is built, and what was read off the tree read
// again (reread).
func (l *loading) bindRefs(ctx context.Context) (bool, error) {
	if l.opts.Reader != nil {
		l.reader = newExternal(l.opts, newExternalReads(l.self, l.opts.MaxExternalDocuments), &l.aside)
	}
	table, unread := l.readTable(ctx)
	if len(l.judged.pending) > 0 && l.judgeLate(ctx) {
		if err := l.reread(ctx); err != nil {
			return false, err
		}
		table, unread = l.readTable(ctx)
	}
	l.self.judged = l.judged
	// A cancel that cut the judging short builds nothing on what it left.
	if err := l.judgeLandings(ctx, table); err != nil {
		return false, err
	}
	others := l.bound.others
	l.bound = bindAll(ctx, table, l.doc, &l.sibs, l.media)
	l.bound.others = others
	l.resolved = slices.Concat(l.bound.others.found, unread, l.bound.faults.diags(l.sites, table), bindingDiags(l.sites, table),
		buildFailures(l.sites, l.bound.built, l.bound.objects))
	if l.reader != nil {
		l.resolved = append(l.resolved, l.reader.read.exhausted(l.place)...)
	}
	return false, nil
}

// judgeLate judges the Schema Objects the bindings give a dialect
// (judgeSchemas) with the anchors readyForModel cleared named again, since
// whether another place may read a node is read off them
// (specrules.Options.Judged), and turns what that leaves refused (turnRefused),
// reporting whether it turned any.
func (l *loading) judgeLate(ctx context.Context) bool {
	for _, a := range l.anchors {
		a.node.Anchor = a.name
	}
	l.judged = judgeSchemas(ctx, l.judged, l.named)
	for _, a := range l.anchors {
		a.node.Anchor = ""
	}
	return turnRefused(l.judged)
}

// readTable binds the source's references (bindTable) and reads off the table
// what the load places and lowers by: the other documents a reference read,
// where a finding is placed, and the dialect each schema is read in, with
// what that reading could not read.
func (l *loading) readTable(ctx context.Context) (*reftable.ReferenceTargetTable, []ir.Diagnostic) {
	table := bindTable(ctx, l.doc, l.self, l.reader, refusedByName(l.opts, l.self.path, l.places), l.dialects)
	l.bound.others = Others{}
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
	return table, unread
}

// reread builds again, from the tree a late turn changed (bindRefs), what was
// read off it before: the shapes set aside, the parser's model and what it
// leaves out. The anchors are released already, and the version stands as the
// first build read it: a value the table refuses there is one the model
// decodes nothing from (docs/parser-library.md#undecoded-scalar), which that
// build refused.
func (l *loading) reread(ctx context.Context) error {
	setAside(l.judged, &l.aside)
	doc, err := l.decode(ctx)
	if err != nil {
		return err
	}
	l.doc = doc
	l.modelBeside(ctx)
	return nil
}

// judgeLandings judges what the references reach before any of it is built
// (judgeReached), over the tree as written, and makes the tree read as those
// judgments and the source's left it (settle): what they refuse and no reading
// accepts is turned, and the shapes the model faults on are set aside. It
// returns the context's error where that cut the judging short.
func (l *loading) judgeLandings(ctx context.Context, table *reftable.ReferenceTargetTable) error {
	var (
		j   judgment
		cut error
	)
	l.aside.whileWritten(func() {
		l.reached, j, cut = judgeReached(ctx, l.sites, table, l.doc, l.opts, l.self, l.bound.others, l.named)
		turnRefused(j)
	})
	setAside(j, &l.aside)
	return cut
}

// reach reports what judging what the references reach found (judgeReached),
// once every build of the model is done and what settle set aside for it is
// back, so every later reader sees the source as written.
func (l *loading) reach(context.Context) (bool, error) {
	l.aside.restore()
	l.resolved = slices.Concat(l.resolved, l.reached, unreportedRepeats(l.bound.others.repeats, l.reached))
	return false, nil
}

// unreportedRepeats returns each key another document writes twice that no
// check of an object a reference reached in it reports at the same place,
// under the same code: the object's wording stands (GitHub #1033).
func unreportedRepeats(repeats, reached []ir.Diagnostic) []ir.Diagnostic {
	type place struct {
		code    string
		source  int
		pointer jsontext.Pointer
	}
	said := make(map[place]bool, len(reached))
	for _, d := range reached {
		said[place{d.Code, d.Provenance.Source, d.Provenance.Pointer}] = true
	}
	var out []ir.Diagnostic
	for _, d := range repeats {
		if !said[place{d.Code, d.Provenance.Source, d.Provenance.Pointer}] {
			out = append(out, d)
		}
	}
	return out
}

// checkSource runs the source checks over the source (sourceChecks).
func (l *loading) checkSource(ctx context.Context) (bool, error) {
	l.checked = sourceChecks(ctx, l.sites, l.doc, l.sibs, l.media, l.bound.bindings)
	return false, nil
}

// document assembles the loaded Document from the phases, and what they
// reported in the order the load lists it.
func (l *loading) document(hash string) (*LoadedOpenAPIDocument, []ir.Diagnostic) {
	diags := slices.Concat(l.early, l.sibFaults, l.judged.diags, l.resolved, l.checked)
	return &LoadedOpenAPIDocument{
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
