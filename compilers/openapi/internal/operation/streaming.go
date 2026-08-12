package operation

import (
	"strings"

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
func applyStreaming(c lowering.Ctx, op *ir.Operation, declPtr string) (string, []ir.Diagnostic) {
	request := classifyStream(streamCandidates(c, op.Request))
	response := classifyStream(responseCandidates(c, op.Responses))
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
// The one thing it refuses to do is elect an element type from candidates that
// disagree. StreamDetail holds one Events per direction while Payload keeps
// every media type, so naming one of two differing contents would be exactly
// the primary-content selection a compiler must not make (invariant 2). The
// direction still streams — that much all the candidates agree on — and the
// element is left unnamed for a lowering that has the whole set to choose from.
//
// Candidates naming the same element are not that case, so they are compared
// rather than counted. A response offering one frame as both text/event-stream
// and application/x-ndjson is content negotiation over a single element type,
// and there is nothing to elect between: refusing on the count alone left the
// commonest streaming shape there is with its element unnamed, which says less
// than the source did rather than declining to choose.
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
// streaming.
func responseCandidates(c lowering.Ctx, responses []ir.Response) []streamCandidate {
	out := make([]streamCandidate, 0, len(responses))
	for i := range responses {
		out = append(out, streamCandidates(c, responses[i].Payload)...)
	}
	return out
}

// streamCandidates collects the contents of one payload that carry a stream. A
// declared itemSchema wins over the media-type reading at the same content, so
// the two can never both classify it.
func streamCandidates(c lowering.Ctx, payload *ir.Payload) []streamCandidate {
	if payload == nil {
		return nil
	}
	var out []streamCandidate
	for _, content := range payload.Contents {
		switch {
		case content.Item != nil:
			out = append(out, streamCandidate{events: *content.Item, declared: true})
		case c.MediaTypeStreams(content.MediaType):
			// For a frame format the schema under the media type describes one
			// frame, not the whole body — the opposite of how an ordinary content
			// is read — so it is the element type as well as the content type.
			out = append(out, streamCandidate{events: content.Type})
		}
	}
	return out
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
func unelectedElementDiag(c lowering.Ctx, pointer, direction string) ir.Diagnostic {
	return c.DiagAt(ir.SeverityInfo, diag.DegradedConstruct, pointer,
		"the %s streams more than one element type, so it is left unnamed rather than electing one", direction)
}

// joinInferred names every heuristic that shaped one node, in the order the
// lowering applied them.
//
// Provenance.Inferred holds one string and more than one heuristic can reach a
// single operation — grouping by path prefix and reading a stream out of a media
// type are independent choices. Keeping only the first would make the second
// invisible, which defeats the marker's whole purpose, so they are listed.
func joinInferred(markers ...string) string {
	kept := make([]string, 0, len(markers))
	for _, marker := range markers {
		if marker != "" {
			kept = append(kept, marker)
		}
	}
	return strings.Join(kept, ",")
}
