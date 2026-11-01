// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package operation

import (
	"encoding/json/jsontext"

	"github.com/dexpace/morphic/compilers/compile"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/ids"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/ir"
)

// streamCandidate is one lowered content that carries a stream, and how the
// compiler came to say so. declared distinguishes a 3.2 itemSchema — which
// states outright that the body is a sequence — from a media type the policy
// recognizes, which is a guess about what the media type implies.
type streamCandidate struct {
	events   ir.TypeRef
	declared bool
}

// streamDirection is one direction's classification: the detail to write, and
// the two facts about how it was reached that the caller has to act on.
type streamDirection struct {
	detail *ir.StreamDetail
	// inferred reports that at least one contributing content was recognized by
	// media type rather than by a declaration, which is what the operation's
	// Provenance.Inferred marker records.
	inferred bool
	// ambiguous reports that the direction's streaming contents named different
	// element types, so none was elected.
	ambiguous bool
}

// applyStreaming writes the operation's streaming summary and per-direction
// details, and returns the heuristic marker its provenance should carry ("" when
// the streams it found were all declared).
//
// It runs over the lowered operation rather than the source, so it reads one
// answer per direction no matter how many places a payload was assembled from,
// and it is the only writer of the three streaming fields.
func applyStreaming(c lowering.Ctx, ts *compile.Types, op *ir.Operation, declPtr jsontext.Pointer) (ir.Inferred, []ir.Diagnostic) {
	request := classifyStream(streamCandidates(c, ts, op.Request))
	response := classifyStream(responseCandidates(c, ts, op.Responses))
	if request.detail == nil && response.detail == nil {
		return "", nil
	}
	op.RequestStream, op.ResponseStream = request.detail, response.detail
	op.Streaming = streamingMode(request.detail != nil, response.detail != nil)

	var diags []ir.Diagnostic
	if request.ambiguous {
		diags = append(diags, unelectedElementDiag(c, declPtr+ids.Ptr("requestBody"), "request"))
	}
	if response.ambiguous {
		diags = append(diags, unelectedElementDiag(c, declPtr+ids.Ptr("responses"), "response"))
	}
	if request.inferred || response.inferred {
		return lowering.StreamingMediaTypeHeuristic, diags
	}
	return "", diags
}

// classifyStream folds one direction's candidates into the detail to write.
//
// It refuses to elect an element type from candidates that disagree:
// StreamDetail holds one Events per direction while Payload keeps every media
// type, so naming one of two differing contents would be the primary-content
// selection invariant 2 forbids. The direction still streams and the element is
// left unnamed for a lowering that has the whole set to choose from.
//
// Candidates naming the same element do not disagree, so they are compared, not
// counted: one frame offered as both text/event-stream and application/x-ndjson
// is content negotiation over one element type.
func classifyStream(candidates []streamCandidate) streamDirection {
	if len(candidates) == 0 {
		return streamDirection{}
	}
	out := streamDirection{detail: &ir.StreamDetail{}}
	for _, candidate := range candidates {
		if !candidate.declared {
			out.inferred = true
		}
	}
	// ir.TypeRef is a TypeID and a nullability bit, so == is the whole of what
	// "the same element" means: two contents differing in either name different
	// elements.
	events := candidates[0].events
	for _, candidate := range candidates[1:] {
		if candidate.events != events {
			out.ambiguous = true
			return out
		}
	}
	out.detail.Events = &events
	return out
}

// responseCandidates collects the streaming contents of every success response.
// Error responses are deliberately not read: an operation's ResponseStream
// describes what it streams back, and a 4xx body is what it sends instead of
// streaming. Nor is the default, whose class only the plan's policy decides
// (docs/ir/responses.md): a stream it alone declares sets no ResponseStream,
// and a plan taking it for the success response reads its Contents itself.
func responseCandidates(c lowering.Ctx, ts *compile.Types, responses []ir.Response) []streamCandidate {
	out := make([]streamCandidate, 0, len(responses))
	for i := range responses {
		out = append(out, streamCandidates(c, ts, responses[i].Payload)...)
	}
	return out
}

// streamCandidates collects the contents of one payload that carry a stream. A
// declared itemSchema wins over the media-type reading at the same content, so
// the two can never both classify it.
func streamCandidates(c lowering.Ctx, ts *compile.Types, payload *ir.Payload) []streamCandidate {
	if payload == nil {
		return nil
	}
	var out []streamCandidate
	for _, content := range payload.Contents {
		switch {
		case content.Item != nil:
			out = append(out, streamCandidate{events: *content.Item, declared: true})
		case c.MediaTypeStreams(content.MediaType):
			out = append(out, streamCandidate{events: frameType(ts, content)})
		}
	}
	return out
}

// frameType is what one frame of a streaming content holds. For a frame format
// the schema under the media type is read as one frame, not the whole body —
// the opposite of how an ordinary content is read — so it is the element type
// as well as the content type. A file body's schema describes the file, which
// has no frames (docs/emitters/plan/streams.md): what travels is bytes, whatever
// its docs or bounds say of the whole.
func frameType(ts *compile.Types, content ir.Content) ir.TypeRef {
	if content.File != nil {
		return ts.PrimRef(ir.PrimBytes)
	}
	return content.Type
}

// streamingMode is the summary the two directions derive. It is only ever asked
// when at least one of them streams.
func streamingMode(request, response bool) ir.StreamingMode {
	switch {
	case request && response:
		return ir.StreamingBidi
	case request:
		return ir.StreamingClient
	default:
		return ir.StreamingServer
	}
}

// unelectedElementDiag reports a direction whose element type was left unnamed
// because its streaming contents name different ones. Nothing is lost — every
// content is still on the payload — but the IR now says less than the source
// did, which is what makes it a degradation rather than a silent choice.
//
// It names the disagreement rather than counting media types: contents that
// agree keep their element, and one media type appearing on two responses is
// not several media types.
func unelectedElementDiag(c lowering.Ctx, pointer jsontext.Pointer, direction string) ir.Diagnostic {
	return c.DiagAt(ir.SeverityInfo, diag.DegradedConstruct, pointer,
		"the %s streams more than one element type, so it is left unnamed rather than electing one", direction)
}
