// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package schema

import (
	"fmt"
	"strings"
	"testing"

	"github.com/speakeasy-api/openapi/jsonschema/oas3"
	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/bind"
	"github.com/dexpace/morphic/compilers/openapi/internal/bind/typed"
	"github.com/dexpace/morphic/compilers/openapi/internal/ids"
	"github.com/dexpace/morphic/compilers/openapi/internal/load"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/compilers/openapi/internal/overlay"
	"github.com/dexpace/morphic/ir"
)

// TestRefTypeRef_SaysWhyAnUnboundRefIsUnresolved pins the reasons refTypeRef
// gives for a $ref the bindings hold no answer of their own for: none where
// there are no bindings, why the table never read one past the merge bound,
// none for one landing where the bindings hold no schema, as when building one
// failed, which the load phase reports, and why the table could not place what
// a reference object resolved to.
func TestRefTypeRef_SaysWhyAnUnboundRefIsUnresolved(t *testing.T) {
	t.Parallel()
	message := func(t *testing.T, l *Lowerer, js *oas3.JSONSchema[oas3.Referenceable]) string {
		t.Helper()
		ref, diags := refTypeRef(l.Ctx, l.Types, js, "/components/schemas/A")
		assert.Equal(t, ir.TypeID("t/prim/any"), ref.Target)
		require.Len(t, diags, 1)
		return diags[0].Message
	}

	bare := NewRawLowerer(&soa.OpenAPI{})
	assert.Equal(t, `unresolved $ref "#/x"`, message(t, bare, oas3.NewJSONSchemaFromReference("#/x")))

	var deep strings.Builder
	deep.WriteString("openapi: 3.1.0\ninfo: {title: t, version: '1'}\npaths: {}\nx-c0: &c0 {$ref: '#/components/schemas/T'}\n")
	for i := 1; i <= 70; i++ {
		fmt.Fprintf(&deep, "x-c%d: &c%d {<<: *c%d}\n", i, i, i-1)
	}
	deep.WriteString("components:\n  schemas:\n    T: {type: string}\n    A: {<<: *c70}\n")
	past, _ := LoweredFor(t, deep.String())
	a, ok := past.Ctx.Doc.Components.GetSchemas().Get("A")
	require.True(t, ok)
	assert.Equal(t, `unresolved $ref "#/components/schemas/T": it is past a `+"`<<`"+` merge chain deeper than `+
		"the 64 levels the compiler expands", message(t, past, a))

	unheld := unheldLowerer(t)
	a, ok = unheld.Ctx.Doc.Components.GetSchemas().Get("A")
	require.True(t, ok)
	assert.Equal(t, `unresolved $ref "#/components/schemas/H/properties/p"`, message(t, unheld, a))

	bd := typed.NewBuilder(bind.Bind(t.Context(), unheld.Ctx.Doc.GetRootNode(), bind.Config{Self: "spec.yaml"}))
	bd.Unplaced(a, "/paths/~1a/get/parameters/0")
	unplaced := LowererOver(lowering.New(0, unheld.Ctx.Doc, ir.SourceInfo{}, "", lowering.Limits{},
		lowering.StreamingMedia{}, lowering.ExtensionPromotions{}, lowering.NullableWidening{}, overlay.Origin{}).WithLoaded(
		&load.Document{Doc: unheld.Ctx.Doc, Bindings: bd.Bindings()}))
	assert.Equal(t, `unresolved $ref "#/components/schemas/H/properties/p": it is in what the reference at `+
		"/paths/~1a/get/parameters/0 resolved to, which the compiler cannot read where the parser placed it",
		message(t, unplaced, a))
}

// TestResolvePointer_PromisesWhatItNames pins that a $ref never builds what it
// names: it takes the ID the target's pointer derives and promises the node,
// which Finish builds only where no lowering interned one, and a target it
// cannot hoist resolves to nothing.
func TestResolvePointer_PromisesWhatItNames(t *testing.T) {
	t.Parallel()
	l := NewRawLowerer(&soa.OpenAPI{})
	object := oas3.NewJSONSchemaFromSchema[oas3.Referenceable](
		&oas3.Schema{Type: oas3.NewTypeFromString(oas3.SchemaTypeObject)})

	id, ok := resolvePointer(l.Ctx, l.Types, deepPointer, object)
	require.True(t, ok)
	assert.Equal(t, ids.Document{}.AnonType(deepPointer), id)
	_, built := l.Types.Lookup(deepPointer)
	assert.False(t, built, "the reference builds nothing itself")

	_, finished := l.Types.Finish(t.Context())
	assert.Empty(t, finished, "an object body hoists cleanly, and nothing is refused")
	seeded, built := l.Types.Lookup(deepPointer)
	require.True(t, built, "Finish builds what no lowering interned")
	assert.Equal(t, id, seeded)

	_, ok = resolvePointer(l.Ctx, l.Types, deepPointer, nil)
	assert.False(t, ok, "a target the bindings hold no schema at cannot be hoisted")
}

func TestResolveSchemaRef_UnboundDeepRefDropped(t *testing.T) {
	t.Parallel()
	l := NewRawLowerer(&soa.OpenAPI{})
	// A same-file $ref to a deep pointer no binding read: no interned node and
	// nothing the bindings know it lands on, so the reference is dropped.
	js := oas3.NewJSONSchemaFromReference("#" + deepPointer)

	_, ok := resolveSchemaRef(l.Ctx, l.Types, js)
	assert.False(t, ok, "an unresolved deep sub-schema $ref is dropped, not synthesized")
}

func TestHoistSubSchema_BodyInternsAtPointer(t *testing.T) {
	t.Parallel()
	l := NewRawLowerer(&soa.OpenAPI{})
	// An object body interns a node at the sub-schema's own pointer, so the
	// pointer-owns-a-node branch keeps that node rather than aliasing it.
	object := oas3.NewJSONSchemaFromSchema[oas3.Referenceable](
		&oas3.Schema{Type: oas3.NewTypeFromString(oas3.SchemaTypeObject)})

	assert.Empty(t, hoistSubSchema(l.Ctx, l.Types, object, deepPointer), "an object body hoists cleanly")
	seeded, _ := l.Types.Lookup(deepPointer)
	assert.Equal(t, ids.Document{}.AnonType(deepPointer), seeded)
	td, ok := l.Types.NodeAt(deepPointer)
	require.True(t, ok)
	assert.Equal(t, ir.KindModel, td.Kind(), "the body's own node, not an alias over it")
}

// unheldLowerer is a lowerer over bindings that know where A's $ref lands but
// hold no schema there: only A was recorded.
func unheldLowerer(t *testing.T) *Lowerer {
	t.Helper()
	const spec = "openapi: 3.1.0\ninfo: {title: t, version: '1'}\npaths: {}\ncomponents:\n  schemas:\n" +
		"    A: {$ref: '#/components/schemas/H/properties/p'}\n    H: {properties: {p: {type: object}}}\n"
	doc, _, err := soa.Unmarshal(t.Context(), strings.NewReader(spec))
	require.NoError(t, err)
	bd := typed.NewBuilder(bind.Bind(t.Context(), doc.GetRootNode(), bind.Config{Self: "spec.yaml"}))
	a, ok := doc.Components.GetSchemas().Get("A")
	require.True(t, ok)
	bd.Schema(a, bind.Position{Pointer: "/components/schemas/A"})
	ctx := lowering.New(0, doc, ir.SourceInfo{}, "", lowering.Limits{}, lowering.StreamingMedia{},
		lowering.ExtensionPromotions{}, lowering.NullableWidening{}, overlay.Origin{}).WithLoaded(&load.Document{Doc: doc, Bindings: bd.Bindings()})
	return LowererOver(ctx)
}
