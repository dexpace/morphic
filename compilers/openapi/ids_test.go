package openapi

import (
	"testing"

	"github.com/stretchr/testify/assert"

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
