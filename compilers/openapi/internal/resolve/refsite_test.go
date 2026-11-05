// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package resolve_test

import (
	"encoding/json/jsontext"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/speakeasy-api/openapi/jsonschema/oas3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi/internal/bind"
	"github.com/dexpace/morphic/compilers/openapi/internal/ids"
	"github.com/dexpace/morphic/compilers/openapi/internal/load"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/compilers/openapi/internal/resolve"
	"github.com/dexpace/morphic/engine/reader"
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
// takes and into whichever document, so a $ref into another document has that
// document's schema; a non-reference, an unresolved reference and a loop have
// none, so a use-site annotation never falls back to a referent its type does
// not name. A scope that holds no bindings finds none either.
func TestScope_TargetSchema_TheEndOfTheChainOrNothing(t *testing.T) {
	t.Parallel()
	b := loadBound(t, externalDocument(t), openapitest.ComponentSpec(targetsSpec), load.Options{Reader: reader.Disk{}})
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
	end := b.scope.Bindings.At(e.Chain.End).GetSchema()
	assert.Same(t, end, b.scope.TargetSchema(other, other.GetSchema()), "a $ref into another document ends on E")

	via := b.component(t, "Via")
	assert.Nil(t, resolve.Scope{}.TargetSchema(via, via.GetSchema()), "no bindings, no referent")
}

// TestScope_NamesReferent_ADeclaredTargetOrAResolvedOne pins what counts as
// naming something this compilation can intern. A component the lowering
// declares counts; one it does not, one the document lacks and a bare name do
// not; a pointer deeper than a component counts only when it landed on a
// schema with a body or a boolean one; and so does a $ref into another
// document, which counts when it landed on one and not when it landed on none.
func TestScope_NamesReferent_ADeclaredTargetOrAResolvedOne(t *testing.T) {
	t.Parallel()
	const schemas = `
    User: {type: object}
    ToUser: {$ref: '#/components/schemas/User'}
    ToMissing: {$ref: '#/components/schemas/Missing'}
    ToOther: {$ref: 'ext.yaml#/components/schemas/E'}
    ToOtherMissing: {$ref: 'ext.yaml#/components/schemas/Missing'}
    ToBare: {$ref: Bare}
    Outer:
      $defs: {k: {type: string}, off: false}
      properties:
        sub: {$ref: '#/$defs/k'}
        boolean: {$ref: '#/$defs/off'}
        gone: {$ref: '#/$defs/gone'}
`
	path := externalDocument(t)
	opts := load.Options{Reader: reader.Disk{}}
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
		"other":      {b.component(t, "ToOther"), true, "a schema another document declares is a target"},
		"other gone": {b.component(t, "ToOtherMissing"), false, "another document that lacks the schema is a dangling reference"},
		"bare":       {b.component(t, "ToBare"), false, "a bare name addresses no pointer at all"},
		"sub-schema": {b.at(t, outer+"sub"), true, "a sub-schema pointer counts once it landed on a body"},
		"boolean":    {b.at(t, outer+"boolean"), true, "a boolean schema lowers as at its own position"},
		"no rule":    {b.at(t, outer+"gone"), false, "a sub-schema pointer that landed nowhere does not"},
	} {
		assert.Equal(t, tc.want, b.scope.NamesReferent(tc.js), "%s: %s", name, tc.why)
	}

	assert.False(t, none.scope.NamesReferent(none.component(t, "ToUser")),
		"a component the lowering does not declare is no target, though the $ref landed on it")
	assert.False(t, resolve.Scope{}.NamesReferent(b.component(t, "ToUser")), "no bindings, no target")
}

// TestIsRefSite_IncludesTheDegenerateRef pins the deliberately broad test. A
// position carrying a $ref field is $ref-shaped even when the ref is empty:
// callers here ask "is there a $ref-carrying body", not "is there a followable
// reference", and reading the narrower question would treat {$ref: ""} as an
// ordinary schema body.
func TestIsRefSite_IncludesTheDegenerateRef(t *testing.T) {
	t.Parallel()
	ref := openapitest.SchemaFromYAML(t.Context(), t, "{$ref: '#/$defs/T'}\n")
	assert.True(t, resolve.IsRefSite(ref, ref.GetSchema()))

	empty := openapitest.SchemaFromYAML(t.Context(), t, "{$ref: ''}\n")
	assert.True(t, resolve.IsRefSite(empty, empty.GetSchema()),
		"a present but empty $ref still carries a Ref field")

	plain := openapitest.SchemaFromYAML(t.Context(), t, "type: string\n")
	assert.False(t, resolve.IsRefSite(plain, plain.GetSchema()))
	assert.False(t, resolve.IsRefSite(plain, nil), "a boolean schema carries no body to inspect")
}

func TestResolveComponentRef(t *testing.T) {
	t.Parallel()
	sc := resolve.Scope{Declares: func(n string) bool { return n == "User" }}

	id, state := sc.ComponentRef("/components/schemas/User")
	assert.Equal(t, ids.NamedComponent, state)
	assert.Equal(t, ids.Document{}.NamedType("/components/schemas/User"), id)

	_, state = sc.ComponentRef("/components/schemas/Missing")
	assert.Equal(t, ids.UndeclaredComponent, state, "a name the document does not declare does not resolve")

	_, state = sc.ComponentRef("/components/schemas/")
	assert.Equal(t, ids.UnnamedComponent, state, "the component keyed \"\" is its own state, never folded into another")

	_, state = sc.ComponentRef("/components/schemas/Foo/properties/bar")
	assert.Equal(t, ids.NotComponent, state, "a sub-schema pointer is not a top-level component pointer")
}

// TestResolveComponentRef_NonCanonicalEscape pins that the resolved ID is built
// from the component's canonical name, so a $ref that escapes non-canonically
// (a raw '~' for a component named "A~B", interned under "A~0B") still resolves to
// the interned node rather than an unbacked ID (issue #14).
func TestResolveComponentRef_NonCanonicalEscape(t *testing.T) {
	t.Parallel()
	sc := resolve.Scope{Declares: func(n string) bool { return n == "A~B" }}

	id, state := sc.ComponentRef("/components/schemas/A~B")
	assert.Equal(t, ids.NamedComponent, state)
	assert.Equal(t, ids.Document{}.NamedType("/components/schemas/A~0B"), id,
		"the ID is canonically re-escaped to match the interned node")
	assert.Equal(t, ids.Document{}.NamedType(ids.Ptr("components", "schemas", "A~B")), id,
		"and equals the ID the component was interned under")
}
