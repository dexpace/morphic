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

// homesAt returns which bounds the position s is written at, lowered to target,
// holds for its voices, as the census asks of those written beside its allOf. A
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
// owns holds for the voices of its allOf, read off what lowers the position
// rather than off the node: none of a value set's (a principal's, or the
// election's const or enum), the value bounds of the types a Scalar built for
// each type its lowering names takes, the collection bounds of an array's List
// or Tuple, a union's of oneOf or anyOf the bounds the position admits, and
// none of an object's, which lowerModel builds with no voice.
func ownHomes(c lowering.Ctx, s *statement.Schema, pointer jsontext.Pointer) boundHomes {
	if r, refined := refinementOf(c, s, pointer); refined && r.principal != nil {
		return boundHomes{}
	}
	d, _ := electionOf(c, s, pointer)
	if d.won == "const" || d.won == "enum" {
		return boundHomes{}
	}
	takes := boundKinds(c, s)
	if declaresUnion(s) {
		branches, _, _ := unionBranches(c, s, s)
		return boundHomes{value: takes, collection: slices.ContainsFunc(branches, func(b statement.Sub) bool {
			kinds, _ := lowersTo(c, c.Statements().Sub(b))
			return kinds == kindArray
		})}
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
			if carriesText(s, st) {
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
func nodeTypes(c lowering.Ctx, s *statement.Schema, pointer jsontext.Pointer) (types []statement.Kind, top bool) {
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
	return slices.DeleteFunc(slices.Clone(inferredKinds), func(st statement.Kind) bool { return typeKinds(st)&kinds == 0 }), false
}
