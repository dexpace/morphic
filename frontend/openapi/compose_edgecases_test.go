package openapi

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
)

func TestAllOf_DiscriminatorHierarchy(t *testing.T) {
	t.Parallel()
	spec := componentSpecVer("3.2.0", `    Pet:
      type: object
      discriminator:
        propertyName: petType
        mapping:
          cat: '#/components/schemas/Cat'
        defaultMapping: '#/components/schemas/Dog'
      properties: {petType: {type: string}}
    Extra:
      type: object
      properties: {x: {type: string}}
    Cat:
      allOf:
        - {$ref: '#/components/schemas/Pet'}
        - {type: object, properties: {meow: {type: boolean}}}
    Dog:
      allOf:
        - {$ref: '#/components/schemas/Pet'}
        - {$ref: '#/components/schemas/Extra'}
        - {type: object, properties: {bark: {type: boolean}}}
`)
	doc, _ := lowerSpec(t, spec)

	pet := typeByName(doc, "Pet").(*ir.Model)
	require.NotNil(t, pet.Discriminator)
	assert.NotEmpty(t, pet.Discriminator.Property, "declared petType resolves to a PropID")
	assert.NotEmpty(t, pet.Discriminator.Mapping)
	assert.NotEmpty(t, pet.Discriminator.Default, "defaultMapping resolved")

	cat := typeByName(doc, "Cat").(*ir.Model)
	require.NotNil(t, cat.Base)
	assert.Equal(t, "cat", cat.DiscriminatorValue, "mapping key wins")

	dog := typeByName(doc, "Dog").(*ir.Model)
	require.NotNil(t, dog.Base, "the discriminator-anchoring ref is the base")
	require.Len(t, dog.Mixins, 1, "the second ref becomes a mixin")
	assert.Equal(t, "Dog", dog.DiscriminatorValue, "falls back to schema name")
}

func TestModelDiscriminator_UndeclaredPropertyAndBadMapping(t *testing.T) {
	t.Parallel()
	spec := componentSpec(`    Vehicle:
      type: object
      discriminator:
        propertyName: kind
        mapping:
          car: '#'
      properties: {name: {type: string}}
`)
	doc, diags := lowerSpec(t, spec)
	v := typeByName(doc, "Vehicle").(*ir.Model)
	require.NotNil(t, v.Discriminator)
	assert.Empty(t, v.Discriminator.Property, "undeclared property")
	assert.Equal(t, "kind", v.Discriminator.PropertyName)
	var sawBad bool
	for _, d := range diags {
		if d.Code == codeUnresolvedRef {
			sawBad = true
		}
	}
	assert.True(t, sawBad, "bad mapping target diagnostic")
}

func TestOneOf_DiscriminatorWithDefault(t *testing.T) {
	t.Parallel()
	spec := componentSpecVer("3.2.0", `    Shape:
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
	doc, _ := lowerSpec(t, spec)
	u := typeByName(doc, "Shape").(*ir.Union)
	require.NotNil(t, u.Discriminator)
	assert.Equal(t, "shapeType", u.Discriminator.PropertyName)
	assert.NotEmpty(t, u.Discriminator.Default)
}

func TestAnyOf_ThreeVariantsWithNull(t *testing.T) {
	t.Parallel()
	spec := componentSpec(`    N:
      anyOf:
        - {type: string}
        - {type: integer}
        - {type: 'null'}
`)
	doc, diags := lowerSpec(t, spec)
	requireNoErrorDiags(t, diags)
	u := typeByName(doc, "N").(*ir.Union)
	assert.Len(t, u.Variants, 2, "null branch stripped from variants")
	assert.False(t, u.Exclusive, "anyOf is not exclusive")
}

func TestUnion_VariantHints(t *testing.T) {
	t.Parallel()
	spec := componentSpec(`    U:
      oneOf:
        - {$ref: '#/components/schemas/Named', description: sibling}
        - {type: string}
    Named: {type: object, properties: {a: {type: string}}}
`)
	doc, diags := lowerSpec(t, spec)
	requireNoErrorDiags(t, diags)
	u := typeByName(doc, "U").(*ir.Union)
	require.Len(t, u.Variants, 2)
	hints := []string{u.Variants[0].Name.Hint, u.Variants[1].Name.Hint}
	assert.Contains(t, hints, "Named", "ref-with-siblings hint from target name")
	assert.Contains(t, hints, "variant_1", "inline branch positional hint")
}

func TestAllOf_UnresolvedRefBranch(t *testing.T) {
	t.Parallel()
	spec := componentSpec(`    Bad:
      allOf:
        - {$ref: '#'}
        - {type: object, properties: {a: {type: string}}}
`)
	doc, diags := lowerSpec(t, spec)
	require.NotNil(t, doc)
	var sawUnresolved bool
	for _, d := range diags {
		if d.Code == codeUnresolvedRef {
			sawUnresolved = true
		}
	}
	assert.True(t, sawUnresolved)
}

func TestAllOf_MultiRefWithUnresolvedDoesNotAnchor(t *testing.T) {
	t.Parallel()
	spec := componentSpec(`    Base:
      type: object
      discriminator: {propertyName: t}
      properties: {t: {type: string}}
    Sub:
      allOf:
        - {$ref: '#/components/schemas/Ghost'}
        - {$ref: '#/components/schemas/Base'}
        - {type: object, properties: {a: {type: string}}}
`)
	doc, diags := lowerSpec(t, spec)
	require.NotNil(t, doc)
	sub := typeByName(doc, "Sub").(*ir.Model)
	require.NotNil(t, sub.Base, "the discriminator-anchoring Base becomes base despite an unresolved sibling ref")
	assert.Equal(t, "Sub", sub.DiscriminatorValue)
	_ = diags
}

func TestEnum_ValueTypeVariants(t *testing.T) {
	t.Parallel()
	spec := componentSpec(`    EInt: {type: integer, enum: [1, 2]}
    ENum: {type: number, enum: [1.5, 2.5]}
    EBool: {type: boolean, enum: [true, false]}
    ENoTypeBool: {enum: [true, false]}
    ENoTypeNum: {enum: [1, 2]}
    ENoTypeStr: {enum: [a, b]}
    EBytes: {enum: [!!binary aGk=]}
`)
	doc, diags := lowerSpec(t, spec)
	requireNoErrorDiags(t, diags)
	want := map[string]ir.PrimKind{
		"EInt": ir.PrimInteger, "ENum": ir.PrimNumber, "EBool": ir.PrimBool,
		"ENoTypeBool": ir.PrimBool, "ENoTypeNum": ir.PrimNumber,
		"ENoTypeStr": ir.PrimString, "EBytes": ir.PrimString,
	}
	for name, prim := range want {
		e, ok := typeByName(doc, name).(*ir.Enum)
		require.True(t, ok, "%s is an enum", name)
		assert.Equal(t, prim, e.ValueType, "%s value type", name)
	}
}

func TestEnum_HeterogeneousBecomesUnionWithBadValue(t *testing.T) {
	t.Parallel()
	spec := componentSpec("    Mixed:\n      enum: [active, .inf]\n")
	doc, diags := lowerSpec(t, spec)
	u, ok := typeByName(doc, "Mixed").(*ir.Union)
	require.True(t, ok, "heterogeneous enum lowers to a union of literals")
	assert.Len(t, u.Variants, 2)
	var sawDegraded, sawValueWarn bool
	for _, d := range diags {
		if d.Code == codeDegradedConstruct && d.Severity == ir.SeverityInfo {
			sawDegraded = true
		}
		if d.Severity == ir.SeverityWarning {
			sawValueWarn = true
		}
	}
	assert.True(t, sawDegraded, "heterogeneous-enum info diagnostic")
	assert.True(t, sawValueWarn, "unconvertible literal value warning")
}
