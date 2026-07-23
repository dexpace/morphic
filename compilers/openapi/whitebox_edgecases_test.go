package openapi

import (
	"testing"

	oas3 "github.com/speakeasy-api/openapi/jsonschema/oas3"
	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/speakeasy-api/openapi/sequencedmap"
	"github.com/speakeasy-api/openapi/values"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
)

// emptyEitherSchema is a JSONSchema whose either-value has neither a Left schema
// nor a Right bool set: IsSchema() is true (IsLeft defaults true) yet GetSchema()
// is nil. The parser never produces this, so it drives the nil-schema guards.
func emptyEitherSchema() *oas3.JSONSchema[oas3.Referenceable] {
	return oas3.NewJSONSchemaFromSchema[oas3.Referenceable](nil)
}

func TestSchemaRef_EmptyEitherIsAny(t *testing.T) {
	t.Parallel()
	l := newRawLowerer(&soa.OpenAPI{})
	ref := l.schemaRef(emptyEitherSchema(), "/p", "h")
	assert.Equal(t, ir.TypeID("t/prim/any"), ref.Target)
}

func TestFillParamSchema_EmptyEitherNoOp(t *testing.T) {
	t.Parallel()
	l := newRawLowerer(&soa.OpenAPI{})
	param := &ir.Parameter{}
	l.fillParamSchema(param, emptyEitherSchema(), "/p")
	assert.Nil(t, param.Constraints)
	assert.Nil(t, param.Default)
}

func TestIsNullSchema_EmptyEitherFalse(t *testing.T) {
	t.Parallel()
	assert.False(t, isNullSchema(emptyEitherSchema()), "empty either is not a null schema")
}

func TestApplyExclusive_NumericWithoutRootNode(t *testing.T) {
	t.Parallel()
	f := 5.0
	s := &oas3.Schema{ExclusiveMinimum: &values.EitherValue[bool, bool, float64, float64]{Right: &f}}
	c := &ir.Constraints{}
	diags := applyExclusive(c, s, true, false)
	// The numeric arm is taken (2020-12 dialect, numeric value) but there is no raw
	// node to read the exact literal from, so nothing is set and no diagnostic.
	assert.Nil(t, diags)
	assert.False(t, c.ExclusiveMin)
}

func TestFillSequential_ItemEncodingWithoutRootNode(t *testing.T) {
	t.Parallel()
	l := newRawLowerer(&soa.OpenAPI{})
	c := &ir.Content{}
	media := &soa.MediaType{ItemEncoding: &soa.Encoding{}}
	l.fillSequential(c, media, "/mp", "h")
	assert.Nil(t, c.Extensions, "itemEncoding with no raw node is dropped")
	assert.Empty(t, l.diags)
}

func TestBodySchemaPointer_ExternalRefNoFragment(t *testing.T) {
	t.Parallel()
	js := oas3.NewJSONSchemaFromReference("external.yaml")
	assert.Equal(t, "/local", bodySchemaPointer(js, "/local"), "a fragmentless ref falls back to the local pointer")
}

func TestLowerTagDefs_NilEntrySkipped(t *testing.T) {
	t.Parallel()
	l := newRawLowerer(&soa.OpenAPI{Tags: []*soa.Tag{nil, {}}})
	l.lowerTagDefs()
	assert.Len(t, l.out.TagDefs, 1, "nil tag entry skipped")
}

func TestApplyPathServers_WithoutRootNode(t *testing.T) {
	t.Parallel()
	l := newRawLowerer(&soa.OpenAPI{})
	op := &ir.Operation{}
	l.applyPathServers(op, &soa.PathItem{Servers: []*soa.Server{{URL: "https://x"}}})
	assert.Nil(t, op.Extensions, "servers with no raw node are not preserved")
	assert.Empty(t, l.diags)
}

func TestPreserveErrorHeaders_WithoutRootNode(t *testing.T) {
	t.Parallel()
	l := newRawLowerer(&soa.OpenAPI{})
	headers := sequencedmap.New(
		sequencedmap.NewElem("X-H", &soa.ReferencedHeader{}),
	)
	ec := &ir.ErrorCase{}
	l.preserveErrorHeaders(ec, &soa.Response{Headers: headers}, "/r")
	assert.Nil(t, ec.Extensions, "headers with no raw node are not preserved")
	require.Empty(t, l.diags)
}

func TestIntern_IdempotentOnSamePointer(t *testing.T) {
	t.Parallel()
	l := newRawLowerer(&soa.OpenAPI{})
	calls := 0
	build := func() ir.TypeDef {
		calls++
		return &ir.Primitive{TypeCommon: ir.TypeCommon{ID: "x"}, Prim: ir.PrimString}
	}
	first := l.intern("/p", "x", build)
	second := l.intern("/p", "x", build)
	assert.Equal(t, first, second)
	assert.Equal(t, 1, calls, "build runs only on first intern of a pointer")
}

func TestIsRefBranch_Nil(t *testing.T) {
	t.Parallel()
	assert.False(t, isRefBranch(nil))
}

func TestBodySchemaPointer_NilSchema(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "/local", bodySchemaPointer(nil, "/local"))
}

func TestPreserveUnionSiblings_MissingNode(t *testing.T) {
	t.Parallel()
	l := newRawLowerer(&soa.OpenAPI{})
	// No node registered under the id → the guard returns without panicking.
	l.preserveUnionSiblings("t/anon/missing", &oas3.Schema{}, "/p")
	assert.Empty(t, l.diags)
}

func TestLowerResponses_NoResponses(t *testing.T) {
	t.Parallel()
	l := newRawLowerer(&soa.OpenAPI{})
	responses, errs := l.lowerResponses(&soa.Operation{}, "/op")
	assert.Nil(t, responses)
	assert.Nil(t, errs)
}

func TestLowerPayload_NilMediaEntriesYieldNil(t *testing.T) {
	t.Parallel()
	l := newRawLowerer(&soa.OpenAPI{})
	content := sequencedmap.New(
		sequencedmap.NewElem("application/json", (*soa.MediaType)(nil)),
	)
	assert.Nil(t, l.lowerPayload(content, "/p", "hint"), "all-nil media map yields no payload")
}

// The redeclaration-conflict helpers carry defensive guards for states a
// well-formed lowered document never reaches: a reference into no interned type,
// a base-less opaque scalar, a cyclic base chain, and an unparseable numeric
// literal. These exercise those guards directly.

func TestResolvePrimKind_DanglingTargetIsNotResolved(t *testing.T) {
	t.Parallel()
	l := newRawLowerer(&soa.OpenAPI{})
	_, ok := l.resolvePrimKind(ir.TypeRef{Target: "t/missing"})
	assert.False(t, ok, "a target absent from the registry resolves to no primitive")
}

func TestResolvePrimKind_BaselessScalarIsNotResolved(t *testing.T) {
	t.Parallel()
	l := newRawLowerer(&soa.OpenAPI{})
	l.out.Types["t/opaque"] = &ir.Scalar{TypeCommon: ir.TypeCommon{ID: "t/opaque"}}
	_, ok := l.resolvePrimKind(ir.TypeRef{Target: "t/opaque"})
	assert.False(t, ok, "a base-less opaque scalar has no underlying primitive")
}

func TestResolvePrimKind_CyclicBaseChainTerminates(t *testing.T) {
	t.Parallel()
	l := newRawLowerer(&soa.OpenAPI{})
	// A scalar whose Base points back at itself: the bounded walk must terminate
	// and report no primitive rather than spin.
	self := ir.TypeRef{Target: "t/cycle"}
	l.out.Types["t/cycle"] = &ir.Scalar{TypeCommon: ir.TypeCommon{ID: "t/cycle"}, Base: &self}
	_, ok := l.resolvePrimKind(self)
	assert.False(t, ok, "a cyclic base chain terminates without resolving")
}

func TestIsAnyType_DanglingTargetIsNotAny(t *testing.T) {
	t.Parallel()
	l := newRawLowerer(&soa.OpenAPI{})
	assert.False(t, l.isAnyType(ir.TypeRef{Target: "t/missing"}),
		"an unresolvable target is not classified as the top type")
}

func TestDifferentTypeKind_UnresolvableTargetIsNotAConflict(t *testing.T) {
	t.Parallel()
	l := newRawLowerer(&soa.OpenAPI{})
	l.out.Types["t/model"] = &ir.Model{TypeCommon: ir.TypeCommon{ID: "t/model"}}
	assert.False(t,
		l.differentTypeKind(ir.TypeRef{Target: "t/model"}, ir.TypeRef{Target: "t/missing"}),
		"an unresolvable target is not treated as a differing kind")
}

func TestBigValEqual_UnparseableFallsBackToStringEquality(t *testing.T) {
	t.Parallel()
	assert.True(t, bigValEqual(ir.BigVal("not-a-number"), ir.BigVal("not-a-number")),
		"unparseable operands compare by exact string")
	assert.False(t, bigValEqual(ir.BigVal("not-a-number"), ir.BigVal("other")))
}
