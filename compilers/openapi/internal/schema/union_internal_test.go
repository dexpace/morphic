package schema

import (
	"testing"

	oas3 "github.com/speakeasy-api/openapi/jsonschema/oas3"
	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/stretchr/testify/assert"

	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/ir"
)

func TestIsNullSchema_EmptyEitherFalse(t *testing.T) {
	t.Parallel()
	assert.False(t, isNullSchema(openapitest.EmptyEitherSchema()), "empty either is not a null schema")
}

// TestAllNullUnion_NoBranchesFalse covers the guard's own contract rather than
// its one caller: lowerOneOfAnyOf never reaches allNullUnion without a
// non-empty oneOf or anyOf, so a schema declaring neither is a case only a
// direct call can drive.
func TestAllNullUnion_NoBranchesFalse(t *testing.T) {
	t.Parallel()
	assert.False(t, allNullUnion(&oas3.Schema{}), "no oneOf/anyOf branches admits nothing to be null-only")
}

func TestPreserveUnionSiblings_MissingNode(t *testing.T) {
	t.Parallel()
	l := NewRawLowerer(&soa.OpenAPI{})
	// No node registered under the id: the union branches have nowhere to go, so
	// the guard reports the broken invariant instead of dropping them quietly.
	diags := preserveUnionSiblings(l.Ctx, l.Types, "t/anon/missing", &oas3.Schema{}, "/p", ir.ReasonDegradedLowering, "why")
	assertInternalInvariant(t, diags)
}

// TestPreserveNullOnlyUnion_MissingNode is preserveNullOnlyUnion's half of
// TestPreserveUnionSiblings_MissingNode: no node registered under id means the
// null-only oneOf/anyOf has nowhere to attach, so the guard reports the broken
// invariant instead of dropping it quietly.
func TestPreserveNullOnlyUnion_MissingNode(t *testing.T) {
	t.Parallel()
	l := NewRawLowerer(&soa.OpenAPI{})
	diags := preserveNullOnlyUnion(l.Ctx, l.Types, "t/anon/missing", &oas3.Schema{}, "/p")
	assertInternalInvariant(t, diags)
}

// TestPreserveNullOnlyUnion_NothingToKeep covers a registered node whose
// schema declares neither oneOf nor anyOf: preserveBranchSets keeps nothing,
// so nothing announces a keeping that never happened. lowerNullOnlyUnion never
// reaches this — its caller always has one of the two — so only a direct call
// drives it, the same way TestAllNullUnion_NoBranchesFalse drives its sibling
// guard.
func TestPreserveNullOnlyUnion_NothingToKeep(t *testing.T) {
	t.Parallel()
	l := NewRawLowerer(&soa.OpenAPI{})
	id := internNode(l.Ctx, l.Types, "/components/schemas/S", func(cm ir.TypeCommon) ir.TypeDef {
		return &ir.Scalar{TypeCommon: cm}
	})
	diags := preserveNullOnlyUnion(l.Ctx, l.Types, id, &oas3.Schema{}, "/components/schemas/S")
	assert.Empty(t, diags, "nothing was kept, so nothing is announced")
}
