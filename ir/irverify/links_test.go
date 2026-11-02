// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package irverify_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/ir/irverify"
)

// linkValueCodes are the codes checkLinkValues reports.
var linkValueCodes = []string{
	"ir/unknown-link-value-kind", "ir/link-value-missing-payload", "ir/link-value-stray-payload",
}

// linkValueViolations returns the paths checkLinkValues reports for a link
// that passes lv as its body, each prefixed by its code.
func linkValueViolations(lv ir.LinkValue) []string {
	op := ir.Operation{ID: "op/x/S/op", Responses: []ir.Response{{Links: []ir.OperationLink{{
		Name: named("next"), Operation: "op/x/S/op", Body: &lv,
	}}}}}
	var got []string
	for _, v := range irverify.Verify(opDoc(op)) {
		for _, code := range linkValueCodes {
			if v.Code == code {
				got = append(got, code+" "+v.Path)
			}
		}
	}
	return got
}

// TestVerify_ALinkValueSetsWhatItsKindNames holds every kind to the one field
// it names, a kind naming none to no field, and the request body whole to
// sound: an input path with neither a parameter nor a segment is the body.
func TestVerify_ALinkValueSetsWhatItsKindNames(t *testing.T) {
	t.Parallel()
	const at = "doc.Services[0].Groups[0].Operations[0].Responses[0].Links[0].Body"
	param := ir.ParamID("param/x/S/op/id")
	constant := &ir.Value{Kind: ir.ValueString, Str: "x"}
	for _, tc := range []struct {
		name string
		lv   ir.LinkValue
		want []string
	}{
		{"a constant", ir.LinkValue{Kind: ir.LinkConstant, Constant: constant}, nil},
		{"a request parameter", ir.LinkValue{Kind: ir.LinkRequest, Request: &ir.InputPath{Param: &param}}, nil},
		{"the request body whole", ir.LinkValue{Kind: ir.LinkRequest, Request: &ir.InputPath{}}, nil},
		{"the response body whole", ir.LinkValue{Kind: ir.LinkResponse, Response: &ir.PropPath{}}, nil},
		{"the url", ir.LinkValue{Kind: ir.LinkURL}, nil},
		{"the method", ir.LinkValue{Kind: ir.LinkMethod}, nil},
		{"the status", ir.LinkValue{Kind: ir.LinkStatus}, nil},
		{"an undeclared kind", ir.LinkValue{Kind: "header", Constant: constant},
			[]string{"ir/unknown-link-value-kind " + at + ".Kind"}},
		{"no kind", ir.LinkValue{}, []string{"ir/unknown-link-value-kind " + at + ".Kind"}},
		{"a constant with none", ir.LinkValue{Kind: ir.LinkConstant},
			[]string{"ir/link-value-missing-payload " + at + ".Constant"}},
		{"a request with none", ir.LinkValue{Kind: ir.LinkRequest},
			[]string{"ir/link-value-missing-payload " + at + ".Request"}},
		{"a response with none", ir.LinkValue{Kind: ir.LinkResponse},
			[]string{"ir/link-value-missing-payload " + at + ".Response"}},
		{"a request beside a constant", ir.LinkValue{Kind: ir.LinkRequest, Request: &ir.InputPath{}, Constant: constant},
			[]string{"ir/link-value-stray-payload " + at + ".Constant"}},
		{"the status with every payload", ir.LinkValue{
			Kind: ir.LinkStatus, Constant: constant, Request: &ir.InputPath{}, Response: &ir.PropPath{},
		}, []string{
			"ir/link-value-stray-payload " + at + ".Constant",
			"ir/link-value-stray-payload " + at + ".Request",
			"ir/link-value-stray-payload " + at + ".Response",
		}},
		{"a response with a root", ir.LinkValue{Kind: ir.LinkResponse, Response: &ir.PropPath{Root: &ir.TypeRef{Target: "t/x/M"}}},
			[]string{"ir/link-value-stray-payload " + at + ".Response.Root"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, linkValueViolations(tc.lv))
		})
	}
}
