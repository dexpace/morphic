// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package e2e_test

import (
	"encoding/json/jsontext"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/ir"
)

// kindInferenceMarker is the name a node carries in Provenance.Inferred when
// its kind was read off the shape keywords its schema writes.
const kindInferenceMarker ir.Inferred = "kind-inference"

// kindNode returns the node stored at id, failing the test where there is none.
func kindNode(t *testing.T, doc *ir.Document, id ir.TypeID) ir.TypeDef {
	t.Helper()
	td, ok := doc.Types[id]
	require.True(t, ok, "the document holds %s", id)
	return td
}

// keptOn returns the reason of each Unmodeled entry on td, by key.
func keptOn(td ir.TypeDef) map[string]ir.UnmodeledReason {
	kept := map[string]ir.UnmodeledReason{}
	for key, entry := range td.Common().Unmodeled {
		kept[key] = entry.Reason
	}
	return kept
}

// degradedInfoPointers lists, sorted, where the document reports a construct
// a node had no home for.
func degradedInfoPointers(diags []ir.Diagnostic) []jsontext.Pointer {
	return openapitest.PointersOf(diags, diag.DegradedConstruct)
}

// markedKindInference reports whether td names the kind-inference policy
// among the heuristics that chose its kind.
func markedKindInference(td ir.TypeDef) bool {
	return slices.Contains(
		strings.Split(string(td.Common().Provenance.Inferred), ","), string(kindInferenceMarker))
}

// typedSchemas are schemas that name their kind, each with the node kind it
// lowers to. None of them reads its kind off a keyword.
var typedSchemas = []struct {
	schema string
	name   string
	kind   ir.TypeKind
	only31 bool
}{
	{"MemTyped: {type: object, properties: {foo: {type: string}}, required: [foo]}", "MemTyped", ir.KindModel, false},
	{"ElTyped: {type: array, items: {type: string}}", "ElTyped", ir.KindList, false},
	{"AddlTyped: {type: object, additionalProperties: {type: string}}", "AddlTyped", ir.KindMap, false},
	{"ReqTyped: {type: object, required: [a]}", "ReqTyped", ir.KindModel, false},
	{"DiscTyped: {type: object, discriminator: {propertyName: k}}", "DiscTyped", ir.KindModel, false},
	{"PatTyped: {type: object, patternProperties: {'^x': {type: string}}}", "PatTyped", ir.KindModel, true},
	{"PrefixTyped: {type: array, prefixItems: [{type: string}]}", "PrefixTyped", ir.KindTuple, true},
	{"BothTyped: {type: [object, array], properties: {a: {type: string}}, items: {type: string}}", "BothTyped", ir.KindUnion, true},
}

// TestKindInference_ATypedSchemaIsNeverMarked holds that a schema writing its
// own type lowers to the node that type names and carries no kind-inference
// marker, whatever shape keywords it writes beside the type: the marker says
// a kind was read off keywords, and these were declared.
func TestKindInference_ATypedSchemaIsNeverMarked(t *testing.T) {
	t.Parallel()
	for _, version := range []string{"3.0.3", "3.1.0"} {
		var schemas []string
		for _, tc := range typedSchemas {
			if tc.only31 && version == "3.0.3" {
				continue
			}
			schemas = append(schemas, tc.schema)
		}
		t.Run(version, func(t *testing.T) {
			t.Parallel()
			inBothOrders(t, schemas, func(t *testing.T, schemas []string) {
				doc, diags := compileAnnotationSpec(t, "typed", admissionSpec(version, "", schemas...))
				requireErrorsAndWarnings(t, diags, nil)
				assert.Empty(t, degradedInfoPointers(diags), "a typed schema keeps nothing")
				for _, tc := range typedSchemas {
					if tc.only31 && version == "3.0.3" {
						continue
					}
					td := kindNode(t, doc, componentID(tc.name))
					assert.Equal(t, tc.kind, td.Kind(), tc.name)
					assert.False(t, markedKindInference(td), "%s declares its kind", tc.name)
					assert.Empty(t, td.Common().Provenance.Inferred, "%s declares its kind", tc.name)
					assert.Empty(t, keptOn(td), tc.name)
				}
			})
		})
	}
}

// TestKindInference_AKindTheSchemaNamesKeepsItsStrayKeywordsUnmarked holds that
// a keyword of another kind written beside a declared type is kept verbatim
// and claims nothing: a string stays a string, an object stays an object,
// and neither node is marked.
func TestKindInference_AKindTheSchemaNamesKeepsItsStrayKeywordsUnmarked(t *testing.T) {
	t.Parallel()
	schemas := []string{
		"StrProps: {type: string, properties: {a: {type: string}}}",
		"ObjItems: {type: object, items: {type: string}}",
	}
	for _, version := range []string{"3.0.3", "3.1.0"} {
		t.Run(version, func(t *testing.T) {
			t.Parallel()
			inBothOrders(t, schemas, func(t *testing.T, schemas []string) {
				doc, diags := compileAnnotationSpec(t, "stray", admissionSpec(version, "", schemas...))
				requireErrorsAndWarnings(t, diags, nil)
				assert.Equal(t, []jsontext.Pointer{
					"/components/schemas/ObjItems", "/components/schemas/StrProps",
				}, degradedInfoPointers(diags))

				str, ok := kindNode(t, doc, componentID("StrProps")).(*ir.Scalar)
				require.True(t, ok, "a declared string is a scalar")
				require.NotNil(t, str.Base)
				assert.Equal(t, prim(ir.PrimString), str.Base.Target)
				assert.Equal(t, map[string]ir.UnmodeledReason{"openapi:properties": ir.ReasonDegradedLowering}, keptOn(str))
				assert.Empty(t, str.Provenance.Inferred)

				obj := kindNode(t, doc, componentID("ObjItems"))
				assert.Equal(t, ir.KindModel, obj.Kind())
				assert.Equal(t, map[string]ir.UnmodeledReason{"openapi:items": ir.ReasonDegradedLowering}, keptOn(obj))
				assert.Empty(t, obj.Common().Provenance.Inferred)
			})
		})
	}
}

// TestKindInference_ANullOrEmptyShapeKeywordClaimsNoKind holds that a shape
// keyword with nothing in it speaks of no kind: a null one is the keyword left
// out and an empty `properties` is kept beside the top type, and a bound
// claims no kind either. None of the four nodes is marked, whichever way the
// policy reads, so these are the schemas the policy leaves as they are.
func TestKindInference_ANullOrEmptyShapeKeywordClaimsNoKind(t *testing.T) {
	t.Parallel()
	schemas := []string{
		"PropsNull: {properties: null}",
		"EmptyProps: {properties: {}}",
		"AddlNull: {additionalProperties: null}",
		"ItemsNull: {items: null}",
		"MinLen: {minLength: 2}",
	}
	for _, version := range []string{"3.0.3", "3.1.0"} {
		t.Run(version, func(t *testing.T) {
			t.Parallel()
			inBothOrders(t, schemas, func(t *testing.T, schemas []string) {
				doc, diags := compileAnnotationSpec(t, "claimless", admissionSpec(version, "", schemas...))
				requireErrorsAndWarnings(t, diags, []wantFinding{
					{code: typeMismatch, pointer: "/components/schemas/PropsNull/properties"},
					{code: typeMismatch, pointer: "/components/schemas/AddlNull/additionalProperties"},
					{code: typeMismatch, pointer: "/components/schemas/ItemsNull/items"},
				})
				assert.Equal(t, []jsontext.Pointer{"/components/schemas/EmptyProps"}, degradedInfoPointers(diags))

				for name, kept := range map[string]map[string]ir.UnmodeledReason{
					"PropsNull":  {},
					"EmptyProps": {"openapi:properties": ir.ReasonDegradedLowering},
					"AddlNull":   {},
					"ItemsNull":  {},
					"MinLen":     {},
				} {
					scalar, ok := kindNode(t, doc, componentID(name)).(*ir.Scalar)
					require.True(t, ok, "%s claims no kind, so it is the top type", name)
					require.NotNil(t, scalar.Base, name)
					assert.Equal(t, prim(ir.PrimAny), scalar.Base.Target, name)
					assert.Equal(t, kept, keptOn(scalar), name)
					assert.Empty(t, scalar.Provenance.Inferred, name)
				}
				minLen, ok := kindNode(t, doc, componentID("MinLen")).(*ir.Scalar)
				require.True(t, ok)
				require.NotNil(t, minLen.Constraints)
				require.NotNil(t, minLen.Constraints.MinLength, "a bound lands on the top type")
				assert.EqualValues(t, 2, *minLen.Constraints.MinLength)
			})
		})
	}
}
