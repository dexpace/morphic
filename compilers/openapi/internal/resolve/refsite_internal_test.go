package resolve

import (
	"strings"
	"testing"

	oas3 "github.com/speakeasy-api/openapi/jsonschema/oas3"
	"github.com/speakeasy-api/openapi/marshaller"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
	root := schemaFromYAML(t, body)
	prop, ok := root.GetSchema().GetProperties().Get(p)
	require.True(t, ok, "the fixture declares property %q", p)
	valErrs, err := prop.Resolve(t.Context(), oas3.ResolveOptions{
		RootDocument: root, TargetLocation: "spec.yaml", DisableExternalRefs: true,
	})
	require.NoError(t, err)
	require.Empty(t, valErrs)
	return prop
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

	prop := resolvedProperty(t,
		"$defs:\n  Target: {type: string}\nproperties:\n  p: {$ref: '#/$defs/Target'}\n", "p")
	assert.True(t, declaresUser.NamesReferent(prop, "#/$defs/Target"),
		"a sub-schema pointer counts once it resolved to a body")

	unresolved := schemaFromYAML(t, "{$ref: '#/$defs/Missing'}\n")
	assert.False(t, declaresUser.NamesReferent(unresolved, "#/$defs/Missing"),
		"a sub-schema pointer that resolved to nothing does not")
}
