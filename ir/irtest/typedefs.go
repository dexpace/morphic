// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package irtest

import "github.com/dexpace/morphic/ir"

// DocOf builds a document whose registry holds defs, each keyed by its own ID.
func DocOf(defs ...ir.TypeDef) *ir.Document {
	types := ir.TypeRegistry{}
	for _, typeDef := range defs {
		types[typeDef.Common().ID] = typeDef
	}
	return &ir.Document{Types: types}
}

// ModelOf builds the model id, shaped by mutate when it is not nil.
func ModelOf(id ir.TypeID, mutate func(*ir.Model)) *ir.Model {
	model := &ir.Model{ID: id}
	if mutate != nil {
		mutate(model)
	}
	return model
}

// ScalarOf builds the scalar id over base, or an opaque one when base is empty.
func ScalarOf(id, base ir.TypeID) *ir.Scalar {
	scalar := &ir.Scalar{ID: id}
	if base != "" {
		scalar.Base = &ir.TypeRef{Target: base}
	}
	return scalar
}

// RefsTo builds a reference to each of ids.
func RefsTo(ids ...ir.TypeID) []ir.TypeRef {
	out := make([]ir.TypeRef, 0, len(ids))
	for _, id := range ids {
		out = append(out, ir.TypeRef{Target: id})
	}
	return out
}
