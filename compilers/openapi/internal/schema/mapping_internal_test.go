// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package schema

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/speakeasy-api/openapi/jsonschema/oas3"
	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi/internal/bind"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/ids"
	"github.com/dexpace/morphic/compilers/openapi/internal/load"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/engine/reader"
	"github.com/dexpace/morphic/ir"
)

// TestChainOf_EndsWhereTheValueNames pins where a mapping value's chain ends,
// by the ID of the type declared there: a component named by a $ref or by name,
// a name holding '/' among them (issue #14, f07), and the component keyed by
// the empty string, which is interned anonymously, not under a named ID
// nothing backs (issue #14, f31). An undeclared component, an external
// reference no compile here reads, and any value of a discriminator the
// bindings do not know resolve to no schema.
func TestChainOf_EndsWhereTheValueNames(t *testing.T) {
	t.Parallel()
	l, _ := LoweredFor(t, openapitest.ComponentSpec(`    Cat: {type: object}
    Dog: {type: object}
    A/B: {type: object}
    '': {type: object}
    Pet:
      type: object
      discriminator:
        propertyName: k
        mapping: {c: '#/components/schemas/Cat', g: '#/components/schemas/Ghost', e: 'a.yaml#/A'}
`))
	d := petDiscriminator(t, l)
	for value, want := range map[string]ir.TypeID{
		"#/components/schemas/Cat": ids.Document{}.NamedType("/components/schemas/Cat"),
		"Dog":                      ids.Document{}.NamedType(ids.Ptr("components", "schemas", "Dog")),
		"A/B":                      ids.Document{}.NamedType(ids.Ptr("components", "schemas", "A/B")),
		"":                         ids.Document{}.AnonType(ids.Ptr("components", "schemas", "")),
	} {
		chain, ok := chainOf(l.Ctx, d, value)
		require.True(t, ok, value)
		assert.False(t, chain.broken, value)
		assert.Equal(t, want, chain.end().id, value)
	}
	for _, value := range []string{"#/components/schemas/Ghost", "a.yaml#/A"} {
		_, ok := chainOf(l.Ctx, d, value)
		assert.False(t, ok, "%s resolves to no schema", value)
	}
	_, ok := chainOf(l.Ctx, &oas3.Discriminator{}, "#/components/schemas/Cat")
	assert.False(t, ok, "a discriminator the document does not hold maps nothing")
}

// TestDeclaredAt_NamesNoUndeclaredComponentOrUnreadDocument pins the
// positions no route can name: a pointer naming a component the source does
// not declare, which a named ID nothing backs would answer, a position holding
// no schema, and a position of a document the bindings read and the compile
// does not list beside the source, whose IDs it cannot qualify. A value naming
// the last resolves to no schema.
func TestDeclaredAt_NamesNoUndeclaredComponentOrUnreadDocument(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "ext.yaml"), []byte("X: {type: object}\n"), 0o600))
	loaded, _, err := load.Load(t.Context(), 0, compilers.Source{Path: filepath.Join(dir, "root.yaml"),
		Data: []byte(openapitest.ComponentSpec("    Pet: {discriminator: {propertyName: k, mapping: {x: './ext.yaml#/X'}}}\n"))},
		load.Options{Reader: reader.Disk{}})
	require.NoError(t, err)
	require.NotNil(t, loaded)
	unlisted := *loaded
	unlisted.Others = load.Others{}
	c := lowering.New(0, &unlisted, lowering.Policy{})
	js, ok := c.Doc.Components.GetSchemas().Get("Pet")
	require.True(t, ok)
	d := js.GetSchema().GetDiscriminator()
	at, decl, ok := c.RefScope().MappingTarget(d, "./ext.yaml#/X")
	require.True(t, ok, "the bindings read ext.yaml")

	_, ok = declaredAt(c, at, decl)
	assert.False(t, ok, "a document the compile does not list")
	_, ok = chainOf(c, d, "./ext.yaml#/X")
	assert.False(t, ok)
	_, ok = declaredAt(c, bind.Position{Pointer: "/components/schemas/Ghost"}, nil)
	assert.False(t, ok, "an undeclared component")
	_, ok = declaredAt(c, bind.Position{Pointer: "/x-none"}, nil)
	assert.False(t, ok, "no schema")
}

func TestDiscriminatorDefault_ResolvesDeclaredComponent(t *testing.T) {
	t.Parallel()
	l := NewRawLowerer(openapitest.DocDeclaring("Cat"))
	d := &oas3.Discriminator{PropertyName: "kind", DefaultMapping: new("Cat")}

	id, diags := discriminatorDefault(l.Ctx, l.Types, d, nil, "/components/schemas/Pet")
	assert.Equal(t, ids.Document{}.NamedType("/components/schemas/Cat"), id)
	assert.Empty(t, diags, "a resolvable defaultMapping produces no diagnostic")
}

func TestDiscriminatorDefault_DroppedWhenUnresolved(t *testing.T) {
	t.Parallel()
	l := NewRawLowerer(&soa.OpenAPI{})
	// "Missing" is neither a declared component nor a reference the bindings
	// read, so the defaultMapping does not resolve and is dropped with one error.
	d := &oas3.Discriminator{PropertyName: "kind", DefaultMapping: new("Missing")}

	id, diags := discriminatorDefault(l.Ctx, l.Types, d, nil, "/components/schemas/Pet")
	assert.Empty(t, id, "an unresolved defaultMapping yields no target")
	require.Len(t, diags, 1)
	assert.Equal(t, diag.UnresolvedRef, diags[0].Code)
}

func TestDiscriminatorDefault_EmptyIsNoOp(t *testing.T) {
	t.Parallel()
	l := NewRawLowerer(&soa.OpenAPI{})
	id, diags := discriminatorDefault(l.Ctx, l.Types, &oas3.Discriminator{PropertyName: "kind"}, nil, "/components/schemas/Pet")
	assert.Empty(t, id)
	assert.Empty(t, diags)
}

// mappingTargetSpec declares a discriminator whose mapping names each kind of
// value routeEntry answers for.
const mappingTargetSpec = `    Cat: {type: string}
    Pet:
      type: object
      properties: {kind: {type: string}}
      discriminator:
        propertyName: kind
        mapping:
          c: '#/components/schemas/Cat'
          o: 'other.yaml#/A'
          g: '#/components/schemas/Ghost'
          m: '#/components/schemas/Pet/properties/missing'
          k: '#/components/schemas/Pet/properties/kind'
`

// TestRouteEntry_PromisesWhatAModelRoutesTo pins each route routeEntry gives a
// model's value that resolves: a declared component, by name or by $ref, and a
// position, a bare scalar property included, by the ID its pointer derives,
// as declaredAt names it, its node promised: its model carries it, so it
// interns no node of its own until every declaration has lowered.
func TestRouteEntry_PromisesWhatAModelRoutesTo(t *testing.T) {
	t.Parallel()
	l, _ := LoweredFor(t, openapitest.ComponentSpec(mappingTargetSpec))
	d := petDiscriminator(t, l)
	route := func(value string) (ir.TypeID, bool) {
		id, ok, diags := routeEntry(l.Ctx, l.Types, d, nil, value, "discriminator mapping", "/x")
		assert.Empty(t, diags, value)
		return id, ok
	}

	cat := ids.Document{}.NamedType(ids.Ptr("components", "schemas", "Cat"))
	for _, value := range []string{"Cat", "#/components/schemas/Cat"} {
		id, ok := route(value)
		assert.True(t, ok, "%s names a declared component", value)
		assert.Equal(t, cat, id)
	}

	const scalar = "#/components/schemas/Pet/properties/kind"
	kind := ids.Document{}.ForPointer("/components/schemas/Pet/properties/kind")
	chain, ok := chainOf(l.Ctx, d, scalar)
	require.True(t, ok)
	assert.Equal(t, kind, chain.end().id, "an un-interned scalar position is named by its pointer")

	id, ok := route(scalar)
	require.True(t, ok, "routeEntry promises it")
	assert.Equal(t, kind, id)
	_, interned := l.Types.Node(id)
	assert.False(t, interned, "a promise builds nothing until every declaration has lowered")
	_, finished := l.Types.Finish(t.Context())
	assert.Empty(t, finished)
	_, interned = l.Types.Node(id)
	assert.True(t, interned, "the hoisted alias is registered in the type registry")
}

// TestRouteEntry_ReportsAndPromisesNothingForAValueNamingNoSchema pins the
// values routeEntry refuses: an external reference, an undeclared component
// and a position the document does not declare, each reported once at its
// entry. Nothing else is lowered, so the registry Finish hands over holds
// whatever a refusal interned or promised, and it must be empty.
func TestRouteEntry_ReportsAndPromisesNothingForAValueNamingNoSchema(t *testing.T) {
	t.Parallel()
	l, _ := LoweredFor(t, openapitest.ComponentSpec(mappingTargetSpec))
	d := petDiscriminator(t, l)
	for value, why := range map[string]string{
		"other.yaml#/A":                               "an external reference never resolves",
		"#/components/schemas/Ghost":                  "an undeclared component is refused, not hoisted as an inline position",
		"#/components/schemas/Pet/properties/missing": "a position the document does not declare does not resolve",
	} {
		_, ok, diags := routeEntry(l.Ctx, l.Types, d, nil, value, "discriminator mapping", "/x")
		assert.False(t, ok, why)
		require.Len(t, diags, 1, why)
		assert.Equal(t, diag.UnresolvedRef, diags[0].Code, why)
	}
	reg, finished := l.Types.Finish(t.Context())
	assert.Empty(t, finished)
	assert.Empty(t, reg, "a refused value interns and promises nothing")
}

// TestDeclaredAt_NamesAPositionByItsPointerBeforeItIsInterned pins the ID of
// the type a value naming a position deeper than a component routes to: the
// one its pointer derives, read from the bindings alone, before anything is
// interned there and after alike, so no tag turns on which schema lowered
// first. A value the bindings land nowhere is unresolved.
func TestDeclaredAt_NamesAPositionByItsPointerBeforeItIsInterned(t *testing.T) {
	t.Parallel()
	const (
		sub     = "#/components/schemas/Pet/properties/kind"
		missing = "#/components/schemas/Pet/properties/missing"
	)

	l, _ := LoweredFor(t, openapitest.ComponentSpec("    Pet:\n      type: object\n"+
		"      properties: {kind: {type: object, properties: {a: {type: string}}}}\n"+
		"      discriminator: {propertyName: k, mapping: {s: '"+sub+"', m: '"+missing+"'}}\n"))
	_, ok := chainOf(l.Ctx, petDiscriminator(t, l), missing)
	assert.False(t, ok, "the bindings land the value nowhere, so it does not resolve")
	want := ids.Document{}.ForPointer("/components/schemas/Pet/properties/kind")
	chain, ok := chainOf(l.Ctx, petDiscriminator(t, l), sub)
	require.True(t, ok)
	assert.Equal(t, want, chain.end().id, "nothing is interned yet, and the position is named all the same")

	l.Diags.AppendAll(LowerComponentSchemas(t.Context(), l.Ctx, l.Types))
	_, finished := l.Types.Finish(t.Context())
	l.Diags.AppendAll(finished)
	after, ok := declaredAt(l.Ctx, chain.end().at, chain.end().decl)
	require.True(t, ok)
	assert.Equal(t, want, after, "the node interned there has the ID it was named by")
	_, interned := l.Types.Node(want)
	assert.True(t, interned)
}

// TestChainOf_ReadsEachPositionOnce pins that the chain from the position a
// mapping value names is read once a compile (lowering.Remembered), not once
// for each value or each union naming it.
func TestChainOf_ReadsEachPositionOnce(t *testing.T) {
	t.Parallel()
	l, _ := LoweredFor(t, openapitest.ComponentSpec("    Cat: {type: object}\n"+
		"    Hold: {type: object, properties: {p: {$ref: '#/components/schemas/Cat'}}}\n"+
		"    Pet: {discriminator: {propertyName: k, mapping: {p: '#/components/schemas/Hold/properties/p'}}}\n"))

	chain, ok := chainOf(l.Ctx, petDiscriminator(t, l), "#/components/schemas/Hold/properties/p")
	require.True(t, ok)
	assert.Equal(t, ids.Document{}.NamedType("/components/schemas/Cat"), chain.end().id, "the position reads through to Cat")
	held := bind.Position{Pointer: "/components/schemas/Hold/properties/p"}
	again := lowering.Remembered(l.Ctx, chainFrom{held, l.Ctx.Bindings().At(held)}, func() valueChain {
		t.Error("the position was read again")
		return valueChain{}
	})
	assert.Equal(t, chain, again)
}

// TestMappingWhy_SaysBothReadingsOfAValueWithNoHash pins the reason a mapping
// value holding no '#' gives where it resolves to nothing: it names no
// component, and read as a URI reference it fails, for the reason its hop
// gives, not the table's, whose merge bound cut a read elsewhere in the
// document. One no table read says it names no schema.
func TestMappingWhy_SaysBothReadingsOfAValueWithNoHash(t *testing.T) {
	t.Parallel()
	var deep strings.Builder
	deep.WriteString("openapi: 3.1.0\ninfo: {title: t, version: '1'}\npaths: {}\nx-c0: &c0 {$ref: '#/components/schemas/T'}\n")
	for i := 1; i <= 70; i++ {
		fmt.Fprintf(&deep, "x-c%d: &c%d {<<: *c%d}\n", i, i, i-1)
	}
	deep.WriteString("components:\n  schemas:\n    T: {type: string}\n    A: {<<: *c70}\n" +
		"    P: {oneOf: [{$ref: '#/components/schemas/T'}], discriminator: {propertyName: k, mapping: {u: Undeclared}}}\n")
	l, _ := LoweredFor(t, deep.String())
	require.True(t, l.Ctx.Bindings().Table().Truncated(), "the merge bound cut a read")
	p, ok := l.Ctx.Doc.Components.GetSchemas().Get("P")
	require.True(t, ok)
	d := p.GetSchema().GetDiscriminator()
	require.NotNil(t, d)
	assert.Equal(t, ": the entry document declares no schema component of that name, and as a URI reference "+
		"it names another document, and external references are not read", mappingWhy(l.Ctx, d, "Undeclared"))
	assert.Equal(t, ": the entry document declares no schema component of that name, and as a URI reference "+
		"it names no schema", mappingWhy(NewRawLowerer(&soa.OpenAPI{}).Ctx, &oas3.Discriminator{}, "Undeclared"))
}

// TestMappingWhy_GivesAnUnreadReferenceTheTablesReason pins that a mapping
// value spelling a reference, which the table reads, takes the table's reason
// where it went unread: P's discriminator sits past a merge chain deeper than
// the compiler expands, so the binder never read its mapping. A value holding
// no '#' says the name it may spell names no component, too.
func TestMappingWhy_GivesAnUnreadReferenceTheTablesReason(t *testing.T) {
	t.Parallel()
	var deep strings.Builder
	deep.WriteString("openapi: 3.1.0\ninfo: {title: t, version: '1'}\npaths: {}\n" +
		"x-c0: &c0 {oneOf: [{$ref: '#/components/schemas/T'}], " +
		"discriminator: {propertyName: k, mapping: {t: '#/components/schemas/T', n: Nowhere}}}\n")
	for i := 1; i <= 70; i++ {
		fmt.Fprintf(&deep, "x-c%d: &c%d {<<: *c%d}\n", i, i, i-1)
	}
	deep.WriteString("components:\n  schemas:\n    T: {properties: {k: {type: string}}}\n    P: {<<: *c70}\n")
	l, _ := LoweredFor(t, deep.String())
	p, ok := l.Ctx.Doc.Components.GetSchemas().Get("P")
	require.True(t, ok)
	d := p.GetSchema().GetDiscriminator()
	require.NotNil(t, d, "the parser expands the whole chain")
	const unread = "it is past a `<<` merge chain deeper than the 64 levels the compiler expands"
	assert.Equal(t, ": "+unread, mappingWhy(l.Ctx, d, "#/components/schemas/T"))
	assert.Equal(t, ": the entry document declares no schema component of that name, and as a URI reference "+unread,
		mappingWhy(l.Ctx, d, "Nowhere"))
}

// TestMappingTagsFor_ReadsTheMappingOnce pins what a subtype asking for its
// tags costs: the base's whole mapping is read once a run, into the keys each
// type it routes to takes, rather than once for each of its subtypes, which
// made a mapping of n subtypes cost n² reads. The first ask reads all n.
func TestMappingTagsFor_ReadsTheMappingOnce(t *testing.T) {
	t.Parallel()
	const n = 8
	var spec strings.Builder
	spec.WriteString("    Pet:\n      type: object\n      discriminator:\n        propertyName: kind\n        mapping:\n")
	for i := range n {
		fmt.Fprintf(&spec, "          t%d: '#/components/schemas/S%d'\n", i, i)
	}
	for i := range n {
		fmt.Fprintf(&spec, "    S%d: {allOf: [{$ref: '#/components/schemas/Pet'}]}\n", i)
	}
	l, _ := LoweredFor(t, openapitest.ComponentSpec(spec.String()))
	d := petDiscriminator(t, l)

	assert.Equal(t, []string{"t3"}, mappingTagsFor(l.Ctx, d, ids.Document{}.NamedType("/components/schemas/S3")))
	inverted := lowering.Remembered(l.Ctx, tagsOf{d}, func() map[ir.TypeID][]string {
		t.Error("the mapping was read again")
		return nil
	})
	assert.Len(t, inverted, n, "every type the mapping routes to, from the one read")
}
