package resolve_test

import (
	"encoding/json/jsontext"
	"os"
	"path/filepath"
	"slices"
	"testing"

	oas3 "github.com/speakeasy-api/openapi/jsonschema/oas3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi/internal/bind"
	"github.com/dexpace/morphic/compilers/openapi/internal/load"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/compilers/openapi/internal/resolve"
)

// schemaRef is the schema type the lowering holds.
type schemaRef = oas3.JSONSchema[oas3.Referenceable]

// bound is a loaded source with the scope lowering reads it in.
type bound struct {
	doc   *load.Document
	scope resolve.Scope
}

// loadBound loads src as the source at path and returns it with a scope over
// its bindings, which declares the components named in declared. Load reports
// the $refs src leaves unresolved, and those are the fixtures, so its
// diagnostics are not asserted here.
func loadBound(t *testing.T, path, src string, opts load.Options, declared ...string) bound {
	t.Helper()
	doc, _, err := load.Load(t.Context(), 0, compilers.Source{Path: path, Data: []byte(src)}, opts)
	require.NoError(t, err)
	require.NotNil(t, doc)
	require.NotNil(t, doc.Bindings, "the source is bound")
	return bound{doc: doc, scope: resolve.Scope{
		SelfPath: path,
		Doc:      doc.Doc,
		Bindings: doc.Bindings,
		Declares: func(name string) bool { return slices.Contains(declared, name) },
	}}
}

// at returns the schema the bindings hold at pointer in the source.
func (b bound) at(t *testing.T, pointer string) *schemaRef {
	t.Helper()
	js := b.doc.Bindings.At(bind.Position{Pointer: jsontext.Pointer(pointer)})
	require.NotNil(t, js, "the source holds a schema at %s", pointer)
	return js
}

// component returns the component schema called name.
func (b bound) component(t *testing.T, name string) *schemaRef {
	t.Helper()
	js, ok := b.doc.Doc.Components.Schemas.Get(name)
	require.True(t, ok, "the fixture declares component %q", name)
	return js
}

// targetsSpec declares schemas whose $refs end on a schema, on nothing, on a
// loop and in another document, for the readers of where a $ref lands.
const targetsSpec = `
    Plain: {type: string}
    Target: {type: string, title: fromTarget}
    Via: {$ref: '#/components/schemas/Target'}
    Chain: {$ref: '#/components/schemas/Via'}
    Dangling: {$ref: '#/components/schemas/Missing'}
    LoopA: {$ref: '#/components/schemas/LoopB'}
    LoopB: {$ref: '#/components/schemas/LoopA'}
    Other: {$ref: 'ext.yaml#/components/schemas/E'}
`

// externalDocument writes ext.yaml, which targetsSpec names, beside a spec in
// a directory of its own and returns the path the spec is loaded by.
func externalDocument(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	body := "components:\n  schemas:\n    E: {type: string, title: external}\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "ext.yaml"), []byte(body), 0o600))
	return filepath.Join(dir, "spec.yaml")
}

// TestScope_TargetSchema_TheEndOfTheChainOrNothing pins the fallback readers
// rely on: a $ref has the schema its chain ends on, however many hops that
// takes; a non-reference, an unresolved reference, a loop and a $ref into
// another document, which the lowering lowers nothing of, have none, so a
// use-site annotation never falls back to a referent its type does not name. A
// scope that holds no bindings finds none either.
func TestScope_TargetSchema_TheEndOfTheChainOrNothing(t *testing.T) {
	t.Parallel()
	b := loadBound(t, externalDocument(t), openapitest.ComponentSpec(targetsSpec), load.Options{AllowExternalRefs: true})
	target := b.component(t, "Target").GetSchema()

	for name, want := range map[string]*oas3.Schema{
		"Plain":    nil,
		"Dangling": nil,
		"LoopA":    nil,
		"Via":      target,
		"Chain":    target,
	} {
		js := b.component(t, name)
		got := b.scope.TargetSchema(js, js.GetSchema())
		if want == nil {
			assert.Nil(t, got, name)
			continue
		}
		assert.Same(t, want, got, "%s ends on Target", name)
	}

	other := b.component(t, "Other")
	e, ok := b.scope.Bindings.Entry(other)
	require.True(t, ok)
	require.Equal(t, "external", b.scope.Bindings.At(e.Chain.End).GetSchema().GetTitle(), "the chain ends there")
	assert.Nil(t, b.scope.TargetSchema(other, other.GetSchema()), "a $ref into another document")

	via := b.component(t, "Via")
	assert.Nil(t, resolve.Scope{}.TargetSchema(via, via.GetSchema()), "no bindings, no referent")
}

// TestScope_NamesReferent_ADeclaredTargetOrAResolvedOne pins what counts as
// naming something this compilation can intern. A component the lowering
// declares counts; one it does not, one the document lacks, another document
// and a bare name do not; and a pointer deeper than a component counts only
// when it landed on a schema with a body.
func TestScope_NamesReferent_ADeclaredTargetOrAResolvedOne(t *testing.T) {
	t.Parallel()
	const schemas = `
    User: {type: object}
    ToUser: {$ref: '#/components/schemas/User'}
    ToMissing: {$ref: '#/components/schemas/Missing'}
    ToOther: {$ref: 'ext.yaml#/components/schemas/E'}
    ToBare: {$ref: Bare}
    Outer:
      $defs: {k: {type: string}, off: false}
      properties:
        sub: {$ref: '#/$defs/k'}
        boolean: {$ref: '#/$defs/off'}
        gone: {$ref: '#/$defs/gone'}
`
	path := externalDocument(t)
	opts := load.Options{AllowExternalRefs: true}
	b := loadBound(t, path, openapitest.ComponentSpec(schemas), opts, "User", "Outer")
	none := loadBound(t, path, openapitest.ComponentSpec(schemas), opts)
	outer := "/components/schemas/Outer/properties/"

	for name, tc := range map[string]struct {
		js   *schemaRef
		want bool
		why  string
	}{
		"declared":   {b.component(t, "ToUser"), true, "a declared component is a target"},
		"missing":    {b.component(t, "ToMissing"), false, "an undeclared component is a dangling reference"},
		"other":      {b.component(t, "ToOther"), false, "another document is not this compilation's to intern"},
		"bare":       {b.component(t, "ToBare"), false, "a bare name addresses no pointer at all"},
		"sub-schema": {b.at(t, outer+"sub"), true, "a sub-schema pointer counts once it landed on a body"},
		"boolean":    {b.at(t, outer+"boolean"), false, "a boolean schema has no body to intern"},
		"no rule":    {b.at(t, outer+"gone"), false, "a sub-schema pointer that landed nowhere does not"},
	} {
		assert.Equal(t, tc.want, b.scope.NamesReferent(tc.js), "%s: %s", name, tc.why)
	}

	assert.False(t, none.scope.NamesReferent(none.component(t, "ToUser")),
		"a component the lowering does not declare is no target, though the $ref landed on it")
	assert.False(t, resolve.Scope{}.NamesReferent(b.component(t, "ToUser")), "no bindings, no target")
}

// TestScope_TargetPointer_WhereTheCompilerResolvedTheRef pins the pointer a
// $ref lands on as the compiler resolved it. A "#/$defs/..." pointer is the
// definition the $defs rule finds, the root's first and otherwise the nearest
// ancestor's; any other is the position it names; and a document part naming
// the source reads from its root, as any other pointer does, not by the $defs
// rule. A $ref that lands on no schema, in another document or on a loop has no
// pointer, nor does a schema the bindings do not know.
func TestScope_TargetPointer_WhereTheCompilerResolvedTheRef(t *testing.T) {
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
		load.Options{AllowExternalRefs: true})
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
		"other doc":   {b.component(t, "Other"), ""},
		"no ref":      {b.component(t, "Target"), ""},
		"unknown":     {&schemaRef{}, ""},
	} {
		got, ok := b.scope.TargetPointer(tc.js)
		assert.Equal(t, tc.want != "", ok, name)
		assert.Equal(t, tc.want, string(got), name)
	}

	other, ok := b.doc.Bindings.Entry(b.component(t, "Other"))
	require.True(t, ok)
	assert.Equal(t, bind.Resolved, other.Hop.Status, "the $ref into another document resolved")
	assert.False(t, other.Hop.Named, "but not in the source, whose pointers it spells")

	_, ok = resolve.Scope{}.TargetPointer(b.component(t, "Direct"))
	assert.False(t, ok, "no bindings, no pointer")
}

// TestScope_MappingTarget_WhereTheCompilerResolvedTheValue pins the pointer a
// discriminator mapping value names, read as a $ref written at the
// discriminator is, and the schema written there: a pointer or a component, and
// a "#/$defs/..." value, which is the definition the nearest ancestor holds. A
// value into another document, one a document part leaves reading from the
// root, one naming no definition and a bare name land on none, and neither does
// a missing discriminator, one the bindings do not know or a scope without them.
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
	b := loadBound(t, externalDocument(t), openapitest.ComponentSpec(schemas), load.Options{AllowExternalRefs: true})
	d := b.component(t, "Pet").GetSchema().GetDiscriminator()
	require.NotNil(t, d)

	for value, want := range map[string]string{
		"#/components/schemas/Cat": "/components/schemas/Cat",
		"#/$defs/dog":              "/components/schemas/Pet/$defs/dog",
	} {
		got, decl, ok := b.scope.MappingTarget(d, value)
		assert.True(t, ok, value)
		assert.Equal(t, want, string(got), value)
		assert.Same(t, b.at(t, want), decl, "%s: the schema written there", value)
	}

	for value, why := range map[string]string{
		"ext.yaml#/components/schemas/E": "a value into another document is not the source's",
		"spec.yaml#/$defs/dog":           "a document part reads from the root, which holds no dog",
		"#/$defs/missing":                "the $defs rule finds no such definition",
		"Cat":                            "a bare name is a component's name, read as no $ref",
		"#/components/schemas/Unwritten": "the table read no such value",
	} {
		got, decl, ok := b.scope.MappingTarget(d, value)
		assert.False(t, ok, "%s: %s", value, why)
		assert.Empty(t, got, value)
		assert.Nil(t, decl, value)
	}

	_, _, ok := b.scope.MappingTarget(nil, "#/components/schemas/Cat")
	assert.False(t, ok, "no discriminator to read a position from")
	_, _, ok = b.scope.MappingTarget(&oas3.Discriminator{}, "#/components/schemas/Cat")
	assert.False(t, ok, "a discriminator the bindings do not know")
	_, _, ok = resolve.Scope{}.MappingTarget(d, "#/components/schemas/Cat")
	assert.False(t, ok, "no bindings, no target")
}
