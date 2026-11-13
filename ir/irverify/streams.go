// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package irverify

import (
	"reflect"
	"strconv"

	"github.com/dexpace/morphic/ir"
)

var contentType = reflect.TypeFor[ir.Content]()

// contentPath holds the types between a Document and each ir.Content, past
// operationPath: the responses, errors and messages holding payloads
// (TestOperationPath_ListsEveryCarrier).
var contentPath = map[reflect.Type]bool{
	reflect.TypeFor[[]ir.Response]():               true,
	reflect.TypeFor[ir.Response]():                 true,
	reflect.TypeFor[*ir.Response]():                true,
	reflect.TypeFor[[]ir.ErrorCase]():              true,
	reflect.TypeFor[ir.ErrorCase]():                true,
	reflect.TypeFor[map[ir.MessageID]ir.Message](): true,
	reflect.TypeFor[ir.Message]():                  true,
	reflect.TypeFor[*ir.Payload]():                 true,
	reflect.TypeFor[ir.Payload]():                  true,
	reflect.TypeFor[[]ir.Content]():                true,
}

// checkContentStreams asserts that the contents that stream and an operation's
// summary of them agree (docs/ir/payloads.md#a-content-that-streams): a content
// declaring an Item streams (ir/content-item-without-stream), and one that
// streams in a request or a success response has its side's StreamDetail
// (ir/content-stream-without-detail). The converse is not held, as a side may
// stream with no content, its input folded into its parameters, and nor is an
// error's or the default's content, which no StreamDetail summarizes.
//
// It prunes all but operationPath and contentPath, for checkStreamingModes's
// reason.
func checkContentStreams(*ir.Document) walkCheck {
	var vs []Violation
	return walkCheck{
		visit: func(v reflect.Value, at ir.WalkPath) bool {
			switch v.Type() {
			case operationType:
				if op, ok := reflect.TypeAssert[ir.Operation](v); ok {
					vs = appendUnsummarizedStreams(vs, &op, at)
				}
				return true
			case contentType:
				if c, ok := reflect.TypeAssert[ir.Content](v); ok && c.Item != nil && c.Stream == nil {
					vs = append(vs, Violation{
						Code:    "ir/content-item-without-stream",
						Message: "content declares a stream item and is not marked as streaming",
						Path:    at.String() + ".Stream",
					})
				}
				return false
			default:
				return operationPath[v.Type()] || contentPath[v.Type()]
			}
		},
		report: reporting(&vs),
	}
}

// appendUnsummarizedStreams reports each content of op, the operation at at,
// that streams in its request or a success response while that side carries
// no StreamDetail.
func appendUnsummarizedStreams(vs []Violation, op *ir.Operation, at ir.WalkPath) []Violation {
	if op.RequestStream == nil && op.Request != nil {
		vs = appendUnsummarized(vs, op.Request.Contents, at, ".Request", "request")
	}
	if op.ResponseStream != nil {
		return vs
	}
	for i := range op.Responses {
		if payload := op.Responses[i].Payload; payload != nil {
			vs = appendUnsummarized(vs, payload.Contents, at, ".Responses["+strconv.Itoa(i)+"].Payload", "response")
		}
	}
	return vs
}

// appendUnsummarized reports each of contents, the payload at owner's path and
// then payload, that streams, on a side of the operation at owner that has no
// StreamDetail.
func appendUnsummarized(vs []Violation, contents []ir.Content, owner ir.WalkPath, payload, side string) []Violation {
	for i := range contents {
		if contents[i].Stream == nil {
			continue
		}
		vs = append(vs, Violation{
			Code:    "ir/content-stream-without-detail",
			Message: "a " + side + " content streams, and the operation carries no " + side + " stream",
			Path:    owner.String() + payload + ".Contents[" + strconv.Itoa(i) + "].Stream",
		})
	}
	return vs
}
