package schema

import (
	"testing"

	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/compile"
	"github.com/dexpace/morphic/compilers/openapi/internal/load"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/compilers/openapi/internal/overlay"
	"github.com/dexpace/morphic/ir"
)

// Lowerer is test scaffolding, not a compiler type. The compiler has no such
// struct: every lowering is a function of the context and the registry, and
// the caller owns the document being built (#177). It is declared here, once,
// so the package's in-package and external tests drive one fixture.
type Lowerer struct {
	Ctx   lowering.Ctx
	Out   *ir.Document
	Types *compile.Types
	Diags compile.Diags
}

// LowererOver is the only place the fixture's fields are initialised. Both
// entry points below build on it, so a field added to Lowerer cannot reach one
// of them and miss the other.
func LowererOver(ctx lowering.Ctx) *Lowerer {
	types := compile.NewTypes()
	return &Lowerer{
		Ctx:   ctx,
		Out:   &ir.Document{},
		Types: types,
	}
}

// LoweredFor loads src and returns the fixture over it with nothing lowered yet,
// plus the load diagnostics, so a test can drive one entry point at a time.
func LoweredFor(t *testing.T, src string) (*Lowerer, []ir.Diagnostic) {
	t.Helper()
	loadedDoc, diags, err := load.Load(t.Context(), 0, openapitest.SourceOf(src), load.Options{})
	require.NoError(t, err)
	require.NotNil(t, loadedDoc, "load returned no document: %+v", diags)
	return LowererOver(lowering.New(0, loadedDoc.Doc, loadedDoc.Source,
		lowering.GroupByTags, lowering.Limits{}, lowering.StreamingMedia{}, lowering.ExtensionPromotions{}, lowering.NullableWidening{}, overlay.Origin{}).
		WithLoaded(loadedDoc)), diags
}

// LowerSpec loads src and lowers its component schemas, returning the document
// under construction and all diagnostics.
func LowerSpec(t *testing.T, src string) (*ir.Document, []ir.Diagnostic) {
	t.Helper()
	l, diags := LoweredFor(t, src)
	l.Diags.AppendAll(LowerComponentSchemas(t.Context(), l.Ctx, l.Types))
	doc := l.Document(t)
	return doc, append(diags, l.Diags.List()...)
}

// NewRawLowerer builds a fixture over a hand-constructed document, bypassing the
// parser so nil slice/map entries (which the parser panics on) can be exercised.
func NewRawLowerer(doc *soa.OpenAPI) *Lowerer {
	return LowererOver(lowering.New(0, doc, ir.SourceInfo{}, "", lowering.Limits{}, lowering.StreamingMedia{}, lowering.ExtensionPromotions{}, lowering.NullableWidening{}, overlay.Origin{}))
}

// Document finishes the registry as run does once every lowering has
// returned, and returns the document under construction holding it. What the
// settling reports joins the fixture's diagnostics; a refusal is a compiler bug
// no fixture provokes, so one fails the test.
func (l *Lowerer) Document(t *testing.T) *ir.Document {
	t.Helper()
	reg, settled := l.Types.Finish(t.Context())
	for _, d := range settled {
		require.NotEqual(t, compile.RegistryRefused, d.Code, "the registry refused an entry: %s", d.Message)
	}
	l.Diags.AppendAll(settled)
	l.Out.Types = reg
	return l.Out
}
