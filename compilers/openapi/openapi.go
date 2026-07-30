package openapi

import (
	"context"
	"fmt"

	"github.com/dexpace/morphic/compilers"
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
	l := newLowerer(0, loadedDoc, formatOpts)
	out := l.run() // components schemas → auth → service/operations → meta; assembles Document
	//nolint:gocritic // deliberate concat: load diagnostics precede lowering diagnostics
	out.Diagnostics = append(diags, l.diags.List()...)
	return out, out.Diagnostics, nil
}

// run drives the four-phase pipeline over one loaded document (architecture
// §2.1). Order matters: named component schemas first, so refs from operations
// find interned IDs; then security schemes, so requirements reference registered
// IDs; then the service walk; then document metadata. It assembles and returns
// the Document.
func (l *lowerer) run() *ir.Document {
	l.lowerComponentSchemas()
	auth, authDiags := lowerSecuritySchemes(l.ctx)
	l.out.Auth = auth
	l.diags.AppendAll(authDiags)
	l.out.Services = []ir.Service{l.lowerService()}
	m, metaDiags := lowerMeta(l.ctx)
	l.out.Name, l.out.Version = m.Name, m.Version
	l.out.TermsOfService, l.out.Docs = m.TermsOfService, m.Docs
	l.out.Contact, l.out.License = m.Contact, m.License
	if len(m.Servers) > 0 {
		l.out.Servers = m.Servers
	}
	if len(m.Unmodeled) > 0 {
		l.out.Unmodeled = annotation.MergeUnmodeled(l.out.Unmodeled, m.Unmodeled)
	}
	l.diags.AppendAll(metaDiags)
	l.out.IRVersion = ir.IRVersion
	l.out.Sources = []ir.SourceInfo{l.ctx.Source}
	// An entry the registry refused is a compiler bug no source can provoke, and
	// a refusal nothing reports hides the bug rather than the symptom: the node is
	// simply absent and every reference to it dangles.
	for _, v := range l.types.Violations() {
		l.diag(ir.SeverityError, diag.InternalInvariant, "", "internal: %s", v)
	}
	return l.out
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
