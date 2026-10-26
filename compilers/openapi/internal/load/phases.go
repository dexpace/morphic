package load

import (
	"context"
	"encoding/json/jsontext"
	"fmt"
	"slices"

	soa "github.com/speakeasy-api/openapi/openapi"
	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/overlay"
	"github.com/dexpace/morphic/compilers/openapi/internal/scan"
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
	phaseSiblings phase = "siblings" // the fields beside path item $refs modelled
	phaseResolve  phase = "resolve"  // reference objects through the parser's resolver
	phaseBind     phase = "bind"     // schema $refs bound in the compiler
	phaseReach    phase = "reach"    // what references reach, validated
	phaseRestore  phase = "restore"  // the models put back as the source writes them
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
		{phaseResolve, (*loading).resolveRefs},
		{phaseBind, (*loading).bindRefs},
		{phaseReach, (*loading).reach},
		{phaseRestore, (*loading).restoreModels},
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
	// early is what the phases up to the model report: a refusal among them
	// ends the load with these alone.
	early  []ir.Diagnostic
	origin overlay.Origin
	tree   sourceTree
	judged judgment
	// esc is how the trees were respelled for the parser, nil before they are.
	esc   *escapes
	doc   *soa.OpenAPI
	minor string
	sibs  Siblings
	// sibFaults is what modelling the fields beside a path item $ref faulted on.
	sibFaults []ir.Diagnostic
	self      sourceDocument
	reader    *external
	pass      *resolution
	missed    []usedDocument
	bound     bound
	// placeSite places a finding at a reference site, in whichever document
	// writes it.
	placeSite func(refSite) ir.Provenance
	// resolved and reached are what resolution, binding and validating what
	// references reach report, and checked what the source checks report.
	resolved, reached, checked []ir.Diagnostic
}

// at places a pointer as the lowering places it (pointerAt).
func (l *loading) at(p jsontext.Pointer) ir.Provenance {
	return pointerAt(l.srcIndex, l.origin)(p)
}

// preParse runs the pre-parse refusals, so the parser is never handed a tree
// it faults on. A non-fatal finding, the scan's report that it could not
// finish, is carried forward.
func (l *loading) preParse(ctx context.Context) (bool, error) {
	l.early = refusals(ctx, scan.InTree(l.srcIndex, scan.Pointers(ctx, l.root)), l.root, l.opts)
	return diag.HasError(l.early), nil
}

// patchOverlay reads and applies the caller's overlay, refusing to lower a
// half-patched tree.
func (l *loading) patchOverlay(ctx context.Context) (bool, error) {
	origin, diags, err := patch(ctx, l.srcIndex, l.root, l.opts)
	if err != nil {
		return false, err
	}
	l.origin, l.early = origin, append(l.early, diags...)
	return diag.HasError(l.early), nil
}

// measure holds the tree to the node budget. It is measured after the overlay
// for the reason patch re-runs the pre-parse refusals: the tree the model is
// built from is no longer the bytes that were measured, and an overlay action
// can graft nodes onto it. Building that model and resolving its references is
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

// judge holds the tree to the rule table as the source writes it, before
// anything after it respells the tree for the parser.
func (l *loading) judge(ctx context.Context) (bool, error) {
	l.tree = newSourceTree(ctx, l.srcIndex, l.origin, l.root)
	l.judged = judgeSource(ctx, l.root, l.tree, nodesAdmitted(l.opts))
	return false, nil
}

// buildModel builds the parser's model of the tree, its anchors released and
// its $refs respelled for the parser, and refuses a version the compiler does
// not support. The trees are put back when build returns, and the models at
// the end of resolution.
func (l *loading) buildModel(ctx context.Context) (bool, error) {
	releaseAnchors(ctx, l.root)
	l.esc = newEscapes(refusedByName(l.opts, l.src.Path))
	l.esc.escape(ctx, l.root)
	// The model build reads no context, so a cancel that cut the walks above
	// short is seen before it starts, and never waited out on a tree half
	// prepared for it.
	if err := ctx.Err(); err != nil {
		return false, err
	}
	doc, err := unmarshal(ctx, l.src.Data, l.root)
	if err != nil {
		return false, fmt.Errorf("openapi: unmarshal source %d: %w", l.srcIndex, err)
	}
	minor, ok := SupportedMinor(doc.OpenAPI)
	if !ok {
		l.early = append(l.early, diag.Newf(ir.SeverityError, diag.UnsupportedVersion,
			ir.Provenance{Source: l.srcIndex},
			"unsupported OpenAPI version %q; want 3.0, 3.1, or 3.2", doc.OpenAPI))
		return true, nil
	}
	l.doc, l.minor = doc, minor
	return false, nil
}

// findSiblings models the fields the source writes beside its path items'
// $refs.
func (l *loading) findSiblings(ctx context.Context) (bool, error) {
	l.sibs, l.sibFaults = modelSiblings(ctx, l.doc, l.tree.locate)
	l.self = newSourceDocument(l.src.Path, l.src.Data, l.root, l.judged, l.tree, l.esc)
	return false, nil
}

// resolveRefs resolves every reference object through the parser's resolver
// (resolveWith), reading other documents through a preparing reader when the
// caller supplied one (resolveExternal). That reader can rebuild the model.
func (l *loading) resolveRefs(ctx context.Context) (bool, error) {
	if l.opts.Reader == nil {
		l.pass = resolveWith(ctx, l.doc, l.sibs, l.self, l.opts, nil)
		return false, nil
	}
	doc, sibs, reader, pass, missed, err := resolveExternal(ctx, l.doc, l.sibs, l.self, l.opts,
		l.opts.rebuilder(ctx, l.src.Data, l.root))
	if err != nil {
		return false, fmt.Errorf("openapi: rebuild source %d: %w", l.srcIndex, err)
	}
	l.doc, l.sibs, l.reader, l.pass, l.missed = doc, sibs, reader, pass, missed
	return false, nil
}

// bindRefs binds every schema $ref in the compiler (bindSchemas), once every
// reference object is resolved, and reports what resolution found, placed
// where the binding places each document. The parser reads the $refs as the
// trees were rewritten for it here, and up to restoreModels.
func (l *loading) bindRefs(ctx context.Context) (bool, error) {
	l.bound = bindSchemas(ctx, l.doc, l.sibs, l.self, l.reader, l.pass.ends())
	if l.reader != nil {
		first := l.srcIndex + 1
		if l.origin.Applied() {
			first = l.opts.OverlaySrcIndex + 1
		}
		l.bound.others = newOthers(ctx, l.reader.read, first, "openapi@"+l.minor)
	}
	l.placeSite = sitePlacer(l.at, l.bound.others)
	l.resolved = append(l.pass.diags(l.placeSite), stillUnprepared(l.placeSite, l.missed)...)
	return false, nil
}

// reach validates what the references reach (validateReached), over the
// document resolved, since a rebuild discards a first resolution's objects.
func (l *loading) reach(ctx context.Context) (bool, error) {
	l.reached = validateReached(ctx, l.at, l.bound, l.doc, l.sibs, l.opts, l.self, l.pass.elsewhere(l.placeSite))
	return false, nil
}

// restoreModels puts the models back as the source writes them, then reports
// what binding found and the budgets the reads crossed.
func (l *loading) restoreModels(ctx context.Context) (bool, error) {
	site, err := l.self.escapes.restore(ctx, l.doc, l.sibs, l.bound.built)
	place := l.bound.placer(l.at)
	l.resolved = append(l.resolved, restoreFault(l.at, site, err)...)
	l.resolved = append(l.resolved, bindingDiags(place, l.bound.bindings.Table())...)
	l.resolved = append(l.resolved, buildFailures(place, l.bound.built)...)
	if l.reader != nil {
		l.resolved = append(l.resolved, crossed(l.at, l.reader)...)
	}
	l.resolved = append(l.resolved, l.reached...)
	return false, nil
}

// checkSource runs the source checks over the resolved source (sourceChecks).
func (l *loading) checkSource(ctx context.Context) (bool, error) {
	l.checked = sourceChecks(ctx, l.at, l.doc, l.sibs, l.resolved)
	return false, nil
}

// restoreTrees puts the trees back as the source writes them, once they were
// respelled.
func (l *loading) restoreTrees() {
	if l.esc != nil {
		l.esc.restoreTrees()
	}
}

// document assembles the loaded Document from the phases, and what they
// reported in the order the load lists it, each $ref as the source writes it.
func (l *loading) document(hash string) (*Document, []ir.Diagnostic) {
	diags := slices.Concat(l.early, l.sibFaults, l.judged.diags, l.resolved, l.checked)
	return &Document{
		Doc:      l.doc,
		Bindings: l.bound.bindings,
		Holds:    newTreeReads().holds,
		Source: ir.SourceInfo{
			Format:  "openapi@" + l.minor,
			Path:    l.src.Path,
			Hash:    hash,
			SelfURI: l.doc.GetSelf(),
		},
		Overlay:  l.origin,
		Siblings: l.sibs,
		Others:   l.bound.others,
	}, l.esc.asWritten(diags)
}
