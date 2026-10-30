// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package irtest

import "github.com/dexpace/morphic/ir"

// DocOf builds a document whose registry holds defs, each keyed by its own ID.
func DocOf(defs ...ir.TypeDef) *ir.Document {
	types := ir.TypeRegistry{}
	for _, d := range defs {
		types[d.Common().ID] = d
	}
	return &ir.Document{Types: types}
}

// ModelOf builds the model id, shaped by mutate when it is not nil.
func ModelOf(id ir.TypeID, mutate func(*ir.Model)) *ir.Model {
	m := &ir.Model{ID: id}
	if mutate != nil {
		mutate(m)
	}
	return m
}

// ScalarOf builds the scalar id over base, or an opaque one when base is empty.
func ScalarOf(id, base ir.TypeID) *ir.Scalar {
	s := &ir.Scalar{ID: id}
	if base != "" {
		s.Base = &ir.TypeRef{Target: base}
	}
	return s
}

// RefsTo builds a reference to each of ids.
func RefsTo(ids ...ir.TypeID) []ir.TypeRef {
	out := make([]ir.TypeRef, 0, len(ids))
	for _, id := range ids {
		out = append(out, ir.TypeRef{Target: id})
	}
	return out
}
