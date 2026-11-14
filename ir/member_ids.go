// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package ir

import (
	"encoding/base64"
	"encoding/json/jsontext"
	"strconv"
)

// EnumMemberKey returns the key an enum member holding v is identified by
// (docs/ir/enums.md#member-identity): a one-letter tag for the kind of value
// and the value itself, so the string "1" and the number 1 are two members. A
// number is keyed by its canonical decimal, so 10 and 1e1 are one value.
//
// ok is false for a list, object, reference, constructor or non-finite number:
// no enum holds one.
func EnumMemberKey(v Value) (key string, ok bool) {
	var tag, payload string
	switch v.Kind {
	case ValueString:
		tag, payload = "s", v.Str
	case ValueSymbol:
		tag, payload = "y", v.Str
	case ValueNumber:
		tag, payload = "n", numberKey(v.Num)
	case ValueBool:
		tag, payload = "b", strconv.FormatBool(v.Bool)
	case ValueBytes:
		tag, payload = "x", base64.RawURLEncoding.EncodeToString(v.Bytes)
	case ValueNull:
		tag = "z"
	case ValueList, ValueObject, ValueRefKind, ValueCtor, ValueInf, ValueNegInf, ValueNaN:
		return "", false
	default:
		return "", false
	}
	return tag + ":" + payload, true
}

// numberKey returns n's canonical decimal, or n as spelled when no Decimal holds
// it. Every canonical decimal reads as one, so the two never meet as one key.
func numberKey(n BigVal) string {
	if d, err := n.Decimal(); err == nil {
		return d.String()
	}
	return string(n)
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
