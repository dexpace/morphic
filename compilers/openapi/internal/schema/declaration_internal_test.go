// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package schema

import (
	"encoding/json/jsontext"
	"maps"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/annotation"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/compilers/openapi/internal/statement"
	"github.com/dexpace/morphic/ir"
)

// TestDeclarationOn_WritesTheDeclarationOnTheNodeInHand pins the finishing of
// a position's node: its annotations wherever it stands, and what binds a use
// only where the node is its home, a carrier keeping that itself.
func TestDeclarationOn_WritesTheDeclarationOnTheNodeInHand(t *testing.T) {
	t.Parallel()
	l, s, at := lowererDeclaringS(t)
	target := ir.TypeRef{Target: l.Ctx.IDs().ForPointer(at)}

	carried := &ir.Model{ID: target.Target}
	assert.Empty(t, declarationOn(l.Ctx, l.Types, s, target, at, annotation.HomeCarrier, carried))
	assert.Equal(t, "kept", carried.Docs.Description, "a carrier's node takes the annotations")
	assert.NotContains(t, carried.Unmodeled, "openapi:readOnly", "and no residue, which the carrier holds")

	owned := &ir.Model{ID: target.Target}
	assert.NotEmpty(t, declarationOn(l.Ctx, l.Types, s, target, at, annotation.HomeOwnNode, owned),
		"what it keeps is reported")
	assert.Equal(t, "kept", owned.Docs.Description)
	assert.Contains(t, owned.Unmodeled, "openapi:readOnly", "the node it is home to keeps the residue")

	none := &ir.Model{ID: target.Target}
	assert.Empty(t, declarationOn(l.Ctx, l.Types, &statement.Schema{Truth: statement.Anything}, target, at,
		annotation.HomeOwnNode, none))
	assert.Equal(t, &ir.Model{ID: target.Target}, none, "a schema that is no object writes nothing")
}

// TestHomeDeclaration_HoistsAHomeFinishedWithTheDeclaration pins the alias a
// declaration needing a node of its own takes over a target it does not own:
// built once, the declaration written on it in its build, then the outer
// frame's finisher. A $ref site, its one caller lowering a reference, writes
// nothing beside it to need one, so no source reaches this.
func TestHomeDeclaration_HoistsAHomeFinishedWithTheDeclaration(t *testing.T) {
	t.Parallel()
	l, s, at := lowererDeclaringS(t)
	var outer []ir.TypeDef
	ref, diags := homeDeclaration(l.Ctx, l.Types, s, l.Types.PrimRef(ir.PrimString), at, annotation.HomeOwnNode, false,
		func(td ir.TypeDef) { outer = append(outer, td) })

	assert.Equal(t, l.Ctx.IDs().ForPointer(at), ref.Target, "the position resolves to its own alias")
	assert.NotEmpty(t, diags, "what the alias keeps is reported")
	td, ok := l.Types.Node(ref.Target)
	require.True(t, ok)
	assert.Equal(t, "kept", td.Common().Docs.Description, "the declaration is written on the alias")
	assert.Contains(t, td.Common().Unmodeled, "openapi:readOnly")
	assert.Equal(t, []ir.TypeDef{td}, outer, "and the outer finisher runs on it once")
}

// TestSchemaBody_ALaterVisitWritesNothing pins that only the visit building a
// position's node finishes it: a second lowering of the position resolves to
// the node the first built and keeps nothing on it a second time.
func TestSchemaBody_ALaterVisitWritesNothing(t *testing.T) {
	t.Parallel()
	l, s, at := lowererDeclaringS(t)
	js := l.Ctx.Statements().Read(s)
	first, firstDiags := Ref(l.Ctx, l.Types, TopLevelDepth, js, at)
	require.NotEmpty(t, firstDiags, "the first visit keeps readOnly and reports it")
	td, ok := l.Types.Node(first.Target)
	require.True(t, ok, "the first visit built S")
	before := maps.Clone(td.Common().Unmodeled)

	ref, diags := Ref(l.Ctx, l.Types, TopLevelDepth, js, at)

	assert.Equal(t, first.Target, ref.Target, "the position resolves to the node the first visit built")
	assert.Empty(t, diags, "and reports nothing again")
	assert.Equal(t, before, td.Common().Unmodeled, "nor keeps anything a second time")
}

// lowererDeclaringS returns a lowerer over a component S that declares a
// description and readOnly, with S's schema as the run reads it and its
// pointer, for driving a reader
// at S while its node is still being built and again once the build returns.
func lowererDeclaringS(t *testing.T) (*Lowerer, *statement.Schema, jsontext.Pointer) {
	t.Helper()
	l, diags := LoweredFor(t, openapitest.ComponentSpec(
		"    S: {type: object, description: kept, readOnly: true}\n"))
	openapitest.RequireNoErrorDiags(t, diags)
	decl, ok := l.Doc.Components.Schemas.Get("S")
	require.True(t, ok)
	return l, l.Ctx.Statements().Of(decl).Schema, "/components/schemas/S"
}
