// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package pass_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dexpace/morphic/ir"
)

// linkOps returns an operation "op/x/get" declaring "param/x/get/id", and an
// operation "op/x/post" declaring "param/x/post/id" whose responses each carry
// link: its success response, its error case and its default.
func linkOps(link ir.OperationLink) []ir.Operation {
	get := ir.Operation{ID: "op/x/get", Params: []ir.Parameter{{ID: "param/x/get/id", Name: ir.Naming{Source: "id"}}}}
	post := ir.Operation{
		ID:        "op/x/post",
		Params:    []ir.Parameter{{ID: "param/x/post/id", Name: ir.Naming{Source: "id"}}},
		Responses: []ir.Response{{Links: []ir.OperationLink{link}}},
		Errors:    []ir.ErrorCase{{Links: []ir.OperationLink{link}}},
		Default:   &ir.Response{Links: []ir.OperationLink{link}},
	}
	return []ir.Operation{get, post}
}

// mismatchesAt returns where each pass/param-binding-mismatch of diags sits.
func mismatchesAt(diags []ir.Diagnostic) []string {
	var at []string
	for _, d := range diags {
		if d.Code == "pass/param-binding-mismatch" {
			at = append(at, d.Provenance.Node)
		}
	}
	return at
}

// requestValue reads the request's parameter id.
func requestValue(id ir.ParamID) ir.LinkValue {
	return ir.LinkValue{Kind: ir.LinkRequest, Request: &ir.InputPath{Param: &id}}
}

// TestValidate_LinkParams_HeldToTheTarget pins that a link gives values to the
// parameters of the operation it leads to, each once, at every response a link
// can sit on.
func TestValidate_LinkParams_HeldToTheTarget(t *testing.T) {
	t.Parallel()
	constant := ir.LinkValue{Kind: ir.LinkConstant, Constant: &ir.Value{Kind: ir.ValueString, Str: "a"}}
	for _, tc := range []struct {
		name   string
		params []ir.LinkParam
		want   []string
	}{
		{"the target's own parameter", []ir.LinkParam{{Param: "param/x/get/id", Value: constant}}, nil},
		{"the carrying operation's parameter", []ir.LinkParam{{Param: "param/x/post/id", Value: constant}}, []string{
			"op/x/post/responses/0/links/0/params/0", "op/x/post/errors/0/links/0/params/0",
			"op/x/post/default/links/0/params/0",
		}},
		{"one parameter twice", []ir.LinkParam{
			{Param: "param/x/get/id", Value: constant}, {Param: "param/x/get/id", Value: constant},
		}, []string{
			"op/x/post/responses/0/links/0/params/1", "op/x/post/errors/0/links/0/params/1",
			"op/x/post/default/links/0/params/1",
		}},
		{"a parameter nothing declares, the dangling walk's", []ir.LinkParam{{Param: "param/x/ghost", Value: constant}}, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			link := ir.OperationLink{Name: ir.Naming{Source: "next"}, Operation: "op/x/get", Params: tc.params}
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
		Name: ir.Naming{Source: "next"}, Operation: "op/x/ghost",
		Params: []ir.LinkParam{{Param: "param/x/get/id", Value: ir.LinkValue{Kind: ir.LinkURL}}},
	}
	diags := validated(t, docWithOperations(linkOps(link)...))
	assert.Empty(t, mismatchesAt(diags))
	assert.Equal(t, 3, countCode(t, diags, "ir/dangling-op-ref"), "one for each response carrying the link")
}

// TestValidate_LinkRequestValues_HeldToTheCarryingOperation pins that what a
// link reads of the request names a parameter of the operation whose response
// carries it, in a parameter's value and in the body's.
func TestValidate_LinkRequestValues_HeldToTheCarryingOperation(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		from ir.ParamID
		want []string
	}{
		{"its own parameter", "param/x/post/id", nil},
		{"the target's parameter", "param/x/get/id", []string{
			"op/x/post/responses/0/links/0/params/0", "op/x/post/responses/0/links/0/body",
			"op/x/post/errors/0/links/0/params/0", "op/x/post/errors/0/links/0/body",
			"op/x/post/default/links/0/params/0", "op/x/post/default/links/0/body",
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			body := requestValue(tc.from)
			link := ir.OperationLink{
				Name: ir.Naming{Source: "next"}, Operation: "op/x/get", Body: &body,
				Params: []ir.LinkParam{{Param: "param/x/get/id", Value: requestValue(tc.from)}},
			}
			assert.Equal(t, tc.want, mismatchesAt(validated(t, docWithOperations(linkOps(link)...))))
		})
	}
}
