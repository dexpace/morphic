// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package irverify

import (
	"slices"
	"strconv"

	"github.com/dexpace/morphic/ir"
)

// memberKinds is, for each value type, the kinds of value an enum of it holds as
// a member (docs/ir/enums.md#member-values). The kind is held, not the value: a
// string member of an enum of integers is wrong whatever it spells. A kind no
// row lists is held by no enum.
var memberKinds = func() map[ir.PrimitiveKind][]ir.ValueKind {
	text := []ir.ValueKind{ir.ValueString, ir.ValueSymbol}
	number := []ir.ValueKind{ir.ValueNumber}
	rows := map[ir.PrimitiveKind][]ir.ValueKind{
		ir.PrimitiveBool:  {ir.ValueBool},
		ir.PrimitiveBytes: {ir.ValueBytes},
		ir.PrimitiveAny:   {ir.ValueBool, ir.ValueString, ir.ValueSymbol, ir.ValueNumber, ir.ValueBytes, ir.ValueNull},
	}
	for _, kind := range []ir.PrimitiveKind{
		ir.PrimitiveString, ir.PrimitiveDate, ir.PrimitiveTime, ir.PrimitiveDatetime, ir.PrimitiveDatetimeOffset, ir.PrimitiveDuration,
		ir.PrimitiveURL, ir.PrimitiveUUID,
	} {
		rows[kind] = text
	}
	for _, kind := range []ir.PrimitiveKind{
		ir.PrimitiveInt8, ir.PrimitiveInt16, ir.PrimitiveInt32, ir.PrimitiveInt64, ir.PrimitiveUint8, ir.PrimitiveUint16, ir.PrimitiveUint32,
		ir.PrimitiveUint64, ir.PrimitiveInteger, ir.PrimitiveFloat32, ir.PrimitiveFloat64, ir.PrimitiveFloat, ir.PrimitiveNumber,
		ir.PrimitiveDecimal, ir.PrimitiveDecimal128,
	} {
		rows[kind] = number
	}
	return rows
}()

// checkEnumMemberKinds asserts every member of an enum whose ValueType is
// declared holds a value of a kind that value type admits, so a consumer
// rendering the member as its enum's type is never handed another. An undeclared
// ValueType has no row: checkPrimKinds reports it, and the members are left.
func checkEnumMemberKinds(doc *ir.Document) []Violation {
	var vs []Violation
	for id, td := range doc.Types {
		enum, isEnum := td.(*ir.Enum)
		if !isEnum || ir.IsNilTypeDef(td) || !enum.ValueType.Valid() {
			continue
		}
		admitted := memberKinds[enum.ValueType]
		for i, member := range enum.Members {
			if slices.Contains(admitted, member.Value.Kind) {
				continue
			}
			vs = append(vs, Violation{
				Code: "ir/enum-member-kind",
				Message: "enum of value type " + string(enum.ValueType) + " holds a " +
					string(member.Value.Kind) + " member",
				Path: typePath(id) + ".Members[" + strconv.Itoa(i) + "].Value",
			})
		}
	}
	return vs
}
