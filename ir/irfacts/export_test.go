// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package irfacts

import "github.com/dexpace/morphic/ir"

// SubtypeIndexAnswer answers IsSubtype from x's hierarchy index alone, with
// settled false where only a walk can. Tests read it to hold the index's own
// answers to ir.IsSubtype, since IsSubtype falls back to that very walk.
func (x *DocumentFacts) SubtypeIndexAnswer(target, base ir.TypeID) (sub, settled bool) {
	return x.hierarchyIndex().subtype(target, base)
}

// ExposesIndexAnswer answers Exposes from x's exposure index alone, with
// settled false where only a walk can, as SubtypeIndexAnswer does for
// IsSubtype.
func (x *DocumentFacts) ExposesIndexAnswer(id ir.TypeID, property ir.PropertyID) (exposed, settled bool) {
	return x.exposureIndex().exposes(id, property)
}

// Indexed reports which of the walked relations x has indexed so far.
func (x *DocumentFacts) Indexed() (hierarchy, exposure bool) {
	return x.hierarchy != nil, x.exposure != nil
}

// WalkStepsLeft returns the steps x may still walk for each relation before it
// indexes that relation instead.
func (x *DocumentFacts) WalkStepsLeft() (subtype, exposure int64) {
	return x.subtypeWalks.Load(), x.exposureWalks.Load()
}

// AliasTargetsOf exposes the registry-wide alias index to the external tests:
// what each alias scalar of types stands for.
func AliasTargetsOf(types ir.TypeRegistry) map[ir.TypeID]ir.TypeID { return newAliasTargets(types) }

// Parts exposes how the type graph reads td: the types it holds, composes,
// aliases and names as field arguments.
func Parts(td ir.TypeDef) (held, composed []ir.TypeID, alias ir.TypeID, args []ir.TypeID) {
	var typePartsValue typeParts
	typePartsValue.read(td)
	return typePartsValue.held, typePartsValue.composed, typePartsValue.alias, typePartsValue.args
}
