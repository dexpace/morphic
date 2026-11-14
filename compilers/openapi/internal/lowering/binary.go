// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package lowering

import (
	"strings"

	"github.com/dexpace/morphic/ir"
)

// BinaryMediaTypeHeuristic is the name FileInfo.Inferred and
// PartEncoding.Inferred carry on a body or part read as a file because its
// media type is binary, rather than because its schema or
// application/octet-stream says so. It is a constant for the reason
// StreamingMediaTypeHeuristic is.
const BinaryMediaTypeHeuristic ir.Inferred = "binary-media-type"

// BinaryMedia is the binary media-type policy: which media types mean "this body
// is a file" where its schema says no more than that it is a string.
//
// It is a policy rather than a table in the lowering because the reading is a
// guess (architecture principle 6). OpenAPI says any binary media type marks
// binary content, but no registry says which types are binary, so a list is a
// judgment a caller may not share. application/octet-stream is not on it: RFC
// 2046 defines that type as arbitrary octets, so the lowering reads it as
// declared, whatever this says.
type BinaryMedia struct {
	// MediaTypes replaces the default list rather than extending it. Empty
	// means the default.
	//
	// Entries are matched as StreamingMedia's are, case-insensitively and
	// without parameters, an entry starting with `+` naming a structured
	// suffix. An entry `type/*` matches each media type of that type whose
	// subtype has no structured suffix, since a suffix names the syntax the
	// body is written in: `image/*` matches `image/png`, not `image/svg+xml`.
	MediaTypes []string `json:"mediaTypes,omitempty"`
	// Disabled turns the inference off: a body or part is then a file only
	// where its schema or application/octet-stream declares one.
	Disabled bool `json:"disabled,omitzero"`
}

// DefaultBinaryMediaTypes is the list the policy uses when the caller states
// none. It is a default and not a standard: it names registered binary types,
// and top-level types whose members are binary but for rare text ones such as
// an ASCII image/vnd.dxf. A caller who reads them otherwise names its own list.
func DefaultBinaryMediaTypes() []string {
	return []string{
		"application/gzip", "application/pdf", "application/vnd.tcpdump.pcap", "application/wasm",
		"application/zip", "audio/*", "font/*", "image/*", "video/*",
	}
}

// MediaTypeBinary reports whether the policy classifies mediaType as binary. It
// is a predicate rather than a getter for the reason MediaTypeStreams is.
func (c Ctx) MediaTypeBinary(mediaType string) bool {
	normalized := NormalizeMediaType(mediaType)
	if c.binary[normalized] {
		return true
	}
	// A structured suffix closes the subtype (RFC 6838 §4.2.8), so the last
	// "+" starts the only one a media type has.
	if suffix := strings.LastIndexByte(normalized, '+'); suffix >= 0 {
		return c.binary[normalized[suffix:]]
	}
	top, _, ok := strings.Cut(normalized, "/")
	return ok && c.binary[top+"/*"]
}

// binarySet normalizes a policy into the set MediaTypeBinary answers from, or
// nil when the inference is off, as streamingSet does.
func binarySet(p BinaryMedia) map[string]bool {
	if p.Disabled {
		return nil
	}
	types := p.MediaTypes
	if len(types) == 0 {
		types = DefaultBinaryMediaTypes()
	}
	set := make(map[string]bool, len(types))
	for _, mt := range types {
		if normalized := NormalizeMediaType(mt); normalized != "" {
			set[normalized] = true
		}
	}
	return set
}
