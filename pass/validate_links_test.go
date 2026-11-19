// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package pass_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dexpace/morphic/ir"
)

// linkOps returns an operation "operation/x/get" declaring "parameter/x/get/id", and an
// operation "operation/x/post" declaring "parameter/x/post/id" whose responses each carry
// link: its success response, its error case and its default.
func linkOps(link ir.OperationLink) []ir.Operation {
	get := ir.Operation{ID: "operation/x/get", Params: []ir.Parameter{{ID: "parameter/x/get/id", Name: ir.Naming{Source: "id"}}}}
	post := ir.Operation{
		ID:        "operation/x/post",
		Params:    []ir.Parameter{{ID: "parameter/x/post/id", Name: ir.Naming{Source: "id"}}},
		Responses: []ir.Response{{Links: []ir.OperationLink{link}}},
		Errors:    []ir.ErrorCase{{Links: []ir.OperationLink{link}}},
		Default:   &ir.Response{Links: []ir.OperationLink{link}},
	}
	return []ir.Operation{get, post}
}

// mismatchesAt returns where each pass/param-binding-mismatch of diags sits.
func mismatchesAt(diags []ir.Diagnostic) []string {
	var at []string
	for _, diagnostic := range diags {
		if diagnostic.Code == "pass/param-binding-mismatch" {
			at = append(at, diagnostic.Provenance.Node)
		}
	}
	return at
}

// requestValue reads the request's parameter id.
func requestValue(id ir.ParameterID) ir.LinkValue {
	return ir.LinkValue{Kind: ir.LinkRequest, Request: &ir.InputPath{Param: &id}}
}

// TestValidate_LinkParams_HeldToTheTarget pins that a link gives values to the
// parameters of the operation it leads to, each once, at every response a link
// can sit on.
func TestValidate_LinkParams_HeldToTheTarget(t *testing.T) {
	t.Parallel()
	constant := ir.LinkValue{Kind: ir.LinkConstant, Constant: &ir.Value{Kind: ir.ValueString, String: "a"}}
	for _, tc := range []struct {
		name   string
		params []ir.LinkParam
		want   []string
	}{
		{"the target's own parameter", []ir.LinkParam{{Param: "parameter/x/get/id", Value: constant}}, nil},
		{"the carrying operation's parameter", []ir.LinkParam{{Param: "parameter/x/post/id", Value: constant}}, []string{
			"operation/x/post/responses/0/links/0/params/0", "operation/x/post/errors/0/links/0/params/0",
			"operation/x/post/default/links/0/params/0",
		}},
		{"one parameter twice", []ir.LinkParam{
			{Param: "parameter/x/get/id", Value: constant}, {Param: "parameter/x/get/id", Value: constant},
		}, []string{
			"operation/x/post/responses/0/links/0/params/1", "operation/x/post/errors/0/links/0/params/1",
			"operation/x/post/default/links/0/params/1",
		}},
		{"a parameter nothing declares, the dangling walk's", []ir.LinkParam{{Param: "parameter/x/ghost", Value: constant}}, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			link := ir.OperationLink{Name: ir.Naming{Source: "next"}, Operation: "operation/x/get", Params: tc.params}
			assert.Equal(t, tc.want, mismatchesAt(validated(t, docWithOperations(linkOps(link)...))))
		})
	}
}

// TestValidate_LinkParams_AnUndeclaredTargetIsTheDanglingWalks pins that a link
// to an operation the document does not declare draws no parameter finding:
// there is no parameter list to hold its parameters to.
func TestValidate_LinkParams_AnUndeclaredTargetIsTheDanglingWalks(t *testing.T) {
	t.Parallel()
	link := ir.OperationLink{
		Name: ir.Naming{Source: "next"}, Operation: "operation/x/ghost",
		Params: []ir.LinkParam{{Param: "parameter/x/get/id", Value: ir.LinkValue{Kind: ir.LinkURL}}},
	}
	diags := validated(t, docWithOperations(linkOps(link)...))
	assert.Empty(t, mismatchesAt(diags))
	assert.Equal(t, 3, countCode(t, diags, "ir/dangling-operation-ref"), "one for each response carrying the link")
}

// TestValidate_LinkRequestValues_HeldToTheCarryingOperation pins that what a
// link reads of the request names a parameter of the operation whose response
// carries it, in a parameter's value and in the body's.
func TestValidate_LinkRequestValues_HeldToTheCarryingOperation(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		from ir.ParameterID
		want []string
	}{
		{"its own parameter", "parameter/x/post/id", nil},
		{"the target's parameter", "parameter/x/get/id", []string{
			"operation/x/post/responses/0/links/0/params/0", "operation/x/post/responses/0/links/0/body",
			"operation/x/post/errors/0/links/0/params/0", "operation/x/post/errors/0/links/0/body",
			"operation/x/post/default/links/0/params/0", "operation/x/post/default/links/0/body",
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			body := requestValue(tc.from)
			link := ir.OperationLink{
				Name: ir.Naming{Source: "next"}, Operation: "operation/x/get", Body: &body,
				Params: []ir.LinkParam{{Param: "parameter/x/get/id", Value: requestValue(tc.from)}},
			}
			assert.Equal(t, tc.want, mismatchesAt(validated(t, docWithOperations(linkOps(link)...))))
		})
	}
}
