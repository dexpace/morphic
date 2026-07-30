package openapi

import (
	"testing"

	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/ids"
	"github.com/dexpace/morphic/ir"
)

func TestInternalPointer(t *testing.T) {
	t.Parallel()
	l := &lowerer{ctx: lowerCtx{Source: ir.SourceInfo{Path: "m.yaml"}}}
	cases := []struct {
		ref    string
		want   string
		wantOK bool
	}{
		{"#/components/schemas/User", "/components/schemas/User", true},
		{"#/components/schemas/Foo/properties/bar", "/components/schemas/Foo/properties/bar", true},
		{"m.yaml#/components/schemas/Foo", "/components/schemas/Foo", true}, // same-file external
		{"other.yaml#/components/schemas/X", "", false},                     // genuine external
		{"Bare", "", false}, // bare name, no fragment
		{"", "", false},
		{"#", "", false}, // empty fragment
		// A fragment that is not a JSON pointer names a JSON Schema $anchor, not a
		// coordinate. Milestone 1 resolves no anchors, and returning the anchor name
		// as though it were a pointer derived IDs from a path no source coordinate
		// spells (GitHub #141).
		{"#addr", "", false},
		{"m.yaml#addr", "", false},
		{"#a/b", "", false}, // pointer-shaped only from the second segment on
	}
	for _, tc := range cases {
		got, ok := l.internalPointer(tc.ref)
		assert.Equal(t, tc.wantOK, ok, tc.ref)
		assert.Equal(t, tc.want, got, tc.ref)
	}
}

func TestResolveComponentRef(t *testing.T) {
	t.Parallel()
	l := &lowerer{ctx: lowerCtx{schemas: map[string]bool{"User": true}}}

	id, ok, handled := l.resolveComponentRef("/components/schemas/User")
	assert.True(t, handled)
	assert.True(t, ok)
	assert.Equal(t, ids.NamedType("/components/schemas/User"), id)

	_, ok, handled = l.resolveComponentRef("/components/schemas/Missing")
	assert.True(t, handled, "an undeclared component pointer is still classified as a component")
	assert.False(t, ok, "an undeclared component does not resolve")

	_, _, handled = l.resolveComponentRef("/components/schemas/Foo/properties/bar")
	assert.False(t, handled, "a sub-schema pointer is not a top-level component pointer")
}

// TestResolveComponentRef_NonCanonicalEscape pins that the resolved ID is built
// from the component's canonical name, so a $ref that escapes non-canonically
// (a raw '~' for a component named "A~B", interned under "A~0B") still resolves to
// the interned node rather than an unbacked ID (issue #14).
func TestResolveComponentRef_NonCanonicalEscape(t *testing.T) {
	t.Parallel()
	l := &lowerer{ctx: lowerCtx{schemas: map[string]bool{"A~B": true}}}

	id, ok, handled := l.resolveComponentRef("/components/schemas/A~B")
	assert.True(t, handled)
	assert.True(t, ok)
	assert.Equal(t, ids.NamedType("/components/schemas/A~0B"), id,
		"the ID is canonically re-escaped to match the interned node")
	assert.Equal(t, ids.NamedType(ids.Ptr("components", "schemas", "A~B")), id,
		"and equals the ID the component was interned under")
}

func TestSameFile(t *testing.T) {
	t.Parallel()
	l := &lowerer{ctx: lowerCtx{Source: ir.SourceInfo{Path: "dir/m.yaml"}}}
	assert.True(t, l.sameFile("dir/m.yaml"), "exact path")
	assert.True(t, l.sameFile("m.yaml"), "bare filename equal to our basename")
	assert.False(t, l.sameFile("other.yaml"))
	assert.False(t, l.sameFile("other/m.yaml"),
		"a doc part with its own directory is a distinct path, not a basename match")
	assert.False(t, (&lowerer{}).sameFile("m.yaml"), "empty source path never matches")
}

func TestInternedID_ByPointerHit(t *testing.T) {
	t.Parallel()
	l := newRawLowerer(&soa.OpenAPI{})
	l.types.Intern(deepPointer, "t/anon/prev", func() ir.TypeDef { return &ir.Any{} })

	id, ok := l.internedID(deepPointer)
	require.True(t, ok, "a pointer already recorded in byPointer resolves")
	assert.Equal(t, ir.TypeID("t/anon/prev"), id)
}

func TestInternedID_RegistryHit(t *testing.T) {
	t.Parallel()
	l := newRawLowerer(&soa.OpenAPI{})
	// A node lives at the pointer-derived ID without a byPointer entry: internedID
	// still finds it through the type registry.
	id := ids.AnonType(deepPointer)
	l.types.Register(id, &ir.Primitive{TypeCommon: ir.TypeCommon{ID: id}, Prim: ir.PrimString})

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
