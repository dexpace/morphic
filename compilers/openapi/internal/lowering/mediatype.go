package lowering

import "strings"

// NormalizeMediaType reduces a media type to the form every classification of
// one compares: lowercased, with any parameters dropped and surrounding space
// trimmed. RFC 6838 §4.2 makes the type and subtype case-insensitive, and a
// parameter states how a body is encoded rather than naming another type, so
// every spelling it collapses names one media type.
//
// It is the one rule every classification reads, so a policy and a lowering
// cannot drift apart. It is never what the IR records: a caller normalizes a
// copy, and the IR keeps the spelling the document wrote.
func NormalizeMediaType(mediaType string) string {
	if i := strings.IndexByte(mediaType, ';'); i >= 0 {
		mediaType = mediaType[:i]
	}
	return strings.ToLower(strings.TrimSpace(mediaType))
}
