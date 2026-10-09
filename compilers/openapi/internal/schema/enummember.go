package schema

import (
	"encoding/base64"
	"strconv"
	"strings"

	"github.com/dexpace/morphic/compilers/openapi/internal/ids"
	"github.com/dexpace/morphic/ir"
)

// occurrenceMark introduces the tie-break suffix of a repeated value's ID.
// memberKeyEscaper keeps it, and the escape it needs, out of a payload, so a
// suffix is never mistaken for part of a value.
const occurrenceMark = "#"

var memberKeyEscaper = strings.NewReplacer("%", "%25", occurrenceMark, "%23")

// enumMemberKey encodes a member's value as one ID path segment, or reports
// false for a kind with no place in an Enum.
//
// The encoding is kind-tagged and injective: 1 and "1" differ, and a symbol
// differs from a string of the same text. Numbers use the canonical BigVal
// decimal and bytes the unpadded URL alphabet, which holds no "/".
func enumMemberKey(v ir.Value) (string, bool) {
	var tag, payload string
	switch v.Kind {
	case ir.ValueString:
		tag, payload = "s", v.Str
	case ir.ValueSymbol:
		tag, payload = "y", v.Str
	case ir.ValueNumber:
		tag, payload = "n", string(v.Num)
	case ir.ValueBool:
		tag, payload = "b", strconv.FormatBool(v.Bool)
	case ir.ValueBytes:
		tag, payload = "x", base64.RawURLEncoding.EncodeToString(v.Bytes)
	case ir.ValueNull:
		// Unreachable today: enumMembers drops or refuses null before it asks.
		tag = "z"
	case ir.ValueList, ir.ValueObject, ir.ValueRefKind, ir.ValueCtor:
		return "", false
	default:
		return "", false
	}
	return ir.EscapeIDSegment(tag + ":" + memberKeyEscaper.Replace(payload)), true
}

// memberID returns the ID of the occurrence-th member (1 for the first) keyed
// key within the enum. The first keeps the bare ID; each repeat takes "#n".
func memberID(enum ir.TypeID, key string, occurrence int) ir.EnumMemberID {
	if occurrence > 1 {
		key += occurrenceMark + strconv.Itoa(occurrence)
	}
	return ids.Member(enum, key)
}
