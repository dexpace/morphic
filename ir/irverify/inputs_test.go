// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package irverify_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/ir/irverify"
)

// TestVerify_AnInputPathNamesARequestValue pins ir/input-path-empty at every
// carrier of an input path: a pagination input and a stream's condition each
// name a parameter, or a member of the body, and one naming neither is
// reported at the path itself.
func TestVerify_AnInputPathNamesARequestValue(t *testing.T) {
	t.Parallel()
	const op = "doc.Services[0].Groups[0].Operations[0]"
	param := ir.ParamID("param/x/S/op/cursor")
	for _, tc := range []struct {
		name string
		edit func(*ir.Operation)
		want []string
	}{
		{"a parameter", func(o *ir.Operation) {
			o.Pagination = &ir.Pagination{InputCursor: &ir.InputPath{Param: &param}}
		}, nil},
		{"a body member", func(o *ir.Operation) {
			o.Pagination = &ir.Pagination{InputCursor: &ir.InputPath{Segments: []ir.PropID{"p/x/Body/cursor"}}}
		}, nil},
		{"neither, as a cursor", func(o *ir.Operation) {
			o.Pagination = &ir.Pagination{InputLimit: &ir.InputPath{}}
		}, []string{op + ".Pagination.InputLimit"}},
		{"neither, as a stream's condition", func(o *ir.Operation) {
			o.ResponseStream = &ir.StreamDetail{Condition: &ir.StreamCondition{Value: ir.Value{Kind: ir.ValueBool, Bool: true}}}
		}, []string{op + ".ResponseStream.Condition.Input"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			o := ir.Operation{ID: "op/x/S/op"}
			tc.edit(&o)
			assert.Equal(t, tc.want, violationPaths(irverify.Verify(opDoc(o)), "ir/input-path-empty"))
		})
	}
}

// TestVerify_APaginationStopIsOneIRDeclares holds Pagination.Stop to the
// declared conditions, each undeclared one at its own index.
func TestVerify_APaginationStopIsOneIRDeclares(t *testing.T) {
	t.Parallel()
	o := ir.Operation{ID: "op/x/S/op", Pagination: &ir.Pagination{
		Stop: []ir.PageStop{ir.PageStopEmpty, "Empty", ir.PageStopNoNext, ir.PageStopShort, ir.PageStopTotal, "never"},
	}}
	assert.Equal(t, []string{
		"doc.Services[0].Groups[0].Operations[0].Pagination.Stop[1]",
		"doc.Services[0].Groups[0].Operations[0].Pagination.Stop[5]",
	}, violationPaths(irverify.Verify(opDoc(o)), "ir/unknown-page-stop"))
}
