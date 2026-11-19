// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package e2e_test

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"fmt"
	"maps"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
)

// closedObjects are two closed objects each requiring a member, which a union
// beside an object keyword names as its branches.
var closedObjects = []string{
	"A: {type: object, properties: {a: {type: string}}, required: [a], additionalProperties: false}",
	"B: {type: object, properties: {b: {type: string}}, required: [b], additionalProperties: false}",
}

// unionOver is a oneOf over $refs to the components named, in that order.
func unionOver(names ...string) string {
	refs := make([]string, 0, len(names))
	for _, n := range names {
		refs = append(refs, "{$ref: '#/components/schemas/"+n+"'}")
	}
	return "oneOf: [" + strings.Join(refs, ", ") + "]"
}

// distributedOver returns the models a union at id composes, each by the
// target its Base names, failing where id is no union of models over a base.
func distributedOver(t *testing.T, doc *ir.Document, id ir.TypeID) map[ir.TypeID]*ir.Model {
	t.Helper()
	union, ok := kindNode(t, doc, id).(*ir.Union)
	require.True(t, ok, "%s is a union", id)
	out := map[ir.TypeID]*ir.Model{}
	for _, variant := range union.Variants {
		model, isModel := kindNode(t, doc, variant.Type.Target).(*ir.Model)
		require.True(t, isModel, "%s: variant %s", id, variant.Type.Target)
		require.NotNil(t, model.Base, "%s: variant %s", id, variant.Type.Target)
		out[model.Base.Target] = model
	}
	return out
}

// unionAsWritten is the union at id with each variant's node in place of its
// reference, encoded less the IDs, names and provenance a twin written at
// another component cannot share.
func unionAsWritten(t *testing.T, doc *ir.Document, id ir.TypeID) string {
	t.Helper()
	union, ok := kindNode(t, doc, id).(*ir.Union)
	require.True(t, ok, "%s is a union", id)
	tree := nodeWithoutNames(t, doc, id)
	variants := make([]any, 0, len(union.Variants))
	for _, variant := range union.Variants {
		variants = append(variants, nodeWithoutNames(t, doc, variant.Type.Target))
	}
	tree["variants"] = variants
	raw, err := json.Marshal(tree, json.Deterministic(true))
	require.NoError(t, err)
	return string(raw)
}

// TestKindInference_AClosureBesideAUnionOfObjectsDistributes holds a closure
// written beside a union of objects with no type: the branches declare the
// object, so it is the schema writing `type: object` beside them, a union of
// closed models over each branch, keeping nothing, under either policy and in
// both branch orders, a `nullable: true` beside it included, which at 3.0 has
// no effect beside no type and draws the load's warning.
func TestKindInference_AClosureBesideAUnionOfObjectsDistributes(t *testing.T) {
	t.Parallel()
	for _, version := range []string{"3.0.3", "3.1.0"} {
		for _, order := range [][]string{{"A", "B"}, {"B", "A"}} {
			union := unionOver(order...)
			schemas := append(slices.Clone(closedObjects),
				"Closed: {"+union+", additionalProperties: false}",
				"Nullable: {"+union+", additionalProperties: false, nullable: true}",
				"Typed: {"+union+", type: object, additionalProperties: false}")
			t.Run(fmt.Sprintf("%s/%s", version, strings.Join(order, "")), func(t *testing.T) {
				t.Parallel()
				for _, off := range []bool{false, true} {
					inBothOrders(t, schemas, func(t *testing.T, schemas []string) {
						doc, diags := compileKindInference(t, "closure", admissionSpec(version, "", schemas...), off)
						var findings []wantFinding
						if version == "3.0.3" {
							findings = []wantFinding{{code: "openapi/validation/validation-no-effect", pointer: "/components/schemas/Nullable/nullable"}}
						}
						requireErrorsAndWarnings(t, diags, findings)
						for _, name := range []string{"Closed", "Nullable", "Typed"} {
							variants := distributedOver(t, doc, componentID(name))
							assert.ElementsMatch(t, []ir.TypeID{componentID("A"), componentID("B")},
								slices.Collect(maps.Keys(variants)), name)
							for base, model := range variants {
								assert.Equal(t, ir.AdditionalClosed, model.Additional, "%s over %s", name, base)
								assert.Empty(t, model.Properties, "%s over %s", name, base)
								assert.False(t, markedKindInference(model), "%s over %s: the branch declares the object", name, base)
							}
							assert.Empty(t, keptUnder(doc, jsontext.Pointer("/components/schemas/"+name)), name)
						}
						assert.Equal(t, unionAsWritten(t, doc, componentID("Typed")),
							strings.ReplaceAll(unionAsWritten(t, doc, componentID("Closed")), "/Closed/", "/Typed/"),
							"the closure lowers as the same schema writing type: object")
					})
				}
			})
		}
	}
}

// TestKindInference_ACatchAllBesideAUnionOfObjectsDistributes holds a catch-all
// beside a union of objects with no type: a union of two models over its
// branches, each holding the catch-all's value type.
func TestKindInference_ACatchAllBesideAUnionOfObjectsDistributes(t *testing.T) {
	t.Parallel()
	for _, version := range []string{"3.0.3", "3.1.0"} {
		t.Run(version, func(t *testing.T) {
			t.Parallel()
			schemas := append(slices.Clone(closedObjects),
				"Open: {"+unionOver("A", "B")+", additionalProperties: {type: string}}")
			for _, off := range []bool{false, true} {
				inBothOrders(t, schemas, func(t *testing.T, schemas []string) {
					doc, diags := compileKindInference(t, "catch-all", admissionSpec(version, "", schemas...), off)
					requireErrorsAndWarnings(t, diags, nil)
					variants := distributedOver(t, doc, componentID("Open"))
					require.Len(t, variants, 2)
					for base, model := range variants {
						require.NotNil(t, model.AdditionalProperties, "%s holds the catch-all", base)
						require.NotNil(t, model.AdditionalProperties.Value, base)
						assert.Equal(t, prim(ir.PrimitiveString), model.AdditionalProperties.Value.Target, base)
					}
					assert.Empty(t, keptUnder(doc, "/components/schemas/Open"))
				})
			}
		})
	}
}

// TestKindInference_AClosureBesideAUnionOfOtherKindsIsTheTopType holds a closure
// beside a union naming an object and a string: the branches admit a kind the
// closure does not speak of, so under either policy the schema is the top
// type keeping both.
func TestKindInference_AClosureBesideAUnionOfOtherKindsIsTheTopType(t *testing.T) {
	t.Parallel()
	for _, version := range []string{"3.0.3", "3.1.0"} {
		t.Run(version, func(t *testing.T) {
			t.Parallel()
			schemas := append(slices.Clone(closedObjects),
				"Mixed: {oneOf: [{$ref: '#/components/schemas/A'}, {type: string}], additionalProperties: false}")
			for _, off := range []bool{false, true} {
				inBothOrders(t, schemas, func(t *testing.T, schemas []string) {
					doc, diags := compileKindInference(t, "mixed", admissionSpec(version, "", schemas...), off)
					requireErrorsAndWarnings(t, diags, nil)
					assertUntypedOutcome(t, doc, "Mixed", topKeeping("additionalProperties", "oneOf"))
				})
			}
		})
	}
}

// TestKindInference_AClosedFormBodyBesideAUnionKeysNoPartItsModelsLack holds a
// multipart body writing a closure beside a union of objects: every part its
// content keys an encoding by is a property a model of the document holds.
func TestKindInference_AClosedFormBodyBesideAUnionKeysNoPartItsModelsLack(t *testing.T) {
	t.Parallel()
	body := `paths:
  /f:
    post:
      operationId: f
      requestBody:
        content:
          multipart/form-data:
            schema: {` + unionOver("A", "B") + `, additionalProperties: false}
      responses: {"204": {description: d}}
`
	for _, version := range []string{"3.0.3", "3.1.0"} {
		for _, off := range []bool{false, true} {
			doc, _ := compileKindInference(t, "form", admissionSpec(version, body, closedObjects...), off)
			held := map[ir.PropertyID]bool{}
			for _, td := range doc.Types {
				if model, ok := td.(*ir.Model); ok {
					for _, property := range model.Properties {
						held[property.ID] = true
					}
				}
			}
			content := requestContent(t, operationAt(t, doc, "POST", "/f"))
			for _, part := range slices.Sorted(maps.Keys(content.Encoding)) {
				assert.True(t, held[part], "%s off=%t: the part %s is no property of the document", version, off, part)
			}
		}
	}
}
