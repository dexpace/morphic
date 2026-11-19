// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package ir

import (
	"encoding/base64"
	"encoding/json/jsontext"
	"strconv"
)

// EnumMemberKey returns the key an enum member holding value is identified by
// (docs/ir/enums.md#member-identity): the name of the kind of value
// and the value itself, so the string "1" and the number 1 are two members. A
// number is keyed by its canonical decimal, so 10 and 1e1 are one value.
//
// ok is false for a list, object, reference, constructor or non-finite number:
// no enum holds one.
func EnumMemberKey(value Value) (key string, ok bool) {
	var tag, payload string
	switch value.Kind {
	case ValueString:
		tag, payload = "string", value.String
	case ValueSymbol:
		tag, payload = "symbol", value.String
	case ValueNumber:
		tag, payload = "number", numberKey(value.Number)
	case ValueBool:
		tag, payload = "bool", strconv.FormatBool(value.Bool)
	case ValueBytes:
		tag, payload = "bytes", base64.RawURLEncoding.EncodeToString(value.Bytes)
	case ValueNull:
		tag = "null"
	case ValueList, ValueObject, ValueRefKind, ValueConstructor, ValueInfinity, ValueNegativeInfinity, ValueNaN:
		return "", false
	default:
		return "", false
	}
	return tag + ":" + payload, true
}

// numberKey returns bigVal's canonical decimal, or bigVal as spelled when no Decimal holds
// it. Every canonical decimal reads as one, so the two never meet as one key.
func numberKey(bigVal BigVal) string {
	if decimal, err := bigVal.Decimal(); err == nil {
		return decimal.String()
	}
	return string(bigVal)
}

// EnumMemberIDFor returns the ID of the occurrence-th member, counting from one,
// of the enum identified by enum to hold the value key names
// (docs/ir/identity.md#enum-members): enum's namespace and path, then key as one
// RFC 6901 token, then the occurrence as one more token for a repeat. Every
// format derives a member's ID this way, from its enum's ID and its value, so a
// pass building an enum derives the IDs a compiler would.
//
// ok is false for an enum that is not a well-formed type ID and for an
// occurrence below one.
func EnumMemberIDFor(enum TypeID, key string, occurrence int) (EnumMemberID, bool) {
	rest, ok := idRest(IDKindType, string(enum))
	if !ok || occurrence < 1 {
		return "", false
	}
	path := jsontext.Pointer("").AppendToken(key)
	if occurrence > 1 {
		path = path.AppendToken(strconv.Itoa(occurrence))
	}
	return EnumMemberID(IDKindEnumMember + IDSeparator + rest + string(path)), true
}
