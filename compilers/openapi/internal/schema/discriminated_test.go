package schema_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/pass"
)

const (
	cat = "    Cat: {type: object, properties: {kind: {type: string}, meow: {type: boolean}}}\n"
	dog = "    Dog: {type: object, properties: {kind: {type: string}, bark: {type: boolean}}}\n"

	// subtypes are Cat and Dog declared as the allOf subtypes of Pet.
	subtypes = "    Cat: {allOf: [{$ref: '#/components/schemas/Pet'}, {type: object, properties: {meow: {type: boolean}}}]}\n" +
		"    Dog: {allOf: [{$ref: '#/components/schemas/Pet'}, {type: object, properties: {bark: {type: boolean}}}]}\n"

	petBranches = "      oneOf:\n        - $ref: '#/components/schemas/Cat'\n        - $ref: '#/components/schemas/Dog'\n"
)

// petWith declares Pet, a model whose oneOf names Cat and Dog, beside the
// discriminator text given.
func petWith(discriminator string) string {
	return "    Pet:\n      type: object\n      discriminator: " + discriminator + "\n" + petBranches
}

// discriminatedOrders compiles pet and rest in both declaration orders, so a
// lowering that read the registry mid-walk would answer differently for each.
func discriminatedOrders(t *testing.T, version, pet, rest string) []*ir.Document {
	t.Helper()
	docs := make([]*ir.Document, 0, 2)
	for _, src := range []string{pet + rest, rest + pet} {
		doc, diags := parseFull(t, openapitest.ComponentSpecVer(version, src))
		openapitest.RequireNoErrorDiags(t, diags)
		assert.Empty(t, pass.Validate(doc), "the document must validate")
		docs = append(docs, doc)
	}
	assert.Empty(t, cmp.Diff(docs[0], docs[1], orderInvariantIR()...), "declaration order must not change the document")
	return docs
}

// TestDiscriminatedUnion_NonSubtypeTargetsStayVerbatim pins that the union kept
// verbatim beside a model does not leave its discriminator on that model when a
// target it routes to is not a subtype of it, which pass.Validate rejects as
// discriminator-missing-variant. The whole discriminator moves to Unmodeled
// beside the union, with the reason the union is kept for. Each row is compiled
// in both declaration orders.
func TestDiscriminatedUnion_NonSubtypeTargetsStayVerbatim(t *testing.T) {
	t.Parallel()
	const mixed = "    Cat: {allOf: [{$ref: '#/components/schemas/Pet'}], type: object}\n"
	cases := []struct {
		name          string
		version       string
		discriminator string
		rest          string
		wantValue     string
	}{
		{"explicit mapping", "3.1.0", "{propertyName: kind, mapping: {cat: '#/components/schemas/Cat'}}", cat + dog, "mapping"},
		{"inferred by name", "3.1.0", "{propertyName: kind}", cat + dog, "propertyName"},
		{"one subtype and one not", "3.1.0",
			"{propertyName: kind, mapping: {cat: '#/components/schemas/Cat', dog: '#/components/schemas/Dog'}}",
			mixed + dog, "dog"},
		{"default mapping", "3.2.0", "{propertyName: kind, defaultMapping: '#/components/schemas/Dog'}", cat + dog, "defaultMapping"},
		{"mixin only", "3.1.0", "{propertyName: kind, mapping: {cat: '#/components/schemas/Cat'}}",
			"    Cat: {allOf: [{$ref: '#/components/schemas/Dog'}, {$ref: '#/components/schemas/Other'}]}\n" +
				dog + "    Other: {type: object}\n", "mapping"},
		{"position that declares an inline object", "3.1.0",
			"{propertyName: kind, mapping: {cat: '#/components/schemas/Holder/properties/pet'}}",
			cat + dog + "    Holder: {type: object, properties: {pet: {type: object, properties: {kind: {type: string}}}}}\n",
			"mapping"},
		{"component that is only an alias", "3.1.0", "{propertyName: kind, mapping: {cat: '#/components/schemas/Alias'}}",
			subtypes + "    Alias: {$ref: '#/components/schemas/Cat'}\n", "mapping"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			for _, doc := range discriminatedOrders(t, c.version, petWith(c.discriminator), c.rest) {
				pet, ok := typeByName(doc, "Pet").(*ir.Model)
				require.True(t, ok, "Pet lowers to a model beside its union")
				assert.Nil(t, pet.Discriminator, "a discriminator routing to a non-subtype stays off the model")
				entry, ok := pet.Unmodeled["openapi:discriminator"]
				require.True(t, ok, "the discriminator is kept verbatim")
				assert.Equal(t, ir.ReasonDegradedLowering, entry.Reason)
				assert.Contains(t, string(entry.Value), c.wantValue)
				assert.Contains(t, string(entry.Value), `"propertyName":"kind"`, "the whole object is kept")
				_, ok = pet.Unmodeled["openapi:oneOf"]
				assert.True(t, ok, "the union stays beside it")
			}
		})
	}
}

// TestDiscriminatedUnion_SaysItKeptTheDiscriminator pins that the diagnostic
// for the kept union names the discriminator when it moved too, and does not
// when it stayed on the model.
func TestDiscriminatedUnion_SaysItKeptTheDiscriminator(t *testing.T) {
	t.Parallel()
	const pointer = "/components/schemas/Pet"
	moved := petWith("{propertyName: kind, mapping: {cat: '#/components/schemas/Cat'}}")
	_, diags := lowerSpec(t, openapitest.ComponentSpec(moved+cat+dog))
	assert.Contains(t, openapitest.DiagMessageAt(t, diags, diag.DegradedConstruct, ir.SeverityInfo, pointer),
		"not a subtype of this model, so the discriminator is kept verbatim too")

	stayed := petWith("{propertyName: kind, mapping: {cat: '#/components/schemas/Cat'}}")
	_, diags = lowerSpec(t, openapitest.ComponentSpec(stayed+subtypes))
	assert.NotContains(t, openapitest.DiagMessageAt(t, diags, diag.DegradedConstruct, ir.SeverityInfo, pointer),
		"discriminator is kept")
}

// TestDiscriminatedUnion_SubtypeTargetsKeepTheDiscriminator pins the one case
// the model keeps its discriminator beside a kept union: every target it routes
// to is a subtype, at any distance. It must hold in both declaration orders,
// which is why the rule reads the schemas and not the lowered registry, where a
// subtype declared after Pet has no Base yet while Pet lowers.
func TestDiscriminatedUnion_SubtypeTargetsKeepTheDiscriminator(t *testing.T) {
	t.Parallel()
	const grand = "    Cat: {allOf: [{$ref: '#/components/schemas/Mid'}], type: object}\n" +
		"    Mid: {allOf: [{$ref: '#/components/schemas/Pet'}], type: object}\n" +
		"    Dog: {allOf: [{$ref: '#/components/schemas/Pet'}], type: object}\n"
	cases := []struct {
		name          string
		version       string
		discriminator string
		rest          string
	}{
		{"explicit mapping", "3.1.0", "{propertyName: kind, mapping: {cat: '#/components/schemas/Cat'}}", subtypes},
		{"inferred by name", "3.1.0", "{propertyName: kind}", subtypes},
		{"mapping by bare name", "3.1.0", "{propertyName: kind, mapping: {cat: Cat, dog: Dog}}", subtypes},
		{"position that is only a $ref", "3.1.0",
			"{propertyName: kind, mapping: {cat: '#/components/schemas/Holder/properties/pet'}}",
			subtypes + "    Holder: {type: object, properties: {pet: {$ref: '#/components/schemas/Cat'}}}\n"},
		{"grandchild", "3.1.0", "{propertyName: kind, mapping: {cat: '#/components/schemas/Cat'}}", grand},
		{"default mapping", "3.2.0", "{propertyName: kind, defaultMapping: '#/components/schemas/Dog'}", subtypes},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			for _, doc := range discriminatedOrders(t, c.version, petWith(c.discriminator), c.rest) {
				pet, ok := typeByName(doc, "Pet").(*ir.Model)
				require.True(t, ok, "Pet lowers to a model beside its union")
				require.NotNil(t, pet.Discriminator, "every target is a subtype, so the model keeps it")
				assert.Equal(t, "kind", pet.Discriminator.PropertyName)
				assert.NotContains(t, pet.Unmodeled, "openapi:discriminator", "nothing is stored twice")
				assert.Contains(t, pet.Unmodeled, "openapi:oneOf")
			}
		})
	}
}

// TestDiscriminatedUnion_UnresolvedAndCyclicTargets pins that the rule
// terminates and drops what it cannot read: a mapping value that names nothing
// is skipped (discriminatorMapping reports it), and a composition cycle that
// never reaches Pet is no subtype.
func TestDiscriminatedUnion_UnresolvedAndCyclicTargets(t *testing.T) {
	t.Parallel()
	pet := petWith("{propertyName: kind, mapping: {gone: '#/components/schemas/Nope', far: 'https://example.com/x.yaml#/A', " +
		"loop: '#/components/schemas/Loop'}}")
	rest := cat + dog +
		"    Loop: {allOf: [{$ref: '#/components/schemas/LoopB'}]}\n" +
		"    LoopB: {allOf: [{$ref: '#/components/schemas/Loop'}]}\n"
	doc, _ := lowerSpec(t, openapitest.ComponentSpec(pet+rest))
	m, ok := typeByName(doc, "Pet").(*ir.Model)
	require.True(t, ok)
	assert.Nil(t, m.Discriminator, "the cycle never reaches Pet")
	assert.Contains(t, m.Unmodeled, "openapi:discriminator")
}

// TestDiscriminatedUnion_DeepBaseChainIsBounded pins that the walk up the base
// chain stops at its bound on a chain longer than it, rather than following it.
func TestDiscriminatedUnion_DeepBaseChainIsBounded(t *testing.T) {
	t.Parallel()
	var b strings.Builder
	for i := range 300 {
		fmt.Fprintf(&b, "    C%d: {allOf: [{$ref: '#/components/schemas/C%d'}]}\n", i, i+1)
	}
	b.WriteString("    C300: {type: object}\n")
	pet := petWith("{propertyName: kind, mapping: {c: '#/components/schemas/C0'}}")
	doc, _ := lowerSpec(t, openapitest.ComponentSpec(pet+cat+dog+b.String()))
	m, ok := typeByName(doc, "Pet").(*ir.Model)
	require.True(t, ok)
	assert.Nil(t, m.Discriminator)
	assert.Contains(t, m.Unmodeled, "openapi:discriminator")
}

// TestDiscriminatedUnion_KeepsAnAllOfBodyToo pins the other model lowering
// reachable beside a kept union: a body composed with allOf lowers through
// lowerAllOf, and its discriminator is held to the same rule.
func TestDiscriminatedUnion_KeepsAnAllOfBodyToo(t *testing.T) {
	t.Parallel()
	pet := "    Pet:\n      allOf: [{$ref: '#/components/schemas/Base'}]\n" +
		"      discriminator: {propertyName: kind, mapping: {cat: '#/components/schemas/Cat'}}\n" + petBranches
	doc, diags := parseFull(t, openapitest.ComponentSpec(pet+cat+dog+"    Base: {type: object}\n"))
	openapitest.RequireNoErrorDiags(t, diags)
	assert.Empty(t, pass.Validate(doc))
	m, ok := typeByName(doc, "Pet").(*ir.Model)
	require.True(t, ok)
	assert.Nil(t, m.Discriminator)
	assert.Contains(t, m.Unmodeled, "openapi:discriminator")
}

// degradedShapes are the model-bodied unions kept verbatim beside a
// discriminator other than the one the rule was first written for, each with
// the branch text that makes it that shape.
var degradedShapes = []struct {
	name     string
	branches string
}{
	{"inline branch", "      oneOf: [{$ref: '#/components/schemas/Cat'}, {type: object, properties: {kind: {type: string}}}]\n"},
	{"oneOf beside anyOf", "      oneOf: [{$ref: '#/components/schemas/Cat'}]\n      anyOf: [{$ref: '#/components/schemas/Dog'}]\n"},
	{"validation-only branches", "      oneOf: [{required: [kind]}, {required: [x]}]\n"},
}

const degradedMapping = "{propertyName: kind, mapping: {cat: '#/components/schemas/Cat'}}"

func degradedPet(branches string) string {
	return "    Pet:\n      type: object\n      discriminator: " + degradedMapping + "\n" + branches
}

// TestDiscriminatedUnion_EveryDegradedShapeGatesTheDiscriminator pins that a
// discriminator routing to a non-subtype leaves the model for Unmodeled on
// every degraded union, not only the discriminated one, so pass.Validate
// accepts the document in both declaration orders.
func TestDiscriminatedUnion_EveryDegradedShapeGatesTheDiscriminator(t *testing.T) {
	t.Parallel()
	for _, shape := range degradedShapes {
		t.Run(shape.name, func(t *testing.T) {
			t.Parallel()
			for _, doc := range discriminatedOrders(t, "3.1.0", degradedPet(shape.branches), cat+dog) {
				pet, ok := typeByName(doc, "Pet").(*ir.Model)
				require.True(t, ok, "Pet lowers to a model beside its union")
				assert.Nil(t, pet.Discriminator, "a non-subtype target stays off the model")
				assert.Contains(t, pet.Unmodeled, "openapi:discriminator")
			}
			for _, src := range []string{degradedPet(shape.branches) + cat + dog, cat + dog + degradedPet(shape.branches)} {
				_, diags := parseFull(t, openapitest.ComponentSpecVer("3.1.0", src))
				assert.Equal(t, 1, countMovedDiscriminator(diags), "exactly one diagnostic names the move: %+v", diags)
			}
		})
	}
}

// countMovedDiscriminator counts the diagnostics saying the discriminator was
// kept verbatim because it routes to a target that is not a subtype.
func countMovedDiscriminator(diags []ir.Diagnostic) int {
	n := 0
	for _, d := range diags {
		if strings.Contains(d.Message, "not a subtype of this model") {
			n++
		}
	}
	return n
}

// TestDiscriminatedUnion_DegradedShapesKeepSubtypeDiscriminators pins that the
// gate does not over-reach: a mapping whose every target is a subtype keeps the
// discriminator on the model on each degraded shape.
func TestDiscriminatedUnion_DegradedShapesKeepSubtypeDiscriminators(t *testing.T) {
	t.Parallel()
	for _, shape := range degradedShapes {
		t.Run(shape.name, func(t *testing.T) {
			t.Parallel()
			for _, doc := range discriminatedOrders(t, "3.1.0", degradedPet(shape.branches), subtypes) {
				pet, ok := typeByName(doc, "Pet").(*ir.Model)
				require.True(t, ok)
				assert.NotNil(t, pet.Discriminator, "every target is a subtype")
				assert.NotContains(t, pet.Unmodeled, "openapi:discriminator")
			}
		})
	}
}

// TestDiscriminatedUnion_InlineBranchesNameNoSubtype pins that with no mapping
// entry resolving, a union whose only branch is inline gives the discriminator
// nothing to route to, so it moves to Unmodeled.
func TestDiscriminatedUnion_InlineBranchesNameNoSubtype(t *testing.T) {
	t.Parallel()
	pet := "    Pet:\n      type: object\n      discriminator: {propertyName: kind}\n" +
		"      oneOf: [{type: object, properties: {kind: {type: string}}}]\n"
	for _, doc := range discriminatedOrders(t, "3.1.0", pet, cat) {
		m, ok := typeByName(doc, "Pet").(*ir.Model)
		require.True(t, ok)
		assert.Nil(t, m.Discriminator)
		assert.Contains(t, m.Unmodeled, "openapi:discriminator")
	}
}

// TestDiscriminatedUnion_UnresolvedBranchGatesTheDiscriminator pins the same
// rule on a union with a $ref branch that resolves nothing. The unresolved
// $ref is itself an error diagnostic, so the document is checked with
// pass.Validate directly rather than through the no-error harness.
func TestDiscriminatedUnion_UnresolvedBranchGatesTheDiscriminator(t *testing.T) {
	t.Parallel()
	const branches = "      oneOf: [{$ref: '#/components/schemas/Cat'}, {$ref: '#/components/schemas/Nope'}]\n"
	cases := []struct {
		name, rest string
		keeps      bool
	}{
		{"non-subtype target", cat + dog, false},
		{"subtype target", subtypes, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			pet := degradedPet(branches)
			for _, src := range []string{pet + c.rest, c.rest + pet} {
				doc, _ := lowerSpec(t, openapitest.ComponentSpec(src))
				assert.Empty(t, pass.Validate(doc), "the document must validate")
				m, ok := typeByName(doc, "Pet").(*ir.Model)
				require.True(t, ok)
				assert.Equal(t, c.keeps, m.Discriminator != nil)
				assert.Equal(t, !c.keeps, hasKey(m.Unmodeled, "openapi:discriminator"))
			}
		})
	}
}

func hasKey(m ir.Unmodeled, key string) bool {
	_, ok := m[key]
	return ok
}
