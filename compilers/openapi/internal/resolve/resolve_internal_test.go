package resolve

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/compile"
	"github.com/dexpace/morphic/compilers/openapi/internal/ids"
	"github.com/dexpace/morphic/ir"
)

func TestInternalPointer(t *testing.T) {
	t.Parallel()
	sc := Scope{SelfPath: "m.yaml", Declares: func(string) bool { return false }}
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
		got, ok := sc.InternalPointer(tc.ref)
		assert.Equal(t, tc.wantOK, ok, tc.ref)
		assert.Equal(t, tc.want, got, tc.ref)
	}
}

// TestInternalPointer_MatchesTheResolversNormalization pins that the fragment is
// read the way the resolver reads it, which is what makes a reference this
// compiler calls unresolved one the resolver also failed to resolve. A $ref is a
// URI, so `%2D` in the fragment is a hyphen; comparing the raw text against
// declared names failed every spec-correct escape (GitHub #40). It carries the
// same name as nodeview's test of the same two accessors, so the pair is one
// grep apart — a dependency bump has to satisfy both.
func TestInternalPointer_MatchesTheResolversNormalization(t *testing.T) {
	t.Parallel()
	sc := Scope{SelfPath: "m.yaml", Declares: func(string) bool { return false }}
	tests := []struct {
		name, ref, want string
		internal        bool
	}{
		{name: "hyphen", ref: "#/components/schemas/Foo%2DBar", want: "/components/schemas/Foo-Bar", internal: true},
		{name: "underscore", ref: "#/components/schemas/Foo%5FBar", want: "/components/schemas/Foo_Bar", internal: true},
		{name: "dot", ref: "#/components/schemas/Foo%2EBar", want: "/components/schemas/Foo.Bar", internal: true},
		{name: "space", ref: "#/components/schemas/A%20B", want: "/components/schemas/A B", internal: true},
		{name: "percent", ref: "#/components/schemas/A%25B", want: "/components/schemas/A%B", internal: true},
		// %2F decodes to a separator, so it deepens the pointer rather than naming
		// a component with a slash in it — RFC 6901 spells that one `~1`.
		{name: "encoded separator deepens", ref: "#/components/schemas/A%2FB", want: "/components/schemas/A/B", internal: true},
		{name: "undecodable escape kept raw", ref: "#/components/schemas/A%zzB", want: "/components/schemas/A%zzB", internal: true},
		{name: "trailing space", ref: "#/components/schemas/A ", want: "/components/schemas/A", internal: true},
		{name: "leading space", ref: " #/components/schemas/A", want: "/components/schemas/A", internal: true},
		{name: "second hash ends the pointer", ref: "#/a#b", want: "/a", internal: true},
		{name: "self-document part still internal", ref: "m.yaml#/components/schemas/A%2DB", want: "/components/schemas/A-B", internal: true},
		// The document half is not decoded, because the resolver does not decode it
		// either: GetURI trims and stops. A self-reference has to be spelled the way
		// the file is named.
		{name: "document half is not decoded", ref: "m%2Eyaml#/components/schemas/A", internal: false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, internal := sc.InternalPointer(tc.ref)
			assert.Equal(t, tc.internal, internal)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestResolveComponentRef(t *testing.T) {
	t.Parallel()
	sc := Scope{Declares: func(n string) bool { return n == "User" }}

	id, ok, handled := sc.ComponentRef("/components/schemas/User")
	assert.True(t, handled)
	assert.True(t, ok)
	assert.Equal(t, ids.NamedType("/components/schemas/User"), id)

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
	assert.Equal(t, ids.NamedType("/components/schemas/A~0B"), id,
		"the ID is canonically re-escaped to match the interned node")
	assert.Equal(t, ids.NamedType(ids.Ptr("components", "schemas", "A~B")), id,
		"and equals the ID the component was interned under")
}
func TestSameFile(t *testing.T) {
	t.Parallel()
	sc := Scope{SelfPath: "dir/m.yaml"}
	assert.True(t, sc.sameFile("dir/m.yaml"), "exact path")
	assert.True(t, sc.sameFile("m.yaml"), "bare filename equal to our basename")
	assert.False(t, sc.sameFile("other.yaml"))
	assert.False(t, sc.sameFile("other/m.yaml"),
		"a doc part with its own directory is a distinct path, not a basename match")
	assert.False(t, Scope{}.sameFile("m.yaml"), "empty source path never matches")
}

func TestInternedID_ByPointerHit(t *testing.T) {
	t.Parallel()
	ts := compile.NewTypes(0)
	ts.Intern(deepPointer, "t/anon/prev", func() ir.TypeDef { return &ir.Any{} })

	id, ok := InternedID(ts, deepPointer)
	require.True(t, ok, "a pointer already recorded in byPointer resolves")
	assert.Equal(t, ir.TypeID("t/anon/prev"), id)
}

func TestInternedID_RegistryHit(t *testing.T) {
	t.Parallel()
	ts := compile.NewTypes(0)
	// A node lives at the pointer-derived ID without a byPointer entry: internedID
	// still finds it through the type registry.
	id := ids.AnonType(deepPointer)
	ts.Register(id, &ir.Primitive{TypeCommon: ir.TypeCommon{ID: id}, Prim: ir.PrimString})

	got, ok := InternedID(ts, deepPointer)
	require.True(t, ok, "a node registered under its pointer-derived ID resolves")
	assert.Equal(t, id, got)
}

func TestInternedID_Miss(t *testing.T) {
	t.Parallel()
	ts := compile.NewTypes(0)
	_, ok := InternedID(ts, deepPointer)
	assert.False(t, ok, "an un-interned pointer does not resolve")
}

// deepPointer is a sub-schema coordinate, deep enough that no component-name
// rule could classify it as a top-level declaration.
const deepPointer = "/components/schemas/Obj/properties/inner"
