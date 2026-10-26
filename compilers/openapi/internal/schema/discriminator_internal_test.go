package schema

import (
	"fmt"
	"strings"
	"testing"

	oas3 "github.com/speakeasy-api/openapi/jsonschema/oas3"
	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/bind"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/ids"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/compilers/openapi/internal/overlay"
	"github.com/dexpace/morphic/ir"
)

func TestPropIDByName_NotFound(t *testing.T) {
	t.Parallel()
	m := &ir.Model{Properties: []ir.Property{{ID: "p1", Name: ir.Naming{Source: "a"}}}}
	_, ok := propIDByName(m, "missing")
	assert.False(t, ok)
	id, ok := propIDByName(m, "a")
	assert.True(t, ok)
	assert.Equal(t, ir.PropID("p1"), id)
}

func TestMappingTargetID(t *testing.T) {
	t.Parallel()
	l, _ := LoweredFor(t, openapitest.ComponentSpec(`    Cat: {type: object}
    Dog: {type: object}
    A/B: {type: object}
    Pet:
      type: object
      discriminator:
        propertyName: k
        mapping: {c: '#/components/schemas/Cat', g: '#/components/schemas/Ghost', e: 'a.yaml#/A'}
`))
	d := petDiscriminator(t, l)
	// A $ref to a declared component.
	id, ok := mappingTargetID(l.Ctx, l.Ctx.RefScope(), d, "#/components/schemas/Cat")
	require.True(t, ok)
	assert.Equal(t, ids.Document{}.NamedType("/components/schemas/Cat"), id)
	// A bare schema name.
	id, ok = mappingTargetID(l.Ctx, l.Ctx.RefScope(), d, "Dog")
	require.True(t, ok)
	assert.Equal(t, ids.Document{}.NamedType(ids.Ptr("components", "schemas", "Dog")), id)
	// A bare name that contains '/' but names an existing schema must resolve, not
	// dangle as a misclassified external $ref (issue #14, f07).
	id, ok = mappingTargetID(l.Ctx, l.Ctx.RefScope(), d, "A/B")
	require.True(t, ok)
	assert.Equal(t, ids.Document{}.NamedType(ids.Ptr("components", "schemas", "A/B")), id)
	// An undeclared component and a genuine external ref are dropped, never
	// synthesized into a dangling ID.
	_, ok = mappingTargetID(l.Ctx, l.Ctx.RefScope(), d, "#/components/schemas/Ghost")
	assert.False(t, ok, "undeclared component target dropped")
	_, ok = mappingTargetID(l.Ctx, l.Ctx.RefScope(), d, "a.yaml#/A")
	assert.False(t, ok, "external target dropped")
	_, ok = mappingTargetID(l.Ctx, l.Ctx.RefScope(), &oas3.Discriminator{}, "#/components/schemas/Cat")
	assert.False(t, ok, "a discriminator the document does not hold maps nothing")
	// A declared but empty-named component ("") is interned anonymously, so its
	// bare mapping name must resolve to that anon ID, not an unbacked ids.NamedType
	// (issue #14, f31). It gets a context of its own rather than being added to the
	// one above: the declared set is derived from the document now, so saying "and
	// also this one" means saying it to a document.
	empty := lowering.New(0, openapitest.DocDeclaring(""), ir.SourceInfo{}, "", lowering.Limits{}, lowering.StreamingMedia{}, lowering.ExtensionPromotions{}, lowering.NullableWidening{}, overlay.Origin{})
	id, ok = mappingTargetID(empty, empty.RefScope(), &oas3.Discriminator{}, "")
	require.True(t, ok)
	assert.Equal(t, ids.Document{}.AnonType(ids.Ptr("components", "schemas", "")), id)
	assert.NotEqual(t, ids.Document{}.NamedType(ids.Ptr("components", "schemas", "")), id)
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
	// "Missing" is neither a declared component nor an internal pointer, so the
	// defaultMapping does not resolve and is dropped with one error diagnostic.
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
// target resolveMappingTarget answers for.
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

// TestResolveMappingTarget_Branches pins each answer resolveMappingTarget can
// give for a target it resolves. A declared name or component resolves through
// mappingTargetID. A declared position, a bare scalar property included,
// resolves to the ID its pointer derives, as mappingTargetID names it
// (TestMappingTargetID_NamesAPositionByItsPointer), and its node is promised:
// its model carries it, so it interns no node of its own until every
// declaration has lowered.
func TestResolveMappingTarget_Branches(t *testing.T) {
	t.Parallel()
	l, _ := LoweredFor(t, openapitest.ComponentSpec(mappingTargetSpec))
	d := petDiscriminator(t, l)
	resolveTarget := func(target string) (ir.TypeID, bool) {
		return resolveMappingTarget(l.Ctx, l.Types, d, target, nil)
	}

	cat := ids.Document{}.NamedType(ids.Ptr("components", "schemas", "Cat"))
	for _, target := range []string{"Cat", "#/components/schemas/Cat"} {
		id, ok := resolveTarget(target)
		assert.True(t, ok, "%s names a declared component", target)
		assert.Equal(t, cat, id)
	}

	const scalarTarget = "#/components/schemas/Pet/properties/kind"
	kind := ids.Document{}.ForPointer("/components/schemas/Pet/properties/kind")
	got, mappingOK := mappingTargetID(l.Ctx, l.Ctx.RefScope(), d, scalarTarget)
	require.True(t, mappingOK, "mappingTargetID names an un-interned scalar position by its pointer")
	assert.Equal(t, kind, got)

	id, ok := resolveTarget(scalarTarget)
	require.True(t, ok, "resolveMappingTarget promises it")
	assert.Equal(t, kind, id)
	_, interned := l.Types.Node(id)
	assert.False(t, interned, "a promise builds nothing until every declaration has lowered")
	_, finished := l.Types.Finish(t.Context())
	assert.Empty(t, finished)
	_, interned = l.Types.Node(id)
	assert.True(t, interned, "the hoisted alias is registered in the type registry")
}

// TestResolveMappingTarget_RefusesWhatTheDocumentDoesNotDeclare pins the
// targets resolveMappingTarget refuses: an external reference, an undeclared
// component and a position the document does not declare. Nothing else is
// lowered, so the registry Finish hands over holds whatever a refusal interned
// or promised, and it must be empty.
func TestResolveMappingTarget_RefusesWhatTheDocumentDoesNotDeclare(t *testing.T) {
	t.Parallel()
	l, _ := LoweredFor(t, openapitest.ComponentSpec(mappingTargetSpec))
	d := petDiscriminator(t, l)
	for target, why := range map[string]string{
		"other.yaml#/A":                               "an external reference never resolves",
		"#/components/schemas/Ghost":                  "an undeclared component is refused, not hoisted as an inline position",
		"#/components/schemas/Pet/properties/missing": "a position the document does not declare does not resolve",
	} {
		_, ok := resolveMappingTarget(l.Ctx, l.Types, d, target, nil)
		assert.False(t, ok, why)
	}
	reg, finished := l.Types.Finish(t.Context())
	assert.Empty(t, finished)
	assert.Empty(t, reg, "a refused target interns and promises nothing")
}

// TestMappingTargetID_NamesAPositionByItsPointer pins the answer for a mapping
// target deeper than a component: the ID its pointer derives, read from the
// bindings alone, before anything is interned there and after alike, so no
// tag turns on which schema lowered first. A target the bindings land nowhere
// is unresolved.
func TestMappingTargetID_NamesAPositionByItsPointer(t *testing.T) {
	t.Parallel()
	const sub = "#/components/schemas/Pet/properties/kind"

	empty := NewRawLowerer(openapitest.DocDeclaring("Pet"))
	_, ok := mappingTargetID(empty.Ctx, empty.Ctx.RefScope(), &oas3.Discriminator{}, sub)
	assert.False(t, ok, "with no bindings the target lands nowhere, so it does not resolve")

	l, _ := LoweredFor(t, openapitest.ComponentSpec("    Pet:\n      type: object\n"+
		"      properties: {kind: {type: object, properties: {a: {type: string}}}}\n"+
		"      discriminator: {propertyName: k, mapping: {s: '"+sub+"'}}\n"))
	want := ids.Document{}.ForPointer("/components/schemas/Pet/properties/kind")
	before, ok := mappingTargetID(l.Ctx, l.Ctx.RefScope(), petDiscriminator(t, l), sub)
	require.True(t, ok, "nothing is interned yet, and the position is named all the same")
	assert.Equal(t, want, before)

	l.Diags.AppendAll(LowerComponentSchemas(t.Context(), l.Ctx, l.Types))
	_, finished := l.Types.Finish(t.Context())
	l.Diags.AppendAll(finished)
	after, ok := mappingTargetID(l.Ctx, l.Ctx.RefScope(), petDiscriminator(t, l), sub)
	require.True(t, ok)
	assert.Equal(t, want, after, "the node interned there has the ID it was named by")
	_, interned := l.Types.Node(want)
	assert.True(t, interned)
}

// TestMappingTargetID_ReadsEachPositionOnce pins that the position whose type a
// mapping target names is read once a compile (lowering.Ctx.TypePosition), not
// once for each subtype asking: each reads its base's whole mapping, so a base
// with n subtypes read each of its positions n times.
func TestMappingTargetID_ReadsEachPositionOnce(t *testing.T) {
	t.Parallel()
	l, _ := LoweredFor(t, openapitest.ComponentSpec("    Cat: {type: object}\n"+
		"    Hold: {type: object, properties: {p: {$ref: '#/components/schemas/Cat'}}}\n"+
		"    Pet: {discriminator: {propertyName: k, mapping: {p: '#/components/schemas/Hold/properties/p'}}}\n"))

	id, ok := mappingTargetID(l.Ctx, l.Ctx.RefScope(), petDiscriminator(t, l),
		"#/components/schemas/Hold/properties/p")
	require.True(t, ok)
	assert.Equal(t, componentIDByName("Cat"), id, "the position reads through to Cat")
	at := l.Ctx.TypePosition(bind.Position{Pointer: "/components/schemas/Hold/properties/p"}, func() bind.Position {
		t.Error("the position was read again")
		return bind.Position{}
	})
	assert.Equal(t, bind.Position{Pointer: "/components/schemas/Cat"}, at)
}

// TestResolveMappingTarget_DefsValueNamesItsOwnInternedSibling drives the
// success path end to end: Pet's discriminator mapping values are
// "#/$defs/..." pointers naming Pet's own sibling definitions — the same
// definitions its oneOf branches name — so the mapping and the oneOf branch it
// tags promise the same pointer-derived ID, one node is built for both, and
// each tag keeps its own shape (GitHub #557).
func TestResolveMappingTarget_DefsValueNamesItsOwnInternedSibling(t *testing.T) {
	t.Parallel()
	doc, diags := LowerSpec(t, openapitest.ComponentSpec(defsSiblingMappingSchemas))
	for _, d := range diags {
		assert.NotEqual(t, ir.SeverityError, d.Severity, "unexpected error diagnostic: %+v", d)
	}

	petID := ids.Document{}.NamedType(ids.Ptr("components", "schemas", "Pet"))
	u, ok := doc.Types[petID].(*ir.Union)
	require.True(t, ok, "a discriminated oneOf lowers to a Union")
	require.NotNil(t, u.Discriminator)

	catID, ok := u.Discriminator.Mapping["cat"]
	require.True(t, ok)
	dogID, ok := u.Discriminator.Mapping["dog"]
	require.True(t, ok)
	assert.NotEqual(t, catID, dogID, "each mapping value names its own definition, not a shared one")

	catModel, ok := doc.Types[catID].(*ir.Model)
	require.True(t, ok)
	_, hasMeow := propIDByName(catModel, "meow")
	assert.True(t, hasMeow, "cat's own property, not dog's")

	dogModel, ok := doc.Types[dogID].(*ir.Model)
	require.True(t, ok)
	_, hasBark := propIDByName(dogModel, "bark")
	assert.True(t, hasBark, "dog's own property, not cat's")

	require.Len(t, u.Variants, 2)
	variantTargets := []ir.TypeID{u.Variants[0].Type.Target, u.Variants[1].Type.Target}
	assert.Contains(t, variantTargets, catID, "the mapping names the node its oneOf branch names")
	assert.Contains(t, variantTargets, dogID)
}

// TestResolveMappingTarget_DefsValueNothingInternedIsHoistedAtItsOwnPosition
// pins how a "#/$defs/..." mapping value resolves when its definition is not
// interned yet: hoisted where it is written, as any mapping to a sub-schema is
// (GitHub #530), not at the document-rooted /$defs/cat the value spells, which
// this document does not hold (GitHub #557, #910). Each component's tag names
// its own definition, in either declaration order.
func TestResolveMappingTarget_DefsValueNothingInternedIsHoistedAtItsOwnPosition(t *testing.T) {
	t.Parallel()
	for name, blocks := range map[string]string{
		"Pet declared first": defsMappingPet + defsMappingToy,
		"Toy declared first": defsMappingToy + defsMappingPet,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			doc, diags := LowerSpec(t, openapitest.ComponentSpec(blocks))
			openapitest.RequireNoErrorDiags(t, diags)

			for _, owner := range []string{"Pet", "Toy"} {
				m, ok := doc.Types[ids.Document{}.NamedType(ids.Ptr("components", "schemas", owner))].(*ir.Model)
				require.True(t, ok, owner)
				require.NotNil(t, m.Discriminator, owner)

				want := ids.Document{}.ForPointer(ids.Ptr("components", "schemas", owner, "$defs", "cat"))
				assert.Equal(t, map[string]ir.TypeID{"cat": want}, m.Discriminator.Mapping, owner)
				_, interned := doc.Types[want]
				assert.True(t, interned, "%s's definition is interned where it is written", owner)
			}
		})
	}
}

// TestBesideUnionDiscriminator_ReportsWhatItCannotSettle pins that a settlement
// whose model is missing, nil or no model, or whose union's node is missing, is
// announced rather than skipped: the discriminator it was deferred for would
// otherwise be settled nowhere, unremarked. A nil map is no settled one.
func TestBesideUnionDiscriminator_ReportsWhatItCannotSettle(t *testing.T) {
	t.Parallel()
	for name, reg := range map[string]ir.TypeRegistry{
		"model missing":          {"t/owner": &ir.Union{ID: "t/owner"}},
		"no model":               {"t/owner": &ir.Union{ID: "t/owner"}, "t/ghost": &ir.Scalar{ID: "t/ghost"}},
		"owner missing":          {"t/ghost": &ir.Model{ID: "t/ghost", Discriminator: &ir.Discriminator{Mapping: map[string]ir.TypeID{"cat": "t/cat"}}}},
		"owner of a map missing": {"t/ghost": &ir.MapT{ID: "t/ghost"}},
		"nil model":              {"t/owner": &ir.Union{ID: "t/owner"}, "t/ghost": (*ir.Model)(nil)},
		"nil map":                {"t/owner": &ir.Union{ID: "t/owner"}, "t/ghost": (*ir.MapT)(nil)},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			d := besideUnionDiscriminator{c: lowering.Ctx{}, pointer: "/components/schemas/Ghost", owner: "t/owner", model: "t/ghost"}

			diags := d.Settle(reg)

			require.Len(t, diags, 1)
			assert.Equal(t, diag.InternalInvariant, diags[0].Code)
			assert.Equal(t, ir.SeverityError, diags[0].Severity)
		})
	}
}

// TestBesideUnionDiscriminator_LeavesASettledMapAlone pins the second
// settlement for one model: the first moved the discriminator and left a MapT,
// so the second has nothing to settle and reports nothing.
func TestBesideUnionDiscriminator_LeavesASettledMapAlone(t *testing.T) {
	t.Parallel()
	mt := &ir.MapT{ID: "t/pet", Value: ir.TypeRef{Target: "t/prim/string"}}
	reg := ir.TypeRegistry{"t/owner": &ir.Union{ID: "t/owner"}, "t/pet": mt}
	d := besideUnionDiscriminator{c: lowering.Ctx{}, pointer: "/components/schemas/Pet", owner: "t/owner", model: "t/pet"}

	assert.Empty(t, d.Settle(reg))
	assert.Same(t, mt, reg["t/pet"], "the map is left in place")
}

// TestMappingWhy_GivesAComponentNameNoReasonOfTheTable pins that a mapping
// value naming a component, which the table never reads, takes none of the
// table's reasons: the merge bound cut a read elsewhere in the document, not
// the lookup of the name.
func TestMappingWhy_GivesAComponentNameNoReasonOfTheTable(t *testing.T) {
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
	assert.Empty(t, mappingWhy(l.Ctx, d, "Undeclared"))
}

// TestMappingWhy_GivesAnUnreadReferenceTheTablesReason pins that a mapping
// value spelling a reference, which the table reads, takes the table's reason
// where it went unread: P's discriminator sits past a merge chain deeper than
// the compiler expands, so the binder never read its mapping.
func TestMappingWhy_GivesAnUnreadReferenceTheTablesReason(t *testing.T) {
	t.Parallel()
	var deep strings.Builder
	deep.WriteString("openapi: 3.1.0\ninfo: {title: t, version: '1'}\npaths: {}\n" +
		"x-c0: &c0 {oneOf: [{$ref: '#/components/schemas/T'}], " +
		"discriminator: {propertyName: k, mapping: {t: '#/components/schemas/T'}}}\n")
	for i := 1; i <= 70; i++ {
		fmt.Fprintf(&deep, "x-c%d: &c%d {<<: *c%d}\n", i, i, i-1)
	}
	deep.WriteString("components:\n  schemas:\n    T: {properties: {k: {type: string}}}\n    P: {<<: *c70}\n")
	l, _ := LoweredFor(t, deep.String())
	p, ok := l.Ctx.Doc.Components.GetSchemas().Get("P")
	require.True(t, ok)
	d := p.GetSchema().GetDiscriminator()
	require.NotNil(t, d, "the parser expands the whole chain")
	assert.Equal(t, ": it is past a `<<` merge chain deeper than the 64 levels the compiler expands",
		mappingWhy(l.Ctx, d, "#/components/schemas/T"))
}

// petDiscriminator returns the discriminator the component schema Pet holds
// in l's document.
func petDiscriminator(t *testing.T, l *Lowerer) *oas3.Discriminator {
	t.Helper()
	js, ok := l.Ctx.Doc.Components.GetSchemas().Get("Pet")
	require.True(t, ok)
	d := js.GetSchema().GetDiscriminator()
	require.NotNil(t, d)
	return d
}

// defsSiblingMappingSchemas is the discriminator-mapping shape of GitHub #557:
// Pet's own discriminator maps each tag to a "#/$defs/..." pointer naming its
// own sibling definition, the same one its oneOf branch for that tag already
// $refs.
const defsSiblingMappingSchemas = `    Pet:
      oneOf: [{$ref: "#/$defs/cat"}, {$ref: "#/$defs/dog"}]
      discriminator: {propertyName: kind, mapping: {cat: "#/$defs/cat", dog: "#/$defs/dog"}}
      $defs:
        cat: {type: object, required: [kind], properties: {kind: {type: string}, meow: {type: string}}}
        dog: {type: object, required: [kind], properties: {kind: {type: string}, bark: {type: string}}}
`

// defsMappingPet and defsMappingToy each map a tag to a "#/$defs/cat" definition
// that nothing else in the schema references, so no oneOf branch has interned
// it when the mapping asks. They spell the same pointer for different
// definitions, GitHub #557's shape.
const (
	defsMappingPet = `    Pet:
      type: object
      properties: {kind: {type: string}}
      discriminator: {propertyName: kind, mapping: {cat: "#/$defs/cat"}}
      $defs:
        cat: {type: object, properties: {meow: {type: string}}}
`
	defsMappingToy = `    Toy:
      type: object
      properties: {kind: {type: string}}
      discriminator: {propertyName: kind, mapping: {cat: "#/$defs/cat"}}
      $defs:
        cat: {type: object, properties: {squeak: {type: string}}}
`
)
