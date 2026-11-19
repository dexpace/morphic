// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package operation

import (
	"iter"
	"testing"

	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/compile"
	"github.com/dexpace/morphic/compilers/openapi/internal/auth"
	"github.com/dexpace/morphic/compilers/openapi/internal/compileddoc"
	"github.com/dexpace/morphic/compilers/openapi/internal/load"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/compilers/openapi/internal/schema"
	"github.com/dexpace/morphic/compilers/openapi/internal/servers"
	"github.com/dexpace/morphic/compilers/openapi/internal/service"
	"github.com/dexpace/morphic/compilers/openapi/internal/statement"
	"github.com/dexpace/morphic/ir"
)

// The tests in this half read an unexported lowering directly. Everything
// reachable through LowerService lives in package operation_test, which can
// import the compiler and drive a spec end to end; nothing importable from
// there would reach these.

// lowerer is test scaffolding, not a compiler type: every lowering is a
// function of the context and the registry, or the walk holding it (#177), and
// this holds what a single-lowering test wants in one place.
type lowerer struct {
	ctx   service.Ctx
	types *compile.Types
	diags compile.Diags
}

// lowererOver is the only place the fixture's fields are initialised. Both
// entry points below build on it, so a field added to lowerer cannot reach one
// of them and miss the other.
func lowererOver(ctx service.Ctx) *lowerer {
	types := compile.NewTypes()
	return &lowerer{
		ctx:   ctx,
		types: types,
	}
}

// serviceWalk stands in for the walk LowerService hands the lowerings beneath it:
// l's registry, and a kept set and links of its own. It holds no operationId
// claims, servers or schemes: no lowering a test here hands it to reads them.
func (l *lowerer) serviceWalk() operationLoweringState {
	return operationLoweringState{types: l.types, kept: newKeptOnce(), links: &Links{}}
}

// loweredFor loads src and returns the fixture over it with nothing lowered
// yet, plus the load diagnostics.
func loweredFor(t *testing.T, src string) (*lowerer, []ir.Diagnostic) {
	t.Helper()
	return loweredAt(t, openapitest.SourceOf(src), load.Options{})
}

// loweredAt is loweredFor for a source with a path of its own, loaded under
// opts, which a test needs when the document names another one.
func loweredAt(t *testing.T, src compilers.Source, opts load.Options) (*lowerer, []ir.Diagnostic) {
	t.Helper()
	loadedDoc, diags, err := load.Load(t.Context(), 0, src, opts)
	require.NoError(t, err)
	require.NotNil(t, loadedDoc, "load returned no document: %+v", diags)
	return lowererOver(service.New(0, loadedDoc, service.Policy{Grouping: service.GroupByTags})), diags
}

// newRawLowerer builds a fixture over a hand-constructed document, bypassing
// the parser so nil slice/map entries can be exercised.
func newRawLowerer(doc *soa.OpenAPI) *lowerer {
	return lowererOver(service.New(0, &load.LoadedOpenAPIDocument{Doc: doc}, service.Policy{}))
}

// lowerServiceSpec loads src and runs the phases the service walk needs beneath
// it — component schemas, then security schemes — before the walk itself, and
// the link resolution after it. It drives LowerService directly rather than the
// compiler, which is what lets a test in this package hold an unexported
// lowering to what the walk did with it.
//
// It returns the service alone: nothing here asks what the compiler would
// have built around it.
func lowerServiceSpec(t *testing.T, src string) (ir.Service, []ir.Diagnostic) {
	t.Helper()
	lowerer, lowered, reg, loadDiags := lowerBeforeLinks(t, src)
	compiledDocumentView := compileddoc.New(&ir.Document{Types: reg, Services: []ir.Service{lowered.Service}})
	lowerer.diags.AppendAll(lowered.Links.Resolve(lowerer.ctx, compiledDocumentView, servers.Reopen(lowered.Servers)))
	return lowered.Service, append(loadDiags, lowerer.diags.List()...)
}

// lowerBeforeLinks runs what lowerServiceSpec does short of resolving the
// links, which it returns pending in Lowered beside the finished registry. The
// diagnostics it returns are load's, and lowererValue holds the rest.
func lowerBeforeLinks(t *testing.T, src string) (*lowerer, Lowered, ir.TypeRegistry, []ir.Diagnostic) {
	t.Helper()
	lowererValue, loadDiags := loweredFor(t, src)
	lowererValue.diags.AppendAll(schema.LowerComponentSchemas(t.Context(), lowererValue.ctx.Ctx, lowererValue.types, componentStatements(lowererValue.ctx)))
	declared, authDiags := auth.LowerSecuritySchemes(lowererValue.ctx)
	lowererValue.diags.AppendAll(authDiags)

	lowered, svcDiags := LowerService(t.Context(), lowererValue.ctx, lowererValue.types, declared)
	lowererValue.diags.AppendAll(svcDiags)
	reg, finished := lowererValue.types.Finish(t.Context())
	lowererValue.diags.AppendAll(finished)
	return lowererValue, lowered, reg, loadDiags
}

// componentStatements yields each component schema of serviceContext's document under its
// name, as run hands them to schema.LowerComponentSchemas.
func componentStatements(serviceContext service.Ctx) iter.Seq2[string, *statement.Statement] {
	return func(yield func(string, *statement.Statement) bool) {
		for name, js := range serviceContext.Doc.Components.GetSchemas().All() {
			if !yield(name, serviceContext.Statements().Of(js)) {
				return
			}
		}
	}
}
