// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package schema

import (
	"iter"
	"slices"
	"testing"

	"github.com/speakeasy-api/openapi/jsonschema/oas3"
	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/compile"
	"github.com/dexpace/morphic/compilers/openapi/internal/load"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/compilers/openapi/internal/service"
	"github.com/dexpace/morphic/compilers/openapi/internal/statement"
	"github.com/dexpace/morphic/ir"
)

// MaxDiscriminatorAncestorDepth is maxDiscriminatorAncestorDepth, for the
// external tests.
const MaxDiscriminatorAncestorDepth = maxDiscriminatorAncestorDepth

// MaxChainHops is maxChainHops, for the external tests.
const MaxChainHops = maxChainHops

// MaxAdmissionReads is maxAdmissionReads, for the external tests.
const MaxAdmissionReads = maxAdmissionReads

// MaxAdmissionMembers is maxAdmissionMembers, for the external tests.
const MaxAdmissionMembers = maxAdmissionMembers

// MaxRefinedConjuncts is maxRefinedConjuncts, for the external tests.
const MaxRefinedConjuncts = maxRefinedConjuncts

// Lowerer is test scaffolding, not a compiler type. The compiler has no such
// struct: every lowering is a function of the context and the registry, and
// the caller owns the document being built (#177). It is declared here, once,
// so the package's in-package and external tests drive one fixture.
type Lowerer struct {
	Ctx   lowering.Ctx
	Doc   *soa.OpenAPI
	Out   *ir.Document
	Types *compile.Types
	Diags compile.Diags
}

// LowererOver is the only place the fixture's fields are initialised. Both
// entry points below build on it, so a field added to Lowerer cannot reach one
// of them and miss the other.
func LowererOver(c service.Ctx) *Lowerer {
	types := compile.NewTypes()
	return &Lowerer{
		Ctx:   c.Ctx,
		Doc:   c.Doc,
		Out:   &ir.Document{},
		Types: types,
	}
}

// LoweredFor loads src and returns the fixture over it with nothing lowered yet,
// plus the load diagnostics, so a test can drive one entry point at a time.
func LoweredFor(t *testing.T, src string) (*Lowerer, []ir.Diagnostic) {
	t.Helper()
	return LoweredWith(t, src, lowering.Policy{})
}

// LoweredWith is LoweredFor under policy.
func LoweredWith(t *testing.T, src string, policy lowering.Policy) (*Lowerer, []ir.Diagnostic) {
	t.Helper()
	loadedDoc, diags, err := load.Load(t.Context(), 0, openapitest.SourceOf(src), load.Options{})
	require.NoError(t, err)
	require.NotNil(t, loadedDoc, "load returned no document: %+v", diags)
	return LowererOver(service.New(0, loadedDoc, service.Policy{Schema: policy})), diags
}

// LowerSpec loads src and lowers its component schemas, returning the document
// under construction and all diagnostics.
func LowerSpec(t *testing.T, src string) (*ir.Document, []ir.Diagnostic) {
	t.Helper()
	return LowerSpecWith(t, src, lowering.Policy{})
}

// LowerSpecWith is LowerSpec under policy.
func LowerSpecWith(t *testing.T, src string, policy lowering.Policy) (*ir.Document, []ir.Diagnostic) {
	t.Helper()
	l, diags := LoweredWith(t, src, policy)
	l.Diags.AppendAll(LowerComponentSchemas(t.Context(), l.Ctx, l.Types, l.Schemas()))
	doc := l.Document(t)
	return doc, append(diags, l.Diags.List()...)
}

// LowerSpecCountingLands lowers src as LowerSpec does, and returns too how
// many hop readings were made reading what a $ref lands on.
func LowerSpecCountingLands(t *testing.T, src string) (*ir.Document, []ir.Diagnostic, int) {
	t.Helper()
	l, diags := LoweredFor(t, src)
	l.Diags.AppendAll(LowerComponentSchemas(t.Context(), l.Ctx, l.Types, l.Schemas()))
	doc := l.Document(t)
	return doc, append(diags, l.Diags.List()...), *landingCount(l.Ctx)
}

// NewRawLowerer builds a fixture over a hand-constructed document, bypassing the
// parser so nil slice/map entries (which the parser panics on) can be exercised.
func NewRawLowerer(doc *soa.OpenAPI) *Lowerer {
	return LowererOver(service.New(0, &load.Document{Doc: doc}, service.Policy{}))
}

// Schemas returns the statements of the component schemas of the fixture's
// document, as run hands them to LowerComponentSchemas.
func (l *Lowerer) Schemas() iter.Seq2[string, *statement.Statement] {
	return func(yield func(string, *statement.Statement) bool) {
		for name, js := range l.Doc.Components.GetSchemas().All() {
			if !yield(name, l.Ctx.Statements().Of(js)) {
				return
			}
		}
	}
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

// Standalone returns the statement of js read alone, by a table of its own
// that knows no bindings and reads no rows: for a test handing a schema it
// decoded to a reader that takes a statement.
func Standalone(js *oas3.JSONSchema[oas3.Referenceable]) *statement.Statement {
	return statement.NewTable(statement.Source{}).Of(js)
}

// StandaloneSchema is Standalone for a schema object decoded alone, nil for a
// nil s.
func StandaloneSchema(s *oas3.Schema) *statement.Schema {
	if s == nil {
		return nil
	}
	return Standalone(oas3.NewJSONSchemaFromSchema[oas3.Referenceable](s)).Schema
}

// StatedIn returns the statement of s as l's table reads it, s being the
// schema a component of l's document declares: for a test holding the
// parser's schema object and handing a reader the run's statement of it. It
// panics on any other s, since a statement read alone has no position and no
// reference binding, so the reader would answer for another schema than the
// run's without the test saying so.
func StatedIn(l *Lowerer, s *oas3.Schema) *statement.Schema {
	for _, js := range l.Doc.Components.Schemas.All() {
		if js.GetSchema() == s {
			return l.Ctx.Statements().Of(js).Schema
		}
	}
	panic("schema: StatedIn of a schema no component of the fixture's document declares")
}

// LowerSource loads src and lowers its component schemas, as LowerSpec does,
// returning the document lowered and the component statements, false where
// src does not load.
func LowerSource(t *testing.T, src compilers.Source) (*ir.Document, []*statement.Statement, bool) {
	t.Helper()
	loaded, _, err := load.Load(t.Context(), 0, src, load.Options{})
	if err != nil || loaded == nil || loaded.Doc == nil {
		return nil, nil, false
	}
	l := LowererOver(service.New(0, loaded, service.Policy{}))
	l.Diags.AppendAll(LowerComponentSchemas(t.Context(), l.Ctx, l.Types, l.Schemas()))
	var statements []*statement.Statement
	for _, js := range l.Schemas() {
		statements = append(statements, js)
	}
	return l.Document(t), statements, true
}

// UnreadKeys is unreadKeys, the keys read and handled named.
func UnreadKeys(s *statement.Schema, read, handled []string) []string {
	return unreadKeys(s, keySetOf(read...), handled)
}

// CensusDomain returns censusDomain.
func CensusDomain() []string { return slices.Clone(censusDomain) }
