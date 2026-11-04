// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package bind

import (
	"slices"

	yaml "go.yaml.in/yaml/v3"
)

// DialectRoot is a schema no other schema holds that names its dialect: where
// it is, its node, and the URI its `$schema` writes. OAS 3.1 has a `$schema`
// choose the dialect of the schema it roots
// (docs/compilers/openapi.md#dialect-keywords).
type DialectRoot struct {
	Node *yaml.Node
	URI  string
	At   Position
}

// heldBySchema reports whether a schema holds the position tokens reach from
// a document's root read at s, the state the document is identified by: a
// position below a Schema Object, or below the root of a document that is no
// OpenAPI document, which holds every schema in it. The root of a resource an
// $id embeds is held so, and names no dialect yet (GitHub #1458).
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
