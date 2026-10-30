// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package fragment

import (
	"encoding/json/jsontext"
	"net/url"
	"strings"

	"github.com/speakeasy-api/openapi/references"
)

// Pointer returns the JSON pointer ref's fragment spells, read as RFC 3986 and
// RFC 6901 read it: percent escapes decoded and nothing else, so '+' names
// itself. It is the resolver's reading of ForResolver(ref), so it parts from
// the resolver's reading of ref on '+' alone. It returns "" for a ref with no
// fragment.
func Pointer(ref string) jsontext.Pointer {
	return jsontext.Pointer(references.Reference(ForResolver(ref)).GetJSONPointer())
}

// ForResolver returns ref spelled so the resolver reads Pointer(ref) from it.
//
// The resolver query-unescapes a fragment (references.Reference.GetJSONPointer),
// reading '+' as a space, so each '+' in the part it decodes is written "%2B".
// A fragment it cannot decode it keeps raw, '+' included, so ref is returned
// unchanged, as it is when that part holds no '+'. The document part, and
// anything from a second '#' on, are left as written.
func ForResolver(ref string) string {
	uri, rest, found := strings.Cut(ref, "#")
	if !found {
		return ref
	}
	// The resolver reads the text between the first '#' and the next.
	part, tail, hasTail := strings.Cut(rest, "#")
	if !strings.Contains(part, "+") {
		return ref
	}
	if _, err := url.QueryUnescape(strings.TrimSpace(part)); err != nil {
		return ref
	}
	out := uri + "#" + strings.ReplaceAll(part, "+", "%2B")
	if hasTail {
		out += "#" + tail
	}
	return out
}
