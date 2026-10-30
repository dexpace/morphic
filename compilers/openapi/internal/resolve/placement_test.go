package resolve_test

import (
	"encoding/json/jsontext"
	"testing"

	"github.com/speakeasy-api/openapi/jsonschema/oas3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/bind"
	"github.com/dexpace/morphic/compilers/openapi/internal/load"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/compilers/openapi/internal/resolve"
	"github.com/dexpace/morphic/engine/reader"
)

// TestScope_Target_WhereTheCompilerResolvedTheRef pins where the lowering
// places what a $ref names as the compiler resolved it. A "#/$defs/..."
// pointer is the definition the $defs rule finds, the root's first and
// otherwise the nearest ancestor's; any other is the position it names; a
// document part naming the source reads from its root, as any other pointer
// does, not by the $defs rule; and one naming another document is the position
// it lands on there. A $ref that lands on no schema or on a loop has no
// target, nor does a schema the bindings do not know.
func TestScope_Target_WhereTheCompilerResolvedTheRef(t *testing.T) {
	t.Parallel()
	const schemas = `
    Target: {type: string}
    Direct: {$ref: '#/components/schemas/Target'}
    Spelled: {$ref: 'spec.yaml#/components/schemas/Target'}
    Chain: {$ref: '#/components/schemas/Direct'}
    Dangling: {$ref: '#/components/schemas/Missing'}
    LoopA: {$ref: '#/components/schemas/LoopB'}
    LoopB: {$ref: '#/components/schemas/LoopA'}
    ToLoop: {$ref: '#/components/schemas/LoopA'}
    Other: {$ref: 'ext.yaml#/components/schemas/E'}
    Outer:
      $defs: {k: {type: string}, shared: {type: integer}}
      properties:
        nearest: {$ref: '#/$defs/k'}
        rooted: {$ref: '#/$defs/shared'}
        withDocumentPart: {$ref: 'spec.yaml#/$defs/k'}
`
	path := externalDocument(t)
	b := loadBound(t, path, openapitest.ComponentSpec(schemas)+"$defs:\n  shared: {type: string}\n",
		load.Options{Reader: reader.Disk{}})
	outer := "/components/schemas/Outer/properties/"

	for name, tc := range map[string]struct {
		js   *schemaRef
		want string
	}{
		"direct":      {b.component(t, "Direct"), "/components/schemas/Target"},
		"spelled":     {b.component(t, "Spelled"), "/components/schemas/Target"},
		"one hop":     {b.component(t, "Chain"), "/components/schemas/Direct"},
		"nearest":     {b.at(t, outer+"nearest"), "/components/schemas/Outer/$defs/k"},
		"root first":  {b.at(t, outer+"rooted"), "/$defs/shared"},
		"doc part":    {b.at(t, outer+"withDocumentPart"), ""},
		"unresolved":  {b.component(t, "Dangling"), ""},
		"loop member": {b.component(t, "LoopA"), ""},
		"into loop":   {b.component(t, "ToLoop"), "/components/schemas/LoopA"},
		"no ref":      {b.component(t, "Target"), ""},
		"unknown":     {&schemaRef{}, ""},
	} {
		got, ok := b.scope.Target(tc.js)
		assert.Equal(t, tc.want != "", ok, name)
		assert.Equal(t, bind.Position{Pointer: jsontext.Pointer(tc.want)}, got, name)
	}

	other, ok := b.doc.Bindings.Entry(b.component(t, "Other"))
	require.True(t, ok)
	assert.Equal(t, bind.Resolved, other.Hop.Status, "the $ref into another document resolved")
	assert.False(t, other.Hop.Named, "but not in the source, whose pointers it spells")
	got, ok := b.scope.Target(b.component(t, "Other"))
	require.True(t, ok)
	assert.Equal(t, bind.Position{Doc: other.Hop.Target.Doc, Pointer: "/components/schemas/E"}, got,
		"the position it lands on in the other document")
	assert.NotEqual(t, bind.Source, got.Doc)

	_, ok = resolve.Scope{}.Target(b.component(t, "Direct"))
	assert.False(t, ok, "no bindings, no pointer")
}

// TestScope_MappingTarget_WhereTheCompilerResolvedTheValue pins where the
// lowering places what a discriminator mapping value names, read as a $ref
// written at the discriminator is, and the schema written there: a pointer or a
// component, a "#/$defs/..." value, which is the definition the nearest
// ancestor holds, and a value into another document, the position it lands on
// there. One a document part leaves reading from the root, one naming no
// definition and a bare name land on none, and neither does a missing
// discriminator, one the bindings do not know or a scope without them.
func TestScope_MappingTarget_WhereTheCompilerResolvedTheValue(t *testing.T) {
	t.Parallel()
	const schemas = `
    Cat: {type: object}
    Pet:
      oneOf: [{$ref: '#/components/schemas/Cat'}]
      $defs: {dog: {type: object}}
      discriminator:
        propertyName: kind
        mapping:
          cat: '#/components/schemas/Cat'
          dog: '#/$defs/dog'
          other: 'ext.yaml#/components/schemas/E'
          documentPart: 'spec.yaml#/$defs/dog'
          missing: '#/$defs/missing'
          bare: Cat
`
	b := loadBound(t, externalDocument(t), openapitest.ComponentSpec(schemas), load.Options{Reader: reader.Disk{}})
	d := b.component(t, "Pet").GetSchema().GetDiscriminator()
	require.NotNil(t, d)

	for value, want := range map[string]string{
		"#/components/schemas/Cat": "/components/schemas/Cat",
		"#/$defs/dog":              "/components/schemas/Pet/$defs/dog",
	} {
		got, decl, ok := b.scope.MappingTarget(d, value)
		assert.True(t, ok, value)
		assert.Equal(t, bind.Position{Pointer: jsontext.Pointer(want)}, got, value)
		assert.Same(t, b.at(t, want), decl, "%s: the schema written there", value)
	}
	other, decl, ok := b.scope.MappingTarget(d, "ext.yaml#/components/schemas/E")
	require.True(t, ok, "a value into another document lands there")
	assert.NotEqual(t, bind.Source, other.Doc)
	assert.Equal(t, jsontext.Pointer("/components/schemas/E"), other.Pointer)
	assert.Same(t, b.doc.Bindings.At(other), decl, "the schema held there")

	for value, why := range map[string]string{
		"spec.yaml#/$defs/dog":           "a document part reads from the root, which holds no dog",
		"#/$defs/missing":                "the $defs rule finds no such definition",
		"Cat":                            "a bare name is a component's name, read as no $ref",
		"#/components/schemas/Unwritten": "the table read no such value",
	} {
		got, decl, ok := b.scope.MappingTarget(d, value)
		assert.False(t, ok, "%s: %s", value, why)
		assert.Zero(t, got, value)
		assert.Nil(t, decl, value)
	}

	_, _, ok = b.scope.MappingTarget(nil, "#/components/schemas/Cat")
	assert.False(t, ok, "no discriminator to read a position from")
	_, _, ok = b.scope.MappingTarget(&oas3.Discriminator{}, "#/components/schemas/Cat")
	assert.False(t, ok, "a discriminator the bindings do not know")
	_, _, ok = resolve.Scope{}.MappingTarget(d, "#/components/schemas/Cat")
	assert.False(t, ok, "no bindings, no target")
}
