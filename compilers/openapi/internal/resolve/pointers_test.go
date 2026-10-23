package resolve_test

import (
	"encoding/json/jsontext"
	"testing"

	"github.com/speakeasy-api/openapi/references"
	"github.com/stretchr/testify/assert"

	"github.com/dexpace/morphic/compilers/openapi/internal/fragment"
	"github.com/dexpace/morphic/compilers/openapi/internal/resolve"
)

func TestInternalPointer(t *testing.T) {
	t.Parallel()
	sc := resolve.Scope{SelfPath: "m.yaml", Declares: func(string) bool { return false }}
	cases := []struct {
		ref    string
		want   jsontext.Pointer
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
		// coordinate, and returning the anchor name as though it were a pointer
		// derived IDs from a path no source coordinate spells (GitHub #141).
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

// TestInternalPointer_AgreesWithTheResolverOnWhatItIsHanded pins that the
// fragment is read the way the resolver reads the spelling it is handed
// (fragment.ForResolver), which is what makes a reference this compiler calls
// unresolved one the resolver also failed to resolve. A $ref is a URI, so `%2D`
// in the fragment is a hyphen and '+' is a plus; comparing the raw text against
// declared names failed every spec-correct escape (GitHub #40).
func TestInternalPointer_AgreesWithTheResolverOnWhatItIsHanded(t *testing.T) {
	t.Parallel()
	sc := resolve.Scope{SelfPath: "m.yaml", Declares: func(string) bool { return false }}
	tests := []struct {
		name, ref string
		want      jsontext.Pointer
		internal  bool
	}{
		{name: "hyphen", ref: "#/components/schemas/Foo%2DBar", want: "/components/schemas/Foo-Bar", internal: true},
		{name: "underscore", ref: "#/components/schemas/Foo%5FBar", want: "/components/schemas/Foo_Bar", internal: true},
		{name: "dot", ref: "#/components/schemas/Foo%2EBar", want: "/components/schemas/Foo.Bar", internal: true},
		{name: "space", ref: "#/components/schemas/A%20B", want: "/components/schemas/A B", internal: true},
		{name: "plus is a plus", ref: "#/components/schemas/A+B", want: "/components/schemas/A+B", internal: true},
		{name: "escaped plus is a plus", ref: "#/components/schemas/A%2BB", want: "/components/schemas/A+B", internal: true},
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
		{name: "a plus in the document half is no escape", ref: "m+x.yaml#/components/schemas/A", internal: false},
		// No document key can spell a byte that is not UTF-8, so the fragment can
		// never resolve (GitHub #520).
		{name: "non-UTF-8 fragment", ref: "#/components/schemas/%FF", want: "", internal: false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, internal := sc.InternalPointer(tc.ref)
			assert.Equal(t, tc.internal, internal)
			assert.Equal(t, tc.want, got)
			if internal {
				assert.Equal(t, jsontext.Pointer(references.Reference(fragment.ForResolver(tc.ref)).GetJSONPointer()), got,
					"the resolver's reading of the spelling it is handed")
			}
		})
	}
}

// TestFragmentPointer_ReadsTheFragmentOfAnyDocument pins what a name hint reads
// off a $ref: the pointer its fragment spells, decoded as InternalPointer
// decodes it, whichever document the reference names. Whether this compile can
// resolve that document is InternalPointer's question, not this one's.
func TestFragmentPointer_ReadsTheFragmentOfAnyDocument(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		ref    string
		want   jsontext.Pointer
		wantOK bool
	}{
		{name: "same document", ref: "#/components/schemas/A", want: "/components/schemas/A", wantOK: true},
		{name: "another document is still a pointer", ref: "other.yaml#/components/schemas/A", want: "/components/schemas/A", wantOK: true},
		{name: "percent-decoded", ref: "#/components/schemas/Foo%2DBar", want: "/components/schemas/Foo-Bar", wantOK: true},
		{name: "plus is a plus", ref: "#/components/schemas/A+B", want: "/components/schemas/A+B", wantOK: true},
		{name: `a lone slash, the member keyed ""`, ref: "#/", want: "/", wantOK: true},
		{name: "a $anchor is not a pointer", ref: "#anchor"},
		{name: "a bare # names the whole document, which is refused", ref: "#"},
		{name: "no fragment at all", ref: "other.yaml"},
		{name: "the empty ref", ref: ""},
		{name: "a slash spelled as an escape still introduces a pointer", ref: "#%2F", want: "/", wantOK: true},
		// No document key can spell bytes that are not UTF-8, whichever document
		// the reference names, so both are refused rather than read as pointers
		// carrying them (GitHub #520).
		{name: "non-UTF-8 fragment refused", ref: "#/components/schemas/%FF"},
		{name: "non-UTF-8 fragment refused in another document too", ref: "other.yaml#/components/schemas/%FF"},
		{name: "overlong encoding is not UTF-8", ref: "#/a%C0%AF"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, ok := resolve.FragmentPointer(tc.ref)
			assert.Equal(t, tc.wantOK, ok, tc.ref)
			assert.Equal(t, tc.want, got, tc.ref)
		})
	}
}

// TestInternalPointer_InTheSourcesOwnContent pins which document parts name the
// source in its own content: the resolver's reading, a document part joined
// onto the directory of the file holding it and cleaned. A bare file name, a
// part through the current directory or the parent, and the absolute path
// however it is spelled all name it (GitHub #576). A part with a directory of
// its own is joined onto the source's, so `dir/m.yaml` written in `dir/m.yaml`
// names `dir/dir/m.yaml`, as the resolver reads it.
func TestInternalPointer_InTheSourcesOwnContent(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		self, doc string
		internal  bool
	}{
		{"dir/m.yaml", "m.yaml", true},
		{"dir/m.yaml", "./m.yaml", true},
		{"dir/m.yaml", "../dir/m.yaml", true},
		{"dir/m.yaml", "sub/../m.yaml", true},
		{"dir/m.yaml", "other.yaml", false},
		{"dir/m.yaml", "other/m.yaml", false},
		{"dir/m.yaml", "dir/m.yaml", false},
		{"m.yaml", "./m.yaml", true},
		{"./dir/m.yaml", "m.yaml", true},
		{"/abs/dir/m.yaml", "/abs/dir/m.yaml", true},
		{"/abs/dir/m.yaml", "/abs/dir/./m.yaml", true},
		{"/abs/dir/m.yaml", "/abs/other/../dir/m.yaml", true},
		{"/abs/dir/m.yaml", "/abs/dir/other.yaml", false},
		{"/abs/dir/m.yaml", "m.yaml", true},
		{"https://example.com/api/m.yaml", "m.yaml", true},
		{"https://example.com/api/m.yaml", "./m.yaml", true},
		{"https://example.com/api/m.yaml", "https://example.com/api/m.yaml", true},
		{"https://example.com/api/m.yaml", "https://example.com/api/./m.yaml", false},
		{"https://example.com/api/m.yaml", "other.yaml", false},
		{"", "m.yaml", false},
		// A source path the resolver cannot place as a base (an invalid percent
		// escape) names no document through a document part, as load reports.
		{"dir%/m.yaml", "m.yaml", false},
		{"100%.yaml", "100%.yaml", false},
	} {
		_, ok := resolve.Scope{SelfPath: c.self}.InternalPointer(c.doc + "#/components/schemas/A")
		assert.Equal(t, c.internal, ok, "%q written in %q", c.doc, c.self)
	}
	_, ok := resolve.Scope{SelfPath: "dir/m.yaml"}.InternalPointer("#/components/schemas/A")
	assert.True(t, ok, "a pointer alone names a position in the source's own content")
	_, ok = resolve.Scope{}.InternalPointer("#/components/schemas/A")
	assert.True(t, ok, "whatever the source is called")
}

// TestInternalPointer_ReadsOwnContentAsAForeignScopeHeldByTheSource pins that
// there is one reading: a document part is judged the same in the source's own
// content as in a Foreign scope whose holder is the source, for every
// spelling. The two scopes differ in the base only.
func TestInternalPointer_ReadsOwnContentAsAForeignScopeHeldByTheSource(t *testing.T) {
	t.Parallel()
	selves := []string{"dir/m.yaml", "m.yaml", "./dir/m.yaml", "/abs/dir/m.yaml", "https://example.com/api/m.yaml"}
	docs := []string{
		"m.yaml", "./m.yaml", "../dir/m.yaml", "sub/../m.yaml", "other.yaml", "other/m.yaml", "dir/m.yaml",
		"/abs/dir/m.yaml", "/abs/dir/./m.yaml", "/abs/other/../dir/m.yaml", "/abs/dir//m.yaml",
		"https://example.com/api/m.yaml", "https://example.com/api/./m.yaml", "../../m.yaml",
	}
	for _, self := range selves {
		for _, doc := range docs {
			ref := doc + "#/components/schemas/A"
			_, own := resolve.Scope{SelfPath: self}.InternalPointer(ref)
			_, foreign := resolve.Scope{SelfPath: self, Foreign: true, Holder: self}.InternalPointer(ref)
			assert.Equal(t, foreign, own, "%q written in %q", doc, self)
		}
	}
}

// TestInternalPointer_InAForeignScope pins how a Foreign scope reads a
// reference: against the document holding it, as the resolver does. A pointer
// alone names a position there, and so does a document part naming another
// file. One naming the source, however spelled from there, is internal, and
// nothing is internal with no holder to read against, though read from the
// working directory the last row would name the source.
func TestInternalPointer_InAForeignScope(t *testing.T) {
	t.Parallel()
	const self = "spec.yaml"
	for _, c := range []struct {
		holder, ref string
		internal    bool
	}{
		{"ext.yaml", "#/components/schemas/A", false},
		{"ext.yaml", "spec.yaml#/components/schemas/A", true},
		{"ext.yaml", "./spec.yaml#/components/schemas/A", true},
		{"ext.yaml", "other.yaml#/components/schemas/A", false},
		{"sub/ext.yaml", "../spec.yaml#/components/schemas/A", true},
		{"sub/ext.yaml", "spec.yaml#/components/schemas/A", false},
		{"https://example.com/ext.yaml", "spec.yaml#/components/schemas/A", false},
		{"", "spec.yaml#/components/schemas/A", false},
	} {
		scope := resolve.Scope{SelfPath: self, Foreign: true, Holder: c.holder}
		pointer, ok := scope.InternalPointer(c.ref)
		assert.Equal(t, c.internal, ok, "%s from %s", c.ref, c.holder)
		if c.internal {
			assert.Equal(t, "/components/schemas/A", string(pointer))
		}
	}
}
