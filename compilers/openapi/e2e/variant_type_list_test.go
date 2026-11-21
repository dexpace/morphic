// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package e2e_test

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
)

// typeListComponents are the schemas the branches below name: an object, a
// type list holding an object and declaring a member, a type list holding
// none, and a oneOf holding an object beside a string.
var typeListComponents = []string{
	"Obj: {type: object, properties: {o: {type: string}}}",
	"Mixed: {type: [object, string], properties: {m: {type: string}}}",
	"NoObj: {type: [string, integer]}",
	"Chosen: {oneOf: [{type: object, properties: {c: {type: string}}}, {type: string}]}",
}

// typeListUnion writes an object body bounded by a property count beside a
// oneOf of the components named, in the order given.
func typeListUnion(names []string) string {
	refs := make([]string, 0, len(names))
	for _, name := range names {
		refs = append(refs, "{$ref: '#/components/schemas/"+name+"'}")
	}
	return "{type: object, properties: {a: {type: string}}, minProperties: 1, oneOf: [" + strings.Join(refs, ", ") + "]}"
}

// TestVariantBounds_AVariantOverATypeListComposesItsObject holds that the
// variant over a branch naming a schema whose type list holds an object names
// that list's object variant as its Base, as an allOf entry naming it does, so
// it exposes the members the schema declares beside the body's and holds the
// body's property count, keeping nothing; the variant over an object names the
// object. Both orders of the branches and of every mapping lower alike.
func TestVariantBounds_AVariantOverATypeListComposesItsObject(t *testing.T) {
	t.Parallel()
	bases := map[string]ir.TypeID{"Mixed": "type/anonymous/components/schemas/Mixed/type/0", "Obj": componentID("Obj")}
	members := map[string][]string{"Mixed": {"a", "m"}, "Obj": {"a", "o"}}
	for _, order := range [][]string{{"Mixed", "Obj"}, {"Obj", "Mixed"}} {
		t.Run(strings.Join(order, "-"), func(t *testing.T) {
			t.Parallel()
			inBothMappingOrders(t, conjunctionSpec("3.1.0", typeListUnion(order), typeListComponents...),
				func(t *testing.T, spec string) {
					doc, diags := compileAnnotationSpec(t, "typelist", spec)
					requireErrorsAndWarnings(t, diags, nil)
					requireModelsNameModels(t, doc)
					for _, at := range conjunctionPositions {
						union := variantUnionAt(t, doc, string(at), len(order))
						for j, name := range order {
							model, ok := doc.Types[union.Variants[j].Type.Target].(*ir.Model)
							require.True(t, ok, "the variant over %s at %s is a model", name, at)
							require.NotNil(t, model.Base, "the variant over %s at %s", name, at)
							assert.Equal(t, bases[name], model.Base.Target, "the variant over %s at %s", name, at)
							assert.Empty(t, model.Unmodeled, "the variant over %s at %s keeps nothing", name, at)
							assert.Equal(t, members[name], flattenedNames(doc, model.ID), "%s at %s", name, at)
							assert.Equal(t, map[string]any{"minProperties": 1.0}, variantConstraints(t, doc, model.ID), "%s at %s", name, at)
						}
					}
				})
		})
	}
}

// TestVariantBounds_AVariantKeepsATypeListOrAUnionHoldingNoLoneObject holds
// that a branch naming a union a oneOf writes, which is not unpacked though it
// holds an object, is no variant's Base: the variant keeps the branch verbatim
// with an info. A branch naming a type list with no object is no variant at
// all, kept on the union, in either order of the branches and of every
// mapping.
func TestVariantBounds_AVariantKeepsATypeListOrAUnionHoldingNoLoneObject(t *testing.T) {
	t.Parallel()
	written := []string{"Obj", "NoObj", "Chosen"}
	for _, reverse := range []bool{false, true} {
		order := slices.Clone(written)
		if reverse {
			slices.Reverse(order)
		}
		t.Run(strings.Join(order, "-"), func(t *testing.T) {
			t.Parallel()
			inBothMappingOrders(t, conjunctionSpec("3.1.0", typeListUnion(order), typeListComponents...),
				func(t *testing.T, spec string) {
					doc, diags := compileAnnotationSpec(t, "kept", spec)
					noObj := slices.Index(order, "NoObj")
					requireErrorsAndWarnings(t, diags, deadWarnings("oneOf", []variantBranch{
						{name: order[0], object: noObj != 0}, {name: order[1], object: noObj != 1}, {name: order[2], object: noObj != 2},
					}))
					requireModelsNameModels(t, doc)
					for _, at := range conjunctionPositions {
						union := variantUnionAt(t, doc, string(at), len(order)-1)
						assert.Equal(t, entryKept(fmt.Sprintf("openapi:oneOf/%d", noObj)), keptOn(union),
							"NoObj admits no object, so the union keeps it at %s", at)
						live := slices.DeleteFunc(slices.Clone(order), func(name string) bool { return name == "NoObj" })
						for j, name := range live {
							model, ok := doc.Types[union.Variants[j].Type.Target].(*ir.Model)
							require.True(t, ok, "the variant over %s at %s is a model", name, at)
							if name == "Obj" {
								require.NotNil(t, model.Base)
								assert.Equal(t, componentID("Obj"), model.Base.Target, "at %s", at)
								continue
							}
							assert.Nil(t, model.Base, "the variant over %s at %s names no base", name, at)
							assert.Equal(t, entryKept(fmt.Sprintf("openapi:oneOf/%d", slices.Index(order, name))), keptOn(model),
								"%s at %s", name, at)
						}
					}
				})
		})
	}
}

// TestVariantBounds_ATypeListBranchWritingBesideItsRefIsKept holds that a
// branch writing a keyword beside its $ref to a type list holding an object is
// kept on its variant, as it was before the list's object was composed: the
// branch's own node holds the keyword over the whole list, so composing the
// object variant would leave it on no node the variant names.
func TestVariantBounds_ATypeListBranchWritingBesideItsRefIsKept(t *testing.T) {
	t.Parallel()
	schema := "{type: object, properties: {a: {type: string}}, oneOf: [" +
		"{$ref: '#/components/schemas/Obj'}, {$ref: '#/components/schemas/Mixed', description: the mixed branch}]}"
	inBothMappingOrders(t, conjunctionSpec("3.1.0", schema, typeListComponents...), func(t *testing.T, spec string) {
		doc, diags := compileAnnotationSpec(t, "beside", spec)
		requireErrorsAndWarnings(t, diags, nil)
		for _, at := range conjunctionPositions {
			union := variantUnionAt(t, doc, string(at), 2)
			model, ok := doc.Types[union.Variants[1].Type.Target].(*ir.Model)
			require.True(t, ok, "at %s", at)
			assert.Nil(t, model.Base, "at %s", at)
			assert.Equal(t, entryKept("openapi:oneOf/1"), keptOn(model), "at %s", at)
		}
	})
}
