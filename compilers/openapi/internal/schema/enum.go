package schema

import (
	"encoding/base64"
	"encoding/json/jsontext"
	"slices"
	"strconv"
	"strings"

	oas3 "github.com/speakeasy-api/openapi/jsonschema/oas3"
	"github.com/speakeasy-api/openapi/values"

	"github.com/dexpace/morphic/compilers/compile"
	"github.com/dexpace/morphic/compilers/openapi/internal/annotation"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/ids"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/compilers/openapi/internal/value"
	"github.com/dexpace/morphic/ir"
)

// lowerEnum hoists a schema with `enum` as a closed Enum. A heterogeneous or
// non-scalar member set becomes a Union of Literals over the members, `null`
// included, with an info diagnostic; an empty list takes emptyEnum.
//
// A member count past the budget lowers as the top type with an error
// diagnostic, checked first because both paths cost per member, amplifying a
// small source (GitHub #75).
//
// A listed `null` member is stripped onto the reference's Nullable bit
// (ir-design §3.3) when schemaAdmitsNull holds, the predicate every Nullable
// site shares. A sibling `type: string` forbids that null, so it stays and the
// set becomes a Union.
func lowerEnum(c lowering.Ctx, ts *compile.Types, s *oas3.Schema, pointer jsontext.Pointer, hint string) (ir.TypeID, []ir.Diagnostic) {
	var diags []ir.Diagnostic
	id := internNode(c, ts, pointer, hint, func(common ir.TypeCommon) ir.TypeDef {
		// The degenerate list first, then the budget. They cannot both hold — a
		// member count of zero exceeds no positive budget — so the order decides
		// nothing, but reading the empty case beside the members it lacks is
		// clearer than reading it after a bound on how many there may be.
		if len(s.GetEnum()) == 0 {
			def, emptyDiags := emptyEnum(c, s, common, pointer)
			diags = append(diags, emptyDiags...)
			return def
		}
		if n := len(s.GetEnum()); c.Limits.EnumMembersExceeded(n) {
			diags = append(diags, c.DiagAt(ir.SeverityError, diag.BudgetExceeded, pointer,
				"enum declares %d members, past the %d-member budget; lowered as any",
				n, c.Limits.MaxEnumMembers))
			return &ir.Any{TypeCommon: common}
		}
		diags = append(diags, enumRepeats(c, s.GetEnum(), pointer)...)
		members, memberPrim, ok := enumMembers(s.GetEnum(), schemaAdmitsNull(c, s), c.IDs(), pointer)
		if !ok {
			def, enumDiags := enumAsUnion(c, ts, s, common, pointer, hint)
			diags = append(diags, enumDiags...)
			return def
		}
		return &ir.Enum{
			TypeCommon: common,
			ValueType:  enumValueType(s, memberPrim),
			Members:    members,
			Closed:     true,
		}
	})
	return id, diags
}

// emptyEnum lowers `enum: []`, a value space that accepts no instance, as a
// closed Enum with no member and one warning. The IR has no bottom type, but a
// closed Enum admits only its members, so this is exact.
//
// It avoids enumAsUnion, which would yield a Union with no variants: a node
// nothing rejects and no reader can act on (GitHub #318).
//
// ValueType is the declared scalar type, else the top type. Whether a
// reference admits null is not settled here: schemaAdmitsNull reads the type
// keyword at each use site (GitHub #288).
func emptyEnum(c lowering.Ctx, s *oas3.Schema, common ir.TypeCommon, pointer jsontext.Pointer) (ir.TypeDef, []ir.Diagnostic) {
	diags := []ir.Diagnostic{c.DiagAt(ir.SeverityWarning, diag.EmptyEnum, pointer,
		"enum declares no member, so this position accepts no value; lowered as a closed enum with no members")}
	return &ir.Enum{
		TypeCommon: common,
		ValueType:  enumValueType(s, ir.PrimAny),
		Closed:     true,
	}, diags
}

// enumMembers converts enum nodes into scalar members, reporting ok=false for a
// non-scalar member, members heterogeneous in kind, or a set that keeps none
// (an all-null set degrades rather than becoming a memberless Enum).
//
// dropNull skips `null` members, for a schema whose nullability the enclosing
// reference carries (see lowerEnum). Kind agreement is read off the members
// kept. The PrimKind is the one every kept member maps to, meaningful only when
// ok.
//
// Each member's ID is keyed by its value under the enum at pointer, and a
// repeated value is kept, its ID numbered by occurrence; enumRepeats reports it.
func enumMembers(nodes []values.Value, dropNull bool, d ids.Document, pointer jsontext.Pointer) ([]ir.EnumMember, ir.PrimKind, bool) {
	members := make([]ir.EnumMember, 0, len(nodes))
	occurrences := make(map[string]int, len(nodes))
	var kind ir.ValueKind
	var prim ir.PrimKind
	for _, node := range nodes {
		val, err := value.FromNode(node)
		if err != nil {
			return nil, "", false
		}
		if dropNull && val.Kind == ir.ValueNull {
			continue
		}
		memberPrim, text, admissible := enumMemberForm(val)
		key, keyed := enumMemberKey(val)
		if !admissible || !keyed {
			return nil, "", false
		}
		if len(members) == 0 {
			kind, prim = val.Kind, memberPrim
		} else if val.Kind != kind {
			return nil, "", false
		}
		occurrences[key]++
		members = append(members, ir.EnumMember{
			ID:    d.EnumMember(pointer, key, occurrences[key]),
			Name:  compile.NamingFor(text),
			Value: val,
		})
	}
	if len(members) == 0 {
		return nil, "", false
	}
	return members, prim, true
}

// enumAsUnion lowers a heterogeneous or non-scalar enum to an exclusive Union of
// hoisted Literals, emitting one info diagnostic.
func enumAsUnion(c lowering.Ctx, ts *compile.Types, s *oas3.Schema, common ir.TypeCommon, pointer jsontext.Pointer, hint string) (ir.TypeDef, []ir.Diagnostic) {
	diags := []ir.Diagnostic{c.DiagAt(ir.SeverityInfo, diag.DegradedConstruct, pointer,
		"heterogeneous or non-scalar enum lowered as a union of literals")}
	nodes := s.GetEnum()
	variants := make([]ir.Variant, 0, len(nodes))
	for i, node := range nodes {
		vh := compile.SubHint(hint, strconv.Itoa(i))
		lptr := pointer + ids.Ptr("enum", strconv.Itoa(i))
		litID, litDiags := hoistLiteral(c, ts, node, lptr, vh)
		diags = append(diags, litDiags...)
		variants = append(variants, ir.Variant{
			Name: compile.NamingHint(vh),
			Type: ir.TypeRef{Target: litID},
		})
	}
	return &ir.Union{
		TypeCommon: common,
		Variants:   variants,
		Exclusive:  true,
	}, diags
}

// hoistLiteral hoists a single node as a Literal type at its own pointer, or
// falls back to the schemaless top type when the node is structurally
// unconvertible — never a Literal whose Value lies about being null. It is
// the single entry point for lowering both a bare `const` schema (pointer may
// be a top-level component, so internNode's ids.ForPointer keeps that
// component's stable named ID) and each individual member of a heterogeneous
// enum (enumAsUnion, always an anonymous sub-pointer).
//
// It returns the interned ID, plus the diagnostic an unconvertible node
// produces — none when the node converts.
func hoistLiteral(c lowering.Ctx, ts *compile.Types, node values.Value, pointer jsontext.Pointer, hint string,
) (ir.TypeID, []ir.Diagnostic) {
	// Captured from inside the build rather than reported around it, which keeps
	// the report tied to the node actually being constructed. Reporting eagerly
	// would be indistinguishable today — a second visit produces the identical
	// diagnostic, which Diags.Append drops — so nothing observable rests on this;
	// it is the honest place for it, not a load-bearing one.
	var diags []ir.Diagnostic
	id := internNode(c, ts, pointer, hint, func(common ir.TypeCommon) ir.TypeDef {
		val, err := value.FromNode(node)
		if err != nil {
			diags = append(diags, c.DiagAt(ir.SeverityWarning, diag.DegradedConstruct, pointer,
				"unconvertible value lowered as the top type: %s", err.Error()))
			return &ir.Any{TypeCommon: common}
		}
		return &ir.Literal{TypeCommon: common, Value: val}
	})
	return id, diags
}

// enumValueType picks an Enum's ValueType from the schema's declared scalar
// type, falling back to the primitive its members classified to. It does not
// re-derive that primitive from a ValueKind: enumMemberForm decided it once,
// for the same members, so there is nothing here to disagree with.
func enumValueType(s *oas3.Schema, memberPrim ir.PrimKind) ir.PrimKind {
	if types := effectiveTypes(s); len(types) == 1 {
		switch types[0] {
		case oas3.SchemaTypeString:
			return ir.PrimString
		case oas3.SchemaTypeInteger:
			return ir.PrimInteger
		case oas3.SchemaTypeNumber:
			return ir.PrimNumber
		case oas3.SchemaTypeBoolean:
			return ir.PrimBool
		}
	}
	return memberPrim
}

// enumMemberForm classifies one lowered value as an Enum member: the PrimKind
// its kind declares and the literal text that names the member. ok=false means
// the kind has no place in an Enum, and the caller degrades the whole enum to a
// Union of Literals with a diagnostic.
//
// Its switch has one arm per ir.ValueKind, as enumMemberKey's does; the
// exhaustive linter fails when ir adds a kind either lacks. The default arm
// refuses a kind rather than assert a type the source never wrote.
func enumMemberForm(v ir.Value) (ir.PrimKind, string, bool) {
	switch v.Kind {
	case ir.ValueString:
		return ir.PrimString, v.Str, true
	case ir.ValueSymbol:
		// An interned symbol has no primitive of its own; PrimString is the form
		// it takes on a JSON wire. Unreachable from OpenAPI, whose values come
		// from YAML nodes, but ValueKind is IR-wide and this is its enum home.
		return ir.PrimString, v.Str, true
	case ir.ValueNumber:
		return ir.PrimNumber, string(v.Num), true
	case ir.ValueBool:
		return ir.PrimBool, strconv.FormatBool(v.Bool), true
	case ir.ValueBytes:
		return ir.PrimBytes, base64.StdEncoding.EncodeToString(v.Bytes), true
	case ir.ValueNull, ir.ValueList, ir.ValueObject, ir.ValueRefKind, ir.ValueCtor:
		// Composite, reference and null values are not enum members: none has a
		// primitive to declare or a literal text form to name a member by.
		return "", "", false
	default:
		return "", "", false
	}
}

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
