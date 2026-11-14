// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package schema

import (
	"slices"

	"github.com/speakeasy-api/openapi/jsonschema/oas3"

	"github.com/dexpace/morphic/compilers/openapi/internal/annotation"
)

// keywordKinds maps each keyword JSON Schema applies to instances of some types
// alone to the kinds of those types: the numeric bounds to numbers, the string
// bounds and the content vocabulary to strings, and the property counts to
// objects (JSON Schema Validation 2020-12 §6.2, §6.3, §6.5 and §8, which the
// earlier drafts group alike). An instance of another kind satisfies such a
// keyword (JSON Schema Core 2020-12 §7.6.1), so it says nothing of a value only
// other kinds can be. The collection bounds are left to the List and Tuple
// readers, which ask the node.
var keywordKinds = map[string]kindSet{
	"exclusiveMaximum": kindNumber,
	"exclusiveMinimum": kindNumber,
	"maximum":          kindNumber,
	"minimum":          kindNumber,
	"multipleOf":       kindNumber,
	"maxLength":        kindString,
	"minLength":        kindString,
	"pattern":          kindString,
	"contentEncoding":  kindString,
	"contentMediaType": kindString,
	"contentSchema":    kindString,
	"maxProperties":    kindObject,
	"minProperties":    kindObject,
}

// typeKinds returns the kinds st, one type a type array lists, admits, and every
// kind but null for a spelling no dialect defines, as declaredInstanceTypes
// reads one.
func typeKinds(st oas3.SchemaType) kindSet {
	if kinds := kindOfType(st) &^ kindNull; kinds != 0 {
		return kinds
	}
	return anyInstance
}

// writerTakes returns the kinds whose value bounds a reader of the kinds takes
// reads of w, a position's schema or an allOf branch folding there: those w's
// own types admit too, as a branch declaring a type bounds no value of another
// kind. A reader's kinds are those of a type its position declares, so the
// position's own schema leaves them as they are.
func writerTakes(takes kindSet, w *oas3.Schema) kindSet {
	return takes & declaredInstanceTypes(w)
}

// taken reports whether a reader of values of the kinds takes reads keyword:
// whether keyword applies to a value of one of them. One the table does not list
// applies to every value.
func taken(takes kindSet, keyword string) bool {
	kinds, listed := keywordKinds[keyword]
	return !listed || takes&kinds != 0
}

// untakenBounds returns the value constraints s writes that a reader of the
// kinds takes does not read, in valueConstraintKeywords order, but a count the
// load phase refuses, which it reports and no reader holds (refusedCount).
func untakenBounds(s *oas3.Schema, takes kindSet) []string {
	return slices.DeleteFunc(declaredConstraints(s), func(keyword string) bool {
		return taken(takes, keyword) || refusedCount(s, keyword)
	})
}

// boundsFor returns the groups of value bounds annotation.Constraints reads for
// a reader of the kinds takes: the numeric bounds for a number, the string
// bounds for a string and the property counts for an object, as keywordKinds
// gives each keyword its kinds.
func boundsFor(takes kindSet) annotation.Bounds {
	var b annotation.Bounds
	if takes&kindNumber != 0 {
		b |= annotation.NumberBounds
	}
	if takes&kindString != 0 {
		b |= annotation.StringBounds
	}
	if takes&kindObject != 0 {
		b |= annotation.ObjectBounds
	}
	return b
}
