// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package e2e_test

import (
	"encoding/json/jsontext"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi"
	"github.com/dexpace/morphic/ir"
)

// useBoundKeywords are keywords a carrier holds and a node cannot, each with
// what the carrier of a use writing it beside a $ref to an object holds.
var useBoundKeywords = []struct {
	name, keyword string
	check         func(t *testing.T, vis ir.Visibility, def *ir.Value, nullable bool)
}{
	{"readOnly", "readOnly: true", func(t *testing.T, vis ir.Visibility, _ *ir.Value, _ bool) {
		t.Helper()
		assert.Equal(t, []ir.Lifecycle{ir.LifecycleRead, ir.LifecycleDelete, ir.LifecycleQuery}, vis.Only)
	}},
	{"writeOnly", "writeOnly: true", func(t *testing.T, vis ir.Visibility, _ *ir.Value, _ bool) {
		t.Helper()
		assert.Equal(t, []ir.Lifecycle{ir.LifecycleCreate, ir.LifecycleUpdate}, vis.Only)
	}},
	{"default", "default: {a: x}", func(t *testing.T, _ ir.Visibility, def *ir.Value, _ bool) {
		t.Helper()
		require.NotNil(t, def)
		assert.Equal(t, ir.ValueObject, def.Kind)
	}},
	{"nullable", "nullable: true", func(t *testing.T, _ ir.Visibility, _ *ir.Value, nullable bool) {
		t.Helper()
		assert.True(t, nullable)
	}},
}

// useBoundSpellings write a use-bound keyword beside one $ref to Obj: the
// reference spelling and the allOf spelling, one conjunction.
var useBoundSpellings = map[string]string{
	"beside the ref":   "{$ref: '#/components/schemas/Obj', %s}",
	"beside the allOf": "{allOf: [{$ref: '#/components/schemas/Obj'}], %s}",
}

// TestRefUseBound_ACarriedUseIsTheReference holds that a use whose keywords
// beside its one $ref to an object only bind the use is that reference where
// a property or a parameter carries it, in both spellings and at both
// versions: the carrier names Obj and holds the keyword, and no node stands at
// the position. A component and a list's element own their node, the Model
// over Obj, as before.
func TestRefUseBound_ACarriedUseIsTheReference(t *testing.T) {
	t.Parallel()
	for _, kw := range useBoundKeywords {
		for spelling, layout := range useBoundSpellings {
			for _, version := range []string{"3.0.3", "3.1.0"} {
				t.Run(fmt.Sprintf("%s/%s/%s", kw.name, spelling, version), func(t *testing.T) {
					t.Parallel()
					schema := fmt.Sprintf(layout, kw.keyword)
					doc, diags := compileAnnotationSpec(t, "bound", conjunctionSpec(version, schema,
						"Obj: {type: object, properties: {a: {type: string}}}"))
					assertNoErrorDiags(t, diags)
					holder, ok := kindNode(t, doc, componentID("Holder")).(*ir.Model)
					require.True(t, ok)
					p, ok := propByWire(holder, "p")
					require.True(t, ok)
					assert.Equal(t, componentID("Obj"), p.Type.Target, "the property names the reference")
					kw.check(t, p.Visibility, p.Default, p.Type.Nullable)
					op := operationAt(t, doc, "GET", "/x")
					require.Len(t, op.Params, 1)
					assert.Equal(t, componentID("Obj"), op.Params[0].Type.Target, "the parameter names the reference")
					for _, at := range []string{"/components/schemas/Holder/properties/p", "/paths/~1x/get/parameters/0/schema"} {
						assert.NotContains(t, doc.Types, ir.TypeID("t/anon"+at), "no node stands at %s", at)
					}
					for _, id := range []ir.TypeID{componentID("Pos"), "t/anon/components/schemas/Els/items"} {
						m, ok := kindNode(t, doc, id).(*ir.Model)
						require.True(t, ok, "%s owns the Model over Obj", id)
						require.NotNil(t, m.Base)
						assert.Equal(t, componentID("Obj"), m.Base.Target, id)
					}
				})
			}
		}
	}
}

// TestRefUseBound_AnotherKeywordKeepsTheModel holds the gate: a note beside
// the $ref, which a node holds, keeps the Model over Obj at a carrier, and
// with the nullable-inference policy off a `nullable` there widens nothing
// and is no use-bound keyword.
func TestRefUseBound_AnotherKeywordKeepsTheModel(t *testing.T) {
	t.Parallel()
	const at = "/components/schemas/Holder/properties/p"
	for name, tc := range map[string]struct {
		schema string
		off    bool
	}{
		"a description":        {"{$ref: '#/components/schemas/Obj', readOnly: true, description: d}", false},
		"nullable, policy off": {"{allOf: [{$ref: '#/components/schemas/Obj'}], nullable: true}", true},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			doc := compileClean(t, conjunctionSpec("3.1.0", tc.schema, "Obj: {type: object, properties: {a: {type: string}}}"),
				openapi.Options{NullableInference: openapi.NullableInference{Disabled: tc.off}})
			m, ok := kindNode(t, doc, ir.TypeID("t/anon"+at)).(*ir.Model)
			require.True(t, ok, "the position owns the Model over Obj")
			require.NotNil(t, m.Base)
			assert.Equal(t, componentID("Obj"), m.Base.Target)
		})
	}
}

// TestRefUseBound_AReferenceToACarriedPositionReadsItInEitherOrder holds that
// whether a carrier holds a position is read off where it is written, not
// off which lowering reaches it first: a $ref naming the property's position
// and the property itself compile to one document in either declaration
// order, the property naming Obj and holding its visibility, the $ref's user
// the Model over Obj the position owns for it, which keeps the readOnly it
// cannot carry with an info.
func TestRefUseBound_AReferenceToACarriedPositionReadsItInEitherOrder(t *testing.T) {
	t.Parallel()
	const at = "/components/schemas/H/properties/r1"
	inBothOrders(t, []string{
		"Obj: {type: object, properties: {a: {type: string}}}",
		"H: {type: object, properties: {r1: {$ref: '#/components/schemas/Obj', readOnly: true}}}",
		"X: {type: object, properties: {x: {$ref: '#/components/schemas/H/properties/r1'}}}",
	}, func(t *testing.T, schemas []string) {
		doc, diags := compileAnnotationSpec(t, "order", admissionSpec("3.1.0", "", schemas...))
		requireErrorsAndWarnings(t, diags, nil)
		r1, ok := propByWire(requireEntryModelOf(t, doc, "H"), "r1")
		require.True(t, ok)
		assert.Equal(t, componentID("Obj"), r1.Type.Target)
		assert.NotEmpty(t, r1.Visibility.Only)
		x, ok := propByWire(requireEntryModelOf(t, doc, "X"), "x")
		require.True(t, ok)
		assert.Equal(t, ir.TypeID("t/anon"+at), x.Type.Target)
		assert.NotEmpty(t, x.Visibility.Only, "the user reads the readOnly through its $ref")
		node, ok := kindNode(t, doc, x.Type.Target).(*ir.Model)
		require.True(t, ok, "no Scalar stands over a model")
		assert.Contains(t, node.Unmodeled, "openapi:readOnly")
		assert.Contains(t, degradedInfoPointers(diags), jsontext.Pointer(at+"/readOnly"))
	})
}
