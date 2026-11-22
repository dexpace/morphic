// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package e2e_test

import (
	"encoding/json/jsontext"
	"fmt"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
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
					property, ok := propertyByWire(holder, "p")
					require.True(t, ok)
					assert.Equal(t, componentID("Obj"), property.Type.Target, "the property names the reference")
					kw.check(t, property.Visibility, property.Default, property.Type.Nullable)
					operation := operationAt(t, doc, "GET", "/x")
					require.Len(t, operation.Params, 1)
					assert.Equal(t, componentID("Obj"), operation.Params[0].Type.Target, "the parameter names the reference")
					for _, at := range []string{"/components/schemas/Holder/properties/p", "/paths/~1x/get/parameters/0/schema"} {
						assert.NotContains(t, doc.Types, ir.TypeID("type/anonymous"+at), "no node stands at %s", at)
					}
					for _, id := range []ir.TypeID{componentID("Pos"), "type/anonymous/components/schemas/Els/items"} {
						model, ok := kindNode(t, doc, id).(*ir.Model)
						require.True(t, ok, "%s owns the Model over Obj", id)
						require.NotNil(t, model.Base)
						assert.Equal(t, componentID("Obj"), model.Base.Target, id)
					}
				})
			}
		}
	}
}

// TestRefUseBound_AnotherKeywordKeepsTheModel holds the gate: a key beside
// the $ref, which a node keeps and a carrier cannot, keeps the Model over Obj
// at a carrier, and with the nullable-inference policy off a `nullable` there
// widens nothing and is no use-bound keyword.
func TestRefUseBound_AnotherKeywordKeepsTheModel(t *testing.T) {
	t.Parallel()
	const at = "/components/schemas/Holder/properties/p"
	for name, tc := range map[string]struct {
		schema string
		off    bool
	}{
		"an extension":         {"{$ref: '#/components/schemas/Obj', readOnly: true, description: d, x-note: n}", false},
		"nullable, policy off": {"{allOf: [{$ref: '#/components/schemas/Obj'}], nullable: true}", true},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			doc := compileClean(t, conjunctionSpec("3.1.0", tc.schema, "Obj: {type: object, properties: {a: {type: string}}}"),
				openapi.Options{NullableInference: openapi.NullableInference{Disabled: tc.off}})
			model, ok := kindNode(t, doc, ir.TypeID("type/anonymous"+at)).(*ir.Model)
			require.True(t, ok, "the position owns the Model over Obj")
			require.NotNil(t, model.Base)
			assert.Equal(t, componentID("Obj"), model.Base.Target)
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
		r1, ok := propertyByWire(requireEntryModelOf(t, doc, "H"), "r1")
		require.True(t, ok)
		assert.Equal(t, componentID("Obj"), r1.Type.Target)
		assert.NotEmpty(t, r1.Visibility.Only)
		property, ok := propertyByWire(requireEntryModelOf(t, doc, "X"), "x")
		require.True(t, ok)
		assert.Equal(t, ir.TypeID("type/anonymous"+at), property.Type.Target)
		assert.NotEmpty(t, property.Visibility.Only, "the user reads the readOnly through its $ref")
		node, ok := kindNode(t, doc, property.Type.Target).(*ir.Model)
		require.True(t, ok, "no Scalar stands over a model")
		assert.Contains(t, node.Unmodeled, "openapi:readOnly")
		assert.Contains(t, degradedInfoPointers(diags), jsontext.Pointer(at+"/readOnly"))
	})
}

// noteKeywords are the notes a carrier has a field for, each written beside a
// $ref, with what the carrier holding it shows of it.
var noteKeywords = []struct {
	name, keyword string
	holds         func(notes noteCarrier) bool
}{
	{"description", "description: d", func(notes noteCarrier) bool { return notes.docs.Description == "d" }},
	{"title", "title: s", func(notes noteCarrier) bool { return notes.docs.Summary == "s" }},
	{"externalDocs", "externalDocs: {url: 'https://example.com/d'}", func(notes noteCarrier) bool {
		return len(notes.docs.ExternalDocs) == 1 && notes.docs.ExternalDocs[0].URL == "https://example.com/d"
	}},
	{"deprecated", "deprecated: true", func(notes noteCarrier) bool { return notes.deprecation != nil }},
	{"example", "example: {a: x}", func(notes noteCarrier) bool { return len(notes.examples) == 1 }},
	{"examples", "examples: [{a: x}]", func(notes noteCarrier) bool { return len(notes.examples) == 1 }},
}

// noteCarrier is what a carrier holds of its notes, and the type it names.
type noteCarrier struct {
	target      ir.TypeID
	docs        ir.Docs
	deprecation *ir.Deprecation
	examples    []ir.Example
}

// noteCarrierPositions are where noteUseSpec writes its schema at a carrier,
// by the carrier's name in noteCarriersOf.
var noteCarrierPositions = map[string]jsontext.Pointer{
	"property":          "/components/schemas/Holder/properties/p",
	"parameter":         "/paths/~1x/get/parameters/0/schema",
	"content parameter": "/paths/~1x/get/parameters/1/content/text~1plain/schema",
	"header":            "/paths/~1x/get/responses/200/headers/X-H/schema",
	"content header":    "/paths/~1x/get/responses/200/headers/X-C/content/text~1plain/schema",
}

// noteUseSpec declares schema, a flow-style schema object, at each carrier of
// noteCarrierPositions, and at three positions no carrier holds: the component
// Pos, a list's items and a union's first branch. Obj, the object it names,
// writes a description of its own.
func noteUseSpec(version, schema string, components ...string) string {
	paths := "paths:\n  /x:\n    get:\n      operationId: x\n      parameters:\n" +
		"        - {name: q, in: query, schema: " + schema + "}\n" +
		"        - {name: c, in: query, content: {text/plain: {schema: " + schema + "}}}\n" +
		"      responses:\n        \"200\":\n          description: ok\n          headers:\n" +
		"            X-H: {schema: " + schema + "}\n" +
		"            X-C: {content: {text/plain: {schema: " + schema + "}}}\n"
	return admissionSpec(version, paths, append([]string{
		"Obj: {type: object, description: O, properties: {a: {type: string}}}",
		"Holder: {type: object, properties: {p: " + schema + "}}",
		"Pos: " + schema,
		"Els: {type: array, items: " + schema + "}",
		"Un: {oneOf: [" + schema + ", {type: string}]}",
	}, components...)...)
}

// noteCarriersOf returns what each carrier noteUseSpec declares holds.
func noteCarriersOf(t *testing.T, doc *ir.Document) map[string]noteCarrier {
	t.Helper()
	holder, ok := kindNode(t, doc, componentID("Holder")).(*ir.Model)
	require.True(t, ok)
	property, ok := propertyByWire(holder, "p")
	require.True(t, ok)
	operation := operationAt(t, doc, "GET", "/x")
	require.Len(t, operation.Params, 2)
	require.Len(t, operation.Responses, 1)
	carriers := map[string]noteCarrier{
		"property":          {property.Type.Target, property.Docs, property.Deprecation, property.Examples},
		"parameter":         {operation.Params[0].Type.Target, operation.Params[0].Docs, operation.Params[0].Deprecation, operation.Params[0].Examples},
		"content parameter": {operation.Params[1].Type.Target, operation.Params[1].Docs, operation.Params[1].Deprecation, operation.Params[1].Examples},
	}
	for name, wire := range map[string]string{"header": "X-H", "content header": "X-C"} {
		header, ok := headerByWire(operation.Responses[0].Headers, wire)
		require.True(t, ok, wire)
		carriers[name] = noteCarrier{header.Type.Target, header.Docs, header.Deprecation, header.Examples}
	}
	return carriers
}

// TestRefUseBound_ANoteIsTheCarriers holds that a note beside a $ref to an
// object, all a carried position writes beyond it, is the carrier's, in both
// spellings and at both versions: each carrier names Obj and holds the note,
// the two spellings give it the same, and no node stands at its position. A
// position no carrier holds owns the Model over Obj holding the note, as
// before (docs/compilers/openapi.md#ref-use-site).
func TestRefUseBound_ANoteIsTheCarriers(t *testing.T) {
	t.Parallel()
	for _, note := range noteKeywords {
		for _, version := range []string{"3.0.3", "3.1.0"} {
			t.Run(note.name+"/"+version, func(t *testing.T) {
				t.Parallel()
				bySpelling := map[string]map[string]noteCarrier{}
				for spelling, layout := range useBoundSpellings {
					doc, diags := compileAnnotationSpec(t, "note", noteUseSpec(version, fmt.Sprintf(layout, note.keyword)))
					assertNoErrorDiags(t, diags)
					carriers := noteCarriersOf(t, doc)
					for name, carrier := range carriers {
						assert.Equal(t, componentID("Obj"), carrier.target, "%s %s names the reference", spelling, name)
						assert.True(t, note.holds(carrier), "%s %s holds the note: %+v", spelling, name, carrier)
						assert.NotContains(t, doc.Types, ir.TypeID("type/anonymous"+noteCarrierPositions[name]), "%s %s", spelling, name)
					}
					for _, id := range []ir.TypeID{componentID("Pos"), "type/anonymous/components/schemas/Els/items", "type/anonymous/components/schemas/Un/oneOf/0"} {
						model, ok := kindNode(t, doc, id).(*ir.Model)
						require.True(t, ok, "%s %s owns the Model over Obj", spelling, id)
						require.NotNil(t, model.Base, id)
						assert.Equal(t, componentID("Obj"), model.Base.Target, id)
						assert.True(t, note.holds(noteCarrier{docs: model.Docs, deprecation: model.Deprecation, examples: model.Examples}),
							"%s %s holds the note", spelling, id)
					}
					bySpelling[spelling] = carriers
				}
				assert.Empty(t, cmp.Diff(bySpelling["beside the allOf"], bySpelling["beside the ref"], cmp.AllowUnexported(noteCarrier{})))
			})
		}
	}
}

// TestRefUseBound_WhatACarrierCannotHoldKeepsTheNode holds the other side of
// the gate: where a carried position writes, beside its $ref and its note,
// what a carrier has no field for, or writes the note on a conjunct of its
// own, the position owns the Model over Obj, which holds the note, and each
// carrier names it and holds none of it, as before.
func TestRefUseBound_WhatACarrierCannotHoldKeepsTheNode(t *testing.T) {
	t.Parallel()
	for name, schema := range map[string]string{
		"an extension":                "{$ref: '#/components/schemas/Obj', description: d, x-note: n}",
		"an XML hint":                 "{$ref: '#/components/schemas/Obj', description: d, xml: {name: n}}",
		"a constraint over an object": "{allOf: [{$ref: '#/components/schemas/Obj'}], description: d, required: [a]}",
		"another conjunct":            "{allOf: [{$ref: '#/components/schemas/Obj'}, {description: d}]}",
		"a note on the $ref's entry":  "{allOf: [{$ref: '#/components/schemas/Obj', description: d}]}",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			doc, diags := compileAnnotationSpec(t, "kept", noteUseSpec("3.1.0", schema))
			assertNoErrorDiags(t, diags)
			for carrierName, carrier := range noteCarriersOf(t, doc) {
				id := ir.TypeID("type/anonymous" + noteCarrierPositions[carrierName])
				assert.Equal(t, id, carrier.target, carrierName)
				assert.Empty(t, carrier.docs.Description, carrierName)
				model, ok := kindNode(t, doc, id).(*ir.Model)
				require.True(t, ok, "%s owns the Model over Obj", carrierName)
				require.NotNil(t, model.Base, carrierName)
				assert.Equal(t, componentID("Obj"), model.Base.Target, carrierName)
				assert.Equal(t, "d", model.Docs.Description, carrierName)
			}
		})
	}
}

// TestRefUseBound_ANoteSomethingElseHoldsKeepsTheNode holds where a note
// beside a carried $ref stays on the node, by the rule an inline schema's
// note follows (docs/ir/annotations.md#one-structural-home-per-declaration):
// a parameter or header writing its own, or a Reference Object naming it
// writing docs beside its $ref, leaves the schema's on the node, and a
// position a $ref names keeps them for every reference. A note nothing writes
// over is still the carrier's, beside one that is: a deprecation, which
// either side states, and an example where only docs are written over. Both
// component orders compile to one document.
func TestRefUseBound_ANoteSomethingElseHoldsKeepsTheNode(t *testing.T) {
	t.Parallel()
	const obj = "'#/components/schemas/Obj'"
	const paths = `paths:
  /x:
    get:
      operationId: x
      parameters:
        - {name: own, in: query, description: P, schema: {$ref: ` + obj + `, description: d}}
        - {name: ownExample, in: query, example: {a: p}, schema: {$ref: ` + obj + `, example: {a: s}}}
        - {name: ownDeprecated, in: query, description: P, schema: {$ref: ` + obj + `, deprecated: true}}
        - {name: ownDocs, in: query, description: P, schema: {$ref: ` + obj + `, example: {a: s}}}
        - {$ref: '#/components/parameters/Used', description: U}
        - {name: refObj, in: query, description: P, schema: {$ref: ` + obj + `, description: d, deprecated: true}}
        - {name: refObjEx, in: query, example: {a: p}, schema: {$ref: ` + obj + `, examples: [{a: s}], deprecated: true}}
        - {name: refObjDocs, in: query, description: P, schema: {$ref: ` + obj + `, description: d, example: {a: s}}}
      responses:
        "200":
          description: ok
          headers:
            X-Own: {description: H, schema: {$ref: ` + obj + `, description: d}}
            H1: {description: H, schema: {allOf: [{$ref: ` + obj + `}], description: d, deprecated: true}}
components:
  parameters:
    Used: {name: used, in: query, schema: {$ref: ` + obj + `, description: d}}
`
	schemas := []string{
		"Obj: {type: object, properties: {a: {type: string}}}",
		"H: {type: object, properties: {r1: {$ref: " + obj + ", description: d}}}",
		"X: {type: object, properties: {x: {$ref: '#/components/schemas/H/properties/r1'}}}",
	}
	var compiled []*ir.Document
	inBothOrders(t, schemas, func(t *testing.T, schemas []string) {
		doc, diags := compileAnnotationSpec(t, "kept", strings.Replace(admissionSpec("3.1.0", paths, schemas...), "components:\n  schemas:\n", "  schemas:\n", 1))
		assertNoErrorDiags(t, diags)
		compiled = append(compiled, doc)
		node := func(at string) ir.TypeCommon {
			t.Helper()
			model, ok := kindNode(t, doc, ir.TypeID("type/anonymous"+at)).(*ir.Model)
			require.True(t, ok, "%s owns the Model over Obj", at)
			require.NotNil(t, model.Base, at)
			assert.Equal(t, componentID("Obj"), model.Base.Target, at)
			return model.TypeCommon
		}
		operation := operationAt(t, doc, "GET", "/x")
		params := map[string]ir.Parameter{}
		for _, param := range operation.Params {
			params[param.Name.Source] = param
		}
		require.Len(t, params, 8)

		assert.Equal(t, "P", params["own"].Docs.Description)
		assert.Equal(t, "d", node("/paths/~1x/get/parameters/0/schema").Docs.Description, "the schema's docs stay on the node")
		assert.Equal(t, "{a: p}", exampleOf(t, params["ownExample"].Examples))
		assert.Equal(t, "{a: s}", exampleOf(t, node("/paths/~1x/get/parameters/1/schema").Examples), "the schema's example stays on the node")
		assert.Equal(t, "d", node("/components/parameters/Used/schema").Docs.Description, "a use documenting it leaves the docs on the node")
		assert.Equal(t, "U", params["used"].Docs.Description)

		for name, want := range map[string]string{"ownDeprecated": "", "ownDocs": "{a: s}"} {
			assert.Equal(t, componentID("Obj"), params[name].Type.Target, "%s names Obj", name)
			assert.NotContains(t, doc.Types, ir.TypeID("type/anonymous"+params[name].Provenance.Pointer+"/schema"), name)
			assert.Equal(t, "P", params[name].Docs.Description, name)
			if want != "" {
				assert.Equal(t, want, exampleOf(t, params[name].Examples), name)
			}
		}
		assert.NotNil(t, params["ownDeprecated"].Deprecation)

		header, ok := headerByWire(operation.Responses[0].Headers, "X-Own")
		require.True(t, ok)
		assert.Equal(t, "H", header.Docs.Description)
		assert.Equal(t, "d", node("/paths/~1x/get/responses/200/headers/X-Own/schema").Docs.Description)

		refObj := node("/paths/~1x/get/parameters/5/schema")
		assert.Equal(t, "d", refObj.Docs.Description, "the written-over docs stay on the node")
		assert.Nil(t, refObj.Deprecation, "the deprecation is the carrier's alone")
		assert.Equal(t, "P", params["refObj"].Docs.Description)
		assert.NotNil(t, params["refObj"].Deprecation)
		refObjEx := node("/paths/~1x/get/parameters/6/schema")
		assert.Equal(t, "{a: s}", exampleOf(t, refObjEx.Examples), "the written-over example stays on the node")
		assert.Nil(t, refObjEx.Deprecation)
		assert.Equal(t, "{a: p}", exampleOf(t, params["refObjEx"].Examples))
		assert.NotNil(t, params["refObjEx"].Deprecation)
		refObjDocs := node("/paths/~1x/get/parameters/7/schema")
		assert.Equal(t, "d", refObjDocs.Docs.Description)
		assert.Empty(t, refObjDocs.Examples, "an example nothing writes over is the carrier's")
		assert.Equal(t, "{a: s}", exampleOf(t, params["refObjDocs"].Examples))
		h1, ok := headerByWire(operation.Responses[0].Headers, "H1")
		require.True(t, ok)
		h1Node := node("/paths/~1x/get/responses/200/headers/H1/schema")
		assert.Equal(t, "d", h1Node.Docs.Description)
		assert.Nil(t, h1Node.Deprecation)
		assert.Equal(t, "H", h1.Docs.Description)
		assert.NotNil(t, h1.Deprecation)

		referenced := ir.TypeID("type/anonymous/components/schemas/H/properties/r1")
		r1, ok := propertyByWire(requireEntryModelOf(t, doc, "H"), "r1")
		require.True(t, ok)
		assert.Equal(t, referenced, r1.Type.Target, "a referenced position keeps its node")
		assert.Empty(t, r1.Docs.Description)
		assert.Equal(t, "d", node("/components/schemas/H/properties/r1").Docs.Description)
		x, ok := propertyByWire(requireEntryModelOf(t, doc, "X"), "x")
		require.True(t, ok)
		assert.Equal(t, referenced, x.Type.Target)
	})
	require.Len(t, compiled, 2)
	assert.Empty(t, cmp.Diff(compiled[0].Types, compiled[1].Types), "the types do not depend on component order")
	assert.Empty(t, cmp.Diff(compiled[0].Services, compiled[1].Services), "nor do the operations")
}

// exampleOf renders the one example of examples as flow-style YAML of one
// object of strings.
func exampleOf(t *testing.T, examples []ir.Example) string {
	t.Helper()
	require.Len(t, examples, 1)
	require.NotNil(t, examples[0].Value)
	fields := examples[0].Value.Object
	require.Len(t, fields, 1)
	return "{" + fields[0].Name + ": " + fields[0].Value.String + "}"
}
