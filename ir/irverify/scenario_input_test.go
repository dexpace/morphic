// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package irverify_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dexpace/morphic/ir"
)

// scenarioValue is the string value a scenario's parameters and body hold.
var scenarioValue = ir.Value{Kind: ir.ValueString, String: "v"}

// scenarioOp is the path of the first operation of scenarioBase.
const scenarioOp = "document.Services[0].Groups[0].Operations[0]"

// scenarioBase returns the document the identity and scenario cases below
// mutate: two operations, the first with a query parameter bound once, and
// every namespace its IDs live in declared. It draws no violation of the codes
// those cases look for.
func scenarioBase() *ir.Document {
	prim := &ir.Primitive{ID: "type/primitive/string", Name: named("string"), PrimitiveKind: ir.PrimitiveString}
	prim.Provenance = ir.Provenance{Source: ir.NoSource}
	return &ir.Document{
		IRVersion: ir.IRVersion,
		IDSpaces: map[string][]string{
			"type": {"x"}, "operation": {"x"}, "service": {"x"}, "operation-group": {"x"}, "property": {"x"}, "parameter": {"x"}, "enum-member": {"x"},
		},
		Types: ir.TypeRegistry{
			prim.ID: prim,
			"type/x/M": &ir.Model{ID: "type/x/M", Name: named("m"), Properties: []ir.Property{
				{ID: "property/x/M/a", Name: named("a"), Type: ir.TypeRef{Target: "type/primitive/string"}},
			}},
			"type/x/E": &ir.Enum{ID: "type/x/E", Name: named("e"), ValueType: ir.PrimitiveString, Members: []ir.EnumMember{
				{ID: "enum-member/x/E/string:a", Name: named("a"), Value: ir.Value{Kind: ir.ValueString, String: "a"}},
			}},
		},
		Services: []ir.Service{{ID: "service/x/S", Name: named("s"), Groups: []ir.OperationGroup{{
			ID: "operation-group/x/G", Name: named("g"),
			Operations: []ir.Operation{
				{
					ID: "operation/x/o", Name: named("o"),
					Params: []ir.Parameter{{ID: "parameter/x/o/q", Name: named("q"), Type: ir.TypeRef{Target: "type/primitive/string"}}},
					Bindings: ir.OpBindings{HTTP: []ir.HTTPBinding{{
						Method: "GET", URITemplate: "/o",
						ParamBindings: []ir.HTTPParamBinding{{Param: "parameter/x/o/q", Location: ir.HTTPLocationQuery}},
					}}},
				},
				{
					ID: "operation/x/p", Name: named("p"),
					Params: []ir.Parameter{{ID: "parameter/x/p/z", Name: named("z"), Type: ir.TypeRef{Target: "type/primitive/string"}}},
				},
			},
		}}}},
	}
}

// scenarioOperation returns the first operation of doc, the one the cases edit.
func scenarioOperation(doc *ir.Document) *ir.Operation {
	return &doc.Services[0].Groups[0].Operations[0]
}

// scenarioCodes returns the violations of code that doc draws, as the paths
// they are reported at.
func scenarioCodes(doc *ir.Document, code string) []string {
	return violationPaths(verifyDeclared(doc), code)
}

// TestVerify_ScenarioKeyNamesADeclaredParameter pins that a key of an example's
// Params is a reference: one naming no declared parameter is a dangling
// reference, at the key, and a declared one is clean.
func TestVerify_ScenarioKeyNamesADeclaredParameter(t *testing.T) {
	t.Parallel()
	const code = "ir/dangling-parameter-ref"
	doc := scenarioBase()
	scenarioOperation(doc).Examples = []ir.Example{{Name: "ex", Input: &ir.ScenarioInput{
		Params: map[ir.ParameterID]ir.Value{"parameter/x/none": scenarioValue},
	}}}

	assert.Equal(t, []string{scenarioOp + ".Examples[0].Input.Params[parameter/x/none].key"}, scenarioCodes(doc, code))

	valid := scenarioBase()
	scenarioOperation(valid).Examples = []ir.Example{{Name: "ex", Input: &ir.ScenarioInput{
		Params: map[ir.ParameterID]ir.Value{"parameter/x/o/q": scenarioValue},
	}}}
	assert.Empty(t, scenarioCodes(valid, code))
}

// TestVerify_ScenarioKeyIsNeverEmpty pins that an empty key names nothing,
// where an empty reference is a violation, and that the reference reaches a
// map's key and not only its value.
func TestVerify_ScenarioKeyIsNeverEmpty(t *testing.T) {
	t.Parallel()
	const code = "ir/empty-parameter-ref"
	doc := scenarioBase()
	scenarioOperation(doc).Examples = []ir.Example{{Name: "ex", Input: &ir.ScenarioInput{
		Params: map[ir.ParameterID]ir.Value{"": scenarioValue},
	}}}

	assert.Equal(t, []string{scenarioOp + ".Examples[0].Input.Params[].key"}, scenarioCodes(doc, code))

	valid := scenarioBase()
	scenarioOperation(valid).Examples = []ir.Example{{Name: "ex", Input: &ir.ScenarioInput{
		Params: map[ir.ParameterID]ir.Value{"parameter/x/o/q": scenarioValue},
	}}}
	assert.Empty(t, scenarioCodes(valid, code))
}

// TestVerify_ScenarioBodyNeedsARequest pins ir/input-body-absent at a
// scenario's Body: an operation that declares no request has no content for it
// to be the value of.
func TestVerify_ScenarioBodyNeedsARequest(t *testing.T) {
	t.Parallel()
	const code = "ir/input-body-absent"
	body := scenarioValue
	input := func() []ir.Example {
		return []ir.Example{{Name: "ex", Input: &ir.ScenarioInput{Body: &body}}}
	}
	doc := scenarioBase()
	scenarioOperation(doc).Examples = input()

	assert.Equal(t, []string{scenarioOp + ".Examples[0].Input.Body"}, scenarioCodes(doc, code))

	valid := scenarioBase()
	scenarioOperation(valid).Request = &ir.Payload{}
	scenarioOperation(valid).Examples = input()
	assert.Empty(t, scenarioCodes(valid, code))

	noBody := scenarioBase()
	scenarioOperation(noBody).Examples = []ir.Example{{Name: "ex", Input: &ir.ScenarioInput{}}}
	assert.Empty(t, scenarioCodes(noBody, code), "an input holding no Body asks nothing of the request")
}

// TestVerify_ScenarioBodyIsStatedOneWay pins ir/input-body-twice: where any
// parameter is bound into the body, whole or by property, the parameters state
// the body, so a Body beside them states part of it twice. A parameter bound
// elsewhere leaves the Body alone.
func TestVerify_ScenarioBodyIsStatedOneWay(t *testing.T) {
	t.Parallel()
	const code = "ir/input-body-twice"
	object := ir.Value{Kind: ir.ValueObject, Object: []ir.Field{{Name: "a", Value: scenarioValue}}}
	str := scenarioValue
	for _, tc := range []struct {
		name  string
		bind  ir.HTTPParamBinding
		input ir.ScenarioInput
	}{
		{
			name: "a parameter bound into the body by property",
			bind: ir.HTTPParamBinding{
				Param: "parameter/x/o/q", Location: ir.HTTPLocationBodyProperty, BodyPath: []ir.PropertyID{"property/x/M/a"},
			},
			input: ir.ScenarioInput{Body: &object},
		},
		{
			name:  "a parameter bound as the whole body, valued beside the Body",
			bind:  ir.HTTPParamBinding{Param: "parameter/x/o/q", Location: ir.HTTPLocationBody},
			input: ir.ScenarioInput{Params: map[ir.ParameterID]ir.Value{"parameter/x/o/q": scenarioValue}, Body: &str},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			doc := scenarioBase()
			op := scenarioOperation(doc)
			op.Request = &ir.Payload{}
			op.Bindings.HTTP[0].ParamBindings[0] = tc.bind
			op.Examples = []ir.Example{{Name: "ex", Input: &tc.input}}

			assert.Equal(t, []string{scenarioOp + ".Examples[0].Input.Body"}, scenarioCodes(doc, code))
			assert.Empty(t, scenarioCodes(doc, "ir/input-body-absent"), "the operation has a request")

			valid := scenarioBase()
			vop := scenarioOperation(valid)
			vop.Request = &ir.Payload{}
			vop.Examples = []ir.Example{{Name: "ex", Input: &tc.input}}
			assert.Empty(t, scenarioCodes(valid, code), "the same input beside a parameter bound in the query")
		})
	}
}

// TestVerify_ScenarioParamValueIsHeldAsAnyValue is the reach pin for the
// values an input carries: one of an unknown kind under a parameter's key is
// reported at that value, as it would be at any other value.
func TestVerify_ScenarioParamValueIsHeldAsAnyValue(t *testing.T) {
	t.Parallel()
	const code = "ir/unknown-value-kind"
	doc := scenarioBase()
	scenarioOperation(doc).Examples = []ir.Example{{Name: "ex", Input: &ir.ScenarioInput{
		Params: map[ir.ParameterID]ir.Value{"parameter/x/o/q": {Kind: "bogus"}},
	}}}

	assert.Equal(t, []string{scenarioOp + ".Examples[0].Input.Params[parameter/x/o/q].Kind"}, scenarioCodes(doc, code))

	valid := scenarioBase()
	scenarioOperation(valid).Examples = []ir.Example{{Name: "ex", Input: &ir.ScenarioInput{
		Params: map[ir.ParameterID]ir.Value{"parameter/x/o/q": scenarioValue},
	}}}
	assert.Empty(t, scenarioCodes(valid, code))
}

// TestVerify_ScenarioKeyOfAnotherOperationIsNotIrverifys pins the boundary
// between the two checkers: a key naming a parameter that another operation
// declares resolves, so irverify reports nothing; pass.Validate owns the claim
// that it belongs to this operation.
func TestVerify_ScenarioKeyOfAnotherOperationIsNotIrverifys(t *testing.T) {
	t.Parallel()
	doc := scenarioBase()
	scenarioOperation(doc).Examples = []ir.Example{{Name: "ex", Input: &ir.ScenarioInput{
		Params: map[ir.ParameterID]ir.Value{"parameter/x/p/z": scenarioValue},
	}}}

	got := verifyDeclared(doc)

	assert.Empty(t, got, "irverify reports nothing of a key another operation declares")
}
