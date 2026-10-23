package schema_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/compile"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/ir"
)

// TestEscapedComponentName_DecodesInDiscriminatorValueAndHints pins GitHub
// #505: a name holding a character a pointer escapes reaches the IR decoded
// wherever it is read off a pointer's last token: the implicit
// discriminatorValue sent on the wire, the hints a union variant and a branch
// alias read off the $ref they hold, and the hint a $defs entry takes from its
// own key. A $defs entry that is itself a $ref is named by its key too, not by
// its target (GitHub #519).
func TestEscapedComponentName_DecodesInDiscriminatorValueAndHints(t *testing.T) {
	t.Parallel()
	spec := openapitest.ComponentSpec(`    Pet:
      type: object
      required: [kind]
      properties:
        kind: {type: string}
      discriminator:
        propertyName: kind
    Cat/Dog:
      allOf:
        - $ref: '#/components/schemas/Pet'
      type: object
      properties:
        meow: {type: string}
    Fish-Tank:
      type: object
      properties:
        blub: {type: string}
    Choice:
      oneOf:
        - $ref: '#/components/schemas/Cat~1Dog'
        - $ref: '#/components/schemas/Fish%2DTank'
    Mixed:
      allOf:
        - $ref: '#/components/schemas/Cat~1Dog'
          description: a sibling hoists an alias the branch hint names
        - type: object
          properties:
            z: {type: string}
    Host:
      type: object
      $defs:
        a/b: {type: object, properties: {c: {type: string}}}
        inner:
          $ref: '#/components/schemas/Cat~1Dog'
          description: a sibling gives this position a node of its own
    UsesDef:
      $ref: '#/components/schemas/Host/$defs/a~1b'
    UsesInner:
      $ref: '#/components/schemas/Host/$defs/inner'
`)
	doc, diags := lowerSpec(t, spec)
	openapitest.RequireNoErrorDiags(t, diags)

	catDog, ok := doc.Types[componentID("Cat~1Dog")].(*ir.Model)
	require.True(t, ok, "Cat/Dog should be a model")
	assert.Equal(t, "Cat/Dog", catDog.DiscriminatorValue,
		"the implicit discriminator value is the component's decoded name, not its escaped pointer segment")

	choice, ok := typeByName(doc, "Choice").(*ir.Union)
	require.True(t, ok, "Choice should be a union")
	require.Len(t, choice.Variants, 2)
	assert.Equal(t, compile.NamingHint("Cat/Dog").Hint, choice.Variants[0].Name.Hint,
		"the variant naming a $ref with an RFC 6901 escape takes the decoded name")
	assert.Equal(t, compile.NamingHint("Fish-Tank").Hint, choice.Variants[1].Name.Hint,
		"and one naming a $ref with a percent escape takes its decoded name")

	alias, ok := doc.Types[ir.TypeID("t/anon/components/schemas/Mixed/allOf/0")].(*ir.Scalar)
	require.True(t, ok, "a $ref branch with a sibling hoists an alias")
	assert.Equal(t, compile.NamingHint("Cat/Dog").Hint, alias.Name.Hint,
		"the alias a sibling-carrying branch hoists takes the same decoded hint")

	def, ok := doc.Types[ir.TypeID("t/anon/components/schemas/Host/$defs/a~1b")]
	require.True(t, ok, "a $ref into $defs hoists the definition it names")
	assert.Equal(t, compile.NamingHint("a/b").Hint, def.Common().Name.Hint,
		"a node a $ref names inside another schema takes its decoded key")

	inner, ok := doc.Types[ir.TypeID("t/anon/components/schemas/Host/$defs/inner")]
	require.True(t, ok, "a $ref to a $ref carrying a sibling hoists a node of its own")
	assert.Equal(t, compile.NamingHint("inner").Hint, inner.Common().Name.Hint,
		"and one that is itself a $ref is named for its position, as a property holding a $ref is (GitHub #519)")
}

// TestComposition_BranchAliasIsOrderIndependent pins the hint on the node a
// composition branch and an outside $ref both reach, declared in either order.
// The $ref only promises the node, so the branch's lowering builds it under the
// hint it recorded; a hint that followed declaration order would pass every
// other check, since either spelling is a valid hint.
//
// The registry is compared on its own, name hints included: the hint *is* what
// differed.
func TestComposition_BranchAliasIsOrderIndependent(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"allOf", "oneOf", "anyOf"} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			first, diags := parseFull(t, branchAliasSpec(kind, true))
			openapitest.RequireNoErrorDiags(t, diags)
			last, diags := parseFull(t, branchAliasSpec(kind, false))
			openapitest.RequireNoErrorDiags(t, diags)

			branch := ir.TypeID("t/anon/components/schemas/Host/" + kind + "/0")
			require.Contains(t, first.Types, branch, "the branch position owns a node")
			assert.Empty(t, cmp.Diff(first.Types, last.Types),
				"declaring the composition before or after the outside $ref must not change the registry")
		})
	}
}

func TestLower_InlineSchemaHoistedOnce(t *testing.T) {
	t.Parallel()
	spec := openapitest.ComponentSpec(`    S:
      type: object
      properties:
        tags:
          type: array
          items:
            type: object
            properties:
              name: {type: string}
`)
	doc, diags := lowerSpec(t, spec)
	openapitest.RequireNoErrorDiags(t, diags)
	itemsID := ir.TypeID("t/anon/components/schemas/S/properties/tags/items")
	item, ok := doc.Types[itemsID].(*ir.Model)
	require.True(t, ok, "items object should be hoisted as a model")
	assert.True(t, item.Anonymous)
	assert.Equal(t, "tags_item", item.Name.Hint)
	assert.Empty(t, item.Name.Source, "hoisted inline types carry a hint, not a source name")
}

// TestInlinePosition_HintIsTheSameInBothOrders pins the spelling each inline
// position takes, not only that the two orders agree (GitHub #353): the
// structural spelling composed from the enclosing node's hint and the
// position's role, whichever order an outside $ref is declared in.
//
// refAt says where that $ref aims, at the node asserted or above it. A
// reference only promises a node, so the declaration names each one in either
// order; the rows pin the spelling the SubHint call sites give, and
// TestPositionHint_Shapes the one structuralRoles replays.
func TestInlinePosition_HintIsTheSameInBothOrders(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name  string
		owner string
		id    ir.TypeID
		hint  string
		refAt ir.TypeID
	}{
		{"items", "    A: {type: array, items: " + openapitest.InlineProbeBody + "}\n",
			"t/anon/components/schemas/A/items", "a_item", ""},
		{"additionalProperties", "    A: {type: object, additionalProperties: " + openapitest.InlineProbeBody + "}\n",
			"t/anon/components/schemas/A/additionalProperties", "a_value", ""},
		{"patternProperties", "    A: {type: object, patternProperties: {\"^x\": " + openapitest.InlineProbeBody + "}}\n",
			"t/anon/components/schemas/A/patternProperties/^x", "a_pattern", ""},
		{"prefixItems", "    A: {type: array, prefixItems: [" + openapitest.InlineProbeBody + "]}\n",
			"t/anon/components/schemas/A/prefixItems/0", "a_0", ""},
		{"tuple rest", "    A: {type: array, prefixItems: [{type: string}], items: " + openapitest.InlineProbeBody + "}\n",
			"t/anon/components/schemas/A/items", "a_item", ""},
		// Nested, because the derivation replays the whole chain rather than one
		// step: the outside $ref used to name this "items", losing both levels.
		{"items under items", "    A: {type: array, items: {type: array, items: " + openapitest.InlineProbeBody + "}}\n",
			"t/anon/components/schemas/A/items/items", "a_item_item", ""},
		// Rooted at a property rather than at the component, so the enclosing hint
		// the walk rebuilds from is the property's key.
		{"items under a property", "    A: {type: object, properties: {p: {type: array, items: " +
			openapitest.InlineProbeBody + "}}}\n",
			"t/anon/components/schemas/A/properties/p/items", "p_item", ""},
		{"contentSchema", "    A: {type: string, contentMediaType: application/json, contentSchema: " +
			openapitest.InlineProbeBody + "}\n",
			"t/anon/components/schemas/A/contentSchema", "a_content", ""},
		// The reference is aimed at contentSchema and the assertion at what is
		// under it.
		{"items under contentSchema", "    A: {type: string, contentMediaType: application/json, " +
			"contentSchema: {type: array, items: " + openapitest.InlineProbeBody + "}}\n",
			"t/anon/components/schemas/A/contentSchema/items", "a_content_item",
			"t/anon/components/schemas/A/contentSchema"},
		// Aimed at a tuple's rest: a rest named by a role of its own would part
		// from the /items a pointer walk replays.
		{"items under a tuple rest", "    A: {type: array, prefixItems: [{type: string}], items: {type: array, items: " +
			openapitest.InlineProbeBody + "}}\n",
			"t/anon/components/schemas/A/items/items", "a_item_item",
			"t/anon/components/schemas/A/items"},
		// Aimed at additionalProperties.
		{"items under additionalProperties", "    A: {type: object, additionalProperties: {type: array, items: " +
			openapitest.InlineProbeBody + "}}\n",
			"t/anon/components/schemas/A/additionalProperties/items", "a_value_item",
			"t/anon/components/schemas/A/additionalProperties"},
		// The remaining rows put a key spelled like a keyword, or a branch's
		// ordinal, on the way from the component to refAt (GitHub #518), and
		// assert a node beneath refAt: a reading that takes the key for the
		// keyword, or the ordinal for the branch's hint, shows in the names
		// beneath it.
		{"a property named items is not the items keyword",
			"    A: {type: object, properties: {items: {type: array, items: " +
				openapitest.InlineProbeBody + "}}}\n",
			"t/anon/components/schemas/A/properties/items/items", "items_item",
			"t/anon/components/schemas/A/properties/items"},
		{"a property named additionalProperties is not the additionalProperties keyword",
			"    A: {type: object, properties: {additionalProperties: {type: object, additionalProperties: " +
				openapitest.InlineProbeBody + "}}}\n",
			"t/anon/components/schemas/A/properties/additionalProperties/additionalProperties",
			"additional_properties_value", "t/anon/components/schemas/A/properties/additionalProperties"},
		{"a property named contentSchema is not the contentSchema keyword",
			"    A: {type: object, properties: {contentSchema: {type: array, items: " +
				openapitest.InlineProbeBody + "}}}\n",
			"t/anon/components/schemas/A/properties/contentSchema/items", "content_schema_item",
			"t/anon/components/schemas/A/properties/contentSchema"},
		{"a patternProperties entry keyed items is not the items keyword",
			"    A: {type: object, patternProperties: {items: {type: array, items: " +
				openapitest.InlineProbeBody + "}}}\n",
			"t/anon/components/schemas/A/patternProperties/items/items", "a_pattern_item",
			"t/anon/components/schemas/A/patternProperties/items"},
		{"items beneath a oneOf branch is named off the branch, not the ordinal",
			"    A: {oneOf: [{type: array, items: {type: array, items: " +
				openapitest.InlineProbeBody + "}}, {type: integer}]}\n",
			"t/anon/components/schemas/A/oneOf/0/items/items", "variant_0_item_item",
			"t/anon/components/schemas/A/oneOf/0/items"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			refAt := tc.refAt
			if refAt == "" {
				refAt = tc.id
			}
			pos := stolenPosition{name: tc.name, owner: tc.owner, id: refAt}
			for _, order := range []struct {
				name     string
				refFirst bool
			}{{"owner declared first", false}, {"reference declared first", true}} {
				doc, diags := parseFull(t, pos.spec(order.refFirst))
				openapitest.RequireNoErrorDiags(t, diags)
				td, ok := doc.Types[tc.id]
				require.True(t, ok, "%s: the position owns a node at %s", order.name, tc.id)
				assert.Equal(t, tc.hint, td.Common().Name.Hint, order.name)
			}
		})
	}
}

// TestInlinePosition_UnderPathsIsNamedByItsDeclaration is what GitHub #353 could
// not reach and #372 closed.
//
// The enclosing hint under /paths comes from a response, an operationId or a
// media-type key, none of which the pointer records, so no pointer-derived
// spelling reproduces it. A reference never builds a position its declaration
// lowers, so the position is "response_item" whether or not an unrelated schema
// points at it, and in either declaration order.
func TestInlinePosition_UnderPathsIsNamedByItsDeclaration(t *testing.T) {
	t.Parallel()
	const id = ir.TypeID("t/anon/paths/~1x/get/responses/200/content/application~1json/schema/items")
	const op = `paths:
  /x:
    get:
      operationId: getX
      responses:
        "200":
          description: ok
          content:
            application/json: {schema: {type: array, items: ` + openapitest.InlineProbeBody + `}}
`
	const outsider = "components:\n  schemas:\n" +
		"    Outsider: {$ref: '#/paths/~1x/get/responses/200/content/application~1json/schema/items'}\n"

	ownerFirst, diags := parseFull(t, "openapi: 3.1.0\ninfo: {title: O, version: \"1.0.0\"}\n"+op+outsider)
	openapitest.RequireNoErrorDiags(t, diags)
	refFirst, diags := parseFull(t, "openapi: 3.1.0\ninfo: {title: O, version: \"1.0.0\"}\n"+outsider+op)
	openapitest.RequireNoErrorDiags(t, diags)

	// The reference is a component, which lowers before the path declaring the
	// position, so it is reached first in both spellings; it only promises the
	// node, which the declaration names.
	assert.Equal(t, "response_item", ownerFirst.Types[id].Common().Name.Hint,
		"the declaration names it, whichever order the two blocks are written in")
	assert.Equal(t, "response_item", refFirst.Types[id].Common().Name.Hint,
		"including when the reference is the lowering that interned the node")

	// And the same name with nothing pointing at the position at all, which is
	// the claim: an unrelated $ref elsewhere in the document costs it nothing.
	unreferenced, diags := parseFull(t, "openapi: 3.1.0\ninfo: {title: O, version: \"1.0.0\"}\n"+op)
	openapitest.RequireNoErrorDiags(t, diags)
	assert.Equal(t, "response_item", unreferenced.Types[id].Common().Name.Hint,
		"which is what the two above are being held to")
}

// TestPureRefPosition_IsNamedByItsDeclarationInBothOrders is GitHub #519: a
// pure $ref position interns nothing from its own declaration, unlike the
// structural positions TestInlinePosition_HintIsTheSameInBothOrders covers, so
// when the declaration lowers first it has nothing to rename and a later
// reference's guess would stick.
//
// Either of two things keeps each row green: the reference names the position
// as the declaration does, or a reference arriving later takes the hint the
// declaration recorded. Only the second reaches under /paths
// (TestPureRefPosition_UnderPathsIsNamedByItsDeclaration).
//
// T is a plain object, so the last row's branch is named after it ("t"), the
// one row where that is correct (GitHub #521).
func TestPureRefPosition_IsNamedByItsDeclarationInBothOrders(t *testing.T) {
	t.Parallel()
	const tDecl = "    T: {type: object, properties: {t: {type: string}}}\n"
	for _, tc := range []struct {
		name  string
		owner string
		id    ir.TypeID
		hint  string
	}{
		{"items", "    A: {type: array, items: {$ref: '#/components/schemas/T'}}\n" + tDecl,
			"t/anon/components/schemas/A/items", "a_item"},
		{"additionalProperties", "    A: {type: object, additionalProperties: {$ref: '#/components/schemas/T'}}\n" + tDecl,
			"t/anon/components/schemas/A/additionalProperties", "a_value"},
		{"a property", "    A: {type: object, properties: {p: {$ref: '#/components/schemas/T'}}}\n" + tDecl,
			"t/anon/components/schemas/A/properties/p", "p"},
		{"prefixItems", "    A: {type: array, prefixItems: [{$ref: '#/components/schemas/T'}]}\n" + tDecl,
			"t/anon/components/schemas/A/prefixItems/0", "a_0"},
		{"patternProperties", "    A: {type: object, patternProperties: {\"^x\": {$ref: '#/components/schemas/T'}}}\n" + tDecl,
			"t/anon/components/schemas/A/patternProperties/^x", "a_pattern"},
		// A $ref branch is the one position the composition names after its
		// target rather than its own location, in either order — branchHint
		// and subSchemaHint agree on that (GitHub #521) — so this row's
		// expectation does not flip between the two orders below.
		{"a union branch", "    A: {oneOf: [{$ref: '#/components/schemas/T'}, {type: integer}]}\n" + tDecl,
			"t/anon/components/schemas/A/oneOf/0", "t"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			pos := stolenPosition{name: tc.name, owner: tc.owner, id: tc.id}
			for _, order := range []struct {
				name     string
				refFirst bool
			}{{"owner declared first", false}, {"reference declared first", true}} {
				doc, diags := parseFull(t, pos.spec(order.refFirst))
				openapitest.RequireNoErrorDiags(t, diags)
				td, ok := doc.Types[tc.id]
				require.True(t, ok, "%s: the position owns a node at %s", order.name, tc.id)
				assert.Equal(t, tc.hint, td.Common().Name.Hint, order.name)
			}
		})
	}
}

// TestPureRefPosition_UnderPathsIsNamedByItsDeclaration is
// TestPureRefPosition_IsNamedByItsDeclarationInBothOrders's counterpart under
// /paths, modelled on TestInlinePosition_UnderPathsIsNamedByItsDeclaration:
// positionHint cannot replay the enclosing hint there, so it offers
// "schema_item" where the declaration says "response_item". /b's declaration,
// which interns nothing at the pure $ref, records the name it gives the
// position, and the alias a reference hoists there once every declaration has
// lowered takes it, whichever of /b or /a lowers first.
func TestPureRefPosition_UnderPathsIsNamedByItsDeclaration(t *testing.T) {
	t.Parallel()
	const id = ir.TypeID("t/anon/paths/~1b/get/responses/200/content/application~1json/schema/items")
	const bBlock = `  /b:
    get:
      operationId: getB
      responses:
        "200":
          description: ok
          content:
            application/json: {schema: {type: array, items: {$ref: '#/components/schemas/T'}}}
`
	const aBlock = `  /a:
    get:
      operationId: getA
      responses:
        "200":
          description: ok
          content:
            application/json: {schema: {$ref: '#/paths/~1b/get/responses/200/content/application~1json/schema/items', description: x}}
`
	const components = "components:\n  schemas:\n    T: {type: object, properties: {t: {type: string}}}\n"
	const preamble = "openapi: 3.1.0\ninfo: {title: O, version: \"1.0.0\"}\npaths:\n"

	// b declared first is the order GitHub #519 reports: b's declaration
	// reaches the position and interns nothing there (a pure $ref), so there
	// was nothing for it to rename once a's reference hoisted a node under its
	// own guess.
	bFirst, diags := parseFull(t, preamble+bBlock+aBlock+components)
	openapitest.RequireNoErrorDiags(t, diags)
	aFirst, diags := parseFull(t, preamble+aBlock+bBlock+components)
	openapitest.RequireNoErrorDiags(t, diags)

	assert.Equal(t, "response_item", bFirst.Types[id].Common().Name.Hint,
		"the declaration's hint is recorded and survives even though it interns nothing itself")
	assert.Equal(t, "response_item", aFirst.Types[id].Common().Name.Hint,
		"and in the other order too")
}

// TestAlias_CarriesTheBoundsWrittenBesideIt pins that an alias a position
// hoists to own what it wrote keeps the value constraints written beside it:
// beside a union of nothing but null, and beside an applicator its lowered node
// has no home for. The alias owns the pointer, so hoistDeclarationHome hoists
// nothing else that could carry them.
func TestAlias_CarriesTheBoundsWrittenBesideIt(t *testing.T) {
	t.Parallel()
	one, two, three := ir.BigVal("1"), ir.BigVal("2"), ir.BigVal("3")
	six := int64(6)
	cases := []struct {
		name, schema string
		want         ir.Constraints
	}{
		{
			name:   "a null-only union",
			schema: `{oneOf: [{type: "null"}], minimum: 3, exclusiveMinimum: 1}`,
			want:   ir.Constraints{Min: &three, ExclusiveMin: &one},
		},
		{
			name:   "an applicator with no home",
			schema: "{items: {type: string}, minimum: 2, exclusiveMinimum: 1, minLength: 6}",
			want:   ir.Constraints{Min: &two, ExclusiveMin: &one, MinLength: &six},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			doc, diags := lowerSpec(t, openapitest.ComponentSpec("    A: "+tc.schema+"\n"))
			openapitest.RequireNoErrorDiags(t, diags)
			sc, ok := typeByName(doc, "A").(*ir.Scalar)
			require.True(t, ok, "the position hoists an alias of its own")
			require.NotNil(t, sc.Constraints, "carrying the bounds written beside it")
			assert.Empty(t, cmp.Diff(tc.want, *sc.Constraints))
		})
	}
}

// branchAliasSpec writes a composition whose first branch is a $ref carrying a
// sibling — so the branch position owns a node — with an outside component
// naming that branch pointer. hostFirst permutes which of the two is declared
// first, and kind selects the composition keyword.
func branchAliasSpec(kind string, hostFirst bool) string {
	host := "    Host:\n      " + kind + ":\n" +
		"        - $ref: '#/components/schemas/Base'\n" +
		"          description: narrowed at this branch\n"
	if kind != "allOf" {
		host += "        - type: string\n" // a union needs a second variant
	}
	rest := "    Base: {type: object, properties: {a: {type: string}}}\n" +
		"    Outside: {$ref: '#/components/schemas/Host/" + kind + "/0'}\n"
	if hostFirst {
		return openapitest.ComponentSpec(host + rest)
	}
	return openapitest.ComponentSpec(rest + host)
}
