// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package schema_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/compilers/openapi/internal/schema"
	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/ir/irtest"
)

func TestModelDiscriminator_UndeclaredPropertyAndBadMapping(t *testing.T) {
	t.Parallel()
	spec := openapitest.ComponentSpec(`    Vehicle:
      type: object
      discriminator:
        propertyName: kind
        mapping:
          car: '#'
      properties: {name: {type: string}}
`)
	doc, diags := schema.LowerSpec(t, spec)
	v := typeByName(doc, "Vehicle").(*ir.Model)
	require.NotNil(t, v.Discriminator)
	assert.Empty(t, v.Discriminator.Property, "undeclared property")
	assert.Equal(t, "kind", v.Discriminator.PropertyName)
	assert.True(t, openapitest.HasDiag(diags, diag.UnresolvedRef), "bad mapping target diagnostic")
}

func TestOneOf_DiscriminatorWithDefault(t *testing.T) {
	t.Parallel()
	spec := openapitest.ComponentSpecVer("3.2.0", `    Shape:
      oneOf:
        - {$ref: '#/components/schemas/Circle'}
        - {$ref: '#/components/schemas/Square'}
      discriminator:
        propertyName: shapeType
        mapping: {circle: '#/components/schemas/Circle'}
        defaultMapping: '#/components/schemas/Square'
    Circle: {type: object, properties: {r: {type: number}}}
    Square: {type: object, properties: {s: {type: number}}}
`)
	doc, _ := schema.LowerSpec(t, spec)
	u := typeByName(doc, "Shape").(*ir.Union)
	require.NotNil(t, u.Discriminator)
	assert.Equal(t, "shapeType", u.Discriminator.PropertyName)
	assert.NotEmpty(t, u.Discriminator.Default)
}

// TestDiscriminatorMapping_RoutesToTheVariantAliasingItsTarget pins that a
// mapping names a variant where a keyword beside a branch's $ref gave the branch
// an alias over the component the mapping names, spelled either way and past a
// null branch, and that a component a variant names itself stays the target.
func TestDiscriminatorMapping_RoutesToTheVariantAliasingItsTarget(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, `openapi: 3.1.0
info: {title: t, version: "1"}
paths: {}
components:
  schemas:
    Cat: {type: object, properties: {kind: {type: string}}}
    Dog: {type: object, properties: {kind: {type: string}}}
    Pet:
      oneOf:
        - {type: "null"}
        - {$ref: '#/components/schemas/Cat', description: a cat}
        - {$ref: '#/components/schemas/Dog'}
      discriminator:
        propertyName: kind
        mapping: {cat: '#/components/schemas/Cat', tom: Cat, dog: '#/components/schemas/Dog'}
    Pair:
      oneOf:
        - {$ref: '#/components/schemas/Cat'}
        - {$ref: '#/components/schemas/Cat', description: an alias beside the type itself}
      discriminator: {propertyName: kind, mapping: {cat: '#/components/schemas/Cat'}}
`)
	openapitest.RequireNoErrorDiags(t, diags)
	assert.Empty(t, validated(t, doc))
	pet, ok := typeByName(doc, "Pet").(*ir.Union)
	require.True(t, ok)
	alias := pet.Variants[0].Type.Target
	assert.NotEqual(t, ir.TypeID("t/openapi/components/schemas/Cat"), alias)
	assert.Empty(t, cmp.Diff(map[string]ir.TypeID{
		"cat": alias, "tom": alias, "dog": "t/openapi/components/schemas/Dog",
	}, pet.Discriminator.Mapping))
	pair, ok := typeByName(doc, "Pair").(*ir.Union)
	require.True(t, ok)
	assert.Equal(t, ir.TypeID("t/openapi/components/schemas/Cat"), pair.Discriminator.Mapping["cat"],
		"the type itself is a variant, so the mapping stays on it")
}

// TestDiscriminatorMapping_WritesTheRoutesItImplies pins how a union's mapping
// holds the routes OpenAPI implies (docs/compilers/openapi.md#discriminator):
// where it declares an entry, or a variant's type is not the component its
// branch names, each variant no entry routes takes the name of that component,
// past a null branch, unless two branches name it, an entry spells it already
// or the branch names none. A union whose variants are the components their
// branches name, with no mapping, routes by their names and holds none. Each is
// read in both declaration orders, and the policy reading tags reads nothing in.
func TestDiscriminatorMapping_WritesTheRoutesItImplies(t *testing.T) {
	t.Parallel()
	const cat, dog = "{$ref: '#/components/schemas/Cat'}", "{$ref: '#/components/schemas/Dog'}"
	const aliasedCat = "{$ref: '#/components/schemas/Cat', description: a cat}"
	const inline = "{type: object, properties: {kind: {type: string}}}"
	cases := []struct {
		name, ver, branches, mapping string
		want                         map[string]string // tag -> the variant's target; "alias" for U/oneOf/0's
	}{
		{"an entry beside an unnamed variant", "3.1.0", cat + ", " + dog, "{cat: Cat}",
			map[string]string{"cat": "Cat", "Dog": "Dog"}},
		{"past a null branch", "3.1.0", "{type: 'null'}, " + cat + ", " + dog, "{cat: Cat}",
			map[string]string{"cat": "Cat", "Dog": "Dog"}},
		{"an aliased variant", "3.1.0", aliasedCat + ", " + dog, "",
			map[string]string{"Cat": "alias", "Dog": "Dog"}},
		{"the components themselves", "3.1.0", cat + ", " + dog, "", nil},
		{"an entry spelling the name", "3.1.0", cat + ", " + dog, "{Dog: Cat}", map[string]string{"Dog": "Cat"}},
		{"two branches naming one", "3.1.0", aliasedCat + ", " + cat + ", " + dog, "{d: Dog}", map[string]string{"d": "Dog"}},
		{"an inline variant", "3.1.0", cat + ", " + inline, "{cat: Cat}", map[string]string{"cat": "Cat"}},
		{"the default named", "3.2.0", cat + ", " + dog, "{cat: Cat}, defaultMapping: Dog",
			map[string]string{"cat": "Cat", "Dog": "Dog"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			disc := "      discriminator: {propertyName: kind}\n"
			if c.mapping != "" {
				disc = "      discriminator: {propertyName: kind, mapping: " + c.mapping + "}\n"
			}
			union := "    U:\n      oneOf: [" + c.branches + "]\n" + disc
			const rest = "    Cat: {type: object, properties: {kind: {type: string}}}\n" +
				"    Dog: {type: object, properties: {kind: {type: string}}}\n"
			var docs []*ir.Document
			for _, src := range []string{union + rest, rest + union} {
				doc, diags := parseFull(t, openapitest.ComponentSpecVer(c.ver, src))
				openapitest.RequireNoErrorDiags(t, diags)
				assert.Empty(t, validated(t, doc), "the document must validate")
				docs = append(docs, doc)
			}
			assert.Empty(t, cmp.Diff(docs[0].Types, docs[1].Types))
			u, ok := typeByName(docs[0], "U").(*ir.Union)
			require.True(t, ok)
			require.NotNil(t, u.Discriminator)
			assert.Empty(t, u.Provenance.Inferred, "the routes are the document's")
			if c.want == nil {
				assert.Nil(t, u.Discriminator.Mapping)
				return
			}
			want := make(map[string]ir.TypeID, len(c.want))
			for tag, target := range c.want {
				want[tag] = componentID(target)
				if target == "alias" {
					want[tag] = u.Variants[0].Type.Target
				}
			}
			assert.Empty(t, cmp.Diff(want, u.Discriminator.Mapping))
		})
	}
}

// TestDiscriminatorDefault_RoutesToTheVariantAliasingItsTarget pins that a
// defaultMapping is routed as a mapping's target is, so it names a variant
// where a keyword beside the branch's $ref gave the branch an alias.
func TestDiscriminatorDefault_RoutesToTheVariantAliasingItsTarget(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, `openapi: 3.2.0
info: {title: t, version: "1"}
paths: {}
components:
  schemas:
    Cat: {type: object, properties: {kind: {type: string}}}
    Dog: {type: object, properties: {kind: {type: string}}}
    Pet:
      oneOf:
        - {$ref: '#/components/schemas/Cat', description: a cat}
        - {$ref: '#/components/schemas/Dog'}
      discriminator: {propertyName: kind, defaultMapping: '#/components/schemas/Cat'}
`)
	openapitest.RequireNoErrorDiags(t, diags)
	assert.Empty(t, validated(t, doc))
	pet, ok := typeByName(doc, "Pet").(*ir.Union)
	require.True(t, ok)
	assert.NotEqual(t, ir.TypeID("t/openapi/components/schemas/Cat"), pet.Variants[0].Type.Target)
	assert.Equal(t, pet.Variants[0].Type.Target, pet.Discriminator.Default)
}

// TestDiscriminatorMapping_TwoVariantsAliasingATypeRouteToNeither pins the rule
// for a target that two branches alias, each with its own keyword beside its
// $ref: nothing in the mapping says which variant it means, so it stays on the
// type, in either branch order, and validation reports it.
func TestDiscriminatorMapping_TwoVariantsAliasingATypeRouteToNeither(t *testing.T) {
	t.Parallel()
	spec := func(first, second string) string {
		return `openapi: 3.1.0
info: {title: t, version: "1"}
paths: {}
components:
  schemas:
    Cat: {type: object, properties: {kind: {type: string}}}
    Pet:
      oneOf:
        - {$ref: '#/components/schemas/Cat', description: ` + first + `}
        - {$ref: '#/components/schemas/Cat', description: ` + second + `}
      discriminator: {propertyName: kind, mapping: {cat: '#/components/schemas/Cat'}}
`
	}
	for _, src := range []string{spec("one", "two"), spec("two", "one")} {
		doc, diags := parseFull(t, src)
		openapitest.RequireNoErrorDiags(t, diags)
		u, ok := typeByName(doc, "Pet").(*ir.Union)
		require.True(t, ok)
		assert.Equal(t, ir.TypeID("t/openapi/components/schemas/Cat"), u.Discriminator.Mapping["cat"])
		assert.Contains(t, irtest.DiagCodes(validated(t, doc)), "pass/discriminator-missing-variant")
	}
}

// TestDiscriminatorMapping_ReadsThroughWhateverIsBesideAPositionsRef pins that a
// target naming a position that writes a $ref means what the $ref names,
// whatever is written beside it, in a carried position and at one holding an
// alias, for a union's variants and a model's subtypes alike: the position is
// no variant or subtype, so stopping at it reports a mapping the document
// meant (GitHub #764).
func TestDiscriminatorMapping_ReadsThroughWhateverIsBesideAPositionsRef(t *testing.T) {
	t.Parallel()
	for _, beside := range []string{"description: d", "k-beside: 1", "minLength: 1"} {
		t.Run(beside, func(t *testing.T) {
			t.Parallel()
			doc, diags := parseFull(t, `openapi: 3.1.0
info: {title: t, version: "1"}
paths: {}
components:
  schemas:
    Cat: {type: object, properties: {kind: {type: string}}}
    Dog: {type: object, properties: {kind: {type: string}}}
    Holder: {type: object, properties: {p: {$ref: '#/components/schemas/Cat', `+beside+`}}}
    Dogs: {type: array, items: {$ref: '#/components/schemas/Dog', `+beside+`}}
    Pet:
      oneOf: [{$ref: '#/components/schemas/Cat'}, {$ref: '#/components/schemas/Dog'}]
      discriminator:
        propertyName: kind
        mapping: {c: '#/components/schemas/Holder/properties/p', d: '#/components/schemas/Dogs/items'}
    Base:
      type: object
      properties: {kind: {type: string}}
      discriminator:
        propertyName: kind
        mapping: {s: '#/components/schemas/Subs/items'}
    Sub: {allOf: [{$ref: '#/components/schemas/Base'}]}
    Subs: {type: array, items: {$ref: '#/components/schemas/Sub', `+beside+`}}
`)
			openapitest.RequireNoErrorDiags(t, diags)
			assert.Empty(t, validated(t, doc))
			pet, ok := typeByName(doc, "Pet").(*ir.Union)
			require.True(t, ok)
			assert.Empty(t, cmp.Diff(map[string]ir.TypeID{
				"c": "t/openapi/components/schemas/Cat", "d": "t/openapi/components/schemas/Dog",
			}, pet.Discriminator.Mapping))
			base, ok := typeByName(doc, "Base").(*ir.Model)
			require.True(t, ok)
			assert.Equal(t, ir.TypeID("t/openapi/components/schemas/Sub"), base.Discriminator.Mapping["s"])
			sub, ok := typeByName(doc, "Sub").(*ir.Model)
			require.True(t, ok)
			assert.Equal(t, "s", sub.DiscriminatorValue)
		})
	}
}
