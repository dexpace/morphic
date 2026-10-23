package resolve

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dexpace/morphic/compilers/openapi/internal/ids"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
)

// TestIsRefSite_IncludesTheDegenerateRef pins the deliberately broad test. A
// position carrying a $ref field is $ref-shaped even when the ref is empty:
// callers here ask "is there a $ref-carrying body", not "is there a followable
// reference", and reading the narrower question would treat {$ref: ""} as an
// ordinary schema body.
func TestIsRefSite_IncludesTheDegenerateRef(t *testing.T) {
	t.Parallel()
	ref := openapitest.SchemaFromYAML(t.Context(), t, "{$ref: '#/$defs/T'}\n")
	assert.True(t, IsRefSite(ref, ref.GetSchema()))

	empty := openapitest.SchemaFromYAML(t.Context(), t, "{$ref: ''}\n")
	assert.True(t, IsRefSite(empty, empty.GetSchema()),
		"a present but empty $ref still carries a Ref field")

	plain := openapitest.SchemaFromYAML(t.Context(), t, "type: string\n")
	assert.False(t, IsRefSite(plain, plain.GetSchema()))
	assert.False(t, IsRefSite(plain, nil), "a boolean schema carries no body to inspect")
}

func TestResolveComponentRef(t *testing.T) {
	t.Parallel()
	sc := Scope{Declares: func(n string) bool { return n == "User" }}

	id, ok, handled := sc.ComponentRef("/components/schemas/User")
	assert.True(t, handled)
	assert.True(t, ok)
	assert.Equal(t, ids.Document{}.NamedType("/components/schemas/User"), id)

	_, ok, handled = sc.ComponentRef("/components/schemas/Missing")
	assert.True(t, handled, "an undeclared component pointer is still classified as a component")
	assert.False(t, ok, "an undeclared component does not resolve")

	_, _, handled = sc.ComponentRef("/components/schemas/Foo/properties/bar")
	assert.False(t, handled, "a sub-schema pointer is not a top-level component pointer")
}

// TestResolveComponentRef_NonCanonicalEscape pins that the resolved ID is built
// from the component's canonical name, so a $ref that escapes non-canonically
// (a raw '~' for a component named "A~B", interned under "A~0B") still resolves to
// the interned node rather than an unbacked ID (issue #14).
func TestResolveComponentRef_NonCanonicalEscape(t *testing.T) {
	t.Parallel()
	sc := Scope{Declares: func(n string) bool { return n == "A~B" }}

	id, ok, handled := sc.ComponentRef("/components/schemas/A~B")
	assert.True(t, handled)
	assert.True(t, ok)
	assert.Equal(t, ids.Document{}.NamedType("/components/schemas/A~0B"), id,
		"the ID is canonically re-escaped to match the interned node")
	assert.Equal(t, ids.Document{}.NamedType(ids.Ptr("components", "schemas", "A~B")), id,
		"and equals the ID the component was interned under")
}
