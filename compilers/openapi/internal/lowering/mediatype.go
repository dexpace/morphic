package lowering

import "strings"

// NormalizeMediaType reduces a media type to the form every classification of
// one compares: lowercased, with any parameters dropped and surrounding space
// trimmed. RFC 6838 §4.2 makes the type and subtype case-insensitive, and a
// parameter states how a body carrying that type is encoded rather than naming
// a different type, so every spelling this function collapses names one media
// type.
//
// It is the one rule for every media-type classification in the compiler, so
// that a policy and a lowering cannot drift apart on what "is this media type"
// means. It is never what the IR records: a caller classifying a declared
// spelling normalizes a copy and leaves the declaration itself untouched,
// because the spelling the document wrote is what its IR carries.
func NormalizeMediaType(mediaType string) string {
	if i := strings.IndexByte(mediaType, ';'); i >= 0 {
		mediaType = mediaType[:i]
	}
	return strings.ToLower(strings.TrimSpace(mediaType))
}
