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
var memberKinds = func() map[ir.PrimKind][]ir.ValueKind {
	text := []ir.ValueKind{ir.ValueString, ir.ValueSymbol}
	number := []ir.ValueKind{ir.ValueNumber}
	rows := map[ir.PrimKind][]ir.ValueKind{
		ir.PrimBool:  {ir.ValueBool},
		ir.PrimBytes: {ir.ValueBytes},
		ir.PrimAny:   {ir.ValueBool, ir.ValueString, ir.ValueSymbol, ir.ValueNumber, ir.ValueBytes, ir.ValueNull},
	}
	for _, kind := range []ir.PrimKind{
		ir.PrimString, ir.PrimDate, ir.PrimTime, ir.PrimDatetime, ir.PrimDatetimeOffset, ir.PrimDuration,
		ir.PrimURL, ir.PrimUUID,
	} {
		rows[kind] = text
	}
	for _, kind := range []ir.PrimKind{
		ir.PrimInt8, ir.PrimInt16, ir.PrimInt32, ir.PrimInt64, ir.PrimUint8, ir.PrimUint16, ir.PrimUint32,
		ir.PrimUint64, ir.PrimInteger, ir.PrimFloat32, ir.PrimFloat64, ir.PrimFloat, ir.PrimNumber,
		ir.PrimDecimal, ir.PrimDecimal128,
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
