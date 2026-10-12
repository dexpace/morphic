package schema

import (
	"encoding/base64"
	"encoding/json/jsontext"
	"slices"
	"strconv"
	"strings"

	"github.com/speakeasy-api/openapi/values"

	"github.com/dexpace/morphic/compilers/openapi/internal/annotation"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/ids"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/compilers/openapi/internal/value"
	"github.com/dexpace/morphic/ir"
)

// enumMemberKey returns the key an enum member is identified by: a one-letter
// tag for the kind of value and the value itself, so the string "1" and the
// number 1 are two members. A number is keyed by its canonical decimal
// (numberKey), so 10 and 1e1 are one value.
//
// The key reads the value, never the name or the position.
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
		tag, payload = "n", numberKey(v.Num)
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
	return tag + ":" + payload, true
}

// numberKey returns n's canonical decimal, or n as spelled outside that grammar.
// No canonical decimal is outside it, so the two never meet as one key.
func numberKey(n ir.BigVal) string {
	if canonical, ok := annotation.CanonicalDecimal(n); ok {
		return canonical
	}
	return string(n)
}

// enumRepeats reports each entry of the enum at pointer whose value an earlier
// entry already holds, at the repeat's own pointer. It reads the list as
// written, so a repeat is reported whether the set lowers to an Enum or
// degrades to a Union, and a repeated null is reported though a nullable enum
// keeps no null member.
//
// Values compare by valueKey, which keys a scalar by enumMemberKey as member
// IDs are keyed, so a repeat reported in an Enum is a member whose ID ends in
// its occurrence number.
func enumRepeats(c lowering.Ctx, nodes []values.Value, pointer jsontext.Pointer) []ir.Diagnostic {
	firstAt := make(map[string]int, len(nodes))
	var diags []ir.Diagnostic
	for i, node := range nodes {
		key, keyed := entryKey(node)
		if !keyed {
			continue
		}
		first, repeated := firstAt[key]
		if !repeated {
			firstAt[key] = i
			continue
		}
		diags = append(diags, c.DiagAt(ir.SeverityWarning, diag.DuplicateEnumValue,
			pointer+ids.Ptr("enum", strconv.Itoa(i)),
			"enum value repeats the entry at index %d; every occurrence is kept", first))
	}
	return diags
}

// entryKey returns the key the enum entry node is compared by (valueKey), or
// false for an entry with none: one value.FromNode cannot read, or one it reads
// to a value no key is built for.
func entryKey(node values.Value) (string, bool) {
	val, err := value.FromNode(node)
	if err != nil {
		return "", false
	}
	return valueKey(val, 0)
}

// maxValueKeyDepth bounds valueKey's descent into nested lists and objects. A
// value is read from the source no deeper than value.FromNode's own cap, so a
// value this deep is not one an enum holds.
const maxValueKeyDepth = 128

// valueKey returns the key an enum entry is compared by. A scalar's is its
// enumMemberKey. A list's and an object's are built from their elements' keys,
// each prefixed with its length (lengthPrefixed), and an object's members are
// sorted first, since JSON Schema compares instances regardless of member
// order. A number at any depth reads as its canonical decimal, so [1] repeats
// [1.0].
//
// ok is false for a reference or a constructor, which no source enum holds, and
// for a value nested past maxValueKeyDepth.
func valueKey(v ir.Value, depth int) (key string, ok bool) {
	if depth > maxValueKeyDepth {
		return "", false
	}
	switch v.Kind {
	case ir.ValueString, ir.ValueSymbol, ir.ValueNumber, ir.ValueBool, ir.ValueBytes, ir.ValueNull:
		return enumMemberKey(v)
	case ir.ValueList:
		return listKey(v.List, depth)
	case ir.ValueObject:
		return objectKey(v.Object, depth)
	case ir.ValueRefKind, ir.ValueCtor:
		return "", false
	default:
		return "", false
	}
}

// listKey returns the key of a list at depth: "l", then each element's key in
// order.
func listKey(elems []ir.Value, depth int) (string, bool) {
	var b strings.Builder
	b.WriteString("l")
	for _, e := range elems {
		k, ok := valueKey(e, depth+1)
		if !ok {
			return "", false
		}
		b.WriteString(lengthPrefixed(k))
	}
	return b.String(), true
}

// objectKey returns the key of an object at depth: "o", then each member's name
// and its value's key, the members sorted so their order does not count.
func objectKey(fields []ir.Field, depth int) (string, bool) {
	members := make([]string, 0, len(fields))
	for _, f := range fields {
		k, ok := valueKey(f.Value, depth+1)
		if !ok {
			return "", false
		}
		members = append(members, lengthPrefixed(f.Name)+lengthPrefixed(k))
	}
	slices.Sort(members)
	return "o" + strings.Join(members, ""), true
}

// lengthPrefixed writes s after its length, so a sequence of them reads back
// one way: no two sequences share a spelling. Unlike quoting, it adds the same
// few bytes at every depth, so a key grows with the value, not with its depth.
func lengthPrefixed(s string) string {
	return strconv.Itoa(len(s)) + ":" + s
}
