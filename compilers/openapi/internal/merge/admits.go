// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package merge

import (
	"strconv"

	"github.com/dexpace/morphic/ir"
)

// Bounds of the fixed-width integers, as the decimal literals inRange compares.
const (
	int8Min   ir.BigVal = "-128"
	int8Max   ir.BigVal = "127"
	int16Min  ir.BigVal = "-32768"
	int16Max  ir.BigVal = "32767"
	int32Min  ir.BigVal = "-2147483648"
	int32Max  ir.BigVal = "2147483647"
	int64Min  ir.BigVal = "-9223372036854775808"
	int64Max  ir.BigVal = "9223372036854775807"
	uint8Max  ir.BigVal = "255"
	uint16Max ir.BigVal = "65535"
	uint32Max ir.BigVal = "4294967295"
	uint64Max ir.BigVal = "18446744073709551615"
)

// The IEEE 754 formats Admits judges against: decimal128's digit budget and
// exponent range, and the width of each binary float.
const (
	decimal128Digits    = 34
	decimal128MaxMsdExp = 6144
	decimal128MinLowExp = -6176
	float32Bits         = 32
	float64Bits         = 64
)

// Admits reports whether v is a value of the primitive kind k, judged exactly:
// a bounded read of a number rather than a conversion, so an exponent no
// machine type holds is refused, not overflowed. A kind with no judge admits
// nothing, which keeps a value off a field it cannot be shown to fit.
//
// A null is not judged here: whether null is a value depends on the merged
// field's nullability, not on a kind.
func Admits(k ir.PrimKind, v ir.Value) bool {
	switch k {
	case ir.PrimAny:
		return true
	case ir.PrimBool:
		return v.Kind == ir.ValueBool
	case ir.PrimString:
		return v.Kind == ir.ValueString
	case ir.PrimInt8:
		return inRange(v, int8Min, int8Max)
	case ir.PrimInt16:
		return inRange(v, int16Min, int16Max)
	case ir.PrimInt32:
		return inRange(v, int32Min, int32Max)
	case ir.PrimInt64:
		return inRange(v, int64Min, int64Max)
	case ir.PrimUint8:
		return inRange(v, "0", uint8Max)
	case ir.PrimUint16:
		return inRange(v, "0", uint16Max)
	case ir.PrimUint32:
		return inRange(v, "0", uint32Max)
	case ir.PrimUint64:
		return inRange(v, "0", uint64Max)
	case ir.PrimInteger:
		d, ok := decimalOf(v)
		return ok && d.IsInt()
	case ir.PrimNumber, ir.PrimFloat, ir.PrimDecimal:
		_, ok := decimalOf(v)
		return ok
	case ir.PrimDecimal128:
		return admitsDecimal128(v)
	case ir.PrimFloat32:
		return admitsFloat(v, float32Bits)
	case ir.PrimFloat64:
		return admitsFloat(v, float64Bits)
	case ir.PrimDate:
		return v.Kind == ir.ValueString && parses("2006-01-02", v.Str)
	case ir.PrimTime:
		return v.Kind == ir.ValueString && parsesRFC3339(v.Str, false)
	case ir.PrimDatetimeOffset:
		return v.Kind == ir.ValueString && parsesRFC3339(v.Str, true)
	case ir.PrimUUID:
		return v.Kind == ir.ValueString && isUUID(v.Str)
	case ir.PrimURL:
		return v.Kind == ir.ValueString && isAbsoluteURI(v.Str)
	case ir.PrimDuration:
		return v.Kind == ir.ValueString && isDuration(v.Str)
	case ir.PrimBytes, ir.PrimDatetime:
		return false
	default:
		return false
	}
}

// decimalOf reads v as a number, reporting false for any other kind of value and
// for a number no Decimal holds: reading that as zero would admit it everywhere.
func decimalOf(v ir.Value) (ir.Decimal, bool) {
	if v.Kind != ir.ValueNumber {
		return ir.Decimal{}, false
	}
	d, err := v.Num.Decimal()
	return d, err == nil
}

// inRange reports whether v is an integral number from lo to hi inclusive. An
// end that reads as no number admits nothing.
func inRange(v ir.Value, lo, hi ir.BigVal) bool {
	d, ok := decimalOf(v)
	low, lowErr := lo.Decimal()
	high, highErr := hi.Decimal()
	return ok && lowErr == nil && highErr == nil && d.IsInt() && d.Cmp(low) >= 0 && d.Cmp(high) <= 0
}

// admitsDecimal128 reports whether v fits IEEE 754 decimal128: at most 34
// significant digits, the leading one no higher than 10^6144 and the last no
// lower than 10^-6176.
func admitsDecimal128(v ir.Value) bool {
	d, ok := decimalOf(v)
	if !ok {
		return false
	}
	if d.Sign() == 0 {
		return true
	}
	digits := int64(len(d.Digits()))
	lowest := d.Exponent() - (digits - 1)
	return digits <= decimal128Digits && d.Exponent() <= decimal128MaxMsdExp && lowest >= decimal128MinLowExp
}

// admitsFloat reports whether v rounds to a finite value of a binary float of
// the given width, which strconv settles exactly: rounding is allowed, as
// OpenAPI floats round, and a magnitude at or past the point where
// round-to-nearest overflows is a range error. The float it returns is not kept,
// so no value passes through one.
func admitsFloat(v ir.Value, bits int) bool {
	d, ok := decimalOf(v)
	if !ok {
		return false
	}
	_, err := strconv.ParseFloat(d.String(), bits)
	return err == nil
}
