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

// reads returns the bounds schema writes that a node holding what h says reads,
// schema being the position's own schema or a folded branch of it: the value bounds of the
// kinds h.value names (boundsRead), and where h.collection holds, the
// collection bounds a field states, uniqueItems only where it holds it, true
// (referenceHolds).
func (h boundHomes) reads(schema *statement.Schema) keySet {
	out := boundsRead(schema, h.value, nil)
	if h.collection {
		out |= collectionReads(schema) &^ keyBit("uniqueItems")
		if referenceHolds(schema, "uniqueItems") {
			out |= keyBit("uniqueItems")
		}
	}
	return out
}

// homesAt returns which bounds the position schema is written at, lowered to target,
// holds for its folded branches, as the census asks of those written beside its allOf. A
// carrier or an alias holds the value bounds the types the position admits take
// (boundKinds), and collection bounds where it is a use of the principal, a
// reference (referenceReads), where those types admit an array. A node the position
// owns holds what the lowering that builds it folds of them (ownHomes).
func homesAt(loweringContext lowering.Ctx, schema *statement.Schema, target ir.TypeRef, pointer jsontext.Pointer) boundHomes {
	if !LoweredToOwnNode(loweringContext, pointer, target) {
		refinement, refined := refinementOf(loweringContext, schema, pointer)
		takes := boundKinds(loweringContext, schema)
		return boundHomes{value: takes, collection: refined && refinement.principal != nil && takes&kindArray != 0}
	}
	return ownHomes(loweringContext, schema, pointer)
}

// ownHomes returns which bounds the node the refinement schema, written at pointer,
// owns holds for the folded branches of its allOf: where an inline principal built it,
// that principal's (nodeHomes), none for a value set's or a $ref's, and
// otherwise what schema's own election builds.
func ownHomes(loweringContext lowering.Ctx, schema *statement.Schema, pointer jsontext.Pointer) boundHomes {
	takes := boundKinds(loweringContext, schema)
	if refinement, refined := refinementOf(loweringContext, schema, pointer); refined && refinement.principal != nil {
		allOfBranch := refinement.principal
		if _, set := valueSetPrincipal(refinement); set || allOfBranch.schema.Ref != nil {
			return boundHomes{}
		}
		return nodeHomes(loweringContext, allOfBranch.schema.Schema, allOfBranch.pointer, principalDispatch(*allOfBranch), takes)
	}
	dispatch, _ := electionOf(loweringContext, schema, pointer)
	return nodeHomes(loweringContext, schema, pointer, dispatch, takes)
}

// nodeHomes returns which bounds the node schema, written at pointer, lowers to by
// the election dispatchValue holds, as its builder reads them of the folded branches, takes the
// kinds its position admits: none of a value set's, the value bounds of the
// types a Scalar built for each type its lowering names takes, the collection
// bounds of an array's List or Tuple, a type list's variant's among them, a
// union's of oneOf or anyOf the value bounds alone, and none of an object's,
// which lowerModel builds with no folded branch.
func nodeHomes(loweringContext lowering.Ctx, schema *statement.Schema, pointer jsontext.Pointer, dispatchValue dispatch, takes kindSet) boundHomes {
	if dispatchValue.won == "const" || dispatchValue.won == "enum" {
		return boundHomes{}
	}
	if declaresUnion(schema) {
		return boundHomes{value: takes}
	}
	types, top := nodeTypes(loweringContext, schema, pointer)
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
			if carriesText(schema, st, variantFormat(schema, st, types)) {
				homes.numberText = true
				continue
			}
			homes.value |= takes & typeKinds(st)
		}
	}
	return homes
}

// nodeTypes returns the types the nodes of the position schema, written at pointer,
// are built for where it elects no family and declares no union: the types it
// writes, none where it writes only null, and else the ones its shape keywords
// speak of (untypedKinds) or its allOf admits (lowerUndecided), top where it
// admits none, which lowers to an alias of the top type.
func nodeTypes(loweringContext lowering.Ctx, schema *statement.Schema, pointer jsontext.Pointer) (types []statement.JSONSchemaTypeName, top bool) {
	if types := effectiveTypes(schema); len(types) > 0 {
		return types, false
	}
	if writesType(schema) {
		return nil, false
	}
	kinds, _ := untypedKinds(loweringContext, schema)
	if kinds == 0 {
		types := undecidedTypes(loweringContext, schema, pointer)
		return types, len(types) == 0
	}
	// An object, an array, or both, as lowerUntyped builds them.
	return slices.DeleteFunc(slices.Clone(inferredKinds), func(st statement.JSONSchemaTypeName) bool { return typeKinds(st)&kinds == 0 }), false
}
