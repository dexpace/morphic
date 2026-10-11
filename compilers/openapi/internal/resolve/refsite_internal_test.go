package resolve

import (
	"encoding/json/jsontext"
	"strings"
	"testing"

	oas3 "github.com/speakeasy-api/openapi/jsonschema/oas3"
	"github.com/speakeasy-api/openapi/marshaller"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/defs"
)

// schemaFromYAML unmarshals body as a JSONSchema, keeping the reference form a
// bare *oas3.Schema cannot represent.
func schemaFromYAML(t *testing.T, body string) *oas3.JSONSchema[oas3.Referenceable] {
	t.Helper()
	js := &oas3.JSONSchema[oas3.Referenceable]{}
	valErrs, err := marshaller.Unmarshal(t.Context(), strings.NewReader(body), js)
	require.NoError(t, err)
	require.Empty(t, valErrs, "the fixture parses cleanly")
	return js
}

// resolvedProperty parses body and resolves property p against it, which is
// what gives a reference a target to be read through.
func resolvedProperty(t *testing.T, body, p string) *oas3.JSONSchema[oas3.Referenceable] {
	t.Helper()
	_, prop := resolvedPropertyIn(t, body, p)
	return prop
}

// resolvedPropertyIn is resolvedProperty that also returns the document the
// property sits in, which a "#/$defs/..." pointer is navigated in.
func resolvedPropertyIn(t *testing.T, body, p string) (root, prop *oas3.JSONSchema[oas3.Referenceable]) {
	t.Helper()
	root = schemaFromYAML(t, body)
	prop, ok := root.GetSchema().GetProperties().Get(p)
	require.True(t, ok, "the fixture declares property %q", p)
	valErrs, err := prop.Resolve(t.Context(), oas3.ResolveOptions{
		RootDocument: root, TargetLocation: "spec.yaml", DisableExternalRefs: true,
	})
	require.NoError(t, err)
	require.Empty(t, valErrs)
	return root, prop
}

// TestIsRefSite_IncludesTheDegenerateRef pins the deliberately broad test. A
// position carrying a $ref field is $ref-shaped even when the ref is empty:
// callers here ask "is there a $ref-carrying body", not "is there a followable
// reference", and reading the narrower question would treat {$ref: ""} as an
// ordinary schema body.
func TestIsRefSite_IncludesTheDegenerateRef(t *testing.T) {
	t.Parallel()
	ref := schemaFromYAML(t, "{$ref: '#/$defs/T'}\n")
	assert.True(t, IsRefSite(ref, ref.GetSchema()))

	empty := schemaFromYAML(t, "{$ref: ''}\n")
	assert.True(t, IsRefSite(empty, empty.GetSchema()),
		"a present but empty $ref still carries a Ref field")

	plain := schemaFromYAML(t, "type: string\n")
	assert.False(t, IsRefSite(plain, plain.GetSchema()))
	assert.False(t, IsRefSite(plain, nil), "a boolean schema carries no body to inspect")
}

// TestTargetSchema_OnlyAResolvedReferenceHasOne pins the fallback readers rely
// on: a non-reference and an unresolved reference both yield nothing, so a
// use-site annotation never falls back to a referent that was never found.
func TestTargetSchema_OnlyAResolvedReferenceHasOne(t *testing.T) {
	t.Parallel()
	plain := schemaFromYAML(t, "type: string\n")
	assert.Nil(t, TargetSchema(plain, plain.GetSchema()), "not a reference")

	dangling := schemaFromYAML(t, "{$ref: '#/$defs/Missing'}\n")
	assert.Nil(t, TargetSchema(dangling, dangling.GetSchema()), "a reference that resolved to nothing")

	prop := resolvedProperty(t,
		"$defs:\n  Target: {type: string, title: fromTarget}\n"+
			"properties:\n  p: {$ref: '#/$defs/Target'}\n", "p")
	got := TargetSchema(prop, prop.GetSchema())
	require.NotNil(t, got, "a resolved reference exposes its target")
	assert.Equal(t, "fromTarget", got.GetTitle())
}

// TestNamesReferent_ADeclaredTargetOrAResolvedOne pins what counts as naming
// something this compilation can intern. A component the document declares
// counts; one it does not is a dangling reference; and a pointer deeper than a
// component counts only when it actually resolved to a schema body.
func TestNamesReferent_ADeclaredTargetOrAResolvedOne(t *testing.T) {
	t.Parallel()
	declaresUser := Scope{SelfPath: "spec.yaml", Declares: func(n string) bool { return n == "User" }}

	plain := schemaFromYAML(t, "type: string\n")
	assert.True(t, declaresUser.NamesReferent(plain, "#/components/schemas/User"),
		"a declared component is a target")
	assert.False(t, declaresUser.NamesReferent(plain, "#/components/schemas/Missing"),
		"an undeclared component is a dangling reference")
	assert.False(t, declaresUser.NamesReferent(plain, "other.yaml#/components/schemas/User"),
		"another document is not this compilation's to intern")
	assert.False(t, declaresUser.NamesReferent(plain, "Bare"),
		"a bare name addresses no pointer at all")

	root, prop := resolvedPropertyIn(t,
		"$defs:\n  Target: {type: string}\nproperties:\n  p: {$ref: '#/$defs/Target'}\n", "p")
	inRoot := declaresUser
	inRoot.Doc = root
	assert.True(t, inRoot.NamesReferent(prop, "#/$defs/Target"),
		"a sub-schema pointer counts once it resolved to a body")

	unresolved := schemaFromYAML(t, "{$ref: '#/$defs/Missing'}\n")
	assert.False(t, declaresUser.NamesReferent(unresolved, "#/$defs/Missing"),
		"a sub-schema pointer that resolved to nothing does not")
}

// TestTargetPointer_DocumentPartIsLeftToTheResolver pins TargetPointer's own
// guard: a "#/$defs/..." pointer spelled with an explicit document part (even
// one naming this same file) is not one load holds out of the resolver's pass
// (load.heldRefs matches only a $ref with no document part), so the resolver
// reads it itself and the rule has no answer for it.
func TestTargetPointer_DocumentPartIsLeftToTheResolver(t *testing.T) {
	t.Parallel()
	root := schemaFromYAML(t, "$defs:\n  Target: {type: string}\nproperties:\n  p: {type: string}\n")
	p, ok := root.GetSchema().GetProperties().Get("p")
	require.True(t, ok)

	scope := Scope{SelfPath: "spec.yaml", Doc: root}
	_, ok = scope.TargetPointer(p, "spec.yaml#/$defs/Target")
	assert.False(t, ok, "a document part, even this document's own name, is left to the resolver")
}

// TestTargetPointer_ReadsThroughTheReaderItIsGiven pins that a Scope given a
// reader reads "#/$defs/..." pointers through it: the answer is the one a Scope
// with only a document gives, and the second question about the same position
// probes the root and the one holder, and reads no position again.
func TestTargetPointer_ReadsThroughTheReaderItIsGiven(t *testing.T) {
	t.Parallel()
	root := schemaFromYAML(t, "properties:\n  outer:\n    $defs:\n      k: {type: string}\n    properties:\n      p: {$ref: '#/$defs/k'}\n")
	outer, ok := root.GetSchema().GetProperties().Get("outer")
	require.True(t, ok)
	p, ok := outer.GetSchema().GetProperties().Get("p")
	require.True(t, ok)
	reader := defs.NewReader(root)
	scope := Scope{SelfPath: "spec.yaml", Doc: root, Defs: reader}

	first, ok := scope.TargetPointer(p, "#/$defs/k")
	require.True(t, ok)
	afterFirst := reader.Reads()
	second, ok := scope.TargetPointer(p, "#/$defs/k")
	require.True(t, ok)

	assert.Equal(t, jsontext.Pointer("/properties/outer/$defs/k"), first)
	assert.Equal(t, first, second)
	assert.Equal(t, afterFirst+2, reader.Reads(), "the second question probed the root and the holder, and read no position again")
	plain, ok := Scope{SelfPath: "spec.yaml", Doc: root}.TargetPointer(p, "#/$defs/k")
	require.True(t, ok)
	assert.Equal(t, first, plain, "a scope with only a document answers the same")
}
