package resolve

import (
	"encoding/json/jsontext"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/speakeasy-api/openapi/jsonpointer"
	oas3 "github.com/speakeasy-api/openapi/jsonschema/oas3"
	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/speakeasy-api/openapi/references"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	yaml "gopkg.in/yaml.v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/fragment"
	"github.com/dexpace/morphic/compilers/openapi/internal/ids"
	"github.com/dexpace/morphic/compilers/openapi/internal/navigation"
)

func TestInternalPointer(t *testing.T) {
	t.Parallel()
	sc := Scope{SelfPath: "m.yaml", Declares: func(string) bool { return false }}
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

// TestInternalPointer_AgreesWithTheResolverOnWhatItIsHanded pins that the
// fragment is read the way the resolver reads the spelling it is handed
// (fragment.ForResolver), which is what makes a reference this compiler calls
// unresolved one the resolver also failed to resolve. A $ref is a URI, so `%2D`
// in the fragment is a hyphen and '+' is a plus; comparing the raw text against
// declared names failed every spec-correct escape (GitHub #40). It carries the
// same name as nodeview's test of the same two accessors, so the pair is one
// grep apart — a dependency bump has to satisfy both.
func TestInternalPointer_AgreesWithTheResolverOnWhatItIsHanded(t *testing.T) {
	t.Parallel()
	sc := Scope{SelfPath: "m.yaml", Declares: func(string) bool { return false }}
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
			got, ok := FragmentPointer(tc.ref)
			assert.Equal(t, tc.wantOK, ok, tc.ref)
			assert.Equal(t, tc.want, got, tc.ref)
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
		_, ok := Scope{SelfPath: c.self}.InternalPointer(c.doc + "#/components/schemas/A")
		assert.Equal(t, c.internal, ok, "%q written in %q", c.doc, c.self)
	}
	_, ok := Scope{SelfPath: "dir/m.yaml"}.InternalPointer("#/components/schemas/A")
	assert.True(t, ok, "a pointer alone names a position in the source's own content")
	_, ok = Scope{}.InternalPointer("#/components/schemas/A")
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
			_, own := Scope{SelfPath: self}.InternalPointer(ref)
			_, foreign := Scope{SelfPath: self, Foreign: true, Holder: self}.InternalPointer(ref)
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
		scope := Scope{SelfPath: self, Foreign: true, Holder: c.holder}
		pointer, ok := scope.InternalPointer(c.ref)
		assert.Equal(t, c.internal, ok, "%s from %s", c.ref, c.holder)
		if c.internal {
			assert.Equal(t, "/components/schemas/A", string(pointer))
		}
	}
}

// TestSameDocument pins when the resolver reading one path reads the document
// at another: the same spelling, or the same file however reached. A URL is
// one document only as spelled, and an empty path names none.
func TestSameDocument(t *testing.T) {
	t.Parallel()
	abs, err := filepath.Abs("api/spec.yaml")
	require.NoError(t, err)
	for _, c := range []struct {
		a, b string
		want bool
	}{
		{"api/spec.yaml", "api/spec.yaml", true},
		{"api/spec.yaml", "api/./sub/../spec.yaml", true},
		{"api/spec.yaml", abs, true},
		{"api/spec.yaml", "api/other.yaml", false},
		{"api/x:/spec.yaml", "api/x://spec.yaml", true},
		{"https://example.com/a.yaml", "https://example.com/a.yaml", true},
		{"https://example.com/a.yaml", "https://example.com/./a.yaml", false},
		{"file:/api/spec.yaml", "file:/api/./spec.yaml", false},
		{"", "", false},
	} {
		assert.Equal(t, c.want, SameDocument(c.a, c.b), "%q %q", c.a, c.b)
	}
}

// sameDocumentByWorkingDirectory is SameDocument's reading when every pair of
// file paths is made absolute against the working directory: the reading the
// quicker one must agree with.
func sameDocumentByWorkingDirectory(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	if a == b {
		return true
	}
	if IsURL(a) || IsURL(b) {
		return false
	}
	absA, errA := filepath.Abs(a)
	absB, errB := filepath.Abs(b)
	return errA == nil && errB == nil && absA == absB
}

// TestSameDocument_AgreesWithTheWorkingDirectoryReading pins that reading the
// working directory only where it matters changes no answer. The grid is every
// pair of spellings of two files and a directory, relative and absolute, with
// parents climbing out of the working directory and back in, which cleaned
// strings alone cannot equate. Each file is also spelled rooted without its
// volume: on Windows that path is neither absolute nor below the directory,
// and elsewhere, with no volume, it is the absolute spelling again. So only a
// Windows run reaches that form here; the test for non-local Windows paths pins
// it on any host.
func TestSameDocument_AgreesWithTheWorkingDirectoryReading(t *testing.T) {
	t.Parallel()
	cwd, err := filepath.Abs(".")
	require.NoError(t, err)
	rooted := strings.TrimPrefix(cwd, filepath.VolumeName(cwd))
	up := filepath.Join("..", filepath.Base(cwd))
	names := []string{"spec.yaml", "api/spec.yaml", "other.yaml"}
	forms := []string{"%s", "./%s", "api/../%s", "x/./../%s", up + "/%s", "../../%s"}
	rest := []string{"", ".", "..", cwd, "https://example.com/spec.yaml", "https://example.com/./spec.yaml"}
	paths := make([]string, 0, len(names)*len(forms)*5+len(rest))
	for _, name := range names {
		for _, form := range forms {
			rel := strings.Replace(form, "%s", name, 1)
			paths = append(paths, rel, filepath.Join(cwd, rel), cwd+"//"+rel, cwd+"/./"+rel,
				filepath.Join(rooted, rel))
		}
	}
	paths = append(paths, rest...)
	for _, a := range paths {
		for _, b := range paths {
			assert.Equal(t, sameDocumentByWorkingDirectory(a, b), SameDocument(a, b), "%q %q", a, b)
		}
	}
}

// TestSameDocument_ReadsTheWorkingDirectoryOnlyWhereItMatters pins when
// SameDocument makes a path absolute, which reads the working directory: two
// absolute paths, or two relative ones that stay below it, are compared as
// cleaned and never read it, so a compile that names no document through an
// absolute path has no ambient read. A relative path against an absolute one,
// or one not below the directory, cannot be told apart without it.
func TestSameDocument_ReadsTheWorkingDirectoryOnlyWhereItMatters(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		a, b  string
		wd    string // the working directory the paths are made absolute against
		same  bool
		reads bool
	}{
		{"spec.yaml", "spec.yaml", "/abs", true, false},
		{"spec.yaml", "./sub/../spec.yaml", "/abs", true, false},
		{"spec.yaml", "other.yaml", "/abs", false, false},
		{"api/spec.yaml", "api/sub/spec.yaml", "/abs", false, false},
		{"/abs/spec.yaml", "/abs/./sub/../spec.yaml", "/elsewhere", true, false},
		{"/abs/spec.yaml", "/abs/other.yaml", "/elsewhere", false, false},
		{"https://example.com/a.yaml", "a.yaml", "/abs", false, false},
		{"", "spec.yaml", "/abs", false, false},
		{"spec.yaml", "/abs/spec.yaml", "/abs", true, true},
		{"spec.yaml", "/abs/spec.yaml", "/elsewhere", false, true},
		{"/abs/spec.yaml", "spec.yaml", "/abs", true, true},
		{"../api/spec.yaml", "spec.yaml", "/abs/api", true, true},
		{"../api/spec.yaml", "spec.yaml", "/abs/other", false, true},
		{"..", ".", "/abs", false, true},
		{"../a.yaml", "../b.yaml", "/abs", false, true},
		{"../a.yaml", "../b.yaml", "", false, true}, // no working directory to read: neither is made absolute
	} {
		reads := 0
		abs := func(p string) (string, error) {
			reads++
			if filepath.IsAbs(p) {
				return filepath.Clean(p), nil
			}
			if c.wd == "" {
				return "", errors.New("no working directory")
			}
			return filepath.Join(c.wd, p), nil
		}

		rules := systemRules()
		rules.abs = abs

		got := sameDocument(c.a, c.b, rules)

		assert.Equal(t, c.same, got, "%q %q from %s", c.a, c.b, c.wd)
		assert.Equal(t, c.reads, reads > 0, "reads the working directory: %q %q", c.a, c.b)
	}
}

// TestSameDocument_ReadsTheWorkingDirectoryForNonLocalWindowsPaths pins how
// SameDocument treats the Windows paths that are neither absolute nor below the
// working directory: one rooted without a volume, one naming a drive, a
// reserved name. Each is made absolute before it is compared, so two spellings
// of one file are one document. The table states what filepath documents for
// Windows (IsAbs, IsLocal) and what Abs names, so the test checks how
// SameDocument uses those answers on any host; only a Windows run of the grid
// test checks the answers themselves.
func TestSameDocument_ReadsTheWorkingDirectoryForNonLocalWindowsPaths(t *testing.T) {
	t.Parallel()
	// A path's reading on Windows with C:\specs the working directory: whether
	// IsAbs and IsLocal accept it, and the file Abs names.
	type reading struct {
		abs, local bool
		file       string
	}
	windows := map[string]reading{
		`C:\specs\api.yaml`:   {abs: true, file: `C:\specs\api.yaml`},
		`C:\specs\other.yaml`: {abs: true, file: `C:\specs\other.yaml`},
		`api.yaml`:            {local: true, file: `C:\specs\api.yaml`},
		`sub\api.yaml`:        {local: true, file: `C:\specs\sub\api.yaml`},
		`\specs\api.yaml`:     {file: `C:\specs\api.yaml`},     // rooted, no volume
		`C:sub\api.yaml`:      {file: `C:\specs\sub\api.yaml`}, // names a drive; the backslash makes IsURL read a file
		`NUL`:                 {file: `\\.\NUL`},               // a reserved name
	}
	read := func(path string) reading {
		r, ok := windows[path]
		if !ok {
			t.Errorf("path %q has no Windows reading in this test", path)
		}
		return r
	}
	for _, c := range []struct {
		a, b  string
		same  bool
		reads bool
	}{
		{`\specs\api.yaml`, `api.yaml`, true, true},
		{`api.yaml`, `\specs\api.yaml`, true, true},
		{`C:sub\api.yaml`, `sub\api.yaml`, true, true},
		{`sub\api.yaml`, `C:sub\api.yaml`, true, true},
		{`\specs\api.yaml`, `C:\specs\api.yaml`, true, true},
		{`NUL`, `api.yaml`, false, true},
		{`api.yaml`, `sub\api.yaml`, false, false},
		{`C:\specs\api.yaml`, `C:\specs\other.yaml`, false, false},
	} {
		require.Equal(t, c.same, read(c.a).file == read(c.b).file, "the table disagrees with the row %q %q", c.a, c.b)
		reads := 0
		rules := pathRules{
			abs: func(path string) (string, error) {
				reads++
				return read(path).file, nil
			},
			isAbs:   func(path string) bool { return read(path).abs },
			isLocal: func(path string) bool { return read(path).local },
		}

		got := sameDocument(c.a, c.b, rules)

		assert.Equal(t, c.same, got, "%q %q", c.a, c.b)
		assert.Equal(t, c.reads, reads > 0, "reads the working directory: %q %q", c.a, c.b)
	}
}

// TestIsURL pins which locations name a document by URL, as the resolver
// classifies them: those with a scheme before their first colon, whether or
// not "//" follows it. A file path is no URL, even one holding "://" past a
// directory named like a scheme, nor is a Windows drive before a backslash, or
// a location the resolver cannot parse.
func TestIsURL(t *testing.T) {
	t.Parallel()
	for location, want := range map[string]bool{
		"https://example.com/spec.yaml": true,
		"file:///api/spec.yaml":         true,
		"file:/api/spec.yaml":           true,
		"urn:example:spec":              true,
		"x://spec.yaml":                 true,
		"/api/x://spec.yaml":            false,
		"./x://spec.yaml":               false,
		"api/x://spec.yaml":             false,
		"api/spec.yaml":                 false,
		`C:\api\spec.yaml`:              false,
		"":                              false,
		"%zz":                           false,
	} {
		assert.Equal(t, want, IsURL(location), "%q", location)
	}
}

// rawMappingDoc is a document whose pointers leave the model for raw YAML:
// the root's under an extension, and a schema's.
const rawMappingDoc = `openapi: 3.1.0
info: {title: T, version: "1"}
paths: {}
components:
  schemas:
    S: {type: object, x-ext: {k: {type: object}}}
x-lib:
  a: {b: {type: object}}
`

// wholeModelAt reads the whole pointer in one library call, as the library's
// resolver reads it: the schema found there, or nil.
func wholeModelAt(doc any, pointer jsontext.Pointer) *oas3.JSONSchema[oas3.Referenceable] {
	target, err := jsonpointer.GetTarget(doc, jsonpointer.JSONPointer(pointer), jsonpointer.WithStructTags("key"))
	if js, ok := target.(*oas3.JSONSchema[oas3.Referenceable]); ok && err == nil && js != nil {
		return js
	}
	return nil
}

// TestScope_ReadsNoRawMappingAPointerPasses pins GitHub #778 for the walk
// reached takes: it scans no mapping where a pointer leaves the model, the
// object's own, nor any raw mapping below. Each such mapping is given a first
// key that is no node, which the library's read of it faults on, as the whole
// read shows.
func TestScope_ReadsNoRawMappingAPointerPasses(t *testing.T) {
	t.Parallel()
	for _, at := range []struct{ mapping, pointer jsontext.Pointer }{
		{"", "/x-lib/a/b"},
		{"/x-lib", "/x-lib/a/b"},
		{"/components/schemas/S", "/components/schemas/S/x-ext/k"},
		{"/components/schemas/S/x-ext", "/components/schemas/S/x-ext/k"},
	} {
		doc, _, err := soa.Unmarshal(t.Context(), strings.NewReader(rawMappingDoc))
		require.NoError(t, err)
		mapping := builtAt(t, doc, at.mapping)
		mapping.Content = append([]*yaml.Node{nil, {Kind: yaml.ScalarNode}}, mapping.Content...)

		assert.Panics(t, func() { wholeModelAt(doc, at.pointer) }, "the whole read scans %q", at.mapping)
		sc := Scope{Doc: doc, Ends: ReferenceEnd}
		assert.NotPanics(t, func() {
			assert.False(t, atPointer(sc, at.pointer).Foreign)
		}, "%q", at.pointer)
	}
}

// builtAt returns the mapping the model holds at pointer: raw YAML itself, or
// the one the object there was built from.
func builtAt(t *testing.T, doc *soa.OpenAPI, pointer jsontext.Pointer) *yaml.Node {
	t.Helper()
	switch v := walkTo(doc, pointer).(type) {
	case *yaml.Node:
		return v
	case *oas3.JSONSchema[oas3.Referenceable]:
		return v.GetSchema().GetRootNode()
	case *soa.OpenAPI:
		return v.GetRootNode()
	default:
		require.Failf(t, "no mapping", "%q holds %T", pointer, v)
		return nil
	}
}

// TestScopeReached_PassesAReferenceTheWalkLeavesTheModelFrom pins a walk that
// leaves the model at a reference, for raw YAML its target holds: the
// reference is passed when the target holds the next token, as the resolver
// steps past it into that YAML, and not when it holds nothing there. Holds,
// asked of the target's mapping, answers in place of the library: as the
// library's read of that mapping does, and what it answers is what is passed.
func TestScopeReached_PassesAReferenceTheWalkLeavesTheModelFrom(t *testing.T) {
	t.Parallel()
	doc, _, err := soa.Unmarshal(t.Context(), strings.NewReader(`openapi: 3.1.0
info: {title: T, version: "1"}
paths: {}
components:
  responses:
    R: {$ref: '#/components/responses/S'}
    S: {description: s, x-foo: {a: 1}}
`))
	require.NoError(t, err)
	_, err = doc.ResolveAllReferences(t.Context(), soa.ResolveAllOptions{OpenAPILocation: "spec.yaml"})
	require.NoError(t, err)
	elsewhere := func(node any) (End, bool) {
		if r, ok := node.(*soa.ReferencedResponse); ok && r.IsReference() {
			return End{Document: "elsewhere", Path: "x.yaml"}, true
		}
		return End{}, false
	}
	library := func(raw *yaml.Node, token string) bool {
		_, err := jsonpointer.GetTarget(raw, jsonpointer.JSONPointer("/"+jsonpointer.EscapeString(token)))
		return err == nil
	}
	for _, holds := range []func(*yaml.Node, string) bool{nil, library} {
		sc := Scope{Doc: doc, Ends: elsewhere, Holds: holds}
		assert.True(t, atPointer(sc, "/components/responses/R/x-foo/a").Foreign, "S holds x-foo")
		assert.True(t, atPointer(sc, "/components/responses/R/x-foo").Foreign)
		assert.False(t, atPointer(sc, "/components/responses/R/x-nope").Foreign, "S holds no x-nope")
		assert.False(t, atPointer(sc, "/components/responses/S/x-foo").Foreign, "S is no reference")
	}

	never := Scope{Doc: doc, Ends: elsewhere, Holds: func(*yaml.Node, string) bool { return false }}
	assert.False(t, atPointer(never, "/components/responses/R/x-foo/a").Foreign, "Holds, not the library, answers")
	always := Scope{Doc: doc, Ends: elsewhere, Holds: func(*yaml.Node, string) bool { return true }}
	assert.True(t, atPointer(always, "/components/responses/R/x-nope").Foreign, "Holds, not the library, answers")
	assert.False(t, atPointer(always, "/components/responses/S/x-foo").Foreign, "only a reference asks it")
}

// TestScopeReached_StepsPastAnIndexTheLibraryRetries pins the walk past an
// index the library retries, reading it as a key with the rest below it and as
// an index once that fails: what it steps past is still noted a step at a
// time.
func TestScopeReached_StepsPastAnIndexTheLibraryRetries(t *testing.T) {
	t.Parallel()
	var built yaml.Node
	require.NoError(t, yaml.Unmarshal([]byte("{'0': {j: 5}}"), &built))
	js := schemaFromYAML(t, "type: string\n")
	doc := &indexedDoc{built: built.Content[0], schema: js}
	for _, ends := range []func(any) (End, bool){nil, elsewhere} {
		sc := Scope{Doc: doc, Ends: ends}
		assert.Same(t, js, wholeModelAt(doc, "/0/k"), "sanity: the index answers k, which the mapping's 0 does not hold")
		assert.Equal(t, ends != nil, atPointer(sc, "/0/k").Foreign, "the walk noted the document it stepped past")
	}
}

// indexedDoc is a document that is no model and that the library reads by
// index as well as by the mapping it was built from: that mapping's 0 holds j,
// and its index 0 holds the schema under k.
type indexedDoc struct {
	built  *yaml.Node
	schema *oas3.JSONSchema[oas3.Referenceable]
}

// GetRootNode returns the mapping the document was built from.
func (d *indexedDoc) GetRootNode() *yaml.Node { return d.built }

// NavigateWithIndex answers index 0 with a map holding the schema under k.
func (d *indexedDoc) NavigateWithIndex(i int) (any, error) {
	if i != 0 {
		return nil, errors.New("no such index")
	}
	return map[string]any{"k": d.schema}, nil
}

// walkTo returns what the model holds at pointer, a step at a time.
func walkTo(doc *soa.OpenAPI, pointer jsontext.Pointer) any {
	var node any = doc
	for token := range pointer.Tokens() {
		node, _ = navigation.Step(node, token)
	}
	return node
}

// walkDoc is a document whose positions a walk reaches past a path item and an
// operation's responses, which the model holds by value, into a schema,
// through a response keyed by the empty string, and into an extension's value,
// which the model holds as raw YAML.
const walkDoc = `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /a: {get: {responses: {"200": {description: ok}, default: {description: otherwise}}}}
components:
  schemas:
    S: {type: object, properties: {p: {type: object, properties: {q: {type: string}}}}}
  responses:
    "": {description: keyed by the empty string}
x-lib:
  a: {b: {c: deep}}
`

// unmarshalWalkDoc parses walkDoc.
func unmarshalWalkDoc(t *testing.T) *soa.OpenAPI {
	t.Helper()
	doc, _, err := soa.Unmarshal(t.Context(), strings.NewReader(walkDoc))
	require.NoError(t, err)
	return doc
}

// elsewhere is an Ends that reads every node it is asked about as a reference
// ending in another document, so any node a walk steps past shows.
func elsewhere(any) (End, bool) { return End{Document: "elsewhere", Path: "x.yaml"}, true }

// atPointer is the scope reached reads what pointer names in, a position of
// the source's own model: ScopeOf's answer for a chain whose last hop resolved
// there.
func atPointer(s Scope, pointer jsontext.Pointer) Scope {
	return s.reached(End{Document: s.Doc, Pointer: pointer})
}

// TestScopeReached_PassesAReferenceUnderADefaultResponse pins that the walk goes on
// past an operation's responses, which the model holds by value: a default
// response that is a reference is passed, as one under a status code is.
func TestScopeReached_PassesAReferenceUnderADefaultResponse(t *testing.T) {
	t.Parallel()
	responses := func(node any) (End, bool) {
		if _, ok := node.(*soa.ReferencedResponse); ok {
			return End{Document: "elsewhere", Path: "x.yaml"}, true
		}
		return End{}, false
	}
	sc := Scope{Doc: unmarshalWalkDoc(t), Ends: responses}
	for _, code := range []string{"default", "200"} {
		at := atPointer(sc, jsontext.Pointer("/paths/~1a/get/responses/"+code+"/description"))
		assert.True(t, at.Foreign, "the %s response is passed", code)
		assert.Equal(t, "x.yaml", at.Holder, code)
	}
}

// TestScopeReached_StopsAtASchema pins where the walk stops reading references: at
// the first schema, since the resolver reads a schema's own $ref as a keyword,
// so nothing past it is asked whether it is one.
func TestScopeReached_StopsAtASchema(t *testing.T) {
	t.Parallel()
	asked := 0
	sc := Scope{Doc: unmarshalWalkDoc(t), Ends: func(any) (End, bool) { asked++; return End{}, false }}

	assert.False(t, atPointer(sc, "/components/schemas/S/properties/p/properties/q").Foreign)
	assert.Equal(t, 3, asked, "the document, its components and its schemas, then the schema stops it")
}

// TestScopeReached_StopsAtRawYAML pins the other place the walk stops: at a value
// the model holds as raw YAML, which holds no reference the resolver resolved,
// so a pointer into an extension costs one step rather than a key scan for
// each token past it.
func TestScopeReached_StopsAtRawYAML(t *testing.T) {
	t.Parallel()
	asked := 0
	sc := Scope{Doc: unmarshalWalkDoc(t), Ends: func(any) (End, bool) { asked++; return End{}, false }}

	assert.False(t, atPointer(sc, "/x-lib/a/b/c").Foreign)
	assert.Equal(t, 1, asked, "the document, then the extension's value stops it")

	rawAsReference := func(node any) (End, bool) {
		_, raw := node.(*yaml.Node)
		return End{Document: "elsewhere", Path: "x.yaml"}, raw
	}
	at := atPointer(Scope{Doc: unmarshalWalkDoc(t), Ends: rawAsReference}, "/extensions/x-lib/a/b")
	assert.False(t, at.Foreign, "raw YAML a field holds is passed by no walk, though Ends would read it as a reference")
}

// TestScopeReached_PassesOnlyWhatTheWalkStepsPast pins which nodes a walk passes:
// those it steps past to the next token. One where the walk finds nothing more
// is not passed, nor is the node the pointer ends at.
func TestScopeReached_PassesOnlyWhatTheWalkStepsPast(t *testing.T) {
	t.Parallel()
	sc := Scope{Doc: unmarshalWalkDoc(t), Ends: elsewhere}

	assert.False(t, atPointer(sc, "/nope/x").Foreign, "the walk finds nothing past the document")
	assert.False(t, atPointer(sc, "").Foreign, "the pointer ends at the document")
	past := atPointer(sc, "/paths/~1a")
	assert.True(t, past.Foreign, "the walk steps past the document and its paths")
	assert.Equal(t, "x.yaml", past.Holder)
}

// TestScopeReached_FollowsAtMostMaxRefChainRefs pins the bound on the $refs a walk
// follows, each to where its chain ends: a chain ending back at a position its
// own walk passes it again is followed maxRefChain times, then read as no
// document's. Read as the source's own, content past the bound that another
// document holds read its references against the source (GitHub #762).
func TestScopeReached_FollowsAtMostMaxRefChainRefs(t *testing.T) {
	t.Parallel()
	doc := unmarshalWalkDoc(t)
	turns := 0
	sc := Scope{Doc: doc, Ends: func(node any) (End, bool) {
		if _, ok := node.(*soa.ReferencedPathItem); !ok {
			return End{}, false
		}
		turns++
		return End{Document: doc, Pointer: "/paths/~1a/get"}, true
	}}

	past := atPointer(sc, "/paths/~1a/get")
	assert.True(t, past.Foreign, "past the bound, no document's")
	assert.Empty(t, past.Holder, "so no reference there names a document")
	assert.Equal(t, maxRefChain, turns, "each turn follows one $ref")

}

// TestScopeReached_AnEndInTheSourcesFileIsTheSources pins a hop that resolved
// against the source's file read again rather than its model, as the resolver
// reads it when the load phase does not hold the source: it is the source's
// own content, under any spelling of its path, and not another document's.
func TestScopeReached_AnEndInTheSourcesFileIsTheSources(t *testing.T) {
	t.Parallel()
	abs, err := filepath.Abs("dir/root.yaml")
	require.NoError(t, err)
	sc := Scope{SelfPath: "dir/root.yaml"}
	for _, path := range []string{"dir/root.yaml", "dir/./root.yaml", "dir/x/../root.yaml", abs} {
		got := sc.reached(End{Document: new(int), Path: path, Pointer: "/components/pathItems/Own"})
		assert.False(t, got.Foreign, path)
		assert.Empty(t, got.Holder, path)
	}
	for _, path := range []string{"dir/other.yaml", "other/root.yaml", "https://example.com/dir/root.yaml"} {
		got := sc.reached(End{Document: new(int), Path: path, Pointer: "/x"})
		assert.True(t, got.Foreign, path)
		assert.Equal(t, path, got.Holder, path)
	}
	none := Scope{}.reached(End{Document: new(int), Path: "dir/root.yaml", Pointer: "/x"})
	assert.True(t, none.Foreign, "a scope with no source path names no source file")
}

// TestScopeReached_WithoutEndsPassesNothing pins a scope with no Ends: nothing can
// be read as a reference, so every pointer names the source's own content.
func TestScopeReached_WithoutEndsPassesNothing(t *testing.T) {
	t.Parallel()
	sc := Scope{Doc: unmarshalWalkDoc(t), Foreign: true, Holder: "x.yaml"}

	got := atPointer(sc, "/paths/~1a/get")
	assert.False(t, got.Foreign)
	assert.Empty(t, got.Holder)
}
