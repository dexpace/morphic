package openapi

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
)

func TestPtr_EscapesPerRFC6901(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name     string
		segments []string
		want     string
	}{
		{"plain", []string{"components", "schemas", "User"}, "/components/schemas/User"},
		{"slash in segment", []string{"paths", "/users/{id}", "get"}, "/paths/~1users~1{id}/get"},
		{"tilde in segment", []string{"components", "schemas", "a~b"}, "/components/schemas/a~0b"},
		{"tilde-slash order", []string{"x", "~/"}, "/x/~0~1"},
		{"empty", nil, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, ptr(tc.segments...))
		})
	}
}

func TestInternalPointer(t *testing.T) {
	t.Parallel()
	l := &lowerer{source: ir.SourceInfo{Path: "m.yaml"}}
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
	}
	for _, tc := range cases {
		got, ok := l.internalPointer(tc.ref)
		assert.Equal(t, tc.wantOK, ok, tc.ref)
		assert.Equal(t, tc.want, got, tc.ref)
	}
}

func TestResolveComponentRef(t *testing.T) {
	t.Parallel()
	l := &lowerer{schemas: map[string]bool{"User": true}}

	id, ok, handled := l.resolveComponentRef("/components/schemas/User")
	assert.True(t, handled)
	assert.True(t, ok)
	assert.Equal(t, namedTypeID("/components/schemas/User"), id)

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
	l := &lowerer{schemas: map[string]bool{"A~B": true}}

	id, ok, handled := l.resolveComponentRef("/components/schemas/A~B")
	assert.True(t, handled)
	assert.True(t, ok)
	assert.Equal(t, namedTypeID("/components/schemas/A~0B"), id,
		"the ID is canonically re-escaped to match the interned node")
	assert.Equal(t, namedTypeID(ptr("components", "schemas", "A~B")), id,
		"and equals the ID the component was interned under")
}

func TestSameFile(t *testing.T) {
	t.Parallel()
	l := &lowerer{source: ir.SourceInfo{Path: "dir/m.yaml"}}
	assert.True(t, l.sameFile("dir/m.yaml"), "exact path")
	assert.True(t, l.sameFile("m.yaml"), "bare filename equal to our basename")
	assert.False(t, l.sameFile("other.yaml"))
	assert.False(t, l.sameFile("other/m.yaml"),
		"a doc part with its own directory is a distinct path, not a basename match")
	assert.False(t, (&lowerer{}).sameFile("m.yaml"), "empty source path never matches")
}

func TestPropIDByName_NotFound(t *testing.T) {
	t.Parallel()
	m := &ir.Model{Properties: []ir.Property{{ID: "p1", Name: ir.Naming{Source: "a"}}}}
	_, ok := propIDByName(m, "missing")
	assert.False(t, ok)
	id, ok := propIDByName(m, "a")
	assert.True(t, ok)
	assert.Equal(t, ir.PropID("p1"), id)
}

func TestRefLastSegment(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "Pet", refLastSegment("#/components/schemas/Pet"))
	assert.Equal(t, "bare", refLastSegment("bare"))
}

func TestMappingTargetID(t *testing.T) {
	t.Parallel()
	l := &lowerer{
		schemas: map[string]bool{"Cat": true, "Dog": true, "A/B": true},
		out:     &ir.Document{Types: ir.TypeRegistry{}},
	}
	// A $ref to a declared component.
	id, ok := l.mappingTargetID("#/components/schemas/Cat")
	require.True(t, ok)
	assert.Equal(t, namedTypeID("/components/schemas/Cat"), id)
	// A bare schema name.
	id, ok = l.mappingTargetID("Dog")
	require.True(t, ok)
	assert.Equal(t, namedTypeID(ptr("components", "schemas", "Dog")), id)
	// A bare name that contains '/' but names an existing schema must resolve, not
	// dangle as a misclassified external $ref (issue #14, f07).
	id, ok = l.mappingTargetID("A/B")
	require.True(t, ok)
	assert.Equal(t, namedTypeID(ptr("components", "schemas", "A/B")), id)
	// An undeclared component and a genuine external ref are dropped, never
	// synthesized into a dangling ID.
	_, ok = l.mappingTargetID("#/components/schemas/Ghost")
	assert.False(t, ok, "undeclared component target dropped")
	_, ok = l.mappingTargetID("a.yaml#/A")
	assert.False(t, ok, "external target dropped")
	// A declared but empty-named component ("") is interned anonymously, so its
	// bare mapping name must resolve to that anon ID, not an unbacked namedTypeID
	// (issue #14, f31).
	l.schemas[""] = true
	id, ok = l.mappingTargetID("")
	require.True(t, ok)
	assert.Equal(t, anonTypeID(ptr("components", "schemas", "")), id)
	assert.NotEqual(t, namedTypeID(ptr("components", "schemas", "")), id)
}

func TestFirstPathSegment_Empty(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "", firstPathSegment("/"))
	assert.Equal(t, "users", firstPathSegment("/users/{id}"))
}

func TestPrimID_SecondCallReuses(t *testing.T) {
	t.Parallel()
	l := newLowerer(0, &loaded{Doc: nil, Source: ir.SourceInfo{}}, Options{}.withDefaults())
	first := l.primID(ir.PrimString)
	second := l.primID(ir.PrimString)
	assert.Equal(t, first, second, "interned primitive reused on second call")
}
