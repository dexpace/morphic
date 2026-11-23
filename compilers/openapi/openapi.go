// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package openapi

import (
	"context"
	"errors"
	"fmt"
	"iter"
	"path/filepath"
	"slices"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/compile"
	"github.com/dexpace/morphic/compilers/openapi/internal/annotation"
	"github.com/dexpace/morphic/compilers/openapi/internal/auth"
	"github.com/dexpace/morphic/compilers/openapi/internal/budget"
	"github.com/dexpace/morphic/compilers/openapi/internal/compileddoc"
	"github.com/dexpace/morphic/compilers/openapi/internal/convention"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/document"
	"github.com/dexpace/morphic/compilers/openapi/internal/ids"
	"github.com/dexpace/morphic/compilers/openapi/internal/load"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/compilers/openapi/internal/operation"
	"github.com/dexpace/morphic/compilers/openapi/internal/overlay"
	"github.com/dexpace/morphic/compilers/openapi/internal/pagination"
	"github.com/dexpace/morphic/compilers/openapi/internal/paramchoice"
	"github.com/dexpace/morphic/compilers/openapi/internal/promotion"
	"github.com/dexpace/morphic/compilers/openapi/internal/replacement"
	"github.com/dexpace/morphic/compilers/openapi/internal/schema"
	"github.com/dexpace/morphic/compilers/openapi/internal/servers"
	"github.com/dexpace/morphic/compilers/openapi/internal/service"
	"github.com/dexpace/morphic/compilers/openapi/internal/statement"
	"github.com/dexpace/morphic/compilers/openapi/internal/stream"
	"github.com/dexpace/morphic/ir"
)

var _ compilers.Compiler = (*Compiler)(nil)

// rootSrcIndex is the index of the only source milestone 1 compiles.
//
// Every place that stamps it has to agree: the loader records it in the
// SourceInfo, the lowering stamps every Provenance it builds, and detection
// names the source by it. Naming it says they must, where a bare 0 written at
// each site only happens to. The type registry does not stamp it: a shared
// primitive names no source (GitHub #528).
//
// A varying index arrives with the link pass, from Compile's caller.
const rootSrcIndex = 0

// overlaySrcIndex is the index an applied overlay takes in Document.Sources.
//
// It follows the root because the root is the document being compiled and the
// overlay is a patch on it, and it is spelled once here for the reason
// rootSrcIndex is: the loader stamps it into the overlay's diagnostics and the
// lowering stamps it into every position the overlay introduced, and the two
// have to agree.
const overlaySrcIndex = rootSrcIndex + 1

// Compiler lowers OpenAPI 3.x documents into the IR.
type Compiler struct{}

// New returns the OpenAPI compiler.
func New() *Compiler { return &Compiler{} }

// Formats reports the OpenAPI dialects this compiler accepts.
func (*Compiler) Formats() []compilers.SourceFormat {
	return []compilers.SourceFormat{
		{Name: "openapi", Version: "3.0"},
		{Name: "openapi", Version: "3.1"},
		{Name: "openapi", Version: "3.2"},
	}
}

// Compile implements compilers.Compiler. Milestone 1 accepts exactly one root
// source; multi-document stitching belongs to the link pass.
func (c *Compiler) Compile(ctx context.Context, sources []compilers.Source, opts compilers.Options) (*ir.Document, []ir.Diagnostic, error) {
	if len(sources) != 1 {
		return nil, nil, fmt.Errorf("openapi: expected exactly one source, got %d", len(sources))
	}
	formatOpts, err := optionsFrom(opts) // nil FormatOptions → defaults; wrong type → error
	if err != nil {
		return nil, nil, err
	}
	// Here and not in optionsFrom: detection reads its byte budget through
	// optionsFrom, taking the default one on an error, so a grouping the compile
	// cannot honour would have it read past the caller's.
	if err := formatOpts.checkGrouping(); err != nil {
		return nil, nil, err
	}
	mapped, err := mappingsOf(formatOpts.Locations)
	if err != nil {
		return nil, nil, err
	}
	if err := formatOpts.Promotions.Check(); err != nil {
		return nil, nil, fmt.Errorf("openapi: %w", err)
	}
	loading := loadOptions(formatOpts, opts.Reader, sources[0])
	loading.Mappings = mapped
	loadedDoc, loadDiags, err := load.Load(ctx, rootSrcIndex, sources[0], loading)
	// The load phase reads a document once under each name a reference spells
	// it by, as a relative and an absolute path to one file where external
	// references are allowed, so what it finds there it finds once per name,
	// at one position: one finding, kept once, as compile.Diags keeps the
	// lowering's.
	var loaded compile.Diags
	loaded.AppendAll(loadDiags)
	diags := loaded.List()
	if errors.Is(err, load.ErrParse) {
		// ErrParse is a source Load could not read: bytes that will not parse,
		// which reach here only from a caller compiling directly — detection
		// declines them before any compile — or a fault the parser raised on a
		// document that did decode. Either is the document's problem, so it is a
		// diagnostic: a Go error here leaves engine.Run as a Go error, and the
		// CLI reads that as a misuse of itself rather than as a spec it could
		// not read.
		return nil, append(diags, undecodable(err)), nil
	}
	if err != nil || loadedDoc == nil {
		return nil, diags, err
	}
	// components schemas → auth → service/operations → document metadata;
	// assembles Document
	out, lowerDiags, err := run(ctx, service.New(rootSrcIndex, loadedDoc, loweringPolicy(formatOpts)),
		inferencesOf(formatOpts), compile.NewTypes(),
		compileddoc.New)
	// Load diagnostics precede lowering diagnostics.
	all := slices.Concat(diags, withoutRereported(lowerDiags, diags))
	if err != nil {
		return nil, all, err
	}
	if out == nil {
		// A refusal's findings are read against SourceTable, which names no
		// document a $ref read (compilers.Compiled), so what the load phase found
		// in one is left out with the Document that would have listed it.
		n := len(c.SourceTable(sources, opts))
		return nil, slices.DeleteFunc(all, func(diagnostic ir.Diagnostic) bool { return !diagnostic.Provenance.InTable(n) }), nil
	}
	out.Diagnostics = all
	return out, out.Diagnostics, nil
}

// withoutRereported drops each lowering report of an unresolved reference that
// the load phase already reported at the same position.
//
// The load phase reports a failure at the $ref, and a cycle once, with the
// reason. The lowering reports a schema $ref it could not follow at that
// pointer, and also where the load phase did not, as at a $ref the binding
// never read. So only a report the load phase made at that exact provenance,
// in whichever document, is dropped; kept, one failure would read as two
// (#385).
func withoutRereported(lowered, loaded []ir.Diagnostic) []ir.Diagnostic {
	reported := make(map[ir.Provenance]bool)
	for _, diagnostic := range loaded {
		if diagnostic.Code == diag.UnresolvedRef || diagnostic.Code == diag.CyclicRef {
			reported[diagnostic.Provenance] = true
		}
	}
	out := make([]ir.Diagnostic, 0, len(lowered))
	for _, diagnostic := range lowered {
		if diagnostic.Code == diag.UnresolvedRef && reported[diagnostic.Provenance] {
			continue
		}
		out = append(out, diagnostic)
	}
	return out
}

// SourceTable implements compilers.Compiler: the source at rootSrcIndex, then
// the overlay at overlaySrcIndex when the options name one. Compile takes
// exactly one source, so the two indexes are the ones every provenance this
// compiler builds uses.
//
// The overlay is listed whenever it is named, not only when it applied: a
// compile it refused reports at its index, and that refusal is the case this
// table exists for.
func (*Compiler) SourceTable(sources []compilers.Source, opts compilers.Options) []ir.SourceInfo {
	table := make([]ir.SourceInfo, 0, len(sources)+1)
	for _, src := range sources {
		table = append(table, ir.SourceInfo{Path: src.Path})
	}
	if options, err := optionsFrom(opts); err == nil && options.Overlay != nil {
		table = append(table, ir.SourceInfo{Path: options.Overlay.Path})
	}
	return table
}

// run drives the four-phase pipeline over one loaded document (architecture
// §2.1) and assembles the Document. Order matters: named component schemas
// first, so operations' refs find interned IDs; then security schemes, which
// requirements reference; then the service walk and deferred settlements; then
// the steps reading compileddoc.CompiledDocumentView.
//
// It reports cancellation as a Go error, not a diagnostic: nothing about the
// document is wrong, the caller stopped asking. It drops the document, since
// the phases honouring ctx stop mid-registry, leaving references to types never
// interned or settled, but returns the diagnostics gathered before the stop.
// It refuses a document past Limits.MaxReferenceExpansion without error.
func run(ctx context.Context, serviceContext service.Ctx, infinity inferences, ts *compile.Types,
	newWhole func(*ir.Document) *compileddoc.CompiledDocumentView,
) (*ir.Document, []ir.Diagnostic, error) {
	// out is this function's, not a struct's: a document being built
	// (micro-compiler-design §4.1). Nothing below allocates it.
	out := &ir.Document{}

	// acc is what makes the identity dedup still hold. Every lowering returns its
	// diagnostics now, so a shared declaration reported from N use sites returns N
	// copies; compile.Diags.Append is what collapses them, and it has to see the
	// whole stream to do it.
	var acc compile.Diags
	acc.AppendAll(schema.LowerComponentSchemas(ctx, serviceContext.Ctx, ts, componentStatements(serviceContext)))
	// The checks in this function sit after the phases that honour ctx. Each
	// stops between items and returns what it had, so a non-nil ctx.Err() here
	// is the only way to tell a phase that finished from one that was cut short —
	// and the cut-short one leaves a registry nothing may be assembled from.
	if err := ctx.Err(); err != nil {
		return nil, acc.List(), err
	}

	declared, authDiags := auth.LowerSecuritySchemes(serviceContext)
	acc.AppendAll(authDiags)

	lowered, svcDiags := operation.LowerService(ctx, serviceContext, ts, declared)
	out.Auth = lowered.Auth
	out.Services = []ir.Service{lowered.Service}
	out.TagDefs = lowered.TagDefs
	out.Unmodeled = annotation.MergeUnmodeled(out.Unmodeled, lowered.Kept)
	acc.AppendAll(svcDiags)
	schema.ReserveNodesAtReferenceTargets(serviceContext.Ctx, ts)
	// Every lowering has returned, so the registry is finished: a coordinate a
	// $ref named and none built is built now, in its own context
	// (compile.Types.ReserveNodeAtCoordinate), and what was deferred until the
	// registry is whole is decided (compile.Settlement), before the check that
	// refuses a registry either phase left cut short. What Finish reports
	// carries each entry the registry refused, a compiler bug that would
	// otherwise leave every reference to it dangling with nothing said.
	reg, settled := ts.Finish(ctx)
	acc.AppendAll(settled)
	out.Types = reg
	// Attach typed Targets to kept Unmodeled entries before pruning so every
	// kept $ref and discriminator-mapping landing is explicit in the IR for
	// pruning, irverify, and emitters. Pruning then removes unreachable
	// anonymous types before compileddoc.New runs links, promotions and
	// inferences.
	attachUnmodeledTargets(ctx, out, schema.ReferenceLandings(serviceContext.Ctx))
	var pruneDiags []ir.Diagnostic
	out, pruneDiags = pruneUnreferencedTypes(ctx, out, schema.DiscardedTypes(serviceContext.Ctx))
	acc.AppendAll(pruneDiags)
	if err := ctx.Err(); err != nil {
		return nil, acc.List(), err
	}
	// What is read once the document is whole is read here, in the one order
	// docs/compilers/openapi.md#policies gives: links, then promotions, then
	// inferences, an inference filling only what no keyword or promotion
	// states. A link names operations and members anywhere in the service, and
	// a server it names registers then too, so the servers are read after it. A
	// replacement may be declared after what it replaces, and what a name
	// implies turns on the type a $ref names, built in any order. Each step
	// reads the document through compiledDocumentView, which lists and indexes
	// it once for all; newWhole is compileddoc.New, which a test counts.
	compiledDocumentView := newWhole(out)
	srv := servers.Reopen(lowered.Servers)
	acc.AppendAll(lowered.Links.Resolve(serviceContext, compiledDocumentView, srv))
	out.Servers = srv.Servers()
	// The document's root is lowered before the promotions, as a pagination key
	// an operation writes true reads the root's; its findings keep their place.
	meta, metaDiags := document.Lower(serviceContext)
	out.Unmodeled = annotation.MergeUnmodeled(out.Unmodeled, meta.Unmodeled)
	// The operations whose extensions speak of a pagination are what the
	// pagination inference passes over, by the precedence
	// docs/compilers/openapi.md#policies gives: what they say of one, even that
	// there is none, wins over what the names an operation uses suggest.
	promoter := serviceContext.Promoter()
	stated, pageDiags := pagination.Promote(promoter, compiledDocumentView, out.Unmodeled)
	acc.AppendAll(pageDiags)
	acc.AppendAll(stream.PromoteDetail(promoter, compiledDocumentView))
	acc.AppendAll(paramchoice.PromoteChoices(promoter, compiledDocumentView))
	acc.AppendAll(replacement.Resolve(promoter, compiledDocumentView))
	acc.AppendAll(replacement.Infer(infinity.replacement, promoter, compiledDocumentView))
	acc.AppendAll(convention.Apply(infinity.convention, promoter, compiledDocumentView))
	acc.AppendAll(pagination.InferWalks(infinity.pagination, compiledDocumentView, stated))

	out.Name, out.Version = meta.Name, meta.Version
	out.TermsOfService, out.Docs = meta.TermsOfService, meta.Docs
	out.Contact, out.License = meta.Contact, meta.License
	acc.AppendAll(metaDiags)
	// An extension under a prefix the policy owns is judged once every node
	// that could keep one is built, the document's own Unmodeled included.
	acc.AppendAll(promotion.UnreadExtensions(promoter, serviceContext.GroupingExtension.Keys(serviceContext.Grouping), out))
	// Past the budget no $ref lowered, nor an additionalOperations entry, so
	// what the lowering found depends on which copy came first: the refusal is
	// reported alone, the same in any order. It is asked once every lowering has
	// returned, so none charges after it.
	if refusal, past := serviceContext.ExpansionRefusal(); past {
		return nil, []ir.Diagnostic{refusal}, nil
	}
	out.IRVersion = ir.IRVersion
	out.IDSpaces = ids.Namespaces(service.OtherIDs(serviceContext)...).Declaration()
	out.Sources = serviceContext.Sources()
	return out, acc.List(), nil
}

// componentStatements yields each component schema of serviceContext's document
// under its name, in source order, as the statement serviceContext's table
// reads of it, read when the schema walk asks for it.
func componentStatements(serviceContext service.Ctx) iter.Seq2[string, *statement.Statement] {
	return func(yield func(string, *statement.Statement) bool) {
		for name, js := range serviceContext.Doc.Components.GetSchemas().All() {
			if !yield(name, serviceContext.Statements().Of(js)) {
				return
			}
		}
	}
}

// optionsFrom resolves the compiler-specific options: a nil FormatOptions gets
// defaults, an openapi.Options value is normalized, and any other type is a
// programmer error.
func optionsFrom(opts compilers.Options) (Options, error) {
	switch fo := opts.FormatOptions.(type) {
	case nil:
		return Options{}.withDefaults(), nil
	case Options:
		return fo.withDefaults(), nil
	default:
		return Options{}, fmt.Errorf("openapi: FormatOptions must be openapi.Options, got %T", opts.FormatOptions)
	}
}

// loadOptions projects Options onto the subset the load phase reads, with the
// caller's reader and the confinement Options.AllowExternalRefs sets for it:
// the directory src lies in (load.Location) unless it is on. It exists so the
// mapping lives in one place: load.Options is that package's own input,
// deliberately not this public type, whose shape is a published contract (see
// Options) and most of which describes lowering the loader cannot see. Each
// budget is read as Limits spells one, its default beside it
// (budget.FromOption).
func loadOptions(options Options, reader compilers.DocumentReader, src compilers.Source) load.Options {
	out := load.Options{
		Reader:               reader,
		MaxSourceBytes:       budget.FromOption(options.Limits.MaxSourceBytes, DefaultMaxSourceBytes),
		MaxSourceNodes:       budget.FromOption(options.Limits.MaxSourceNodes, DefaultMaxSourceNodes),
		MaxAliasSurplus:      budget.FromOption(options.Limits.MaxAliasSurplus, DefaultMaxAliasSurplus),
		MaxExternalDocuments: budget.FromOption(options.Limits.MaxExternalDocuments, DefaultMaxExternalDocuments),
	}
	if !options.AllowExternalRefs {
		out.Within = filepath.Dir(load.Location(src))
	}
	if options.Overlay != nil {
		out.Overlay = &overlay.Options{Path: options.Overlay.Path, Data: options.Overlay.Data, Lax: options.Overlay.Lax}
		out.OverlaySrcIndex = overlaySrcIndex
	}
	return out
}

// loweringPolicy projects the caller's options onto the lowerings', and is the
// loadOptions of the phase below: service.Policy is that package's own input,
// deliberately not this public type. It reads the budgets the lowering holds,
// on enums and on what $refs and path items copy, as loadOptions reads the
// others.
func loweringPolicy(options Options) service.Policy {
	return service.Policy{
		Schema: lowering.Policy{
			Limits:                 lowering.Limits{MaxEnumMembers: budget.FromOption(options.Limits.MaxEnumMembers, DefaultMaxEnumMembers)},
			Promotions:             options.Promotions,
			NullableInference:      options.NullableInference,
			DiscriminatorInference: options.DiscriminatorInference,
			KindInference:          options.KindInference,
			TitleNames:             options.TitleNames,
		},
		Grouping:              options.Grouping,
		GroupingExtension:     options.GroupingExtension,
		MaxReferenceExpansion: budget.FromOption(options.Limits.MaxReferenceExpansion, DefaultMaxReferenceExpansion),
		StreamingInference:    options.StreamingInference,
		BinaryInference:       options.BinaryInference,
	}
}

// inferences holds the policies of what run infers once the document is whole,
// each handed to the one step that honours it.
type inferences struct {
	pagination  pagination.Inference
	replacement replacement.Inference
	convention  convention.Policy
}

// inferencesOf projects Options onto the inference policies, as loweringPolicy
// projects it onto the lowering's.
func inferencesOf(options Options) inferences {
	return inferences{
		pagination:  options.PaginationInference,
		replacement: options.ReplacementInference,
		convention: convention.Policy{Idempotency: options.IdempotencyInference, Secret: options.SecretInference,
			HTTPDate: options.HTTPDateInference},
	}
}

// undecodable reports a source this compiler recognized and could not read.
//
// It names the source even though no document is returned: SourceTable is the
// table a refusal's diagnostics index. The loader's own message carries the
// position, which the parse that failed never turned into a node.
func undecodable(err error) ir.Diagnostic {
	return diag.Newf(ir.SeverityError, diag.UndecodableSource, ir.Provenance{Source: rootSrcIndex},
		"source cannot be read: %s", diag.OneLine(err))
}
