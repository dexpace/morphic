// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package pass_test

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
)

// scenarioOpPath is where the walk reaches the first operation of a document
// docWithOperations builds.
const scenarioOpPath = "document.Services[0].Groups[0].Operations[0]"

// scenarioDoc returns a document with two operations, each declaring and
// binding its own parameters, the first of which carries one example whose
// input holds params.
func scenarioDoc(params map[ir.ParameterID]ir.Value) *ir.Document {
	first := paramBindingOp("operation/x/o", ir.Parameter{ID: "parameter/x/o/q", Name: ir.Naming{Source: "q"}})
	first.Examples = []ir.Example{{Name: "ex", Input: &ir.ScenarioInput{Params: params}}}
	second := paramBindingOp("operation/x/p", ir.Parameter{ID: "parameter/x/p/z", Name: ir.Naming{Source: "z"}})
	return docWithOperations(first, second)
}

// nodesOfCode returns the nodes of the diagnostics of code, in report order.
func nodesOfCode(diags []ir.Diagnostic, code string) []string {
	var out []string
	for _, d := range diags {
		if d.Code == code {
			out = append(out, d.Provenance.Node)
		}
	}
	return out
}

// TestValidate_ScenarioKeyNamesADeclaredParameter pins that a key of an
// example's Params is a reference the dangling walk resolves, reported at the
// key, and that a declared one is clean.
func TestValidate_ScenarioKeyNamesADeclaredParameter(t *testing.T) {
	t.Parallel()
	diags := validated(t, scenarioDoc(map[ir.ParameterID]ir.Value{"parameter/x/none": {Kind: ir.ValueString, String: "v"}}))
	assert.Equal(t, []string{scenarioOpPath + ".Examples[0].Input.Params[parameter/x/none].key"},
		nodesOfCode(diags, "ir/dangling-parameter-ref"))

	valid := validated(t, scenarioDoc(map[ir.ParameterID]ir.Value{"parameter/x/o/q": {Kind: ir.ValueString, String: "v"}}))
	assert.Empty(t, nodesOfCode(valid, "ir/dangling-parameter-ref"))
}

// TestValidate_ScenarioKeyIsNeverEmpty pins that an empty key is reported as an
// empty reference, once, at the key.
func TestValidate_ScenarioKeyIsNeverEmpty(t *testing.T) {
	t.Parallel()
	diags := validated(t, scenarioDoc(map[ir.ParameterID]ir.Value{"": {Kind: ir.ValueString, String: "v"}}))

	assert.Equal(t, []string{scenarioOpPath + ".Examples[0].Input.Params[].key"}, nodesOfCode(diags, "ir/empty-parameter-ref"))
	assert.Empty(t, nodesOfCode(diags, "pass/param-binding-mismatch"), "an empty key has one code")
	assert.Empty(t, nodesOfCode(diags, "ir/dangling-parameter-ref"))
}

// TestValidate_ScenarioKeyNamesAParameterOfItsOwnOperation pins the claim only
// pass makes: a key resolves to a parameter some operation declares, and that
// is not the example's own operation, so it is reported once as an error at the
// example's input, where a key of the operation itself reports nothing.
func TestValidate_ScenarioKeyNamesAParameterOfItsOwnOperation(t *testing.T) {
	t.Parallel()
	diags := validated(t, scenarioDoc(map[ir.ParameterID]ir.Value{"parameter/x/p/z": {Kind: ir.ValueString, String: "v"}}))

	require.Equal(t, 1, countCode(t, diags, "pass/param-binding-mismatch"))
	for _, d := range diags {
		if d.Code == "pass/param-binding-mismatch" {
			assert.Equal(t, ir.SeverityError, d.Severity)
			assert.Equal(t, "operation/x/o/examples/0/input", d.Provenance.Node)
		}
	}
	assert.Empty(t, nodesOfCode(diags, "ir/dangling-parameter-ref"), "the parameter is declared")

	valid := validated(t, scenarioDoc(map[ir.ParameterID]ir.Value{"parameter/x/o/q": {Kind: ir.ValueString, String: "v"}}))
	assert.Zero(t, countCode(t, valid, "pass/param-binding-mismatch"))
}

// TestValidate_ScenarioReportHasOneOrder pins that the report of several keys
// that name another operation's parameters is the same on every run, though the
// keys sit in a map.
func TestValidate_ScenarioReportHasOneOrder(t *testing.T) {
	t.Parallel()
	const keys = 8
	params := make(map[ir.ParameterID]ir.Value, keys)
	other := make([]ir.Parameter, 0, keys)
	for i := range keys {
		id := ir.ParameterID(fmt.Sprintf("parameter/x/p/z%d", i))
		params[id] = ir.Value{Kind: ir.ValueString, String: "v"}
		other = append(other, ir.Parameter{ID: id, Name: ir.Naming{Source: fmt.Sprintf("z%d", i)}})
	}
	first := paramBindingOp("operation/x/o", ir.Parameter{ID: "parameter/x/o/q", Name: ir.Naming{Source: "q"}})
	first.Examples = []ir.Example{{Name: "ex", Input: &ir.ScenarioInput{Params: params}}}
	doc := docWithOperations(first, paramBindingOp("operation/x/p", other...))

	require.Equal(t, keys, countCode(t, validated(t, doc), "pass/param-binding-mismatch"))
}

// TestValidate_ScenarioKeyIsPlacedAtItsOwnExample pins where a key naming
// another operation's parameter is reported when the operation carries an
// example that is no scenario before it: at the second example's input, the
// first holding no input to read.
func TestValidate_ScenarioKeyIsPlacedAtItsOwnExample(t *testing.T) {
	t.Parallel()
	doc := scenarioDoc(map[ir.ParameterID]ir.Value{"parameter/x/p/z": {Kind: ir.ValueString, String: "v"}})
	op := &doc.Services[0].Groups[0].Operations[0]
	op.Examples = append([]ir.Example{{Name: "value", Value: &ir.Value{Kind: ir.ValueString, String: "v"}}}, op.Examples...)

	var at []string
	for _, d := range validated(t, doc) {
		if d.Code == "pass/param-binding-mismatch" && d.Severity == ir.SeverityError {
			at = append(at, d.Provenance.Node)
		}
	}
	assert.Equal(t, []string{"operation/x/o/examples/1/input"}, at)
}
