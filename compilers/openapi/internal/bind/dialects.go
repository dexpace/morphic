// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package bind

import (
	"slices"

	yaml "go.yaml.in/yaml/v3"
)

// DialectRoot is a schema resource root writing a `$schema`: a schema no other
// holds, or one whose $id roots a resource of its own; where it is, its node,
// and the value its `$schema` writes, which names its dialect where it is an
// absolute URI. OAS 3.1 has a `$schema` choose the dialect of the resource it
// roots (docs/compilers/openapi.md#dialect-keywords).
type DialectRoot struct {
	Node *yaml.Node
	URI  string
	At   Position
}

// heldBySchema reports whether a schema holds the position tokens reach from
// a document's root read at s, the state the document is identified by: a
// position below a Schema Object, or below the root of a document that is no
// OpenAPI document, which holds every schema in it. The root of a resource an
// $id embeds is held so, and roots a resource all the same.
func heldBySchema(s state, tokens []string) bool {
	for _, token := range tokens {
		if s == sSchema || s == sLoose {
			return true
		}
		s = step(s, token)
	}
	return false
}

// dialectsByDocument returns the dialect roots of roots by the document each
// is in, each document's in pointer order.
func dialectsByDocument(roots map[Position]DialectRoot) map[DocID][]DialectRoot {
	out := map[DocID][]DialectRoot{}
	for _, r := range roots {
		out[r.At.Doc] = append(out[r.At.Doc], r)
	}
	for _, list := range out {
		slices.SortFunc(list, func(a, b DialectRoot) int { return comparePositions(a.At, b.At) })
	}
	return out
}
