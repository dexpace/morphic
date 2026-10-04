package resolve

import (
	"encoding/json/jsontext"
	"path/filepath"
	"strings"
	"testing"

	oas3 "github.com/speakeasy-api/openapi/jsonschema/oas3"
	soa "github.com/speakeasy-api/openapi/openapi"
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
		name, ref string
		want      jsontext.Pointer
		internal  bool
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

// TestNamesHolder pins which references in a Foreign scope name a position in
// the document holding them: a pointer alone, or a document part naming that
// document. Outside a Foreign scope, none does.
func TestNamesHolder(t *testing.T) {
	t.Parallel()
	scope := Scope{SelfPath: "api/spec.yaml", Foreign: true, Holder: "api/ext.yaml"}
	for ref, want := range map[string]bool{
		"#/components/schemas/A":            true,
		"ext.yaml#/components/schemas/A":    true,
		"spec.yaml#/components/schemas/A":   false,
		"third.yaml#/components/schemas/A":  false,
		"https://example.com/ext.yaml#/x/y": false,
	} {
		assert.Equal(t, want, scope.NamesHolder(ref), ref)
	}
	assert.False(t, Scope{SelfPath: "api/spec.yaml"}.NamesHolder("#/components/schemas/A"))
	unplaceable := Scope{SelfPath: "api/spec.yaml", Foreign: true, Holder: "http://[::1"}
	assert.False(t, unplaceable.NamesHolder("#/components/schemas/A"), "a holder the resolver cannot place")
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

func TestInternedID_ByPointerHit(t *testing.T) {
	t.Parallel()
	ts := compile.NewTypes()
	ts.Intern(deepPointer, "t/anon/prev", func() ir.TypeDef { return &ir.Any{} })

	id, ok := InternedID(ts, deepPointer)
	require.True(t, ok, "a pointer already recorded in byPointer resolves")
	assert.Equal(t, ir.TypeID("t/anon/prev"), id)
}

func TestInternedID_RegistryHit(t *testing.T) {
	t.Parallel()
	ts := compile.NewTypes()
	// A node lives at the pointer-derived ID without a byPointer entry: InternedID
	// still finds it through the type registry.
	id := ids.AnonType(deepPointer)
	ts.Register(id, &ir.Primitive{ID: id, Prim: ir.PrimString})

	got, ok := InternedID(ts, deepPointer)
	require.True(t, ok, "a node registered under its pointer-derived ID resolves")
	assert.Equal(t, id, got)
}

func TestInternedID_Miss(t *testing.T) {
	t.Parallel()
	ts := compile.NewTypes()
	_, ok := InternedID(ts, deepPointer)
	assert.False(t, ok, "an un-interned pointer does not resolve")
}

// deepPointer is a sub-schema coordinate, deep enough that no component-name
// rule could classify it as a top-level declaration.
const deepPointer = "/components/schemas/Obj/properties/inner"

// TestScope_DeclaredAt pins which positions resolve: a schema position to the
// very schema the document holds there, and every position holding no schema
// to nil. That covers a non-schema object, an undeclared name, a keyword the
// schema leaves unset (which the pointer walk reaches as a typed nil), raw YAML
// under an extension key or in an enum, which only a $ref makes the resolver
// parse, and a Scope with no Doc.
func TestScope_DeclaredAt(t *testing.T) {
	t.Parallel()
	doc, _, err := soa.Unmarshal(t.Context(), strings.NewReader(`openapi: 3.1.0
info: {title: T, version: "1"}
paths: {}
components:
  schemas:
    Pet:
      type: object
      enum: [{type: object}]
      x-dog: {type: object}
`))
	require.NoError(t, err)
	sc := Scope{Doc: doc}

	pet, ok := doc.Components.Schemas.Get("Pet")
	require.True(t, ok)
	assert.Same(t, pet, sc.DeclaredAt("/components/schemas/Pet"),
		"a schema position resolves to the declaration the document holds")

	for pointer, why := range map[jsontext.Pointer]string{
		"/info":                          "a non-schema position does not resolve",
		"/components/schemas/Ghost":      "a position the document does not declare does not resolve",
		"/components/schemas/Pet/not":    "a keyword the schema leaves unset does not resolve",
		"/components/schemas/Pet/x-dog":  "raw YAML under an extension does not resolve",
		"/components/schemas/Pet/enum/0": "raw YAML in an enum does not resolve",
	} {
		assert.Nil(t, sc.DeclaredAt(pointer), why)
	}

	assert.Nil(t, Scope{}.DeclaredAt("/components/schemas/Pet"), "a nil Doc resolves nothing")
}

// TestScope_DeclaredAt_MappedAnswersFirst pins the order of DeclaredAt's two
// sources: the schema Mapped holds for a pointer wins, even over one the model
// declares there, since the load phase resolved it the way a $ref would. The
// model answers when Mapped holds nothing or the Scope has no Mapped, so a
// value the load phase did not resolve still finds its declaration (GitHub
// #757).
func TestScope_DeclaredAt_MappedAnswersFirst(t *testing.T) {
	t.Parallel()
	doc, _, err := soa.Unmarshal(t.Context(), strings.NewReader(`openapi: 3.1.0
info: {title: T, version: "1"}
paths: {}
components:
  schemas:
    Pet: {type: object}
`))
	require.NoError(t, err)
	pet, ok := doc.Components.Schemas.Get("Pet")
	require.True(t, ok)

	mapped := oas3.NewJSONSchemaFromSchema[oas3.Referenceable](&oas3.Schema{})
	require.NotSame(t, pet, mapped, "the two sources must be told apart")

	const raw = jsontext.Pointer("/x-lib/Cat")
	var asked []jsontext.Pointer
	answering := func(at jsontext.Pointer, js *oas3.JSONSchema[oas3.Referenceable]) func(jsontext.Pointer) *oas3.JSONSchema[oas3.Referenceable] {
		return func(pointer jsontext.Pointer) *oas3.JSONSchema[oas3.Referenceable] {
			asked = append(asked, pointer)
			if pointer == at {
				return js
			}
			return nil
		}
	}

	sc := Scope{Doc: doc, Mapped: answering(raw, mapped)}
	assert.Same(t, mapped, sc.DeclaredAt(raw), "a position only Mapped holds resolves to its answer")
	assert.Same(t, pet, sc.DeclaredAt("/components/schemas/Pet"), "a nil answer falls back to the model")
	assert.Equal(t, []jsontext.Pointer{raw, "/components/schemas/Pet"}, asked, "Mapped is asked for each pointer")

	shadowed := Scope{Doc: doc, Mapped: answering("/components/schemas/Pet", mapped)}
	assert.Same(t, mapped, shadowed.DeclaredAt("/components/schemas/Pet"), "Mapped wins over the model's own declaration")

	assert.Same(t, pet, Scope{Doc: doc}.DeclaredAt("/components/schemas/Pet"), "no Mapped leaves the model to answer")
	assert.Nil(t, Scope{Doc: doc}.DeclaredAt(raw), "no Mapped and no declaration resolves nothing")
}

// TestScope_MappingPointer pins what a mapping value names. A pointer or a
// declared component is InternalPointer's answer; a "#/$defs/..." value is the
// definition read from its discriminator; and every value that cannot be read
// so is refused: one with a document part, which load leaves to the resolver,
// one into another document, one in a Scope with no Doc, one with no
// discriminator, one whose discriminator this document's tree does not contain,
// and one the rule finds nothing for.
func TestScope_MappingPointer(t *testing.T) {
	t.Parallel()
	doc, _, err := soa.Unmarshal(t.Context(), strings.NewReader(`openapi: 3.1.0
info: {title: T, version: "1"}
paths: {}
components:
  schemas:
    Pet:
      oneOf: [{$ref: "#/$defs/cat"}]
      discriminator: {propertyName: kind, mapping: {cat: "#/$defs/cat"}}
      $defs:
        cat: {type: object}
`))
	require.NoError(t, err)
	pet, ok := doc.Components.Schemas.Get("Pet")
	require.True(t, ok)
	d := pet.GetSchema().GetDiscriminator()
	require.NotNil(t, d)
	sc := Scope{SelfPath: "spec.yaml", Doc: doc}

	for value, want := range map[string]jsontext.Pointer{
		"#/components/schemas/Pet": "/components/schemas/Pet",
		"#/$defs/cat":              "/components/schemas/Pet/$defs/cat",
	} {
		got, ok := sc.MappingPointer(d, value)
		assert.True(t, ok, value)
		assert.Equal(t, want, got, value)
	}

	for value, why := range map[string]string{
		"other.yaml#/A":        "a value into another document is refused",
		"spec.yaml#/$defs/cat": "a document part is left to the resolver, as load leaves a $ref spelled so",
		"#/$defs/missing":      "the rule finds no such definition",
		"Pet":                  "a bare name names no pointer",
	} {
		_, ok := sc.MappingPointer(d, value)
		assert.False(t, ok, why)
	}

	_, ok = (Scope{SelfPath: "spec.yaml"}).MappingPointer(d, "#/$defs/cat")
	assert.False(t, ok, "no document to read from")
	_, ok = sc.MappingPointer(nil, "#/$defs/cat")
	assert.False(t, ok, "no discriminator to read a position from")
	// A discriminator this document's tree does not contain has no position to
	// read from. The document is a standalone schema with its own $defs, the one
	// kind a missing position could still read a definition from.
	standalone := Scope{SelfPath: "spec.yaml", Doc: schemaFromYAML(t, "$defs:\n  cat: {type: object}\n")}
	_, ok = standalone.MappingPointer(&oas3.Discriminator{}, "#/$defs/cat")
	assert.False(t, ok, "no position to read the pointer from")
}
