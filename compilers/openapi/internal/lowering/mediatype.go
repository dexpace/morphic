// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

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
	mediaType, _, _ = strings.Cut(mediaType, ";")
	return strings.ToLower(strings.TrimSpace(mediaType))
}

// HoldsOneDocument reports whether mediaType names a syntax whose body is one
// document, JSON's or XML's, by name or structured suffix (RFC 6839, RFC 7303).
// It answers only for those two: which other media types repeat a structure is
// not a closed set, as vendor types and multipart/mixed show.
func HoldsOneDocument(mediaType string) bool {
	normalized := NormalizeMediaType(mediaType)
	switch normalized {
	case "application/json", "application/xml", "text/xml":
		return true
	default:
		return strings.HasSuffix(normalized, "+json") || strings.HasSuffix(normalized, "+xml")
	}
}
