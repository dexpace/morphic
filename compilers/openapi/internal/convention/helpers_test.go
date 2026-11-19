// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package convention_test

import (
	"maps"

	"github.com/dexpace/morphic/compilers/openapi/internal/promotion"
	"github.com/dexpace/morphic/ir"
)

// defaults is the promoter of the default policy, whose keys the conventions
// defer to.
func defaults() promotion.Promoter {
	return promotion.New(promotion.ExtensionPromotions{})
}

// registry is a type registry holding the primitives a test types a use by.
func registry(extra ir.TypeRegistry) ir.TypeRegistry {
	reg := ir.TypeRegistry{}
	for _, primitiveKind := range []ir.PrimitiveKind{ir.PrimitiveString, ir.PrimitiveBytes, ir.PrimitiveBool, ir.PrimitiveDatetimeOffset, ir.PrimitiveDate} {
		reg[ir.PrimitiveTypeID(primitiveKind)] = &ir.Primitive{ID: ir.PrimitiveTypeID(primitiveKind), PrimitiveKind: primitiveKind}
	}
	maps.Copy(reg, extra)
	return reg
}

// prim is a reference to the primitive primitiveKind.
func prim(primitiveKind ir.PrimitiveKind) ir.TypeRef {
	return ir.TypeRef{Target: ir.PrimitiveTypeID(primitiveKind)}
}

// oneOperation is a document of one service holding op alone, over reg.
func oneOperation(op ir.Operation, reg ir.TypeRegistry) *ir.Document {
	return &ir.Document{
		Services: []ir.Service{{Groups: []ir.OperationGroup{{Operations: []ir.Operation{op}}}}},
		Types:    reg,
	}
}

// operationOf returns the one operation oneOperation built doc around.
func operationOf(doc *ir.Document) *ir.Operation { return &doc.Services[0].Groups[0].Operations[0] }
