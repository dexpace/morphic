// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package operation

import (
	"encoding/json/jsontext"
	"slices"

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

// frame is what one frame of the candidate holds. An itemSchema states it. A
// media type's schema applies to the whole body, which OpenAPI maps to an array
// of its values (3.2 §4.14.3.1), so a list describes the body and its Elem one
// frame. Any other schema has no array to read, and is the frame itself.
func (s streamCandidate) frame(reg ir.TypeRegistry) ir.TypeRef {
	if s.declared {
		return s.events
	}
	if list, ok := listBehind(reg, s.events.Target); ok {
		return list.Elem
	}
	return s.events
}

// listBehind returns the List id stands for, read through the alias scalars a
// $ref with siblings hoists, and false where it stands for anything else.
func listBehind(reg ir.TypeRegistry, id ir.TypeID) (*ir.List, bool) {
	for range maxBodyAliasHops {
		base, alias := ir.AliasBase(reg, id)
		if !alias {
			break
		}
		id = base
	}
	list, ok := reg[id].(*ir.List)
	return list, ok
}

// applyStreaming writes the operation's streaming summary and per-direction
// details, and returns the heuristic marker its provenance should carry ("" when
// the streams it found were all declared).
//
// It runs over the lowered operation rather than the source, so it reads one
// answer per direction no matter how many places a payload was assembled from,
// and it is the only writer of the three streaming fields. What each direction
// streams is settled later (streamElement); that it streams needs no other node.
func applyStreaming(c lowering.Ctx, ts *compile.Types, op *ir.Operation, declPtr jsontext.Pointer) ir.Inferred {
	request := streamCandidates(c, ts, op.Request)
	response := responseCandidates(c, ts, op.Responses)
	if len(request) == 0 && len(response) == 0 {
		return ""
	}
	op.RequestStream = electStream(ts, request, unelectedElementDiag(c, declPtr+ids.Ptr("requestBody"), "request"))
	op.ResponseStream = electStream(ts, response, unelectedElementDiag(c, declPtr+ids.Ptr("responses"), "response"))
	op.Streaming = streamingMode(op.RequestStream != nil, op.ResponseStream != nil)
	guessed := func(candidate streamCandidate) bool { return !candidate.declared }
	if slices.ContainsFunc(request, guessed) || slices.ContainsFunc(response, guessed) {
		return lowering.StreamingMediaTypeHeuristic
	}
	return ""
}

// electStream returns the detail of a direction with candidates, nil for one
// with none, and queues the election of its element.
func electStream(ts *compile.Types, candidates []streamCandidate, unelected ir.Diagnostic) *ir.StreamDetail {
	if len(candidates) == 0 {
		return nil
	}
	detail := &ir.StreamDetail{}
	ts.Defer(streamElement{detail: detail, candidates: candidates, unelected: unelected})
	return detail
}

// streamElement elects one direction's Events once the registry is whole
// (compile.Settlement): a frame can be a node a $ref promised, which mid-walk may
// not exist yet, so reading it there would follow declaration order.
//
// Candidates that disagree elect nothing, and unelected reports it: StreamDetail
// holds one Events per direction while Payload keeps every media type, so naming
// one would be the primary-content selection invariant 2 forbids. They are compared,
// not counted: one frame under two media types is content negotiation. ir.TypeRef
// is a TypeID and a nullability bit, so == is all "the same element" means.
type streamElement struct {
	detail     *ir.StreamDetail
	candidates []streamCandidate
	unelected  ir.Diagnostic
}

// Settle writes the element to the detail and no node of reg.
func (e streamElement) Settle(reg ir.TypeRegistry) []ir.Diagnostic {
	events := e.candidates[0].frame(reg)
	for _, candidate := range e.candidates[1:] {
		if candidate.frame(reg) != events {
			return []ir.Diagnostic{e.unelected}
		}
	}
	e.detail.Events = &events
	return nil
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

// frameType is what a streaming content offers as its frame: its Type, which
// streamCandidate.frame reads for a list. A file body's schema describes the
// file, which has no frames (docs/emitters/plan/streams.md): what travels is
// bytes, whatever its docs or bounds say of the whole.
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
