// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package irverify_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/ir/irverify"
)

// TestVerify_AnInputPathNamesARequestValue pins ir/input-body-absent at every
// carrier of an input path: a pagination input, a stream's condition and a
// link's value each name a parameter, or the body the operation sends or a
// member of it, and one reading the body of an operation sending none is
// reported at the path itself.
func TestVerify_AnInputPathNamesARequestValue(t *testing.T) {
	t.Parallel()
	const op = "doc.Services[0].Groups[0].Operations[0]"
	param := ir.ParamID("param/x/S/op/cursor")
	body := ir.InputPath{Segments: []ir.PropID{"p/x/Body/cursor"}}
	// Each value holds a path of its own: the walk enters a pointer once.
	link := func(v ir.InputPath) []ir.OperationLink {
		inParam, inBody := v, v
		return []ir.OperationLink{{
			Operation: "op/x/S/op",
			Params:    []ir.LinkParam{{Value: ir.LinkValue{Kind: ir.LinkRequest, Request: &inParam}}},
			Body:      &ir.LinkValue{Kind: ir.LinkRequest, Request: &inBody},
		}}
	}
	for _, tc := range []struct {
		name     string
		edit     func(*ir.Operation)
		bodyless []string
	}{
		{"a parameter", func(o *ir.Operation) {
			o.Pagination = &ir.Pagination{InputCursor: &ir.InputPath{Param: &param}}
			o.Default = &ir.Response{Links: link(ir.InputPath{Param: &param})}
		}, nil},
		{"the body sent and a member of it", func(o *ir.Operation) {
			o.Request = &ir.Payload{}
			o.Pagination = &ir.Pagination{InputCursor: &body, InputLimit: &ir.InputPath{}}
			o.ResponseStream = &ir.StreamDetail{Condition: &ir.StreamCondition{Input: body}}
			o.Responses = []ir.Response{{Links: link(ir.InputPath{})}}
			o.Errors = []ir.ErrorCase{{Links: link(body)}}
		}, nil},
		{"a member of no body, as a cursor", func(o *ir.Operation) {
			o.Pagination = &ir.Pagination{InputCursor: &body}
		}, []string{op + ".Pagination.InputCursor"}},
		{"a member of no body, as a stream's condition", func(o *ir.Operation) {
			o.ResponseStream = &ir.StreamDetail{Condition: &ir.StreamCondition{Input: body}}
		}, []string{op + ".ResponseStream.Condition.Input"}},
		{"no body whole, as a limit", func(o *ir.Operation) {
			o.Pagination = &ir.Pagination{InputLimit: &ir.InputPath{}}
		}, []string{op + ".Pagination.InputLimit"}},
		{"no body whole, as a stream's condition", func(o *ir.Operation) {
			o.ResponseStream = &ir.StreamDetail{Condition: &ir.StreamCondition{Value: ir.Value{Kind: ir.ValueBool, Bool: true}}}
		}, []string{op + ".ResponseStream.Condition.Input"}},
		{"no body, as a link's values", func(o *ir.Operation) {
			o.Responses = []ir.Response{{Links: link(body)}}
			o.Errors = []ir.ErrorCase{{Links: link(ir.InputPath{})}}
			o.Default = &ir.Response{Links: link(body)}
		}, []string{
			op + ".Default.Links[0].Body.Request",
			op + ".Default.Links[0].Params[0].Value.Request",
			op + ".Errors[0].Links[0].Body.Request",
			op + ".Errors[0].Links[0].Params[0].Value.Request",
			op + ".Responses[0].Links[0].Body.Request",
			op + ".Responses[0].Links[0].Params[0].Value.Request",
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			o := ir.Operation{ID: "op/x/S/op"}
			tc.edit(&o)
			assert.Equal(t, tc.bodyless, violationPaths(irverify.Verify(opDoc(o)), "ir/input-body-absent"))
		})
	}
}

// TestVerify_ABodyInputReadsItsOwnOperation pins that a body input is held to
// the body of the operation carrying it, in either order of two operations of
// which one sends a body and one does not.
func TestVerify_ABodyInputReadsItsOwnOperation(t *testing.T) {
	t.Parallel()
	sends := ir.Operation{ID: "op/x/S/sends", Request: &ir.Payload{}}
	sendsNone := ir.Operation{ID: "op/x/S/none"}
	for _, o := range []*ir.Operation{&sends, &sendsNone} {
		o.Pagination = &ir.Pagination{InputCursor: &ir.InputPath{Segments: []ir.PropID{"p/x/Body/cursor"}}}
	}
	for name, tc := range map[string]struct {
		first, second ir.Operation
		want          string
	}{
		"the sender first": {sends, sendsNone, "doc.Services[0].Groups[0].Operations[1].Pagination.InputCursor"},
		"the sender last":  {sendsNone, sends, "doc.Services[0].Groups[0].Operations[0].Pagination.InputCursor"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			doc := opDoc(tc.first)
			ops := &doc.Services[0].Groups[0].Operations
			*ops = append(*ops, tc.second)
			assert.Equal(t, []string{tc.want}, violationPaths(irverify.Verify(doc), "ir/input-body-absent"))
		})
	}
}

// TestVerify_AStreamTerminatorHoldsData pins ir/stream-terminator-empty on
// either side: an absent terminator and one holding data are clean, and an
// empty one, which would match a frame holding no data, is reported.
func TestVerify_AStreamTerminatorHoldsData(t *testing.T) {
	t.Parallel()
	const op = "doc.Services[0].Groups[0].Operations[0]"
	o := ir.Operation{
		ID:             "op/x/S/op",
		RequestStream:  &ir.StreamDetail{Terminator: new("")},
		ResponseStream: &ir.StreamDetail{Terminator: new("[DONE]")},
	}
	assert.Equal(t, []string{op + ".RequestStream.Terminator"},
		violationPaths(irverify.Verify(opDoc(o)), "ir/stream-terminator-empty"))

	o.RequestStream, o.ResponseStream = &ir.StreamDetail{}, &ir.StreamDetail{Terminator: new("")}
	assert.Equal(t, []string{op + ".ResponseStream.Terminator"},
		violationPaths(irverify.Verify(opDoc(o)), "ir/stream-terminator-empty"))
}

// TestVerify_APaginationStopIsOneIRDeclares holds Pagination.Stop to the
// declared conditions, each undeclared one at its own index, a lone one too.
func TestVerify_APaginationStopIsOneIRDeclares(t *testing.T) {
	t.Parallel()
	doc := opDoc(ir.Operation{ID: "op/x/S/a", Pagination: &ir.Pagination{
		Stop: []ir.PageStop{ir.PageStopEmpty, "Empty", ir.PageStopNoNext, ir.PageStopShort, ir.PageStopTotal, "never"},
	}})
	ops := &doc.Services[0].Groups[0].Operations
	*ops = append(*ops, ir.Operation{ID: "op/x/S/b", Pagination: &ir.Pagination{Stop: []ir.PageStop{"never"}}})

	vs := irverify.Verify(doc)
	assert.Equal(t, []string{
		"doc.Services[0].Groups[0].Operations[0].Pagination.Stop[1]",
		"doc.Services[0].Groups[0].Operations[0].Pagination.Stop[5]",
		"doc.Services[0].Groups[0].Operations[1].Pagination.Stop[0]",
	}, violationPaths(vs, "ir/unknown-page-stop"))
	assert.Empty(t, violationPaths(vs, "ir/set-entry-duplicate"), "no declared stop repeats")
}

// TestVerify_APaginationStopIsListedOnce holds Pagination.Stop to a set, as
// ir/set-entry-duplicate holds the IR's other sets: a repeat is reported at
// its later index, however far apart, and one pagination's stops are no
// repeat of another's.
func TestVerify_APaginationStopIsListedOnce(t *testing.T) {
	t.Parallel()
	stops := []ir.PageStop{ir.PageStopNoNext, ir.PageStopEmpty, "never", ir.PageStopNoNext, ir.PageStopEmpty}
	doc := opDoc(ir.Operation{ID: "op/x/S/a", Pagination: &ir.Pagination{Stop: stops}})
	ops := &doc.Services[0].Groups[0].Operations
	*ops = append(*ops, ir.Operation{ID: "op/x/S/b", Pagination: &ir.Pagination{Stop: []ir.PageStop{ir.PageStopEmpty}}})

	vs := irverify.Verify(doc)
	assert.Equal(t, []string{
		"doc.Services[0].Groups[0].Operations[0].Pagination.Stop[3]",
		"doc.Services[0].Groups[0].Operations[0].Pagination.Stop[4]",
	}, violationPaths(vs, "ir/set-entry-duplicate"))
	var messages []string
	for _, v := range vs {
		if v.Code == "ir/set-entry-duplicate" {
			messages = append(messages, v.Message)
		}
	}
	assert.Equal(t, []string{
		`set entry "no_next" is listed here and at index 0`,
		`set entry "empty" is listed here and at index 1`,
	}, messages, "each repeat names the entry it repeats")
	assert.Equal(t, []string{"doc.Services[0].Groups[0].Operations[0].Pagination.Stop[2]"},
		violationPaths(vs, "ir/unknown-page-stop"), "an undeclared stop is no set entry")
}
