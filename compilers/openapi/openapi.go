package openapi

import (
	"context"
	"fmt"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/compile"
	"github.com/dexpace/morphic/compilers/openapi/internal/annotation"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/load"
	"github.com/dexpace/morphic/ir"
)

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
	loadedDoc, diags, err := load.Load(ctx, 0, sources[0], loadOptions(formatOpts))
	if err != nil || loadedDoc == nil {
		return nil, diags, err
	}
	// components schemas → auth → service/operations → meta; assembles Document
	out, lowerDiags := run(newLowerCtx(0, loadedDoc, formatOpts), compile.NewTypes(0))
	//nolint:gocritic // deliberate concat: load diagnostics precede lowering diagnostics
	out.Diagnostics = append(diags, lowerDiags...)
	return out, out.Diagnostics, nil
}

// run drives the four-phase pipeline over one loaded document (architecture
// §2.1). Order matters: named component schemas first, so refs from operations
// find interned IDs; then security schemes, so requirements reference registered
// IDs; then the service walk; then document metadata. It assembles and returns
// the Document.
func run(c lowerCtx, ts *compile.Types) (*ir.Document, []ir.Diagnostic) {
	// out, the anchor memo and the claimed-operationId set are this function's,
	// not a struct's: a document being built, a memo, and a loop-local set
	// (micro-compiler-design §4.1). Nothing below allocates them.
	out := &ir.Document{Types: ts.Registry()}
	var anchors anchorIndex
	operationIDs := make(map[string]string)

	// acc is what makes the identity dedup still hold. Every lowering returns its
	// diagnostics now, so a shared declaration reported from N use sites returns N
	// copies; compile.Diags.Append is what collapses them, and it has to see the
	// whole stream to do it.
	var acc compile.Diags
	acc.AppendAll(lowerComponentSchemas(c, ts, &anchors))

	auth, authDiags := lowerSecuritySchemes(c)
	out.Auth = auth
	acc.AppendAll(authDiags)
	// Only the service walk gets the auth-carrying context; run keeps the plain
	// one, so no lowering above the schemes can read them as empty.
	svcCtx := c.withAuth(auth)

	svc, tagDefs, svcDiags := lowerService(svcCtx, ts, &anchors, operationIDs)
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
	out.Sources = []ir.SourceInfo{c.Source}
	// An entry the registry refused is a compiler bug no source can provoke, and
	// a refusal nothing reports hides the bug rather than the symptom: the node is
	// simply absent and every reference to it dangles.
	for _, v := range ts.Violations() {
		acc.Append(c.diagAt(ir.SeverityError, diag.InternalInvariant, "", "internal: %s", v))
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
	return load.Options{DisableExternalRefs: o.DisableExternalRefs}
}
