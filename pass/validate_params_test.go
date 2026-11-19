// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package pass_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/ir/irtest"
)

// TestValidate_ParamBinding_UndeclaredIDIsReportedOnceAsDangling pins that a
// binding naming an ID no operation declares draws the dangling walk's report
// alone. The declared parameter is bound too, so no unbound warning shares the
// mismatch code.
func TestValidate_ParamBinding_UndeclaredIDIsReportedOnceAsDangling(t *testing.T) {
	t.Parallel()
	op := paramBindingOp("op", ir.Parameter{ID: "parameter/x/op/id/query", Name: ir.Naming{Source: "id"}})
	op.Bindings.HTTP[0].ParamBindings = append(op.Bindings.HTTP[0].ParamBindings,
		ir.HTTPParamBinding{Param: "parameter/x/op/ghost/query", Location: ir.HTTPLocationQuery})
	diags := validated(t, docWithOperation(op))
	assert.Equal(t, 1, countCode(t, diags, "ir/dangling-parameter-ref"))
	assert.Zero(t, countCode(t, diags, "pass/param-binding-mismatch"), "the dangling walk owns an ID nothing declares")
}

// TestValidate_ParamBinding_EmptyParamIsReportedOnceAsEmpty pins that a binding
// naming no parameter at all draws the empty-reference report alone: neither the
// dangling walk nor the binding check reports it under a second code.
func TestValidate_ParamBinding_EmptyParamIsReportedOnceAsEmpty(t *testing.T) {
	t.Parallel()
	op := paramBindingOp("op", ir.Parameter{ID: "parameter/x/op/id/query", Name: ir.Naming{Source: "id"}})
	op.Bindings.HTTP[0].ParamBindings = append(op.Bindings.HTTP[0].ParamBindings,
		ir.HTTPParamBinding{Location: ir.HTTPLocationQuery})
	diags := validated(t, docWithOperation(op))
	assert.Equal(t, 1, countCode(t, diags, "ir/empty-parameter-ref"))
	assert.Zero(t, countCode(t, diags, "pass/param-binding-mismatch"))
	assert.Zero(t, countCode(t, diags, "ir/dangling-parameter-ref"))
}

func TestValidate_ParamBinding_DoubleBound(t *testing.T) {
	t.Parallel()
	op := ir.Operation{
		ID:     "op",
		Params: []ir.Parameter{{ID: "parameter/x/op/id/query", Name: ir.Naming{Source: "id"}, Type: ir.TypeRef{Target: "type/primitive/string"}}},
		Bindings: ir.OpBindings{HTTP: []ir.HTTPBinding{{
			Method: "GET", URITemplate: "/x",
			ParamBindings: []ir.HTTPParamBinding{
				{Param: "parameter/x/op/id/query", Location: ir.HTTPLocationQuery},
				{Param: "parameter/x/op/id/query", Location: ir.HTTPLocationQuery},
			},
		}}},
	}
	var messages []string
	for _, d := range validated(t, docWithOperation(op)) {
		if d.Code == "pass/param-binding-mismatch" {
			messages = append(messages, d.Message)
		}
	}
	require.Len(t, messages, 1)
	assert.Contains(t, messages[0], `parameter "id" is bound more than once in location "query"`,
		"the report names the parameter, not its ID")
}

// paramBindingOp returns an operation declaring the given parameters and
// binding each one once, in the query.
func paramBindingOp(id ir.OperationID, params ...ir.Parameter) ir.Operation {
	op := ir.Operation{ID: id, Params: params}
	bindings := make([]ir.HTTPParamBinding, 0, len(params))
	for _, p := range params {
		bindings = append(bindings, ir.HTTPParamBinding{Param: p.ID, Location: ir.HTTPLocationQuery})
	}
	op.Bindings = ir.OpBindings{HTTP: []ir.HTTPBinding{{Method: "GET", URITemplate: "/x", ParamBindings: bindings}}}
	return op
}

// TestValidate_ParamBinding_SameNameDifferentLocation pins that a parameter is
// identified by its ID: id in the query and id in the path are two parameters,
// each bound once, so neither binding is a duplicate of the other.
func TestValidate_ParamBinding_SameNameDifferentLocation(t *testing.T) {
	t.Parallel()
	op := paramBindingOp("operation/x/get",
		ir.Parameter{ID: "parameter/x/get/id/query", Name: ir.Naming{Source: "id"}},
		ir.Parameter{ID: "parameter/x/get/id/path", Name: ir.Naming{Source: "id"}},
	)
	op.Bindings.HTTP[0].ParamBindings[1].Location = ir.HTTPLocationPath
	assert.NotContains(t, irtest.DiagCodes(validated(t, docWithOperation(op))), "pass/param-binding-mismatch")
}

// TestValidate_ParamBinding_OtherOperationsParamIsRejected pins that a binding
// names a parameter of its own operation: another operation's parameter is a
// well-formed reference, so only this check can say it is the wrong one.
func TestValidate_ParamBinding_OtherOperationsParamIsRejected(t *testing.T) {
	t.Parallel()
	get := paramBindingOp("operation/x/get", ir.Parameter{ID: "parameter/x/get/id/query", Name: ir.Naming{Source: "id"}})
	post := paramBindingOp("operation/x/post", ir.Parameter{ID: "parameter/x/post/id/query", Name: ir.Naming{Source: "id"}})
	post.Bindings.HTTP[0].ParamBindings[0].Param = "parameter/x/get/id/query"

	doc := validDoc()
	doc.Services = []ir.Service{{ID: "s", Groups: []ir.OperationGroup{{Operations: []ir.Operation{get, post}}}}}
	diags := validated(t, doc)

	mismatches := withCode(diags, "pass/param-binding-mismatch")
	require.Len(t, mismatches, 1, "the foreign binding alone: %v", mismatches)
	assert.Contains(t, mismatches[0].Message, "does not declare")
	assert.Len(t, withCode(diags, "pass/param-unbound"), 1, "the parameter post then leaves unbound")
}

// TestValidate_ParamBinding_EachBindingIsJudgedAlone pins that an operation's
// HTTP bindings are judged one at a time, each report placed at its binding.
// The first binding places the parameter once: a second placing nothing
// leaves it unbound there, one placing it once binds it once there too, and
// the second's repeat and foreign parameter are the second's.
func TestValidate_ParamBinding_EachBindingIsJudgedAlone(t *testing.T) {
	t.Parallel()
	own := ir.HTTPParamBinding{Param: "parameter/x/op/id/query", Location: ir.HTTPLocationQuery}
	foreign := ir.HTTPParamBinding{Param: "parameter/x/other/id/query", Location: ir.HTTPLocationQuery}
	type report struct {
		code     string
		severity ir.Severity
		message  string
	}
	for _, tc := range []struct {
		name   string
		second []ir.HTTPParamBinding
		want   []report
	}{
		{"placing nothing", nil, []report{{"pass/param-unbound", ir.SeverityWarning,
			`parameter "id" on op/bindings/http/1 is not bound to any HTTP location`}}},
		{"placing it once", []ir.HTTPParamBinding{own}, []report{}},
		{"placing it twice", []ir.HTTPParamBinding{own, own}, []report{{"pass/param-binding-mismatch", ir.SeverityError,
			`parameter "id" is bound more than once in location "query" on op/bindings/http/1`}}},
		{"placing another operation's parameter", []ir.HTTPParamBinding{own, foreign}, []report{{"pass/param-binding-mismatch", ir.SeverityError,
			`binding on op/bindings/http/1 names parameter "parameter/x/other/id/query", which this operation does not declare`}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			op := paramBindingOp("op", ir.Parameter{ID: own.Param, Name: ir.Naming{Source: "id"}})
			op.Bindings.HTTP = append(op.Bindings.HTTP,
				ir.HTTPBinding{Method: "POST", URITemplate: "/x", ParamBindings: tc.second})
			other := paramBindingOp("other", ir.Parameter{ID: foreign.Param, Name: ir.Naming{Source: "id"}})

			diags := validated(t, docWithOperations(op, other))
			found := append(withCode(diags, "pass/param-unbound"), withCode(diags, "pass/param-binding-mismatch")...)
			got := make([]report, 0, len(found))
			for _, d := range found {
				assert.Equal(t, "op/bindings/http/1", d.Provenance.Node, d.Message)
				got = append(got, report{d.Code, d.Severity, d.Message})
			}
			assert.Equal(t, tc.want, got)
		})
	}
}

// TestValidate_HostParamBoundMultipleTimesIsLegal covers the host-location
// continue: a host label may be filled by a param several times without a
// double-bind error.
func TestValidate_HostParamBoundMultipleTimesIsLegal(t *testing.T) {
	t.Parallel()
	op := ir.Operation{
		ID:     "op",
		Params: []ir.Parameter{{ID: "parameter/x/op/region/host", Name: ir.Naming{Source: "region"}, Type: ir.TypeRef{Target: "type/primitive/string"}}},
		Bindings: ir.OpBindings{HTTP: []ir.HTTPBinding{{
			Method: "GET", URITemplate: "/x", HostPrefix: "{region}.{region}",
			ParamBindings: []ir.HTTPParamBinding{
				{Param: "parameter/x/op/region/host", Location: ir.HTTPLocationHost},
				{Param: "parameter/x/op/region/host", Location: ir.HTTPLocationHost},
			},
		}}},
	}
	assert.NotContains(t, irtest.DiagCodes(validated(t, docWithOperation(op))), "pass/param-binding-mismatch")
}

// paramRefOp returns an operation declaring one parameter and pointing its
// pagination cursor, limit and idempotency token at the given IDs.
func paramRefOp(id ir.OperationID, own ir.ParameterID, cursor, limit, token ir.ParameterID) ir.Operation {
	return ir.Operation{
		ID:     id,
		Params: []ir.Parameter{{ID: own, Name: ir.Naming{Source: "p"}}},
		Pagination: &ir.Pagination{
			InputCursor: &ir.InputPath{Param: new(cursor)},
			InputLimit:  &ir.InputPath{Param: new(limit)},
		},
		Idempotency: ir.Idempotency{Kind: ir.IdempotencyToken, TokenParam: token},
	}
}

func TestValidate_ParamReferences_OwnOperationIsClean(t *testing.T) {
	t.Parallel()
	op := paramRefOp("operation/x/put", "parameter/x/put/p", "parameter/x/put/p", "parameter/x/put/p", "parameter/x/put/p")
	diags := validated(t, docWithOperations(op))
	assert.NotContains(t, irtest.DiagCodes(diags), "pass/param-binding-mismatch")
	assert.NotContains(t, irtest.DiagCodes(diags), "ir/dangling-parameter-ref")
}

func TestValidate_ParamReferences_OtherOperationsParamIsRejected(t *testing.T) {
	t.Parallel()
	get := paramRefOp("operation/x/get", "parameter/x/get/p", "parameter/x/get/p", "parameter/x/get/p", "parameter/x/get/p")
	post := paramRefOp("operation/x/post", "parameter/x/post/p", "parameter/x/get/p", "parameter/x/get/p", "parameter/x/get/p")

	diags := validated(t, docWithOperations(get, post))

	var at []string
	for _, d := range diags {
		if d.Code == "pass/param-binding-mismatch" {
			at = append(at, d.Provenance.Node)
		}
	}
	assert.Equal(t, []string{
		"operation/x/post/pagination/inputCursor", "operation/x/post/pagination/inputLimit", "operation/x/post/idempotency/tokenParam",
	}, at)
}

func TestValidate_ParamReferences_UndeclaredIDIsReportedOnceAsDangling(t *testing.T) {
	t.Parallel()
	op := paramRefOp("operation/x/get", "parameter/x/get/p", "parameter/x/ghost", "parameter/x/get/p", "parameter/x/get/p")
	diags := validated(t, docWithOperations(op))
	assert.Equal(t, 1, countCode(t, diags, "ir/dangling-parameter-ref"))
	assert.Zero(t, countCode(t, diags, "pass/param-binding-mismatch"), "the dangling walk owns an ID nothing declares")
}

// TestValidate_ParamReferences_StreamConditionsAreHeld pins that the input a
// request or response stream is selected by is a reference like a pagination
// input: one naming the operation's own parameter is clean, one naming another
// operation's is reported at its field.
func TestValidate_ParamReferences_StreamConditionsAreHeld(t *testing.T) {
	t.Parallel()
	streamed := func(id ir.OperationID, own, cond ir.ParameterID) ir.Operation {
		op := paramRefOp(id, own, own, own, own)
		op.RequestStream = &ir.StreamDetail{Condition: &ir.StreamCondition{Input: ir.InputPath{Param: new(cond)}}}
		op.ResponseStream = &ir.StreamDetail{Condition: &ir.StreamCondition{Input: ir.InputPath{Param: new(cond)}}}
		return op
	}
	get := streamed("operation/x/get", "parameter/x/get/p", "parameter/x/get/p")
	post := streamed("operation/x/post", "parameter/x/post/p", "parameter/x/get/p")

	var at []string
	for _, d := range validated(t, docWithOperations(get, post)) {
		if d.Code == "pass/param-binding-mismatch" {
			at = append(at, d.Provenance.Node)
		}
	}
	assert.Equal(t, []string{"operation/x/post/requestStream/condition", "operation/x/post/responseStream/condition"}, at)
}

// TestValidate_ParamReferences_BodyInputsAreNoReference pins that an input
// rooted in the request body names no parameter, whether it continues a page
// walk or asks for a stream, so nothing holds it to the operation's own.
func TestValidate_ParamReferences_BodyInputsAreNoReference(t *testing.T) {
	t.Parallel()
	op := paramRefOp("operation/x/search", "parameter/x/search/p", "parameter/x/search/p", "parameter/x/search/p", "parameter/x/search/p")
	body := ir.InputPath{Segments: []ir.PropertyID{"property/m/a"}}
	op.Pagination = &ir.Pagination{InputCursor: &body, InputLimit: &body}
	op.RequestStream = &ir.StreamDetail{Condition: &ir.StreamCondition{Input: body}}
	op.ResponseStream = &ir.StreamDetail{Condition: &ir.StreamCondition{Input: body}}

	codes := irtest.DiagCodes(validated(t, docWithOperations(op)))
	assert.NotContains(t, codes, "pass/param-binding-mismatch")
	assert.NotContains(t, codes, "ir/dangling-parameter-ref")
	assert.NotContains(t, codes, "ir/dangling-property-ref", "the body member the inputs name is declared")
}

// TestValidate_ParamReferences_EmptyTokenParamWithoutTokenKindIsClean pins that
// an empty token is no reference: it is how an operation that is not
// token-keyed spells having none.
func TestValidate_ParamReferences_EmptyTokenParamWithoutTokenKindIsClean(t *testing.T) {
	t.Parallel()
	op := paramRefOp("operation/x/get", "parameter/x/get/p", "parameter/x/get/p", "parameter/x/get/p", "")
	op.Idempotency = ir.Idempotency{Kind: ir.IdempotencySafe}
	diags := validated(t, docWithOperations(op))
	assert.Zero(t, countCode(t, diags, "pass/param-binding-mismatch"))
	assert.Zero(t, countCode(t, diags, "ir/dangling-parameter-ref"))
}

// TestValidate_ParamReferences_EmptyPaginationInputIsReportedOnceAsEmpty pins
// that a pagination input naming no parameter draws the empty-reference report
// alone, as an empty binding does.
func TestValidate_ParamReferences_EmptyPaginationInputIsReportedOnceAsEmpty(t *testing.T) {
	t.Parallel()
	op := paramRefOp("operation/x/get", "parameter/x/get/p", "", "parameter/x/get/p", "parameter/x/get/p")
	diags := validated(t, docWithOperations(op))
	var at []string
	for _, d := range diags {
		if d.Code == "ir/empty-parameter-ref" {
			at = append(at, d.Provenance.Node)
		}
	}
	assert.Equal(t, []string{"document.Services[0].Groups[0].Operations[0].Pagination.InputCursor.Param"}, at)
	assert.Zero(t, countCode(t, diags, "pass/param-binding-mismatch"))
}

// TestValidate_ParamReferences_FieldArgumentIsRejected pins that a binding or a
// pagination input naming a field argument is reported. The argument is a
// declared parameter, so the dangling walk resolves the reference and only this
// check can say it is no parameter of the operation.
func TestValidate_ParamReferences_FieldArgumentIsRejected(t *testing.T) {
	t.Parallel()
	const arg = ir.ParameterID("parameter/x/m/a/limit")
	op := paramBindingOp("operation/x/get", ir.Parameter{ID: "parameter/x/get/id/query", Name: ir.Naming{Source: "id"}})
	op.Bindings.HTTP[0].ParamBindings = append(op.Bindings.HTTP[0].ParamBindings,
		ir.HTTPParamBinding{Param: arg, Location: ir.HTTPLocationQuery})
	op.Pagination = &ir.Pagination{InputLimit: &ir.InputPath{Param: new(arg)}}
	doc := docWithOperations(op)
	model, ok := doc.Types["type/m"].(*ir.Model)
	require.True(t, ok)
	model.Properties[0].Args = []ir.Parameter{{ID: arg, Name: ir.Naming{Source: "limit"}, Type: ir.TypeRef{Target: "type/primitive/string"}}}

	diags := validated(t, doc)
	var at []string
	for _, d := range diags {
		if d.Code == "pass/param-binding-mismatch" {
			at = append(at, d.Provenance.Node)
		}
	}
	assert.Equal(t, []string{"operation/x/get/bindings/http/0", "operation/x/get/pagination/inputLimit"}, at)
	assert.Zero(t, countCode(t, diags, "ir/dangling-parameter-ref"), "the argument is declared")
}
