package schema

import (
	"encoding/json/jsontext"
	"testing"

	oas3 "github.com/speakeasy-api/openapi/jsonschema/oas3"
	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/annotation"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/ir"
)

// TestAttachDeclaredAnnotations_LeavesANodeStillBeingBuiltToItsBuilder pins
// that a revisit while the node at its coordinate is still being built reads
// nothing there and reports nothing; the frame building the node attaches the
// same declaration once its build returns (GitHub #749).
func TestAttachDeclaredAnnotations_LeavesANodeStillBeingBuiltToItsBuilder(t *testing.T) {
	t.Parallel()
	l, s, at := lowererDeclaringS(t)

	var midBuild []ir.Diagnostic
	id := internNode(l.ctx, l.types, at, "s", func(cm ir.TypeCommon) ir.TypeDef {
		require.NotPanics(t, func() { midBuild = attachDeclaredAnnotations(l.ctx, l.types, &l.anchors, s, at) })
		return &ir.Model{TypeCommon: cm}
	})
	assert.Empty(t, midBuild, "a revisit mid-build reports nothing")

	assert.Empty(t, attachDeclaredAnnotations(l.ctx, l.types, &l.anchors, s, at))
	td, ok := l.types.Node(id)
	require.True(t, ok)
	assert.Equal(t, "kept", td.Common().Docs.Description, "the builder's own pass attaches it")
}

// TestRecordDeclarationResidue_LeavesANodeStillBeingBuiltToItsBuilder is the
// same for the residue an own-node position records: nothing mid-build, and
// readOnly kept and reported once the build has returned (GitHub #749).
func TestRecordDeclarationResidue_LeavesANodeStillBeingBuiltToItsBuilder(t *testing.T) {
	t.Parallel()
	l, s, at := lowererDeclaringS(t)

	var midBuild []ir.Diagnostic
	id := internNode(l.ctx, l.types, at, "s", func(cm ir.TypeCommon) ir.TypeDef {
		require.NotPanics(t, func() { midBuild = recordDeclarationResidue(l.ctx, l.types, s, at, annotation.HomeOwnNode) })
		return &ir.Model{TypeCommon: cm}
	})
	assert.Empty(t, midBuild, "a revisit mid-build records and reports nothing")

	assert.NotEmpty(t, recordDeclarationResidue(l.ctx, l.types, s, at, annotation.HomeOwnNode),
		"the builder's own pass reports what it keeps")
	td, ok := l.types.Node(id)
	require.True(t, ok)
	assert.Contains(t, td.Common().Unmodeled, "openapi:readOnly", "and keeps it")
}

// TestAttachDeclaredAnnotations_MissingNode drives the invariant no source can
// break: an ID the registry never registered and is not building. That state is
// a compiler bug — and the annotations it would swallow are reported rather
// than lost.
func TestAttachDeclaredAnnotations_MissingNode(t *testing.T) {
	t.Parallel()
	l := newRawLowerer(&soa.OpenAPI{})
	// Reached through preserveUnionSiblings rather than attachDeclaredAnnotations:
	// the latter takes its node from the coordinate map, where a coordinate
	// without one is still being built and is left to its builder, so it cannot
	// be handed an ID the registry does not hold. This one takes an ID from its
	// caller.
	diags := preserveUnionSiblings(l.ctx, l.types, "t/anon/missing", &oas3.Schema{}, "/p", ir.ReasonDegradedLowering, "why")
	assertInternalInvariant(t, diags)
}

// lowererDeclaringS returns a lowerer over a component S that declares a
// description and readOnly, with S's schema and pointer, for driving a reader
// at S while its node is still being built and again once the build returns.
func lowererDeclaringS(t *testing.T) (*lowerer, *oas3.Schema, jsontext.Pointer) {
	t.Helper()
	l, diags := loweredFor(t, openapitest.ComponentSpec(
		"    S: {type: object, description: kept, readOnly: true}\n"))
	openapitest.RequireNoErrorDiags(t, diags)
	decl, ok := l.ctx.Doc.Components.Schemas.Get("S")
	require.True(t, ok)
	return l, decl.GetSchema(), "/components/schemas/S"
}
