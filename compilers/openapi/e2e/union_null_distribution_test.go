// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package e2e_test

import (
	"encoding/json/jsontext"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
)

// nullSpellings are the ways a 3.1 union branch admits null alone.
var nullSpellings = []string{"{type: 'null'}", "{enum: [null]}", "{const: null}"}

// distributedBodies are bodies beside a union of two objects and a null
// branch, each with whether the reference to the position admits null: a body
// writing `type: object` forbids it, one listing null in its type admits it,
// and one writing no type says nothing of it, so the null branch decides.
var distributedBodies = []struct {
	name, body string
	nullable   bool
}{
	{"Typed", "type: object, properties: {p: {type: string}}", false},
	{"TypedNull", "type: [object, 'null'], properties: {p: {type: string}}", true},
	{"Closed", "additionalProperties: false", true},
}

// TestUnionNullVariant_ADistributedUnionLiftsItsNullBranch holds a union with a
// null branch beside a body it distributes across: the null branch is no
// variant, as it is none of a plain union, so the union is one model per other
// branch, each over its branch and holding the body, and a reference to the
// position admits null where the body admits it too, in every spelling of the
// branch, in both orders of the branches and under either policy.
func TestUnionNullVariant_ADistributedUnionLiftsItsNullBranch(t *testing.T) {
	t.Parallel()
	for _, null := range nullSpellings {
		for _, last := range []bool{false, true} {
			branches := []string{"{$ref: '#/components/schemas/A'}", "{$ref: '#/components/schemas/B'}", null}
			if !last {
				slices.Reverse(branches)
			}
			schemas := slices.Clone(closedObjects)
			holder := make([]string, 0, len(distributedBodies))
			for _, b := range distributedBodies {
				schemas = append(schemas, b.name+": {"+b.body+", oneOf: ["+strings.Join(branches, ", ")+"]}")
				holder = append(holder, strings.ToLower(b.name)+": {$ref: '#/components/schemas/"+b.name+"'}")
			}
			schemas = append(schemas, "Holder: {type: object, properties: {"+strings.Join(holder, ", ")+"}}")
			t.Run(fmt.Sprintf("%s/null last=%t", null, last), func(t *testing.T) {
				t.Parallel()
				for _, off := range []bool{false, true} {
					inBothOrders(t, schemas, func(t *testing.T, schemas []string) {
						doc, diags := compileKindInference(t, "null-branch", admissionSpec("3.1.0", "", schemas...), off)
						requireErrorsAndWarnings(t, diags, nil)
						holderModel, ok := kindNode(t, doc, componentID("Holder")).(*ir.Model)
						require.True(t, ok)
						for _, b := range distributedBodies {
							variants := distributedOver(t, doc, componentID(b.name))
							assert.ElementsMatch(t, []ir.TypeID{componentID("A"), componentID("B")},
								keysOf(variants), "%s: one model per branch but the null", b.name)
							assert.Empty(t, keptUnder(doc, jsontext.Pointer("/components/schemas/"+b.name)), b.name)
							property, ok := propByWire(holderModel, strings.ToLower(b.name))
							require.True(t, ok, b.name)
							assert.Equal(t, componentID(b.name), property.Type.Target, b.name)
							assert.Equal(t, b.nullable, property.Type.Nullable, "%s: the reference admits null as the body does", b.name)
						}
					})
				}
			})
		}
	}
}

// keysOf returns the keys of m.
func keysOf(m map[ir.TypeID]*ir.Model) []ir.TypeID {
	out := make([]ir.TypeID, 0, len(m))
	for typeID := range m {
		out = append(out, typeID)
	}
	return out
}

// TestUnionNullVariant_AUnionAloneOnAnEntryIsTheReferenceItsBareSpellingIs
// holds a union with a null branch written alone on an allOf entry to lower as
// the same union written at the position: a nullable reference to its one other
// branch, with no node of its own or composed for a variant, nothing kept and
// nothing reported, in both orders of the components.
func TestUnionNullVariant_AUnionAloneOnAnEntryIsTheReferenceItsBareSpellingIs(t *testing.T) {
	t.Parallel()
	schemas := []string{
		"X: {type: object, properties: {id: {type: string}}}",
		"H: {type: object, properties: {p: {allOf: [{oneOf: [{$ref: '#/components/schemas/X'}, {type: 'null'}]}]}, " +
			"q: {oneOf: [{$ref: '#/components/schemas/X'}, {type: 'null'}]}}}",
	}
	inBothOrders(t, schemas, func(t *testing.T, schemas []string) {
		doc, diags := compileKindInference(t, "branch", admissionSpec("3.1.0", "", schemas...), false)
		requireErrorsAndWarnings(t, diags, nil)
		model, ok := kindNode(t, doc, componentID("H")).(*ir.Model)
		require.True(t, ok)
		property, ok := propByWire(model, "p")
		require.True(t, ok)
		irProperty, ok := propByWire(model, "q")
		require.True(t, ok)
		assert.Equal(t, ir.TypeRef{Target: componentID("X"), Nullable: true}, irProperty.Type, "the bare union is X, nullable")
		assert.Equal(t, irProperty.Type, property.Type, "the union on an entry is what the bare union is")
		for id := range doc.Types {
			assert.NotContains(t, string(id), "/properties/p", "no node stands for p or beneath it")
		}
		assert.Empty(t, keptUnder(doc, "/components/schemas/H/properties/p"), "nothing is kept")
		assert.Empty(t, property.Unmodeled, "the property keeps nothing")
		assert.Empty(t, degradedUnder(diags, "/components/schemas/H/properties/p"), "nothing is reported")
		for _, diagnostic := range diags {
			assert.NotContains(t, string(diagnostic.Provenance.Pointer), "/properties/p", "no finding at p: %s", diagnostic.Message)
		}
	})
}
