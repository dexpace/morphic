// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package irverify_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/ir/irverify"
)

// streamingContent is a content that streams, the media type alone saying so.
func streamingContent() ir.Content {
	return ir.Content{MediaType: "text/event-stream", Stream: &ir.ContentStream{Inferred: "streaming-inference"}}
}

// TestVerify_AContentDeclaringAnItemStreams pins ir/content-item-without-stream
// at every carrier of a content: a request, a response, an error, the default,
// a service's common error and a message. Each content beside it holding its
// Item with a Stream is clean, as is one holding neither.
func TestVerify_AContentDeclaringAnItemStreams(t *testing.T) {
	t.Parallel()
	const operationPath = "document.Services[0].Groups[0].Operations[0]"
	item := func() ir.TypeRef { return ir.TypeRef{Target: "type/x/Item"} }
	payload := func() *ir.Payload {
		declared, unmarked := item(), item()
		return &ir.Payload{Contents: []ir.Content{
			{MediaType: "application/json"},
			{MediaType: "application/jsonl", Item: &declared, Stream: &ir.ContentStream{}},
			{MediaType: "application/x-ndjson", Item: &unmarked},
		}}
	}
	doc := singleOperationDocument(ir.Operation{
		ID:             "operation/x/S/op",
		Request:        payload(),
		RequestStream:  &ir.StreamDetail{},
		Responses:      []ir.Response{{Payload: payload()}},
		ResponseStream: &ir.StreamDetail{},
		Errors:         []ir.ErrorCase{{Payload: payload()}},
		Default:        &ir.Response{Payload: payload()},
	})
	doc.Services[0].CommonErrors = []ir.ErrorCase{{Payload: payload()}}
	doc.Messages = map[ir.MessageID]ir.Message{"message/x/m": {ID: "message/x/m", Payload: *payload()}}

	assert.ElementsMatch(t, []string{
		operationPath + ".Request.Contents[2].Stream",
		operationPath + ".Responses[0].Payload.Contents[2].Stream",
		operationPath + ".Errors[0].Payload.Contents[2].Stream",
		operationPath + ".Default.Payload.Contents[2].Stream",
		"document.Services[0].CommonErrors[0].Payload.Contents[2].Stream",
		"document.Messages[message/x/m].Payload.Contents[2].Stream",
	}, violationPaths(irverify.Verify(doc), "ir/content-item-without-stream"))
}

// TestVerify_AStreamingContentHasItsSidesDetail pins
// ir/content-stream-without-detail on each side apart: a request's or a
// success response's content that streams needs that side's StreamDetail, the
// other side's standing for nothing. An error's or the default's content
// streams with none, and a side may stream with no content that does.
func TestVerify_AStreamingContentHasItsSidesDetail(t *testing.T) {
	t.Parallel()
	const operationPath = "document.Services[0].Groups[0].Operations[0]"
	streams := func() *ir.Payload {
		return &ir.Payload{Contents: []ir.Content{{MediaType: "application/json"}, streamingContent()}}
	}
	for _, tc := range []struct {
		name string
		edit func(*ir.Operation)
		want []string
	}{
		{"a request stream summarizing its content", func(operation *ir.Operation) {
			operation.Request, operation.RequestStream = streams(), &ir.StreamDetail{}
		}, nil},
		{"a request content with no request stream", func(operation *ir.Operation) {
			operation.Request, operation.ResponseStream = streams(), &ir.StreamDetail{}
		}, []string{operationPath + ".Request.Contents[1].Stream"}},
		{"a response stream summarizing its content", func(operation *ir.Operation) {
			operation.Responses = []ir.Response{{}, {Payload: streams()}}
			operation.ResponseStream = &ir.StreamDetail{}
		}, nil},
		{"a response content with no response stream", func(operation *ir.Operation) {
			operation.Responses = []ir.Response{{}, {Payload: streams()}}
			operation.RequestStream = &ir.StreamDetail{}
		}, []string{operationPath + ".Responses[1].Payload.Contents[1].Stream"}},
		{"an error's and the default's contents, which no stream summarizes", func(operation *ir.Operation) {
			operation.Errors = []ir.ErrorCase{{Payload: streams()}}
			operation.Default = &ir.Response{Payload: streams()}
		}, nil},
		{"a side streaming with no content that does", func(operation *ir.Operation) {
			operation.Request = &ir.Payload{Contents: []ir.Content{{MediaType: "application/json"}}}
			operation.RequestStream, operation.ResponseStream = &ir.StreamDetail{}, &ir.StreamDetail{}
		}, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			operation := ir.Operation{ID: "operation/x/S/op"}
			tc.edit(&operation)
			assert.Equal(t, tc.want, violationPaths(irverify.Verify(singleOperationDocument(operation)), "ir/content-stream-without-detail"))
		})
	}
}
