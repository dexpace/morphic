package openapi

import (
	"testing"

	oas3 "github.com/speakeasy-api/openapi/jsonschema/oas3"
	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
)

// deepPointer is a sub-schema pointer (not a top-level component pointer), so it
// exercises the interning-lookup paths rather than the named-component path.
const deepPointer = "/components/schemas/Obj/properties/inner"

func TestInternedID_ByPointerHit(t *testing.T) {
	t.Parallel()
	l := newRawLowerer(&soa.OpenAPI{})
	l.byPointer[deepPointer] = "t/anon/prev"

	id, ok := l.internedID(deepPointer)
	require.True(t, ok, "a pointer already recorded in byPointer resolves")
	assert.Equal(t, ir.TypeID("t/anon/prev"), id)
}

func TestInternedID_RegistryHit(t *testing.T) {
	t.Parallel()
	l := newRawLowerer(&soa.OpenAPI{})
	// A node lives at the pointer-derived ID without a byPointer entry: internedID
	// still finds it through the type registry.
	id := anonTypeID(deepPointer)
	l.out.Types[id] = &ir.Primitive{TypeCommon: ir.TypeCommon{ID: id}, Prim: ir.PrimString}

	got, ok := l.internedID(deepPointer)
	require.True(t, ok, "a node registered under its pointer-derived ID resolves")
	assert.Equal(t, id, got)
}

func TestInternedID_Miss(t *testing.T) {
	t.Parallel()
	l := newRawLowerer(&soa.OpenAPI{})
	_, ok := l.internedID(deepPointer)
	assert.False(t, ok, "an un-interned pointer does not resolve")
}

func TestResolveSchemaRef_ReusesInternedSubSchema(t *testing.T) {
	t.Parallel()
	l := newRawLowerer(&soa.OpenAPI{})
	l.byPointer[deepPointer] = "t/anon/prev"

	id, ok := l.resolveSchemaRef(emptyEitherSchema(), "#"+deepPointer)
	require.True(t, ok, "a $ref to an already-hoisted sub-schema reuses its ID")
	assert.Equal(t, ir.TypeID("t/anon/prev"), id)
}

func TestResolveSchemaRef_UnresolvedDeepRefDropped(t *testing.T) {
	t.Parallel()
	l := newRawLowerer(&soa.OpenAPI{})
	// A same-file $ref to a deep pointer the library never resolved: no interned
	// node, GetResolvedSchema is nil, so the reference is dropped (ok=false).
	js := oas3.NewJSONSchemaFromReference("#" + deepPointer)

	_, ok := l.resolveSchemaRef(js, "#"+deepPointer)
	assert.False(t, ok, "an unresolved deep sub-schema $ref is dropped, not synthesized")
}

func TestHoistSubSchema_NilSchema(t *testing.T) {
	t.Parallel()
	l := newRawLowerer(&soa.OpenAPI{})
	_, ok := l.hoistSubSchema(nil, deepPointer)
	assert.False(t, ok, "a nil resolved sub-schema cannot be hoisted")
}

func TestHoistSubSchema_BodyInternsAtPointer(t *testing.T) {
	t.Parallel()
	l := newRawLowerer(&soa.OpenAPI{})
	// An object body interns a node at the sub-schema's own pointer, so the
	// pointer-owns-a-node branch returns that node rather than aliasing it.
	object := &oas3.Schema{Type: oas3.NewTypeFromString(oas3.SchemaTypeObject)}

	id, ok := l.hoistSubSchema(object, deepPointer)
	require.True(t, ok)
	assert.Equal(t, anonTypeID(deepPointer), id)
	assert.Equal(t, anonTypeID(deepPointer), l.byPointer[deepPointer])
}

func strptr(s string) *string { return &s }

func TestDiscriminatorDefault_ResolvesDeclaredComponent(t *testing.T) {
	t.Parallel()
	l := newRawLowerer(&soa.OpenAPI{})
	l.schemas = map[string]bool{"Cat": true}
	d := &oas3.Discriminator{PropertyName: "kind", DefaultMapping: strptr("Cat")}

	id := l.discriminatorDefault(d, "/components/schemas/Pet")
	assert.Equal(t, namedTypeID("/components/schemas/Cat"), id)
	assert.Empty(t, l.diags, "a resolvable defaultMapping produces no diagnostic")
}

func TestDiscriminatorDefault_DroppedWhenUnresolved(t *testing.T) {
	t.Parallel()
	l := newRawLowerer(&soa.OpenAPI{})
	// "Missing" is neither a declared component nor an internal pointer, so the
	// defaultMapping does not resolve and is dropped with one error diagnostic.
	d := &oas3.Discriminator{PropertyName: "kind", DefaultMapping: strptr("Missing")}

	id := l.discriminatorDefault(d, "/components/schemas/Pet")
	assert.Empty(t, id, "an unresolved defaultMapping yields no target")
	require.Len(t, l.diags, 1)
	assert.Equal(t, codeUnresolvedRef, l.diags[0].Code)
}

func TestDiscriminatorDefault_EmptyIsNoOp(t *testing.T) {
	t.Parallel()
	l := newRawLowerer(&soa.OpenAPI{})
	id := l.discriminatorDefault(&oas3.Discriminator{PropertyName: "kind"}, "/components/schemas/Pet")
	assert.Empty(t, id)
	assert.Empty(t, l.diags)
}
