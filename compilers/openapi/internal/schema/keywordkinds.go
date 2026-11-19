// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package schema

import (
	"github.com/dexpace/morphic/compilers/openapi/internal/annotation"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/compilers/openapi/internal/statement"
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

// boundKinds returns the kinds a reader of the bounds schema writes takes: those
// its home lowers to (docs/compilers/openapi.md#constraints), as the run reads
// its statement once (admissionOf), its value sets, a type beside a $ref and
// what a read of any size takes in among them. A read cut by a cycle of
// conjunct edges or a $ref landing on no schema leaves schema's own type to
// decide, which takes every bound where it writes none. Where schema writes no
// bound, nothing asks which kinds take one, and its own type is returned.
func boundKinds(loweringContext lowering.Ctx, schema *statement.Schema) kindSet {
	own := declaredInstanceTypes(schema)
	if !stated(schema) || !writesBound(loweringContext, schema) {
		return own
	}
	admission := admissionOf(loweringContext, loweringContext.Statements().Read(schema))
	if admission.cut {
		return own
	}
	return admission.kinds
}

// writesBound reports whether schema, or an inline branch of its flattened allOf,
// writes a value or collection bound: where none does, no reader asks its types.
// A bound beside a branch's own $ref is that branch's.
func writesBound(loweringContext lowering.Ctx, schema *statement.Schema) bool {
	if declaresBound(schema) {
		return true
	}
	for _, allOfConjunct := range loweringContext.Statements().Read(schema).Entries {
		if stated(allOfConjunct.Schema) && allOfConjunct.Ref == nil && declaresBound(allOfConjunct.Schema) {
			return true
		}
	}
	return false
}

// declaresBound reports whether schema writes a value bound or a collection bound.
func declaresBound(schema *statement.Schema) bool {
	return declaresValueConstraints(schema) || hasAnyKey(schema, collectionBoundKeywords)
}

// writerTakes returns the kinds whose value bounds a reader of the kinds takes
// reads of schema, a position's schema or an allOf branch folding there: those schema's
// own types admit too, as a branch declaring a type bounds no value of another
// kind. A reader's kinds are those its position admits (boundKinds), within
// what the position's own schema declares, so that schema leaves them as they
// are.
func writerTakes(takes kindSet, schema *statement.Schema) kindSet {
	return takes & declaredInstanceTypes(schema)
}

// taken reports whether a reader of values of the kinds takes reads keyword:
// whether keyword applies to a value of one of them. One the table does not list
// applies to every value.
func taken(takes kindSet, keyword string) bool {
	kinds, listed := keywordKinds[keyword]
	return !listed || takes&kinds != 0
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
