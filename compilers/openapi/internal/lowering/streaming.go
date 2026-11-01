// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package lowering

import (
	"strings"

	"github.com/dexpace/morphic/ir"
)

// StreamingMediaTypeHeuristic is the name Provenance.Inferred carries on an
// operation whose streaming was read out of a media type rather than declared.
// It is a constant because the marker is what an auditor greps for, and a
// spelling written at the producing site and again at a reading test can drift.
const StreamingMediaTypeHeuristic ir.Inferred = "streaming-media-type"

// StreamingMedia is the media-type streaming policy: which media types mean
// "this body is a sequence of frames" in a document that declares nothing
// saying so.
//
// It is a policy rather than a table in the lowering because the reading is a
// guess (architecture principle 6). OpenAPI below 3.2 has no keyword for a
// sequential body, so an SSE or NDJSON API says what it does only by naming a
// media type, which is a content encoding, not a promise about framing. The
// vocabulary is declared here, below both walks, and re-exported by the
// compiler's public options, as GroupingStrategy is.
type StreamingMedia struct {
	// Disabled turns the inference off. Off means off: an operation then carries
	// the streaming fields a 3.2 itemSchema declares and nothing else, which is
	// what a caller who does not want guesses in their IR asked for.
	Disabled bool `json:"disabled,omitzero"`
	// MediaTypes replaces the default list rather than extending it, so a caller
	// who states a list gets exactly that list. Empty means the default.
	//
	// Entries are matched against the media type alone: the comparison is
	// case-insensitive and ignores parameters, because `text/event-stream` and
	// `text/event-stream; charset=utf-8` name one type. An entry starting with
	// `+` names a structured suffix, and matches every media type ending in it.
	MediaTypes []string `json:"mediaTypes,omitempty"`
}

// DefaultStreamingMediaTypes is the list the policy uses when the caller states
// none. It is a default and not a standard: `text/event-stream`,
// `application/json-seq` (RFC 7464) and the `+json-seq` suffix (RFC 8091) are
// registered, and the two JSON-lines spellings are conventions that happen to be
// what generators in this space already look for. A document using another
// spelling is not wrong — it names its own list.
func DefaultStreamingMediaTypes() []string {
	return []string{"+json-seq", "application/json-seq", "application/jsonl", "application/x-ndjson", "text/event-stream"}
}

// MediaTypeStreams reports whether the policy classifies mediaType as a stream
// of frames. It is a predicate rather than a getter for the reason
// DeclaresSchema is: handing back the set would make it writable through a copy
// of the context.
func (c Ctx) MediaTypeStreams(mediaType string) bool {
	normalized := NormalizeMediaType(mediaType)
	if c.streaming[normalized] {
		return true
	}
	// A structured suffix closes the subtype (RFC 6838 §4.2.8), so the last "+"
	// starts the only one a media type has.
	suffix := strings.LastIndexByte(normalized, '+')
	return suffix >= 0 && c.streaming[normalized[suffix:]]
}

// streamingSet normalizes a policy into the set MediaTypeStreams answers from,
// or nil when the inference is off — which reads the same as an empty set, so
// no lowering has to ask whether the policy was disabled or merely empty.
func streamingSet(p StreamingMedia) map[string]bool {
	if p.Disabled {
		return nil
	}
	types := p.MediaTypes
	if len(types) == 0 {
		types = DefaultStreamingMediaTypes()
	}
	set := make(map[string]bool, len(types))
	for _, mt := range types {
		if normalized := NormalizeMediaType(mt); normalized != "" {
			set[normalized] = true
		}
	}
	return set
}
