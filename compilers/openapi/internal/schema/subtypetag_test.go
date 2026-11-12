// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package schema_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/ir"
)

// pinnedRules declares Block and Log, objects pinning `action` to a value each,
// for a union to list.
const pinnedRules = "    Block: {type: object, required: [action], properties: {action: {const: block}}}\n" +
	"    Log: {type: object, required: [action], properties: {action: {const: log}}}\n"

// stored composes Rule with a property of its own.
const stored = "    Stored:\n      allOf:\n        - $ref: '#/components/schemas/Rule'\n" +
	"        - {type: object, required: [id], properties: {id: {type: string}}}\n"

// TestDiscriminatorValue_TheImplicitMappingRoutesWhatAUnionLists pins which
// schemas a discriminator routes by name (docs/compilers/openapi.md#discriminator-value):
// one beside no oneOf or anyOf every schema composing it, and one beside either
// only the components that union lists, the oneOf's where both are written, so
// a schema composing it unlisted, a narrowing of the union, carries no tag,
// whatever its variants pin and whether or not a mapping names others. A
// farther ancestor routing by name still tags it. Each is read in both
// declaration orders.
func TestDiscriminatorValue_TheImplicitMappingRoutesWhatAUnionLists(t *testing.T) {
	t.Parallel()
	const union = "      oneOf: [{$ref: '#/components/schemas/Block'}, {$ref: '#/components/schemas/Log'}]\n"
	cases := []struct {
		name, base, rest string
		want             map[string]string // subtype -> its tag, "" for none
	}{
		{"a union pinning its tags", "    Rule:\n      discriminator: {propertyName: action}\n" + union,
			pinnedRules + stored, map[string]string{"Stored": ""}},
		{"a union pinning none", "    Rule:\n      discriminator: {propertyName: action}\n" + union,
			"    Block: {type: object, properties: {action: {type: string}}}\n" +
				"    Log: {type: object, properties: {action: {type: string}}}\n" + stored,
			map[string]string{"Stored": ""}},
		{"a union mapping one variant", "    Rule:\n      discriminator: {propertyName: action, " +
			"mapping: {block: '#/components/schemas/Block'}}\n" + union, pinnedRules + stored,
			map[string]string{"Stored": ""}},
		{"an anyOf", "    Rule:\n      discriminator: {propertyName: action}\n" +
			"      anyOf: [{$ref: '#/components/schemas/Block'}, {$ref: '#/components/schemas/Log'}]\n",
			pinnedRules + stored, map[string]string{"Stored": ""}},
		{"an anyOf listing it beside a oneOf", "    Rule:\n      type: object\n      properties: {action: {type: string}}\n" +
			"      discriminator: {propertyName: action}\n      oneOf: [{$ref: '#/components/schemas/Block'}]\n" +
			"      anyOf: [{$ref: '#/components/schemas/Stored'}]\n", pinnedRules + stored, map[string]string{"Stored": ""}},
		{"a union listing a subtype", "    Rule:\n      type: object\n      properties: {action: {type: string}}\n" +
			"      discriminator: {propertyName: action}\n      oneOf: [{$ref: '#/components/schemas/Listed'}]\n",
			"    Listed:\n      allOf:\n        - $ref: '#/components/schemas/Rule'\n" +
				"        - {type: object, properties: {note: {type: string}}}\n" + stored,
			map[string]string{"Listed": "Listed", "Stored": ""}},
		{"no union", "    Rule:\n      type: object\n      properties: {action: {type: string}}\n" +
			"      discriminator: {propertyName: action}\n", stored, map[string]string{"Stored": "Stored"}},
		{"a farther ancestor routing by name", "    Rule:\n      allOf: [{$ref: '#/components/schemas/Animal'}]\n" +
			"      discriminator: {propertyName: action}\n" + union,
			"    Animal: {type: object, properties: {kind: {type: string}}, discriminator: {propertyName: kind}}\n" +
				pinnedRules + stored, map[string]string{"Stored": "Stored"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			for _, doc := range taggedOrders(t, c.base, c.rest) {
				for name, tag := range c.want {
					m, ok := typeByName(doc, name).(*ir.Model)
					require.True(t, ok, "%s lowers to a model; got %T", name, typeByName(doc, name))
					assert.Equal(t, tag, m.DiscriminatorValue, name)
					assert.Empty(t, m.Provenance.Inferred, name)
				}
			}
		})
	}
}

// TestDiscriminatorValue_AUnionListsTheSchemaItsBranchEndsOn pins what a
// union written in another document lists: the schemas its $ref branches end
// on there, as discriminatedAncestors reads a $ref, so a subtype it lists is
// routed by its name and one it does not is not
// (docs/compilers/openapi.md#discriminator-value), in either declaration order.
func TestDiscriminatorValue_AUnionListsTheSchemaItsBranchEndsOn(t *testing.T) {
	t.Parallel()
	const pet = "    Pet:\n      type: object\n      properties: {kind: {type: string}}\n" +
		"      discriminator: {propertyName: kind}\n      oneOf: [{$ref: '#/components/schemas/Cat'}]\n"
	const subtypes = "    Cat: {allOf: [{$ref: '#/components/schemas/Pet'}, {properties: {meow: {type: boolean}}}]}\n" +
		"    Bird: {allOf: [{$ref: '#/components/schemas/Pet'}, {properties: {wings: {type: integer}}}]}\n"
	for _, schemas := range []string{pet + subtypes, subtypes + pet} {
		doc, diags, _ := compileBeside(t, map[string]string{"pets.yaml": "components:\n  schemas:\n" + schemas}, `openapi: 3.1.0
info: {title: T, version: "1"}
paths: {}
components:
  schemas:
    Holder:
      type: object
      properties:
        cat: {$ref: './pets.yaml#/components/schemas/Cat'}
        bird: {$ref: './pets.yaml#/components/schemas/Bird'}
`)
		openapitest.RequireNoErrorDiags(t, diags)
		for name, tag := range map[string]string{"Cat": "Cat", "Bird": ""} {
			m, ok := doc.Types[ir.TypeID("t/openapi@pets.yaml/components/schemas/"+name)].(*ir.Model)
			require.True(t, ok, "%s lowers to a model", name)
			assert.Equal(t, tag, m.DiscriminatorValue, name)
		}
	}
}

// petBase declares Pet, the base of a hierarchy its discriminator on `kind` routes
// by name, its tag property admitting the type written as kind.
func petBase(kind string) string {
	return "    Pet:\n      type: object\n      required: [kind]\n      properties: {kind: " + kind + "}\n" +
		"      discriminator: {propertyName: kind}\n"
}

// petSubtype declares a subtype of Pet named name, adding the properties props.
func petSubtype(name, props string) string {
	return "    " + name + ":\n      allOf:\n        - $ref: '#/components/schemas/Pet'\n" +
		"        - {type: object, properties: " + props + "}\n"
}

// TestDiscriminatorInference_ReadsTheTagASubtypeAdmits pins the policy's
// reading of a subtype a discriminator routes by its name
// (docs/compilers/openapi.md#discriminator-inference): the strings it admits
// for the tag, every declaration of the member met, may rule the name out. One
// is then its tag, several or none leave it untagged, either marked inferred
// and reported at it. A name admitted, a member bounding no strings, a mapping
// key, or a name only a composed union rules out, which is not read, stands.
// The member read is the routing ancestor's, not a nearer one's. Two subtypes
// admitting one string carry it both.
func TestDiscriminatorInference_ReadsTheTagASubtypeAdmits(t *testing.T) {
	t.Parallel()
	str := petBase("{type: string}")
	cases := []struct {
		name, base, rest string
		want             map[string]string // subtype -> its tag, "" for none
		report           string            // what Cat's report says it admits; "" for none
	}{
		{"its own const", str, petSubtype("Cat", "{kind: {const: cat}}"), map[string]string{"Cat": "cat"}, `only "cat"`},
		{"an enum of one", str, petSubtype("Cat", "{kind: {enum: [cat]}}"), map[string]string{"Cat": "cat"}, `only "cat"`},
		{"through an alias", str, petSubtype("Cat", "{kind: {$ref: '#/components/schemas/Tag'}}") +
			"    Tag: {$ref: '#/components/schemas/Tagged'}\n    Tagged: {const: cat}\n",
			map[string]string{"Cat": "cat"}, `only "cat"`},
		{"null beside it", str, petSubtype("Cat", "{kind: {enum: [cat, null]}}"), map[string]string{"Cat": "cat"}, `only "cat"`},
		{"the base's, shared", petBase("{type: string, enum: [cat]}"), petSubtype("Cat", "{meow: {type: boolean}}") +
			petSubtype("Lion", "{roar: {type: boolean}}"), map[string]string{"Cat": "cat", "Lion": "cat"}, `only "cat"`},
		{"the base's, redeclared a string", petBase("{type: string, enum: [cat]}"), petSubtype("Cat", "{kind: {type: string}}"),
			map[string]string{"Cat": "cat"}, `only "cat"`},
		{"declarations meeting", petBase("{type: string, enum: [cat, dog]}"), petSubtype("Cat", "{kind: {enum: [cat, kitty]}}"),
			map[string]string{"Cat": "cat"}, `only "cat"`},
		{"several", str, petSubtype("Cat", "{kind: {enum: [kitty, cat, tom, lion]}}"), map[string]string{"Cat": ""},
			`only "cat", "kitty", "lion", 1 more; no tag its instances carry routes to it, so it carries none`},
		{"none", petBase("{type: string, enum: [dog]}"), petSubtype("Cat", "{kind: {const: cat}}"), map[string]string{"Cat": ""},
			"no string; no tag its instances carry routes to it"},
		{"a union it composes, not read (#1712)", str, "    Cat:\n      allOf:\n        - $ref: '#/components/schemas/Pet'\n" +
			"        - $ref: '#/components/schemas/Breeds'\n" +
			"    Breeds: {oneOf: [{$ref: '#/components/schemas/Tabby'}, {$ref: '#/components/schemas/Siamese'}]}\n" +
			"    Tabby: {type: object, properties: {kind: {const: tabby}}}\n" +
			"    Siamese: {type: object, properties: {kind: {const: siamese}}}\n",
			map[string]string{"Cat": "Cat"}, ""},
		{"its name among them", str, petSubtype("Cat", "{kind: {enum: [Cat, cat]}}"), map[string]string{"Cat": "Cat"}, ""},
		{"its name", str, petSubtype("Cat", "{kind: {const: Cat}}"), map[string]string{"Cat": "Cat"}, ""},
		{"a number", petBase("{}"), petSubtype("Cat", "{kind: {const: 1}}"), map[string]string{"Cat": "Cat"}, ""},
		{"a mapping key", "    Pet:\n      type: object\n      properties: {kind: {type: string}}\n" +
			"      discriminator: {propertyName: kind, mapping: {kitty: '#/components/schemas/Cat'}}\n",
			petSubtype("Cat", "{kind: {const: cat}}"), map[string]string{"Cat": "kitty"}, ""},
		{"a farther ancestor's tag", str + "    Rule:\n      allOf: [{$ref: '#/components/schemas/Pet'}]\n" +
			"      discriminator: {propertyName: action}\n" +
			"      oneOf: [{$ref: '#/components/schemas/Block'}, {$ref: '#/components/schemas/Log'}]\n",
			pinnedRules + "    Cat: {allOf: [{$ref: '#/components/schemas/Rule'}, {properties: {kind: {const: cat}}}]}\n",
			map[string]string{"Cat": "cat"}, `only "cat"`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			for _, doc := range taggedOrders(t, c.base, c.rest) {
				for name, tag := range c.want {
					m, ok := typeByName(doc, name).(*ir.Model)
					require.True(t, ok, "%s lowers to a model; got %T", name, typeByName(doc, name))
					assert.Equal(t, tag, m.DiscriminatorValue, name)
					if c.report == "" {
						assert.Empty(t, m.Provenance.Inferred, name)
						assert.False(t, openapitest.HasDiagCodeAt(doc.Diagnostics, diag.InferredDiscriminator, "/components/schemas/"+name))
						continue
					}
					assert.Equal(t, openapi.DiscriminatorInferenceHeuristic, m.Provenance.Inferred, name)
				}
				if c.report != "" {
					assert.Contains(t, openapitest.DiagMessageAt(t, doc.Diagnostics, diag.InferredDiscriminator, ir.SeverityInfo,
						"/components/schemas/Cat"), "by its name, \"Cat\", a tag its \"kind\" rejects, admitting "+c.report)
				}
			}
		})
	}
}
