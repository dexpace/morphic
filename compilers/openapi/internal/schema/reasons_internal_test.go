package schema

import (
	"fmt"
	"strings"
	"testing"

	oas3 "github.com/speakeasy-api/openapi/jsonschema/oas3"
	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/bind"
	"github.com/dexpace/morphic/compilers/openapi/internal/bind/typed"
	"github.com/dexpace/morphic/compilers/openapi/internal/load"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/compilers/openapi/internal/overlay"
	"github.com/dexpace/morphic/ir"
)

// TestRefTypeRef_SaysWhyAnUnboundRefIsUnresolved pins the reasons refTypeRef
// gives for a $ref the bindings hold no answer of their own for: none where
// there are no bindings, why the table never read one past the merge bound,
// and none for one landing where the bindings hold no schema, as when building
// one failed, which the load phase reports.
func TestRefTypeRef_SaysWhyAnUnboundRefIsUnresolved(t *testing.T) {
	t.Parallel()
	message := func(t *testing.T, l *lowerer, js *oas3.JSONSchema[oas3.Referenceable]) string {
		t.Helper()
		ref, diags := refTypeRef(l.ctx, l.types, &l.anchors, js, "/components/schemas/A")
		assert.Equal(t, ir.TypeID("t/prim/any"), ref.Target)
		require.Len(t, diags, 1)
		return diags[0].Message
	}

	bare := newRawLowerer(&soa.OpenAPI{})
	assert.Equal(t, `unresolved $ref "#/x"`, message(t, bare, oas3.NewJSONSchemaFromReference("#/x")))

	var deep strings.Builder
	deep.WriteString("openapi: 3.1.0\ninfo: {title: t, version: '1'}\npaths: {}\nx-c0: &c0 {$ref: '#/components/schemas/T'}\n")
	for i := 1; i <= 70; i++ {
		fmt.Fprintf(&deep, "x-c%d: &c%d {<<: *c%d}\n", i, i, i-1)
	}
	deep.WriteString("components:\n  schemas:\n    T: {type: string}\n    A: {<<: *c70}\n")
	past, _ := loweredFor(t, deep.String())
	a, ok := past.ctx.Doc.Components.GetSchemas().Get("A")
	require.True(t, ok)
	assert.Equal(t, `unresolved $ref "#/components/schemas/T": it is past a `+"`<<`"+` merge chain deeper than `+
		"the 64 levels the compiler expands", message(t, past, a))

	unheld := unheldLowerer(t)
	a, ok = unheld.ctx.Doc.Components.GetSchemas().Get("A")
	require.True(t, ok)
	assert.Equal(t, `unresolved $ref "#/components/schemas/H/properties/p"`, message(t, unheld, a))

	bd := typed.NewBuilder(bind.Bind(unheld.ctx.Doc.GetRootNode(), bind.Config{Self: "spec.yaml"}))
	bd.Unplaced(a, "/paths/~1a/get/parameters/0")
	unplaced := lowererOver(lowering.New(0, unheld.ctx.Doc, ir.SourceInfo{}, "", lowering.Limits{},
		lowering.StreamingMedia{}, lowering.ExtensionPromotions{}, lowering.NullableWidening{}, overlay.Origin{}).WithLoaded(
		&load.Document{Doc: unheld.ctx.Doc, Bindings: bd.Bindings()}))
	assert.Equal(t, `unresolved $ref "#/components/schemas/H/properties/p": it is in what the reference at `+
		"/paths/~1a/get/parameters/0 resolved to, which the compiler cannot read where the parser placed it",
		message(t, unplaced, a))
}

// TestMappingWhy_GivesAComponentNameNoReasonOfTheTable pins that a mapping
// value naming a component, which the table never reads, takes none of the
// table's reasons: the merge bound cut a read elsewhere in the document, not
// the lookup of the name.
func TestMappingWhy_GivesAComponentNameNoReasonOfTheTable(t *testing.T) {
	t.Parallel()
	var deep strings.Builder
	deep.WriteString("openapi: 3.1.0\ninfo: {title: t, version: '1'}\npaths: {}\nx-c0: &c0 {$ref: '#/components/schemas/T'}\n")
	for i := 1; i <= 70; i++ {
		fmt.Fprintf(&deep, "x-c%d: &c%d {<<: *c%d}\n", i, i, i-1)
	}
	deep.WriteString("components:\n  schemas:\n    T: {type: string}\n    A: {<<: *c70}\n" +
		"    P: {oneOf: [{$ref: '#/components/schemas/T'}], discriminator: {propertyName: k, mapping: {u: Undeclared}}}\n")
	l, _ := loweredFor(t, deep.String())
	require.True(t, l.ctx.Bindings().Table().Truncated(), "the merge bound cut a read")
	p, ok := l.ctx.Doc.Components.GetSchemas().Get("P")
	require.True(t, ok)
	d := p.GetSchema().GetDiscriminator()
	require.NotNil(t, d)
	assert.Empty(t, mappingWhy(l.ctx, d, "Undeclared"))
}

// TestMappingWhy_GivesAnUnreadReferenceTheTablesReason pins that a mapping
// value spelling a reference, which the table reads, takes the table's reason
// where it went unread: P's discriminator sits past a merge chain deeper than
// the compiler expands, so the binder never read its mapping.
func TestMappingWhy_GivesAnUnreadReferenceTheTablesReason(t *testing.T) {
	t.Parallel()
	var deep strings.Builder
	deep.WriteString("openapi: 3.1.0\ninfo: {title: t, version: '1'}\npaths: {}\n" +
		"x-c0: &c0 {oneOf: [{$ref: '#/components/schemas/T'}], " +
		"discriminator: {propertyName: k, mapping: {t: '#/components/schemas/T'}}}\n")
	for i := 1; i <= 70; i++ {
		fmt.Fprintf(&deep, "x-c%d: &c%d {<<: *c%d}\n", i, i, i-1)
	}
	deep.WriteString("components:\n  schemas:\n    T: {properties: {k: {type: string}}}\n    P: {<<: *c70}\n")
	l, _ := loweredFor(t, deep.String())
	p, ok := l.ctx.Doc.Components.GetSchemas().Get("P")
	require.True(t, ok)
	d := p.GetSchema().GetDiscriminator()
	require.NotNil(t, d, "the parser expands the whole chain")
	assert.Equal(t, ": it is past a `<<` merge chain deeper than the 64 levels the compiler expands",
		mappingWhy(l.ctx, d, "#/components/schemas/T"))
}

// unheldLowerer is a lowerer over bindings that know where A's $ref lands but
// hold no schema there: only A was recorded.
func unheldLowerer(t *testing.T) *lowerer {
	t.Helper()
	const spec = "openapi: 3.1.0\ninfo: {title: t, version: '1'}\npaths: {}\ncomponents:\n  schemas:\n" +
		"    A: {$ref: '#/components/schemas/H/properties/p'}\n    H: {properties: {p: {type: object}}}\n"
	doc, _, err := soa.Unmarshal(t.Context(), strings.NewReader(spec))
	require.NoError(t, err)
	bd := typed.NewBuilder(bind.Bind(doc.GetRootNode(), bind.Config{Self: "spec.yaml"}))
	a, ok := doc.Components.GetSchemas().Get("A")
	require.True(t, ok)
	bd.Schema(a, bind.Position{Pointer: "/components/schemas/A"})
	ctx := lowering.New(0, doc, ir.SourceInfo{}, "", lowering.Limits{}, lowering.StreamingMedia{},
		lowering.ExtensionPromotions{}, lowering.NullableWidening{}, overlay.Origin{}).WithLoaded(&load.Document{Doc: doc, Bindings: bd.Bindings()})
	return lowererOver(ctx)
}
