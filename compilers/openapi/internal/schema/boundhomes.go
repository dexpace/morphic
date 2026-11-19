// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package schema

import (
	"encoding/json/jsontext"
	"slices"

	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/compilers/openapi/internal/statement"
	"github.com/dexpace/morphic/ir"
)

// boundHomes says which bounds the position an allOf is written at has a field
// for, so that a branch writing one folds there: the value bounds that apply to
// a value of the kinds value (keywordKinds), and the collection bounds; and
// whether the node it owns carries a number as text, which holds none of a
// branch's value bounds (keepTextBounds).
type boundHomes struct {
	value      kindSet
	collection bool
	numberText bool
}

// reads returns the bounds s writes that a node holding what h says reads,
// s being the position's own schema or a folded branch of it: the value bounds of the
// kinds h.value names (boundsRead), and where h.collection holds, the
// collection bounds a field states, uniqueItems only where it holds it, true
// (referenceHolds).
func (h boundHomes) reads(s *statement.Schema) keySet {
	out := boundsRead(s, h.value, nil)
	if h.collection {
		out |= collectionReads(s) &^ keyBit("uniqueItems")
		if referenceHolds(s, "uniqueItems") {
			out |= keyBit("uniqueItems")
		}
	}
	return out
}

// homesAt returns which bounds the position s is written at, lowered to target,
// holds for its folded branches, as the census asks of those written beside its allOf. A
// carrier or an alias holds the value bounds the types the position admits take
// (boundKinds), and collection bounds where it is a use of the principal, a
// reference (referenceReads), where those types admit an array. A node the position
// owns holds what the lowering that builds it folds of them (ownHomes).
func homesAt(c lowering.Ctx, s *statement.Schema, target ir.TypeRef, pointer jsontext.Pointer) boundHomes {
	if !LoweredToOwnNode(c, pointer, target) {
		r, refined := refinementOf(c, s, pointer)
		takes := boundKinds(c, s)
		return boundHomes{value: takes, collection: refined && r.principal != nil && takes&kindArray != 0}
	}
	return ownHomes(c, s, pointer)
}

// ownHomes returns which bounds the node the refinement s, written at pointer,
// owns holds for the folded branches of its allOf: where an inline principal built it,
// that principal's (nodeHomes), none for a value set's or a $ref's, and
// otherwise what s's own election builds.
func ownHomes(c lowering.Ctx, s *statement.Schema, pointer jsontext.Pointer) boundHomes {
	takes := boundKinds(c, s)
	if r, refined := refinementOf(c, s, pointer); refined && r.principal != nil {
		e := r.principal
		if _, set := valueSetPrincipal(r); set || e.schema.Ref != nil {
			return boundHomes{}
		}
		return nodeHomes(c, e.schema.Schema, e.pointer, principalDispatch(*e), takes)
	}
	d, _ := electionOf(c, s, pointer)
	return nodeHomes(c, s, pointer, d, takes)
}

// nodeHomes returns which bounds the node s, written at pointer, lowers to by
// the election d holds, as its builder reads them of the folded branches, takes the
// kinds its position admits: none of a value set's, the value bounds of the
// types a Scalar built for each type its lowering names takes, the collection
// bounds of an array's List or Tuple, a type list's variant's among them, a
// union's of oneOf or anyOf the value bounds alone, and none of an object's,
// which lowerModel builds with no folded branch.
func nodeHomes(c lowering.Ctx, s *statement.Schema, pointer jsontext.Pointer, d dispatch, takes kindSet) boundHomes {
	if d.won == "const" || d.won == "enum" {
		return boundHomes{}
	}
	if declaresUnion(s) {
		return boundHomes{value: takes}
	}
	types, top := nodeTypes(c, s, pointer)
	if top {
		return boundHomes{value: takes}
	}
	var homes boundHomes
	for _, st := range types {
		switch st {
		case statement.KindObject:
		case statement.KindArray:
			homes.collection = true
		default:
			// A type list's variant takes the format only where no other listed
			// type pairs with it (variantFormat), as lowerTypeVariant builds it.
			if carriesText(s, st, variantFormat(s, st, types)) {
				homes.numberText = true
				continue
			}
			homes.value |= takes & typeKinds(st)
		}
	}
	return homes
}

// nodeTypes returns the types the nodes of the position s, written at pointer,
// are built for where it elects no family and declares no union: the types it
// writes, none where it writes only null, and else the ones its shape keywords
// speak of (untypedKinds) or its allOf admits (lowerUndecided), top where it
// admits none, which lowers to an alias of the top type.
func nodeTypes(c lowering.Ctx, s *statement.Schema, pointer jsontext.Pointer) (types []statement.JSONSchemaTypeName, top bool) {
	if types := effectiveTypes(s); len(types) > 0 {
		return types, false
	}
	if writesType(s) {
		return nil, false
	}
	kinds, _ := untypedKinds(c, s)
	if kinds == 0 {
		types := undecidedTypes(c, s, pointer)
		return types, len(types) == 0
	}
	// An object, an array, or both, as lowerUntyped builds them.
	return slices.DeleteFunc(slices.Clone(inferredKinds), func(st statement.JSONSchemaTypeName) bool { return typeKinds(st)&kinds == 0 }), false
}
