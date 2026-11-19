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

// Admits reports whether value is a value of the primitive kind primitiveKind,
// judged exactly: a bounded read of a number rather than a conversion, so an
// exponent no machine type holds is refused, not overflowed. A kind with no
// judge admits nothing, which keeps a value off a field it cannot be shown to
// fit.
//
// A null is not judged here: whether null is a value depends on the merged
// field's nullability, not on a kind.
func Admits(primitiveKind ir.PrimitiveKind, value ir.Value) bool {
	switch primitiveKind {
	case ir.PrimitiveAny:
		return true
	case ir.PrimitiveBool:
		return value.Kind == ir.ValueBool
	case ir.PrimitiveString:
		return value.Kind == ir.ValueString
	case ir.PrimitiveInt8:
		return inRange(value, int8Min, int8Max)
	case ir.PrimitiveInt16:
		return inRange(value, int16Min, int16Max)
	case ir.PrimitiveInt32:
		return inRange(value, int32Min, int32Max)
	case ir.PrimitiveInt64:
		return inRange(value, int64Min, int64Max)
	case ir.PrimitiveUint8:
		return inRange(value, "0", uint8Max)
	case ir.PrimitiveUint16:
		return inRange(value, "0", uint16Max)
	case ir.PrimitiveUint32:
		return inRange(value, "0", uint32Max)
	case ir.PrimitiveUint64:
		return inRange(value, "0", uint64Max)
	case ir.PrimitiveInteger:
		decimal, ok := decimalOf(value)
		return ok && decimal.IsInt()
	case ir.PrimitiveNumber, ir.PrimitiveFloat, ir.PrimitiveDecimal:
		_, ok := decimalOf(value)
		return ok
	case ir.PrimitiveDecimal128:
		return admitsDecimal128(value)
	case ir.PrimitiveFloat32:
		return admitsFloat(value, float32Bits)
	case ir.PrimitiveFloat64:
		return admitsFloat(value, float64Bits)
	case ir.PrimitiveDate:
		return value.Kind == ir.ValueString && parses("2006-01-02", value.String)
	case ir.PrimitiveTime:
		return value.Kind == ir.ValueString && parsesRFC3339(value.String, partialTime)
	case ir.PrimitiveDatetime:
		return value.Kind == ir.ValueString && parsesRFC3339(value.String, localDateTime)
	case ir.PrimitiveDatetimeOffset:
		return value.Kind == ir.ValueString && parsesRFC3339(value.String, dateTime)
	case ir.PrimitiveUUID:
		return value.Kind == ir.ValueString && isUUID(value.String)
	case ir.PrimitiveURL:
		return value.Kind == ir.ValueString && isAbsoluteURI(value.String)
	case ir.PrimitiveDuration:
		return value.Kind == ir.ValueString && isDuration(value.String)
	case ir.PrimitiveBytes:
		return false
	default:
		return false
	}
}

// admitsFormat reports whether value is a value of the string format named
// format, and whether the compiler holds that format's grammar at all, which
// the name alone decides. It holds one for a format the table pairs with no
// kind: time, RFC 3339's full-time, whose offset the time kind does not hold
// (docs/ir/primitives.md#offsets). A format the table pairs with a kind is
// judged as that kind, by Admits.
func admitsFormat(format string, value ir.Value) (admits, held bool) {
	switch format {
	case "time":
		return value.Kind == ir.ValueString && parsesRFC3339(value.String, fullTime), true
	default:
		return false, false
	}
}

// decimalOf reads value as a number, reporting false for any other kind of
// value and for a number no Decimal holds: reading that as zero would admit it
// everywhere.
func decimalOf(value ir.Value) (ir.Decimal, bool) {
	if value.Kind != ir.ValueNumber {
		return ir.Decimal{}, false
	}
	decimal, err := value.Number.Decimal()
	return decimal, err == nil
}

// inRange reports whether value is an integral number from lo to hi inclusive.
// An end that reads as no number admits nothing.
func inRange(value ir.Value, lo, hi ir.BigVal) bool {
	decimal, ok := decimalOf(value)
	low, lowErr := lo.Decimal()
	high, highErr := hi.Decimal()
	return ok && lowErr == nil && highErr == nil && decimal.IsInt() && decimal.Cmp(low) >= 0 && decimal.Cmp(high) <= 0
}

// admitsDecimal128 reports whether value fits IEEE 754 decimal128: at most 34
// significant digits, the leading one no higher than 10^6144 and the last no
// lower than 10^-6176.
func admitsDecimal128(value ir.Value) bool {
	decimal, ok := decimalOf(value)
	if !ok {
		return false
	}
	if decimal.Sign() == 0 {
		return true
	}
	digits := int64(len(decimal.Digits()))
	lowest := decimal.Exponent() - (digits - 1)
	return digits <= decimal128Digits && decimal.Exponent() <= decimal128MaxMsdExp && lowest >= decimal128MinLowExp
}

// admitsFloat reports whether value rounds to a finite value of a binary float
// of the given width, which strconv settles exactly: rounding is allowed, as
// OpenAPI floats round, and a magnitude at or past the point where
// round-to-nearest overflows is a range error. The float it returns is not
// kept, so no value passes through one.
func admitsFloat(value ir.Value, bits int) bool {
	decimal, ok := decimalOf(value)
	if !ok {
		return false
	}
	_, err := strconv.ParseFloat(decimal.String(), bits)
	return err == nil
}
