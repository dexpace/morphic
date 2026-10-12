package lowering

import "strings"

// NormalizeMediaType reduces a media type to the form a classification
// compares: lowercased, parameters dropped, surrounding space trimmed. RFC 6838
// §4.2 makes the type and subtype case-insensitive, and a parameter states how
// a body is encoded rather than naming another type.
//
// Every classification reads this one rule, so a policy and a lowering cannot
// drift apart. The IR never records the result: a caller normalizes a copy, and
// the IR keeps the spelling the document wrote.
func NormalizeMediaType(mediaType string) string {
	if i := strings.IndexByte(mediaType, ';'); i >= 0 {
		mediaType = mediaType[:i]
	}
	return strings.ToLower(strings.TrimSpace(mediaType))
}
