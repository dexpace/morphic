// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package typemap

import "github.com/dexpace/morphic/ir"

// Encoding is what a value travels under. The zero Encoding is none.
type Encoding struct {
	// Name is the encoding's name, "" for none.
	Name string
	// WireType is the primitive the value travels as, "" for its own.
	WireType ir.PrimitiveKind
}

// Found says how Lookup found its row.
type Found uint8

const (
	// Exact is the key's own row.
	Exact Found = iota
	// TravelsAs is the default row of the primitive the value travels as, where
	// that is not the key's primitive.
	TravelsAs
	// Primitive is the key primitive's own default row.
	Primitive
)

// Key returns the key a value of primitive is typed by under encoding: a
// string whose encoding has a name and a WireType of string is text to
// encode, keyed as bytes under that name; any other value is keyed by its own
// primitive and the encoding's name. So the key's primitive differs from
// primitive only for text to encode, which a target types as text and, where
// Lookup finds the key's own row, writes as its UTF-8 octets under the
// encoding.
func Key(primitive ir.PrimitiveKind, encoding Encoding) PrimitiveKey {
	if primitive == ir.PrimitiveString && encoding.Name != "" && encoding.WireType == ir.PrimitiveString {
		return PrimitiveKey{Primitive: ir.PrimitiveBytes, Encoding: encoding.Name}
	}
	return PrimitiveKey{Primitive: primitive, Encoding: encoding.Name}
}

// Lookup returns a copy of the row a value of primitive under encoding is
// typed by, and how it was found. It makes two probes: the row of Key's key,
// then the default row of the primitive the value travels as, which is the
// encoding's WireType where set, else string for a string-spelled key
// primitive under a named encoding, else the key's primitive. It reports false
// only where neither probe finds a row, with the zero Row and Found. It
// validates no kind: one ir does not declare finds what the table holds.
func Lookup(table Table, primitive ir.PrimitiveKind, encoding Encoding) (Row, Found, bool) {
	key := Key(primitive, encoding)
	if row, ok := table.Primitives[key.Primitive][key.Encoding]; ok {
		return copyRow(row), Exact, true
	}
	travelsAs := travelsAsPrimitive(key.Primitive, encoding)
	row, ok := table.Primitives[travelsAs][""]
	if !ok {
		return Row{}, Exact, false
	}
	if travelsAs != key.Primitive {
		return copyRow(row), TravelsAs, true
	}
	return copyRow(row), Primitive, true
}

// travelsAsPrimitive returns the primitive whose default row types a value of
// the key primitive under encoding when the key has no row of its own.
func travelsAsPrimitive(keyPrimitive ir.PrimitiveKind, encoding Encoding) ir.PrimitiveKind {
	switch {
	case encoding.WireType != "":
		return encoding.WireType
	case encoding.Name != "" && stringSpelled(keyPrimitive):
		return ir.PrimitiveString
	default:
		return keyPrimitive
	}
}

// stringSpelled reports whether a value of kind travels as a JSON string in
// its own spelling (docs/emitters/contract/type-table.md, "What a built-in row
// writes"), so an encoding naming another spelling leaves it text.
func stringSpelled(kind ir.PrimitiveKind) bool {
	switch kind {
	case ir.PrimitiveURL, ir.PrimitiveBytes, ir.PrimitiveUUID, ir.PrimitiveDate, ir.PrimitiveTime,
		ir.PrimitiveDatetime, ir.PrimitiveDatetimeOffset, ir.PrimitiveDuration:
		return true
	case ir.PrimitiveAny, ir.PrimitiveBool, ir.PrimitiveString, ir.PrimitiveInt8, ir.PrimitiveInt16,
		ir.PrimitiveInt32, ir.PrimitiveInt64, ir.PrimitiveUint8, ir.PrimitiveUint16, ir.PrimitiveUint32,
		ir.PrimitiveUint64, ir.PrimitiveInteger, ir.PrimitiveFloat32, ir.PrimitiveFloat64, ir.PrimitiveFloat,
		ir.PrimitiveNumber, ir.PrimitiveDecimal, ir.PrimitiveDecimal128:
		return false
	default:
		return false
	}
}

// LookupExternal returns a copy of the row an External of identity is typed
// by, and false with the zero Row where the table holds none.
func LookupExternal(table Table, identity string) (Row, bool) {
	row, ok := table.Externals[identity]
	if !ok {
		return Row{}, false
	}
	return copyRow(row), true
}
