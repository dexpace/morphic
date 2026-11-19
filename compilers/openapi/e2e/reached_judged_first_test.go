// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package e2e_test

import (
	"fmt"
	"path/filepath"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi"
	"github.com/dexpace/morphic/ir"
)

// reachedParam is a parameter object of a document's own reading: three values
// the rule table refuses, written where a $ref reaches them.
const reachedParam = `{name: p, in: query, explode: "true", required: on, schema: {type: array, items: hello}}`

// judgedAt lists each type-mismatch diagnostic as its severity, source entry
// and pointer, sorted: where a finding lands, not what it says.
func judgedAt(diags []ir.Diagnostic) []string {
	var out []string
	for _, diagnostic := range diags {
		if diagnostic.Code == typeMismatch {
			out = append(out, fmt.Sprintf("%s %d %s", diagnostic.Severity, diagnostic.Provenance.Source, diagnostic.Provenance.Pointer))
		}
	}
	slices.Sort(out)
	return out
}

// onlyParam returns the one parameter of the one operation of doc and the HTTP
// binding that carries it.
func onlyParam(t *testing.T, doc *ir.Document) (ir.Parameter, ir.HTTPParamBinding) {
	t.Helper()
	require.NotNil(t, doc)
	operations := allOperations(doc)
	require.Len(t, operations, 1)
	require.Len(t, operations[0].Params, 1)
	require.Len(t, operations[0].Bindings.HTTP, 1)
	require.Len(t, operations[0].Bindings.HTTP[0].ParamBindings, 1)
	return operations[0].Params[0], operations[0].Bindings.HTTP[0].ParamBindings[0]
}

// reachedSpec writes the one operation of the reached-parameter documents, its
// parameter the $ref ref names.
func reachedSpec(ref, tail string) string {
	return head31 + "paths:\n  /a:\n    get:\n      operationId: a\n      parameters:\n        - $ref: '" + ref + "'\n" +
		"      responses:\n        '200': {description: ok}\n" + tail
}

// TestCompile_AValueARefLandsOnIsJudgedBeforeItIsBuilt holds a parameter whose
// refused values sit under an extension, where only a reference reaches them,
// to the reading of the same parameter declared under components: three type
// errors, each once, no false-schema information for the refused items, the
// refused required and explode left out, so the parameter is optional and its
// binding states no explode.
func TestCompile_AValueARefLandsOnIsJudgedBeforeItIsBuilt(t *testing.T) {
	t.Parallel()
	reached, diags := compileText(t, "spec.yaml", reachedSpec("#/x-lib/p", "x-lib:\n  p: "+reachedParam+"\n"), openapi.Options{})
	twin, twinDiags := compileText(t, "spec.yaml",
		reachedSpec("#/components/parameters/p", "components:\n  parameters:\n    p: "+reachedParam+"\n"), openapi.Options{})

	assert.Equal(t, []string{
		"error 0 /x-lib/p/explode",
		"error 0 /x-lib/p/required",
		"error 0 /x-lib/p/schema/items",
	}, judgedAt(diags), "%+v", diags)
	assert.Empty(t, byCode(diags, "openapi/false-schema"), "items: hello is read as no schema, so no schema is false")
	for _, diagnostic := range byCode(diags, typeMismatch) {
		assert.Contains(t, diagnostic.Message, "must be a")
	}
	param, binding := onlyParam(t, reached)
	assert.False(t, param.Required, "required: on is no boolean, so it is left out")
	assert.Nil(t, binding.Explode, `explode: "true" is no boolean, so the binding states none`)

	twinParam, twinBinding := onlyParam(t, twin)
	assert.Equal(t, twinParam.Required, param.Required)
	assert.Equal(t, twinBinding.Explode, binding.Explode)
	assert.Equal(t, twinBinding.Style, binding.Style)
	assert.Len(t, byCode(diags, "openapi/false-schema"), len(byCode(twinDiags, "openapi/false-schema")))
	assert.Len(t, judgedAt(diags), len(judgedAt(twinDiags)))
	assert.Len(t, reached.Types, len(twin.Types), "the reached parameter lowers the types its twin does")
}

// TestCompile_AValueAnotherDocumentsRefLandsOnIsJudgedBeforeItIsBuilt holds the
// same defect where the parameter lies in another document: two errors in that
// document's own entry, at the pointers its text writes them at, and the values
// left out.
func TestCompile_AValueAnotherDocumentsRefLandsOnIsJudgedBeforeItIsBuilt(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	layout(t, dir, map[string]string{
		"lib.yaml": "p: {name: p, in: query, explode: \"true\", required: on, schema: {type: string}}\n",
	})

	doc, diags := compileOnDisk(t, filepath.Join(dir, "root.yaml"), reachedSpec("lib.yaml#/p", ""), true)

	assert.Equal(t, []string{"error 1 /p/explode", "error 1 /p/required"}, judgedAt(diags), "%+v", diags)
	assert.Len(t, diags, 2, "the full list: %+v", diags)
	param, binding := onlyParam(t, doc)
	assert.False(t, param.Required)
	assert.Nil(t, binding.Explode)
	assert.Len(t, doc.Sources, 2)
}

// TestCompile_ANullableARefLandsOnWritesWrongIsNotAdmitted holds a 3.0 schema a
// reference reaches, whose nullable is no boolean, to the one error at the
// field and to a target that admits no null: the refusal is read as the field
// left out where the schema is built, not as the string's truthiness.
func TestCompile_ANullableARefLandsOnWritesWrongIsNotAdmitted(t *testing.T) {
	t.Parallel()
	src := "openapi: 3.0.3\ninfo: {title: t, version: \"1\"}\npaths: {}\ncomponents:\n  schemas:\n    A: {$ref: '#/x-lib/s'}\n" +
		"x-lib:\n  s: {type: string, nullable: y}\n"

	doc, diags := compileText(t, "spec.yaml", src, openapi.Options{})

	require.NotNil(t, doc)
	assert.Equal(t, []string{"error 0 /x-lib/s/nullable"}, judgedAt(diags), "%+v", diags)
	assert.Len(t, diags, 1, "the full list: %+v", diags)
	assert.False(t, containsJSON(t, doc, `"nullable":true`), "no use of A or of what it names admits null")
}

// TestCompile_AValueOneReadingRefusesAndAnotherAcceptsIsKeptForTheOther holds a
// node read as a parameter and as a schema: the parameter reading refuses its
// required list, which the schema reading takes, so the schema still requires
// the property and the refusal is reported once, in either declaration order.
func TestCompile_AValueOneReadingRefusesAndAnotherAcceptsIsKeptForTheOther(t *testing.T) {
	t.Parallel()
	const operation = "  /a:\n    get:\n      operationId: a\n      parameters: [{$ref: '#/x-lib/n'}]\n" +
		"      responses: {'200': {description: ok}}\n"
	const comps = "components:\n  schemas:\n    S: {$ref: '#/x-lib/n'}\n"
	const lib = "x-lib:\n  n: {name: p, in: query, type: object, required: [a], properties: {a: {type: string}}}\n"
	orders := map[string]string{
		"paths first":      head31 + "paths:\n" + operation + comps + lib,
		"components first": head31 + comps + "paths:\n" + operation + lib,
	}
	var first []string
	for _, name := range []string{"paths first", "components first"} {
		doc, diags := compileText(t, "spec.yaml", orders[name], openapi.Options{})

		require.NotNil(t, doc, name)
		got := judgedAt(diags)
		assert.Equal(t, []string{"error 0 /x-lib/n/required"}, got, "%s: %+v", name, diags)
		for _, diagnostic := range byCode(diags, typeMismatch) {
			assert.Contains(t, diagnostic.Message, `Parameter Object field "required" must be a boolean`, name)
		}
		model, ok := doc.Types[ir.TypeID("type/anonymous/x-lib/n")].(*ir.Model)
		require.True(t, ok, name)
		a, found := propertyByWire(model, "a")
		require.True(t, found, name)
		assert.True(t, a.Required, "%s: the schema reading accepts required: [a]", name)

		if first == nil {
			first = kinds(diags)
		} else {
			assert.Equal(t, first, kinds(diags), "one diagnostic set in both orders")
		}
	}
}
