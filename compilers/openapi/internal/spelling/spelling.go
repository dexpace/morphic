// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package spelling

import (
	"strings"

	"github.com/dexpace/morphic/ir"
)

// formatTable maps a scalar "type" or "type/format" key to its IR primitive.
//
// An integer width maps under either numeric type, since OpenAPI applies a
// format to integers and other numbers alike (3.1.1 §4.4.1). float, double,
// decimal and decimal128 map under number alone: under integer they admit only
// the integral values, which no primitive narrower than integer holds. Any
// pairing absent here, as time is (docs/ir/primitives.md#offsets), hoists a
// Scalar that keeps the format, byte excepted: the schema walk reads it before
// the table, as base64-encoded bytes.
//
// No row maps to its bare type's kind, which leaves the format nowhere
// (TestFormatTable_EveryRowDistinguishesTheBareType).
var formatTable = map[string]ir.PrimKind{
	"string":                 ir.PrimString,
	"string/date":            ir.PrimDate,
	"string/time-local":      ir.PrimTime,
	"string/duration":        ir.PrimDuration,
	"string/uuid":            ir.PrimUUID,
	"string/uri":             ir.PrimURL,
	"string/date-time":       ir.PrimDatetimeOffset,
	"string/date-time-local": ir.PrimDatetime,
	"string/binary":          ir.PrimBytes,
	"integer":                ir.PrimInteger,
	"integer/int8":           ir.PrimInt8,
	"integer/int16":          ir.PrimInt16,
	"integer/int32":          ir.PrimInt32,
	"integer/int64":          ir.PrimInt64,
	"integer/uint8":          ir.PrimUint8,
	"integer/uint16":         ir.PrimUint16,
	"integer/uint32":         ir.PrimUint32,
	"integer/uint64":         ir.PrimUint64,
	"number":                 ir.PrimNumber,
	"number/int8":            ir.PrimInt8,
	"number/int16":           ir.PrimInt16,
	"number/int32":           ir.PrimInt32,
	"number/int64":           ir.PrimInt64,
	"number/uint8":           ir.PrimUint8,
	"number/uint16":          ir.PrimUint16,
	"number/uint32":          ir.PrimUint32,
	"number/uint64":          ir.PrimUint64,
	"number/float":           ir.PrimFloat32,
	"number/double":          ir.PrimFloat64,
	"number/decimal":         ir.PrimDecimal,
	"number/decimal128":      ir.PrimDecimal128,
	"boolean":                ir.PrimBool,
}

// Lookup returns the primitive a "type" or "type/format" key selects, and
// whether the table has a row for it.
func Lookup(key string) (ir.PrimKind, bool) {
	prim, ok := formatTable[key]
	return prim, ok
}

// Relation is how two primitive kinds compare as the types of one field
// declared twice under allOf.
type Relation int

// The relations Relate returns.
const (
	// Disjoint means no value is of both kinds.
	Disjoint Relation = iota
	// Overlap means the kinds share values and neither holds the other.
	Overlap
	// Same means the kinds are equal.
	Same
	// ANarrower means the first kind is strictly within the second.
	ANarrower
	// BNarrower means the second kind is strictly within the first.
	BNarrower
)

// Relate classifies a against b. any is the top: every other kind is
// narrower. Two distinct unrelated kinds overlap when both are numeric (each
// admits 0), or when one is bytes and the other a string-spelled format.
// Otherwise they are disjoint: different families, or two different string
// formats, since no string satisfies the grammar of two.
func Relate(a, b ir.PrimKind) Relation {
	switch {
	case a == b:
		return Same
	case narrows(a, b):
		return ANarrower
	case narrows(b, a):
		return BNarrower
	case numeric(a) && numeric(b), bytesAgainstFormat(a, b), bytesAgainstFormat(b, a):
		return Overlap
	default:
		return Disjoint
	}
}

// narrows reports whether narrow is strictly within broad: by value domain
// (ir.PrimKind.StrictlyWithin), or because narrow is a string-spelled format
// and broad is string.
func narrows(narrow, broad ir.PrimKind) bool {
	return narrow.StrictlyWithin(broad) || (broad == ir.PrimString && refinesString(narrow))
}

// refinesString reports whether some string/* row of the table selects k. The
// edge set is read off the table rather than restated beside it.
func refinesString(k ir.PrimKind) bool {
	for key, prim := range formatTable {
		if prim == k && strings.HasPrefix(key, "string/") {
			return true
		}
	}
	return false
}

// numeric reports whether k is number or lies within it.
func numeric(k ir.PrimKind) bool {
	return k == ir.PrimNumber || k.StrictlyWithin(ir.PrimNumber)
}

// bytesAgainstFormat reports whether b is bytes and a is another
// string-spelled format: octets may spell any string.
func bytesAgainstFormat(a, b ir.PrimKind) bool {
	return b == ir.PrimBytes && a != ir.PrimBytes && refinesString(a)
}
