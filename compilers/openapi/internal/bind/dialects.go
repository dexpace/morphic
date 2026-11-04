// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package bind

import (
	"slices"

	yaml "go.yaml.in/yaml/v3"
)

// DialectRoot is a schema resource root that names its dialect: a schema no
// other holds, or one whose $id roots a resource of its own; where it is, its
// node, and the URI its `$schema` writes. OAS 3.1 has a `$schema` choose the
// dialect of the resource it roots (docs/compilers/openapi.md#dialect-keywords).
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

// DialectAt returns the innermost of roots, a dialect root by where it is, at
// p or holding it in p's document, and false where none does: the root whose
// `$schema` names the dialect the schema at p is read in. A position reads it
// by where it is, not by its node, so a node an alias places under two roots
// reads each one's at each.
func DialectAt[V any](roots map[Position]V, p Position) (V, bool) {
	for ptr := p.Pointer; len(roots) > 0; ptr = ptr.Parent() {
		if r, ok := roots[Position{Doc: p.Doc, Pointer: ptr}]; ok {
			return r, true
		}
		if ptr == "" {
			break
		}
	}
	var none V
	return none, false
}
