package defs

import (
	"encoding/json/jsontext"
	"strings"
	"testing"

	oas3 "github.com/speakeasy-api/openapi/jsonschema/oas3"
	"github.com/speakeasy-api/openapi/marshaller"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// schemaFromYAML unmarshals body as a standalone JSON Schema document. Nothing
// here calls Resolve: Target and TargetFrom read the parsed tree directly, so a
// raw, unresolved schema is everything a case needs. The schema itself doubles
// as the Navigable document — its own GetRootNode is the whole tree's root,
// exactly as an OpenAPI document's is for a compiled spec.
func schemaFromYAML(t *testing.T, body string) *oas3.JSONSchema[oas3.Referenceable] {
	t.Helper()
	js := &oas3.JSONSchema[oas3.Referenceable]{}
	valErrs, err := marshaller.Unmarshal(t.Context(), strings.NewReader(body), js)
	require.NoError(t, err)
	require.Empty(t, valErrs, "the fixture parses cleanly")
	return js
}

// prop returns js's own property named name.
func prop(t *testing.T, js *oas3.JSONSchema[oas3.Referenceable], name string) *oas3.JSONSchema[oas3.Referenceable] {
	t.Helper()
	p, ok := js.GetSchema().GetProperties().Get(name)
	require.True(t, ok, "fixture declares property %q", name)
	return p
}

// defEntry returns js's own $defs entry named key.
func defEntry(t *testing.T, js *oas3.JSONSchema[oas3.Referenceable], key string) *oas3.JSONSchema[oas3.Referenceable] {
	t.Helper()
	d, ok := js.GetSchema().GetDefs().Get(key)
	require.True(t, ok, "fixture declares $defs/%s", key)
	return d
}

func TestIsPointer(t *testing.T) {
	t.Parallel()
	assert.True(t, IsPointer("/$defs/n"))
	assert.True(t, IsPointer("/$defs/n/properties/x"))
	assert.False(t, IsPointer("/components/schemas/A"), "not a $defs-rooted pointer")
	assert.False(t, IsPointer(""), "the whole-document pointer is not a $defs pointer")
	assert.False(t, IsPointer("/$defs"), "no trailing slash names the map itself, not an entry")
}

// TestTarget_LocalIDRuleAndEscapes pins tryResolveLocalDefs: a schema that
// itself carries $id and its own sibling $defs is read from ITS OWN
// definitions directly, without ever walking to an ancestor. That is the one
// case the generic ancestor search (TargetFrom) can never reach by itself,
// since it always starts one level above the referencing schema, never at it
// (GitHub #557). Dropping this rule (mutation 3 in the PR) turns "p"'s own
// reference unresolved: the ancestor loop starting above p never walks back
// down into p's own $defs.
func TestTarget_LocalIDRuleAndEscapes(t *testing.T) {
	t.Parallel()
	const src = `
type: object
properties:
  p:
    $id: "https://x.test/p"
    $ref: "#/$defs/n"
    $defs:
      n: {type: string}
      a/b: {type: integer}
      "x~y": {type: boolean}
`
	root := schemaFromYAML(t, src)
	p := prop(t, root, "p")

	got, at, ok := Target(root, p, "/$defs/n")
	require.True(t, ok)
	assert.Same(t, defEntry(t, p, "n"), got)
	assert.Equal(t, jsontext.Pointer("/properties/p/$defs/n"), at,
		"the position is where p's own $defs is written, not the root-relative pointer it spelled")

	got, _, ok = Target(root, p, "/$defs/a~1b")
	require.True(t, ok, "~1 decodes to a literal slash in the key")
	assert.Same(t, defEntry(t, p, "a/b"), got)

	got, _, ok = Target(root, p, "/$defs/x~0y")
	require.True(t, ok, "~0 decodes to a literal tilde in the key")
	assert.Same(t, defEntry(t, p, "x~y"), got)
}

// TestTargetFrom_DocumentItselfStep pins the step standard resolution takes
// before ever walking an ancestor: the pointer is read from the document
// itself first. An OpenAPI document has no $defs of its own, so only a
// standalone schema document — its own root — ever answers here.
func TestTargetFrom_DocumentItselfStep(t *testing.T) {
	t.Parallel()
	const src = `
$defs:
  k: {type: string}
properties:
  p: {type: string}
`
	root := schemaFromYAML(t, src)
	p := prop(t, root, "p")

	got, at, ok := Target(root, p, "/$defs/k")
	require.True(t, ok)
	assert.Same(t, defEntry(t, root, "k"), got)
	assert.Equal(t, jsontext.Pointer("/$defs/k"), at, "found at the document's own position, no ancestor prefix")
}

// TestTargetFrom_NearestAncestor pins the fallback for a schema that is not
// itself a resource: the search walks up one JSON-pointer segment at a time
// from the reference's own position — the properties container first, which
// holds no $defs, then the enclosing schema, which does — never the
// reference's own position.
func TestTargetFrom_NearestAncestor(t *testing.T) {
	t.Parallel()
	const src = `
type: object
properties:
  outer:
    $defs:
      k: {type: object}
    properties:
      inner:
        $ref: "#/$defs/k"
`
	root := schemaFromYAML(t, src)
	outer := prop(t, root, "outer")
	inner := prop(t, outer, "inner")

	got, at, ok := Target(root, inner, "/$defs/k")
	require.True(t, ok)
	assert.Same(t, defEntry(t, outer, "k"), got)
	assert.Equal(t, jsontext.Pointer("/properties/outer/$defs/k"), at)
}

// TestTargetFrom_RestPath pins that a pointer deeper than the bare key
// navigates on past it once the owning schema is found: the definition itself
// is a container the rest of the pointer descends into.
func TestTargetFrom_RestPath(t *testing.T) {
	t.Parallel()
	const src = `
type: object
properties:
  outer:
    $defs:
      k:
        type: object
        properties:
          x: {type: string, format: uuid}
    properties:
      p: {$ref: "#/$defs/k/properties/x"}
`
	root := schemaFromYAML(t, src)
	outer := prop(t, root, "outer")
	p := prop(t, outer, "p")

	got, at, ok := Target(root, p, "/$defs/k/properties/x")
	require.True(t, ok)
	require.NotNil(t, got.GetSchema())
	assert.Equal(t, "uuid", got.GetSchema().GetFormat())
	assert.Equal(t, jsontext.Pointer("/properties/outer/$defs/k/properties/x"), at)
}

// TestTargetFrom_NoMatch pins that an absent definition, with the loop running
// out of ancestors, reports ok=false rather than a stale or partial match.
func TestTargetFrom_NoMatch(t *testing.T) {
	t.Parallel()
	root := schemaFromYAML(t, "properties:\n  p: {type: string}\n")
	p := prop(t, root, "p")

	_, _, ok := Target(root, p, "/$defs/missing")
	assert.False(t, ok)
}

// TestTarget_GuardClauses drives Target's own early returns directly, each
// isolated from the others: no document, no reference site, a pointer that is
// not $defs-shaped, and a reference site the document's tree does not contain
// (GetJSONPointer finds no position for it, so from is "").
func TestTarget_GuardClauses(t *testing.T) {
	t.Parallel()
	root := schemaFromYAML(t, "properties:\n  p: {type: string}\n")
	p := prop(t, root, "p")

	_, _, ok := Target(nil, p, "/$defs/n")
	assert.False(t, ok, "no document to navigate")

	_, _, ok = Target(root, nil, "/$defs/n")
	assert.False(t, ok, "no reference site")

	_, _, ok = Target(root, p, "/properties/p")
	assert.False(t, ok, "not a $defs pointer")

	other := schemaFromYAML(t, "properties:\n  q: {type: string}\n")
	otherQ := prop(t, other, "q")
	_, _, ok = Target(root, otherQ, "/$defs/n")
	assert.False(t, ok, "a schema this document's tree does not contain has no position to read the pointer from")
}

// TestTargetFrom_GuardClauses drives the two guards Target's own filtering
// normally shields TargetFrom from: it is called directly elsewhere
// (defsMappingTarget, for a discriminator mapping value), always with a
// pointer already known to be $defs-shaped, so only a direct call exercises
// these two branches.
func TestTargetFrom_GuardClauses(t *testing.T) {
	t.Parallel()
	root := schemaFromYAML(t, "$defs:\n  k: {type: string}\n")

	_, _, ok := TargetFrom(nil, "/properties/p", "/$defs/k")
	assert.False(t, ok, "no document to navigate")

	_, _, ok = TargetFrom(root, "/properties/p", "/properties/p")
	assert.False(t, ok, "not a $defs pointer")
}

// TestLocalDef drives tryResolveLocalDefs' own branches directly: a schema
// that is not a resource at all, a resource whose pointer carries a rest path
// (never handled locally, whatever the schema is), a resource with no $defs of
// its own, and a resource whose $defs does not carry the requested key.
func TestLocalDef(t *testing.T) {
	t.Parallel()

	t.Run("not a resource", func(t *testing.T) {
		t.Parallel()
		root := schemaFromYAML(t, "$defs:\n  n: {type: string}\nproperties:\n  p: {type: string}\n")
		p := prop(t, root, "p")
		_, ok := localDef(p, "/$defs/n")
		assert.False(t, ok, "p carries no $id and inherits no other base")
	})

	t.Run("a rest path is never local", func(t *testing.T) {
		t.Parallel()
		owner := schemaFromYAML(t, "$id: \"https://x.test/a\"\n$defs:\n  n: {type: object, properties: {x: {type: string}}}\n")
		_, ok := localDef(owner, "/$defs/n/properties/x")
		assert.False(t, ok, "tryResolveLocalDefs only ever reads the bare key, never a path beyond it")
	})

	t.Run("own resource but no $defs at all", func(t *testing.T) {
		t.Parallel()
		owner := schemaFromYAML(t, "$id: \"https://x.test/a\"\n")
		_, ok := localDef(owner, "/$defs/n")
		assert.False(t, ok)
	})

	t.Run("own resource, own $defs, key absent", func(t *testing.T) {
		t.Parallel()
		owner := schemaFromYAML(t, "$id: \"https://x.test/a\"\n$defs:\n  n: {type: string}\n")
		_, ok := localDef(owner, "/$defs/missing")
		assert.False(t, ok)
	})
}

// TestDefAt_AncestorNavigationFails drives the branch a real document never
// reaches: parentPointer only ever produces a proper prefix of a position that
// exists, so an ancestor pointer that names nothing is exercised directly
// rather than through TargetFrom's own loop.
func TestDefAt_AncestorNavigationFails(t *testing.T) {
	t.Parallel()
	root := schemaFromYAML(t, "$defs:\n  k: {type: string}\n")
	_, ok := defAt(root, "/does/not/exist", "/$defs/k")
	assert.False(t, ok, "an ancestor pointer with no target in the tree is no answer")
}

func TestOwnResource(t *testing.T) {
	t.Parallel()
	withID := schemaFromYAML(t, "$id: \"https://x.test/a\"\n").GetSchema()
	assert.True(t, ownResource(withID))

	plain := schemaFromYAML(t, "type: string\n").GetSchema()
	assert.False(t, ownResource(plain), "no $id and no registered base on a freshly parsed schema")
}

// TestParentPointer pins getParentJSONPointer's own bound: everything before
// the last '/', and "" once there is no '/' left to strip — the root itself is
// never an answer.
func TestParentPointer(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		in   jsontext.Pointer
		want jsontext.Pointer
	}{
		{"empty has no ancestor", "", ""},
		{"a single segment has no ancestor", "/a", ""},
		{"no leading slash has no ancestor", "a", ""},
		{"two segments strips the last", "/a/b", "/a"},
		{"three segments strips one", "/a/b/c", "/a/b"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, parentPointer(tc.in))
		})
	}
}
