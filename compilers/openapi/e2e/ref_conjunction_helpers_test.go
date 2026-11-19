// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package e2e_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
)

// conjTargets are the schemas the cases below write a use of: a string, an
// object and an array, each a kind a keyword may or may not belong to.
var conjTargets = map[string]string{
	"Str": "Str: {type: string}",
	"Obj": "Obj: {type: object, properties: {a: {type: string}}}",
	"Arr": "Arr: {type: array, items: {type: string}}",
}

// conjSite is a place a schema is written, with the pointer of the schema
// there and how to write a document holding one.
type conjSite struct {
	name    string
	pointer string
	// write returns the document, in the version given, holding schema there.
	write func(version, schema string, others ...string) string
}

// conjSites are the eight places a use of a named schema is written, each
// of which lowers it differently: a component, a property, a parameter, a
// header, an items, a map value, a union branch and a request body.
var conjSites = []conjSite{
	{"component", "/components/schemas/Use", func(v, s string, o ...string) string {
		return admissionSpec(v, "", append(slices.Clone(o), "Use: "+s)...)
	}},
	{"property", "/components/schemas/Holder/properties/p", func(v, s string, o ...string) string {
		return admissionSpec(v, "", append(slices.Clone(o), "Holder: {type: object, properties: {p: "+s+"}}")...)
	}},
	{"parameter", "/paths/~1a/get/parameters/0/schema", func(v, s string, o ...string) string {
		paths := "paths:\n  /a:\n    get:\n      parameters:\n        - {name: p, in: query, schema: " + s +
			"}\n      responses: {'200': {description: ok}}\n"
		return admissionSpec(v, paths, o...)
	}},
	{"header", "/paths/~1a/get/responses/200/headers/X-H/schema", func(v, s string, o ...string) string {
		paths := "paths:\n  /a:\n    get:\n      responses:\n        '200':\n          description: ok\n" +
			"          headers:\n            X-H: {schema: " + s + "}\n"
		return admissionSpec(v, paths, o...)
	}},
	{"items", "/components/schemas/Holder/items", func(v, s string, o ...string) string {
		return admissionSpec(v, "", append(slices.Clone(o), "Holder: {type: array, items: "+s+"}")...)
	}},
	{"map", "/components/schemas/Holder/additionalProperties", func(v, s string, o ...string) string {
		return admissionSpec(v, "", append(slices.Clone(o), "Holder: {type: object, additionalProperties: "+s+"}")...)
	}},
	{"branch", "/components/schemas/Holder/oneOf/0", func(v, s string, o ...string) string {
		return admissionSpec(v, "", append(slices.Clone(o), "Holder: {oneOf: ["+s+", {type: boolean}]}")...)
	}},
	{"body", "/paths/~1a/post/requestBody/content/application~1json/schema", func(v, s string, o ...string) string {
		paths := "paths:\n  /a:\n    post:\n      requestBody:\n        content:\n          application/json:\n" +
			"            schema: " + s + "\n      responses: {'200': {description: ok}}\n"
		return admissionSpec(v, paths, o...)
	}},
}

// refTo is a $ref to the component named.
func refTo(name string) string { return "$ref: '#/components/schemas/" + name + "'" }

// compileUnrefused compiles spec and fails the test, naming the first error,
// where the compiler refuses it: a spelling that cannot be compiled says
// nothing of how it lowers.
func compileUnrefused(t *testing.T, spec string) (*ir.Document, []ir.Diagnostic) {
	t.Helper()
	doc, diags := compileAnnotationSpec(t, "conjunction", spec)
	for _, diagnostic := range diags {
		require.NotEqual(t, ir.SeverityError, diagnostic.Severity, "the case is written to compile: %s", diagnostic.Message)
	}
	return doc, diags
}

// anonymousNodesAt lists the IDs of the nodes of doc minted at or under the
// pointer: the anonymous nodes a position hoisted.
func anonymousNodesAt(doc *ir.Document, pointer string) []ir.TypeID {
	var ids []ir.TypeID
	prefix := "type/anonymous" + pointer
	for id := range doc.Types {
		if s := string(id); s == prefix || strings.HasPrefix(s, prefix+"/") {
			ids = append(ids, id)
		}
	}
	slices.Sort(ids)
	return ids
}

// anonymousOver lists the IDs of the anonymous nodes of doc that name the
// target as their base: the decorators and aliases a use hoisted.
func anonymousOver(doc *ir.Document, target ir.TypeID) []ir.TypeID {
	var ids []ir.TypeID
	for id, td := range doc.Types {
		if !td.Common().Anonymous {
			continue
		}
		switch n := td.(type) {
		case *ir.Model:
			if n.Base != nil && n.Base.Target == target {
				ids = append(ids, id)
			}
		case *ir.Scalar:
			if n.Base != nil && n.Base.Target == target {
				ids = append(ids, id)
			}
		}
	}
	slices.Sort(ids)
	return ids
}

// requireNothingHoisted compiles the schema at the site and holds the use to
// say nothing more than its target: nothing hoisted, and a component the node
// that owns it.
func requireNothingHoisted(t *testing.T, version, target string, site conjSite, schema string) {
	t.Helper()
	doc, diags := compileUnrefused(t, site.write(version, schema, conjTargets[target]))
	requireErrorsAndWarnings(t, diags, nil)
	assert.Zero(t, degradedInfosUnder(diags, "/"), "nothing is kept, so nothing is reported")
	id := componentID(target)
	if site.name == "component" {
		use := kindNode(t, doc, componentID("Use"))
		if target == "Obj" {
			model, ok := use.(*ir.Model)
			require.True(t, ok, "a component over an object is the model that owns it")
			require.NotNil(t, model.Base)
			assert.Equal(t, id, model.Base.Target)
			return
		}
		scalar, ok := use.(*ir.Scalar)
		require.True(t, ok, "a component over any other target is the scalar alias that owns it")
		require.NotNil(t, scalar.Base)
		assert.Equal(t, id, scalar.Base.Target)
		return
	}
	assert.Empty(t, anonymousNodesAt(doc, site.pointer), "no node stands at %s", site.pointer)
	assert.Empty(t, anonymousOver(doc, id), "no node names %s as its base", target)
}

// requireModelNode returns the node at id, which is a model.
func requireModelNode(t *testing.T, doc *ir.Document, id ir.TypeID) *ir.Model {
	t.Helper()
	model, ok := kindNode(t, doc, id).(*ir.Model)
	require.True(t, ok, "%s is a model", id)
	return model
}

// propertyNames lists the sources of a model's own properties, sorted.
func propertyNames(model *ir.Model) []string {
	names := make([]string, 0, len(model.Properties))
	for _, property := range model.Properties {
		names = append(names, property.Name.Source)
	}
	slices.Sort(names)
	return names
}

// twoUnionShapes write one body, a oneOf distributing over objects and an
// anyOf of required members, on one schema and on entries in two orders.
var twoUnionShapes = func() map[string]string {
	body := "type: object, properties: {x: {type: string}, y: {type: string}}"
	oneOf := "oneOf: [{type: object, properties: {p: {type: string}}, required: [p]}, " +
		"{type: object, properties: {q: {type: string}}, required: [q]}]"
	anyOf := "anyOf: [{required: [x]}, {required: [y]}]"
	return map[string]string{
		"Flat":       "{" + body + ", " + oneOf + ", " + anyOf + "}",
		"Entries":    "{allOf: [{" + body + "}, {" + oneOf + "}, {" + anyOf + "}]}",
		"EntriesRev": "{allOf: [{" + anyOf + "}, {" + oneOf + "}, {" + body + "}]}",
	}
}()

// requireOneOfDistributed holds the component Use to the union of the oneOf
// of twoUnionShapes: exclusive, a model for each branch carrying the body, and
// the anyOf kept on each of them under its keyword.
func requireOneOfDistributed(t *testing.T, doc *ir.Document) {
	t.Helper()
	union, ok := kindNode(t, doc, componentID("Use")).(*ir.Union)
	require.True(t, ok, "the oneOf distributes")
	assert.True(t, union.Exclusive)
	require.Len(t, union.Variants, 2)
	for _, irVariant := range union.Variants {
		variant := requireModelNode(t, doc, irVariant.Type.Target)
		assert.Equal(t, []string{"x", "y"}, propertyNames(variant), "each variant carries the body")
		// Its branches declare no shape against the body, so it is validation
		// logic, as the anyOf beside a body alone is.
		assert.Equal(t, map[string]ir.UnmodeledReason{"openapi:anyOf": ir.ReasonValidationOnly}, keptOn(variant),
			"the anyOf is kept on each variant")
	}
}

// holderWirePropertyType is the type the property named of the component Holder names.
func holderWirePropertyType(t *testing.T, doc *ir.Document, name string) ir.TypeID {
	t.Helper()
	property, ok := propertyByWire(requireModelOf(t, doc, "Holder"), name)
	require.True(t, ok, "Holder holds no property %s", name)
	return property.Type.Target
}
