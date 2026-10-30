// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package schema_test

import (
	"encoding/json/jsontext"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/compilers/openapi/internal/schema"
	"github.com/dexpace/morphic/ir"
)

func TestAllOf_SoleRefBecomesBase(t *testing.T) {
	t.Parallel()
	spec := openapitest.ComponentSpec(`    Animal:
      type: object
      properties:
        name: {type: string}
    Dog:
      allOf:
        - {$ref: "#/components/schemas/Animal"}
        - type: object
          properties:
            bark: {type: string}
`)
	doc, diags := schema.LowerSpec(t, spec)
	openapitest.RequireNoErrorDiags(t, diags)
	dog, ok := doc.Types[componentID("Dog")].(*ir.Model)
	require.True(t, ok, "Dog should be a model")
	require.NotNil(t, dog.Base, "sole $ref becomes Base")
	assert.Equal(t, componentID("Animal"), dog.Base.Target)
	assert.Empty(t, dog.Mixins, "no extra refs, no mixins")
	require.Len(t, dog.Properties, 1, "only the inline branch's own property")
	assert.Equal(t, "bark", dog.Properties[0].Name.Source)
	assert.Contains(t, dog.Properties[0].Provenance.Pointer, "/allOf/1",
		"merged property provenance points into its allOf branch")
}

func TestAllOf_ExtraRefsBecomeMixins(t *testing.T) {
	t.Parallel()
	spec := openapitest.ComponentSpec(`    A:
      type: object
      properties:
        a: {type: string}
    B:
      type: object
      properties:
        b: {type: string}
    C:
      allOf:
        - {$ref: "#/components/schemas/A"}
        - {$ref: "#/components/schemas/B"}
        - type: object
          properties:
            c: {type: string}
`)
	doc, diags := schema.LowerSpec(t, spec)
	openapitest.RequireNoErrorDiags(t, diags)
	c, ok := doc.Types[componentID("C")].(*ir.Model)
	require.True(t, ok, "C should be a model")
	assert.Nil(t, c.Base, "two non-hierarchy refs, neither sole: no Base")
	require.Len(t, c.Mixins, 2, "both refs become mixins")
	assert.Equal(t, componentID("A"), c.Mixins[0].Target)
	assert.Equal(t, componentID("B"), c.Mixins[1].Target)
	require.Len(t, c.Properties, 1)
	assert.Equal(t, "c", c.Properties[0].Name.Source)
}

func TestAllOf_DiscriminatorHierarchy(t *testing.T) {
	t.Parallel()
	spec := openapitest.ComponentSpecVer("3.2.0", `    Pet:
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
	doc, _ := schema.LowerSpec(t, spec)

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

func TestAllOf_UnresolvedRefBranch(t *testing.T) {
	t.Parallel()
	spec := openapitest.ComponentSpec(`    Bad:
      allOf:
        - {$ref: '#'}
        - {type: object, properties: {a: {type: string}}}
`)
	doc, diags := schema.LowerSpec(t, spec)
	require.NotNil(t, doc)
	assert.True(t, openapitest.HasDiag(diags, diag.UnresolvedRef))
}

func TestAllOf_MultiRefWithUnresolvedDoesNotAnchor(t *testing.T) {
	t.Parallel()
	spec := openapitest.ComponentSpec(`    Base:
      type: object
      discriminator: {propertyName: t}
      properties: {t: {type: string}}
    Sub:
      allOf:
        - {$ref: '#/components/schemas/Ghost'}
        - {$ref: '#/components/schemas/Base'}
        - {type: object, properties: {a: {type: string}}}
`)
	doc, diags := schema.LowerSpec(t, spec)
	require.NotNil(t, doc)
	sub := typeByName(doc, "Sub").(*ir.Model)
	require.NotNil(t, sub.Base, "the discriminator-anchoring Base becomes base despite an unresolved sibling ref")
	assert.Equal(t, "Sub", sub.DiscriminatorValue)
	_ = diags
}

// TestAllOf_EmptyAllOfBesideNonObjectTypeNamesBoth pins that a branch whose
// empty allOf was not flattened lists allOf among the kept keywords on the
// warning arm too: only a capped branch leaves it out. The source breaks the
// schema's minItems, an error the load reports and parseFull tolerates.
func TestAllOf_EmptyAllOfBesideNonObjectTypeNamesBoth(t *testing.T) {
	t.Parallel()
	doc, _ := parseFull(t, openapitest.ComponentSpec(nestedAB+`    M:
      allOf:
        - {type: string, allOf: []}
        - {$ref: '#/components/schemas/A'}
`))
	msg := openapitest.DiagMessageAt(t, doc.Diagnostics, diag.DegradedConstruct,
		ir.SeverityWarning, "/components/schemas/M/allOf/0")
	assert.Contains(t, msg, "the branch (type, allOf) is kept verbatim")
}

// TestAllOf_FalseBranchBesideUnionClosesEveryVariant pins that a `false` allOf
// branch, flat or inside a wrapper, closes every variant of a schema that also
// declares a oneOf, and is kept and reported once for the document.
func TestAllOf_FalseBranchBesideUnionClosesEveryVariant(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, allOf, key, pointer string
	}{
		{"flat", `        - {$ref: '#/components/schemas/A'}
        - false
`, "openapi:allOf/1", "/components/schemas/U/allOf/1"},
		{"nested", `        - allOf:
            - {$ref: '#/components/schemas/A'}
            - false
`, "openapi:allOf/0/allOf/1", "/components/schemas/U/allOf/0/allOf/1"},
	}
	const pq = `    P: {type: object, properties: {p: {type: string}}}
    Q: {type: object, properties: {q: {type: string}}}
`
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			u := "    U:\n      oneOf: [{$ref: '#/components/schemas/P'}, {$ref: '#/components/schemas/Q'}]\n      allOf:\n" + tc.allOf
			for _, doc := range nestedOrders(t, u, nestedAB+pq) {
				for _, id := range []ir.TypeID{
					"t/composed/components/schemas/U/oneOf/0",
					"t/composed/components/schemas/U/oneOf/1",
				} {
					model, ok := doc.Types[id].(*ir.Model)
					require.True(t, ok, "%s should be a model", id)
					assert.Equal(t, ir.AdditionalClosed, model.Additional, id)
					entry, ok := model.Unmodeled[tc.key]
					require.True(t, ok, "%s: the false is kept under %s; got %+v", id, tc.key, model.Unmodeled)
					assert.Equal(t, "false", string(entry.Value))
					assert.Equal(t, jsontext.Pointer(tc.pointer), entry.Provenance.Pointer)
				}
				n := 0
				for _, d := range doc.Diagnostics {
					if d.Code == diag.FalseSchema && d.Severity == ir.SeverityInfo &&
						d.Provenance.Pointer == jsontext.Pointer(tc.pointer) {
						n++
					}
				}
				assert.Equal(t, 1, n, "one false-schema diagnostic for the document, not one per variant")
			}
		})
	}
}

// TestAllOf_FalseBranchClosesTheComposition pins what a `false` allOf branch
// does to the composed node. `false` admits nothing, so the composition admits
// nothing: the model closes and the branch is kept verbatim beside it, which is
// the same answer a bare `false` schema gets in its own right.
func TestAllOf_FalseBranchClosesTheComposition(t *testing.T) {
	t.Parallel()
	doc, diags := schema.LowerSpec(t, openapitest.ComponentSpec(`    Never:
      allOf:
        - false
        - type: object
          properties: {id: {type: string}}
`))
	openapitest.RequireNoErrorDiags(t, diags)

	m, ok := typeByName(doc, "Never").(*ir.Model)
	require.True(t, ok, "the composition still lowers to a model")
	assert.Equal(t, ir.AdditionalClosed, m.Additional, "a branch matching nothing closes the composition")
	require.Len(t, m.Unmodeled, 1, "the branch is kept verbatim")
	assert.Equal(t, ir.RawValue("false"), m.Unmodeled["openapi:allOf/0"].Value,
		"keyed by the branch index, so sibling branches cannot overwrite one another")
	assert.True(t, openapitest.HasDiagAt(diags, diag.FalseSchema, ir.SeverityInfo), "and it is announced")
}

// nestedOrders compiles first and last in both declaration orders and requires
// each to validate, report no error, and the two to agree on their types and on
// what they report. Flattening reads a nested branch's $refs, so a decision
// taken off the registry while M lowers would see only the targets declared
// before it.
func nestedOrders(t *testing.T, first, last string) []*ir.Document {
	t.Helper()
	require.NotEmpty(t, last, "one block compiles the same document twice")
	docs := make([]*ir.Document, 0, 2)
	for _, src := range []string{first + last, last + first} {
		doc, diags := parseFull(t, openapitest.ComponentSpec(src))
		openapitest.RequireNoErrorDiags(t, diags)
		assert.Empty(t, validated(t, doc), "the document must validate")
		docs = append(docs, doc)
	}
	assert.Empty(t, cmp.Diff(docs[0].Types, docs[1].Types, orderInvariantIR()...),
		"declaration order must not change the types")
	assert.Equal(t, reported(docs[0].Diagnostics), reported(docs[1].Diagnostics),
		"declaration order must not change what is reported")
	return docs
}

const nestedAB = `    A: {type: object, properties: {a: {type: string}}}
    B: {type: object, properties: {b: {type: string}}}
`
