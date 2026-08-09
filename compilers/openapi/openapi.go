package openapi

import (
	"context"
	"fmt"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/compile"
	"github.com/dexpace/morphic/compilers/openapi/internal/annotation"
	"github.com/dexpace/morphic/compilers/openapi/internal/auth"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/load"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/compilers/openapi/internal/operation"
	"github.com/dexpace/morphic/compilers/openapi/internal/overlay"
	"github.com/dexpace/morphic/compilers/openapi/internal/schema"
	"github.com/dexpace/morphic/ir"
)

// rootSrcIndex is the index of the only source milestone 1 compiles.
//
// Three places stamp it and all three have to agree: the loader records it in
// the SourceInfo, the type registry stamps the primitives it interns, and the
// lowering stamps every Provenance it builds. Naming it says they must, where a
// bare 0 written at each site only happens to.
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
	loadedDoc, diags, err := load.Load(ctx, rootSrcIndex, sources[0], loadOptions(formatOpts))
	if err != nil || loadedDoc == nil {
		return nil, diags, err
	}
	// components schemas → auth → service/operations → meta; assembles Document
	out, lowerDiags := run(loweringCtx(loadedDoc, formatOpts), compile.NewTypes(rootSrcIndex))
	//nolint:gocritic // deliberate concat: load diagnostics precede lowering diagnostics
	out.Diagnostics = append(diags, lowerDiags...)
	return out, out.Diagnostics, nil
}

// run drives the four-phase pipeline over one loaded document (architecture
// §2.1). Order matters: named component schemas first, so refs from operations
// find interned IDs; then security schemes, so requirements reference registered
// IDs; then the service walk; then document metadata. It assembles and returns
// the Document.
func run(c lowering.Ctx, ts *compile.Types) (*ir.Document, []ir.Diagnostic) {
	// out, the anchor memo and the claimed-operationId set are this function's,
	// not a struct's: a document being built, a memo, and a loop-local set
	// (micro-compiler-design §4.1). Nothing below allocates them.
	out := &ir.Document{Types: ts.Registry()}
	var anchors schema.AnchorIndex
	operationIDs := make(map[string]string)

	// acc is what makes the identity dedup still hold. Every lowering returns its
	// diagnostics now, so a shared declaration reported from N use sites returns N
	// copies; compile.Diags.Append is what collapses them, and it has to see the
	// whole stream to do it.
	var acc compile.Diags
	acc.AppendAll(schema.LowerComponentSchemas(c, ts, &anchors))

	schemes, authDiags := auth.LowerSecuritySchemes(c)
	out.Auth = schemes
	acc.AppendAll(authDiags)
	// Only the service walk gets the auth-carrying context; run keeps the plain
	// one, so no lowering above the schemes can read them as empty.
	svcCtx := c.WithAuth(schemes)

	svc, tagDefs, svcDiags := operation.LowerService(svcCtx, ts, &anchors, operationIDs)
	out.Services = []ir.Service{svc}
	out.TagDefs = tagDefs
	acc.AppendAll(svcDiags)

	m, metaDiags := lowerMeta(c)
	out.Name, out.Version = m.Name, m.Version
	out.TermsOfService, out.Docs = m.TermsOfService, m.Docs
	out.Contact, out.License = m.Contact, m.License
	if len(m.Servers) > 0 {
		out.Servers = m.Servers
	}
	if len(m.Unmodeled) > 0 {
		out.Unmodeled = annotation.MergeUnmodeled(out.Unmodeled, m.Unmodeled)
	}
	acc.AppendAll(metaDiags)
	out.IRVersion = ir.IRVersion
	out.Sources = c.Sources()
	// An entry the registry refused is a compiler bug no source can provoke, and
	// a refusal nothing reports hides the bug rather than the symptom: the node is
	// simply absent and every reference to it dangles.
	for _, v := range ts.Violations() {
		acc.Append(c.DiagAt(ir.SeverityError, diag.InternalInvariant, "", "internal: %s", v))
	}
	return out, acc.List()
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

// loadOptions projects Options onto the subset the load phase reads. It exists
// so the mapping lives in one place: load.Options is that package's own input,
// deliberately not this public type, whose shape ir-design §10 fixes and most of
// which describes lowering the loader cannot see.
func loadOptions(o Options) load.Options {
	out := load.Options{AllowExternalRefs: o.AllowExternalRefs}
	if o.Overlay != nil {
		out.Overlay = &overlay.Options{Path: o.Overlay.Path, Data: o.Overlay.Data, Lax: o.Overlay.Lax}
		out.OverlaySrcIndex = overlaySrcIndex
	}
	return out
}

// loweringCtx projects a loaded document and the caller's options onto the
// lowering context, and is the loadOptions of the phase below: it is the one
// place the loader's result type meets the lowering, so lowering.New can take
// the two facts it needs rather than the loader's struct.
func loweringCtx(doc *load.Document, o Options) lowering.Ctx {
	return lowering.New(rootSrcIndex, doc.Doc, doc.Source, o.Grouping, o.Promotions, doc.Overlay)
}
