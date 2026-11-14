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
// Scalar that keeps the format, or the encoding encodedTable names for it.
//
// No row maps to its bare type's kind, which would leave the format nowhere
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

// Encoded is a (type, format) pairing whose value is of a primitive other than
// the JSON type it is written on, in a text other than that primitive's own.
type Encoded struct {
	// Prim is the primitive the value is.
	Prim ir.PrimKind
	// Encoding names the text, as docs/ir/constraints.md#encodings does.
	Encoding string
	// Wire is the primitive the value travels as, or "" where Prim's own
	// spelling says it, as a datetime_offset travels as a string.
	Wire ir.PrimKind
}

// encodedTable maps a "type/format" key to the pairing it selects
// (docs/compilers/openapi.md#string-formats,
// docs/compilers/openapi.md#numeric-formats). No key is also formatTable's
// (TestEncodedTable_IsApartFromTheFormatTable).
//
// unixtime is named unixTimestamp, the IR's name for seconds since the epoch,
// since a consumer keys a row by that name and never reads which format a
// document came from. Its Wire is the JSON type it is written on, which tells
// whole seconds from a fraction or a decimal text.
var encodedTable = map[string]Encoded{
	"string/byte":      {Prim: ir.PrimBytes, Encoding: "base64", Wire: ir.PrimString},
	"string/base64url": {Prim: ir.PrimBytes, Encoding: "base64url", Wire: ir.PrimString},
	"string/http-date": {Prim: ir.PrimDatetimeOffset, Encoding: "http-date"},
	"integer/unixtime": {Prim: ir.PrimDatetimeOffset, Encoding: "unixTimestamp", Wire: ir.PrimInteger},
	"number/unixtime":  {Prim: ir.PrimDatetimeOffset, Encoding: "unixTimestamp", Wire: ir.PrimNumber},
	"string/unixtime":  {Prim: ir.PrimDatetimeOffset, Encoding: "unixTimestamp", Wire: ir.PrimString},
}

// LookupEncoded returns the pairing a "type/format" key selects, and whether
// encodedTable has a row for it.
func LookupEncoded(key string) (Encoded, bool) {
	row, ok := encodedTable[key]
	return row, ok
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
