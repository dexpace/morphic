package schema

import (
	"encoding/base64"
	"encoding/json/jsontext"
	"strconv"

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
// Values compare by enumMemberKey, which keys member IDs too, so a repeat
// reported in an Enum is a member whose ID ends in its occurrence number. A list
// or object entry has no key and is never compared (#842).
func enumRepeats(c lowering.Ctx, nodes []values.Value, pointer jsontext.Pointer) []ir.Diagnostic {
	firstAt := make(map[string]int, len(nodes))
	var diags []ir.Diagnostic
	for i, node := range nodes {
		val, err := value.FromNode(node)
		if err != nil {
			continue
		}
		key, keyed := enumMemberKey(val)
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
