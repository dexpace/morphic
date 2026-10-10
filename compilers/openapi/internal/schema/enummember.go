package schema

import (
	"encoding/base64"
	"encoding/json/jsontext"
	"strconv"
	"strings"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/ids"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/ir"
)

// enumMemberKey returns the key an enum member is identified by: a one-letter
// tag for the kind of value and the value itself, so the string "1" and the
// number 1 are two members.
//
// The key reads the value, never the name, so reordering renames nothing. The
// payload has % and # escaped, in that order, so the # that starts a repeat's
// suffix (memberID) cannot occur inside a value.
//
// ok is false for a list, object, reference or constructor: no enum holds one.
func enumMemberKey(v ir.Value) (key string, ok bool) {
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
		tag = "z"
	case ir.ValueList, ir.ValueObject, ir.ValueRefKind, ir.ValueCtor:
		return "", false
	default:
		return "", false
	}
	payload = strings.ReplaceAll(strings.ReplaceAll(payload, "%", "%25"), "#", "%23")
	return tag + ":" + payload, true
}

// memberID returns the ID of the occurrence-th member of the enum at pointer
// that holds key, counting from one. The first keeps the bare key; each repeat
// takes #2, #3 and so on, which cannot collide with a key because a key holds
// no unescaped #.
func memberID(pointer jsontext.Pointer, key string, occurrence int) ir.EnumMemberID {
	if occurrence > 1 {
		key += "#" + strconv.Itoa(occurrence)
	}
	return ids.EnumMember(pointer, key)
}

// memberLedger counts how often each key has been seen in one enum, so a repeat
// is numbered and reported against the member that first held the value.
type memberLedger struct {
	pointer jsontext.Pointer
	seen    map[string]int
	firstAt map[string]int
}

// newMemberLedger returns a ledger for the enum at pointer.
func newMemberLedger(pointer jsontext.Pointer) *memberLedger {
	return &memberLedger{pointer: pointer, seen: map[string]int{}, firstAt: map[string]int{}}
}

// next returns the ID of the member holding key at source index index, and the
// warning a repeated value raises at that entry's own pointer.
func (l *memberLedger) next(c lowering.Ctx, key string, index int) (ir.EnumMemberID, []ir.Diagnostic) {
	l.seen[key]++
	occurrence := l.seen[key]
	id := memberID(l.pointer, key, occurrence)
	if occurrence == 1 {
		l.firstAt[key] = index
		return id, nil
	}
	at := l.pointer + ids.Ptr("enum", strconv.Itoa(index))
	return id, []ir.Diagnostic{c.DiagAt(ir.SeverityWarning, diag.DuplicateEnumValue, at,
		"enum value repeats the member at index %d; every occurrence is kept, and this one takes the ID suffix #%d",
		l.firstAt[key], occurrence)}
}
