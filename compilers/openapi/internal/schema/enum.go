// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package schema

import (
	"encoding/base64"
	"encoding/json/jsontext"
	"slices"
	"strconv"
	"strings"

	"github.com/speakeasy-api/openapi/jsonschema/oas3"
	"github.com/speakeasy-api/openapi/values"

	"github.com/dexpace/morphic/compilers/compile"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/ids"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/compilers/openapi/internal/merge"
	"github.com/dexpace/morphic/compilers/openapi/internal/value"
	"github.com/dexpace/morphic/ir"
)

// lowerEnum hoists a schema with `enum` as a closed Enum
// (docs/compilers/openapi.md#enum-const). A heterogeneous or non-scalar member
// set becomes a Union of Literals over the members, with an info diagnostic;
// an empty list takes emptyEnum.
//
// A member count past the budget lowers as the top type with an error
// diagnostic, checked first because both paths cost per member, amplifying a
// small source (GitHub #75).
//
// A `null` entry is neither member nor variant but the reference's Nullable
// bit, which schemaAdmitsNull decides at every site (enumEntries), so a list
// of null alone is an Enum with no member.
func lowerEnum(c lowering.Ctx, ts *compile.Types, s *oas3.Schema, pointer jsontext.Pointer) (ir.TypeID, []ir.Diagnostic) {
	var diags []ir.Diagnostic
	id := internNode(c, ts, pointer, func(common ir.TypeCommon) ir.TypeDef {
		// The degenerate list first, then the budget. They cannot both hold — a
		// member count of zero exceeds no positive budget — so the order decides
		// nothing, but reading the empty case beside the members it lacks is
		// clearer than reading it after a bound on how many there may be.
		if len(s.GetEnum()) == 0 {
			def, emptyDiags := emptyEnum(c, s, common, pointer)
			diags = append(diags, emptyDiags...)
			return def
		}
		if n := len(s.GetEnum()); !c.Limits.MaxEnumMembers.Admits(n) {
			diags = append(diags, c.DiagAt(ir.SeverityError, diag.BudgetExceeded, pointer,
				"enum declares %d members, past the %d-member budget; lowered as any",
				n, c.Limits.MaxEnumMembers.Cap()))
			return &ir.Any{TypeCommon: common}
		}
		entries := readEnum(s.GetEnum())
		diags = append(diags, enumRepeats(c, entries, pointer)...)
		kept, keptDiags := enumEntries(c, s, entries, pointer)
		diags = append(diags, keptDiags...)
		members, memberPrim, ok := enumMembers(kept, common.ID)
		if !ok {
			def, enumDiags := enumAsUnion(c, ts, s, common, kept, pointer)
			diags = append(diags, enumDiags...)
			return def
		}
		return &ir.Enum{
			TypeCommon: common,
			ValueType:  enumValueType(s, members, memberPrim),
			Members:    members,
			Closed:     true,
		}
	})
	return id, diags
}

// enumEntry is one entry of an enum as written: its node, its index in the
// list, and the value it converts to, or why it converts to none.
type enumEntry struct {
	node  values.Value
	err   error
	val   ir.Value
	index int
}

// readEnum converts each entry of an enum once, for every reader of the list.
func readEnum(nodes []values.Value) []enumEntry {
	out := make([]enumEntry, 0, len(nodes))
	for i, node := range nodes {
		val, err := value.FromNode(node)
		out = append(out, enumEntry{node: node, err: err, val: val, index: i})
	}
	return out
}

// isNull reports whether e converts to the null value.
func (e enumEntry) isNull() bool { return e.err == nil && e.val.Kind == ir.ValueNull }

// enumEntries returns the entries of s's enum that a member or a variant holds:
// each but a null, which is the reference's (lowerEnum). Two kinds of entry
// admit no instance, since JSON Schema conjoins enum with the keywords beside
// it, and each is reported at itself as a warning: a null where those keywords
// exclude null, which nothing then holds, and a value no type s declares
// admits, a type of null included, which is kept as written beside the type
// the census keeps verbatim.
func enumEntries(c lowering.Ctx, s *oas3.Schema, entries []enumEntry, pointer jsontext.Pointer) ([]enumEntry, []ir.Diagnostic) {
	admitsNull := slices.ContainsFunc(entries, enumEntry.isNull) && schemaAdmitsNull(c, s)
	types := s.GetType()
	kept := make([]enumEntry, 0, len(entries))
	var diags []ir.Diagnostic
	for _, e := range entries {
		at := pointer + ids.Ptr("enum", strconv.Itoa(e.index))
		switch {
		case e.isNull():
			if !admitsNull {
				diags = append(diags, c.DiagAt(ir.SeverityWarning, diag.DegradedConstruct, at,
					"enum member null admits no instance: a keyword beside the enum excludes null, and JSON "+
						"Schema conjoins the two; the member is dropped, and the reference admits no null"))
			}
			continue
		case e.err == nil && !typesAdmit(types, e.val):
			diags = append(diags, c.DiagAt(ir.SeverityWarning, diag.DegradedConstruct, at,
				"enum member %s is no value of the declared type %s, which JSON Schema conjoins with enum, so no "+
					"instance matches it; the members are lowered as written", describeValue(e.val), typeList(types)))
		}
		kept = append(kept, e)
	}
	return kept, diags
}

// typesAdmit reports whether v is a value of one of types, read by the JSON
// kinds the admission reading gives a type and a value (kindOfType,
// kindOfValue). An empty list declares no type, and a type no schema defines
// judges nothing, which validation reports; either admits v.
func typesAdmit(types []oas3.SchemaType, v ir.Value) bool {
	var admitted kindSet
	for _, t := range types {
		k := kindOfType(t)
		if k == 0 {
			return true
		}
		admitted |= k
	}
	return len(types) == 0 || kindOfValue(v)&admitted != 0
}

// typeList spells a declared type set for a message: its one type, or the
// list.
func typeList(types []oas3.SchemaType) string {
	names := make([]string, 0, len(types))
	for _, t := range types {
		names = append(names, string(t))
	}
	if len(names) == 1 {
		return names[0]
	}
	return "[" + strings.Join(names, ", ") + "]"
}

// describeValue spells v for a message: a scalar as JSON writes it, and a
// value JSON writes no scalar for by its kind.
func describeValue(v ir.Value) string {
	switch v.Kind {
	case ir.ValueString, ir.ValueSymbol:
		return strconv.Quote(v.Str)
	case ir.ValueNumber:
		return string(v.Num)
	case ir.ValueBool:
		return strconv.FormatBool(v.Bool)
	case ir.ValueNull:
		return "null"
	case ir.ValueBytes, ir.ValueList, ir.ValueObject, ir.ValueRefKind, ir.ValueCtor,
		ir.ValueInf, ir.ValueNegInf, ir.ValueNaN:
		return "of kind " + string(v.Kind)
	default:
		return "of kind " + string(v.Kind)
	}
}

// emptyEnum lowers `enum: []`, a value space that accepts no instance, as a
// closed Enum with no member and one warning. The IR has no bottom type, but a
// closed Enum admits only its members, so this is exact.
//
// It avoids enumAsUnion, which would yield a Union with no variants: a node
// irverify rejects as ir/union-no-variants and no reader can act on (GitHub
// #318).
//
// ValueType is what the declaration selects, else the top type. Whether a
// reference admits null is not settled here: schemaAdmitsNull reads the type
// keyword at each use site (GitHub #288).
func emptyEnum(c lowering.Ctx, s *oas3.Schema, common ir.TypeCommon, pointer jsontext.Pointer) (ir.TypeDef, []ir.Diagnostic) {
	diags := []ir.Diagnostic{c.DiagAt(ir.SeverityWarning, diag.EmptyEnum, pointer,
		"enum declares no member, so this position accepts no value; lowered as a closed enum with no members")}
	return &ir.Enum{
		TypeCommon: common,
		ValueType:  enumValueType(s, nil, ir.PrimAny),
		Closed:     true,
	}, diags
}

// enumMembers converts the entries enumEntries kept into scalar members,
// reporting ok=false for an entry that converts to no value, a non-scalar
// member or members heterogeneous in kind. No entry kept is an Enum with no
// member, whose PrimKind is the top type's; otherwise the PrimKind is the one
// every member maps to, meaningful only when ok.
//
// Each member's ID is keyed by its value beneath enum, the enum's ID, and a
// repeat is kept, numbered; enumRepeats reports it. An ID no member fits under
// keeps none.
func enumMembers(entries []enumEntry, enum ir.TypeID) ([]ir.EnumMember, ir.PrimKind, bool) {
	members := make([]ir.EnumMember, 0, len(entries))
	occurrences := make(map[string]int, len(entries))
	prim := ir.PrimAny
	for i, e := range entries {
		if e.err != nil {
			return nil, "", false
		}
		memberPrim, text, admissible := enumMemberForm(e.val)
		key, keyed := ir.EnumMemberKey(e.val)
		occurrences[key]++
		id, placed := ir.EnumMemberIDFor(enum, key, occurrences[key])
		if !admissible || !keyed || !placed || (i > 0 && e.val.Kind != entries[0].val.Kind) {
			return nil, "", false
		}
		prim = memberPrim
		members = append(members, ir.EnumMember{
			ID:    id,
			Name:  ir.NamingFor(text),
			Value: e.val,
		})
	}
	return members, prim, true
}

// enumAsUnion lowers the entries of a heterogeneous or non-scalar enum that
// enumEntries kept to a Union of Literals, each hoisted at its entry's
// position, emitting one info diagnostic. The Union is exclusive where those
// entries are distinct: otherwise an instance can match two variants.
func enumAsUnion(c lowering.Ctx, ts *compile.Types, s *oas3.Schema, common ir.TypeCommon, entries []enumEntry, pointer jsontext.Pointer) (ir.TypeDef, []ir.Diagnostic) {
	diags := []ir.Diagnostic{c.DiagAt(ir.SeverityInfo, diag.DegradedConstruct, pointer,
		"heterogeneous or non-scalar enum lowered as a union of literals")}
	variants := make([]ir.Variant, 0, len(entries))
	for _, e := range entries {
		lptr := pointer + ids.Ptr("enum", strconv.Itoa(e.index))
		litID, litDiags := hoistLiteral(c, ts, s, e.node, lptr)
		diags = append(diags, litDiags...)
		variants = append(variants, ir.Variant{
			Name: compile.NamingHint(lowering.NameAt(c, lptr)),
			Type: ir.TypeRef{Target: litID},
		})
	}
	return &ir.Union{
		TypeCommon: common,
		Variants:   variants,
		Exclusive:  distinctEntries(entries),
		Closed:     true,
	}, diags
}

// lowerConst hoists s's const as a Literal of its value (hoistLiteral), but a
// null, which is no Literal's (nullConst), and reports, at the const, a value
// no type s declares admits, as enumEntries reports an enum member. The Literal
// holds the value as written, and the census keeps a type it does not fit
// verbatim.
func lowerConst(c lowering.Ctx, ts *compile.Types, s *oas3.Schema, pointer jsontext.Pointer) (ir.TypeID, []ir.Diagnostic) {
	val, err := value.FromNode(s.GetConst())
	if err == nil && val.Kind == ir.ValueNull {
		return nullConst(c, ts, s, pointer)
	}
	id, diags := hoistLiteral(c, ts, s, s.GetConst(), pointer)
	if err != nil {
		return id, diags // hoistLiteral reported it
	}
	if types := s.GetType(); !typesAdmit(types, val) {
		diags = append(diags, c.DiagAt(ir.SeverityWarning, diag.DegradedConstruct, pointer+ids.Ptr("const"),
			"const %s is no value of the declared type %s, which JSON Schema conjoins with const, so no instance "+
				"matches it; the literal holds the value as written", describeValue(val), typeList(types)))
	}
	return id, diags
}

// nullConst lowers `const: null` as `enum: [null]` lowers (lowerEnum), to the
// node of null alone (lowerNullAlone), so no Literal holds a null
// (docs/ir/containers.md#literals). Where a keyword beside the const excludes
// null, the position admits nothing, which is reported at the const as
// enumEntries reports such a member.
func nullConst(c lowering.Ctx, ts *compile.Types, s *oas3.Schema, pointer jsontext.Pointer) (ir.TypeID, []ir.Diagnostic) {
	id := lowerNullAlone(c, ts, s, pointer)
	if schemaAdmitsNull(c, s) {
		return id, nil
	}
	return id, []ir.Diagnostic{c.DiagAt(ir.SeverityWarning, diag.DegradedConstruct, pointer+ids.Ptr("const"),
		"const null admits no instance: a keyword beside the const excludes null, and JSON Schema "+
			"conjoins the two; lowered as an enum with no member, and the reference admits no null")}
}

// lowerNullAlone hoists at pointer the node s, a schema whose own keywords
// leave it no value but null, lowers to: a closed Enum with no member, the empty
// value space, which the nullable reference its position takes widens by null
// alone (docs/compilers/openapi.md#null-alone). Its ValueType is what s's type
// and format select, as an enum's is (enumValueType), the top type where they
// select none.
func lowerNullAlone(c lowering.Ctx, ts *compile.Types, s *oas3.Schema, pointer jsontext.Pointer) ir.TypeID {
	return internNode(c, ts, pointer, func(common ir.TypeCommon) ir.TypeDef {
		return &ir.Enum{TypeCommon: common, ValueType: enumValueType(s, nil, ir.PrimAny), Closed: true}
	})
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
func hoistLiteral(c lowering.Ctx, ts *compile.Types, s *oas3.Schema, node values.Value, pointer jsontext.Pointer,
) (ir.TypeID, []ir.Diagnostic) {
	// Captured from inside the build rather than reported around it, which keeps
	// the report tied to the node actually being constructed. Reporting eagerly
	// would be indistinguishable today — a second visit produces the identical
	// diagnostic, which Diags.Append drops — so nothing observable rests on this;
	// it is the honest place for it, not a load-bearing one.
	var diags []ir.Diagnostic
	id := internNode(c, ts, pointer, func(common ir.TypeCommon) ir.TypeDef {
		val, err := value.FromNode(node)
		if err != nil {
			diags = append(diags, c.DiagAt(ir.SeverityWarning, diag.DegradedConstruct, pointer,
				"unconvertible value lowered as the top type: %s", err.Error()))
			return &ir.Any{TypeCommon: common}
		}
		return &ir.Literal{TypeCommon: common, Value: val, ValueType: literalValueType(s, val)}
	})
	return id, diags
}

// literalValueType picks a Literal's ValueType as enumValueType picks an
// Enum's, for its one value: the primitive the declared type and format
// select, else the declared type's, where the value is one of it (declaredPrim);
// else the primitive the value's own kind declares, and none for a list or an
// object, which no primitive types. A refinement's principal is typed by
// every conjunct after (typeValueSet).
func literalValueType(s *oas3.Schema, val ir.Value) ir.PrimKind {
	if prim, ok := declaredPrim(s, func(p ir.PrimKind) bool { return merge.Admits(p, val) }); ok {
		return prim
	}

	prim, _, scalar := enumMemberForm(val)
	if !scalar {
		return ""
	}

	return prim
}

// enumValueType picks an Enum's ValueType as literalValueType picks a
// Literal's: the primitive the schema's declared type and format select, as a
// scalar's are read, else the declared type's, where every member is a value of
// it (declaredPrim); else the primitive its members classified to. A type or a
// format the members do not fit is one no target could back the enum with, so
// it stays verbatim. It does not re-derive memberPrim from a ValueKind:
// enumMemberForm decided it once, for the same members.
func enumValueType(s *oas3.Schema, members []ir.EnumMember, memberPrim ir.PrimKind) ir.PrimKind {
	if prim, ok := declaredPrim(s, func(p ir.PrimKind) bool { return enumMembersAre(members, p) }); ok {
		return prim
	}
	return memberPrim
}

// enumMembersAre reports whether every member's value is a value of prim.
func enumMembersAre(members []ir.EnumMember, prim ir.PrimKind) bool {
	for _, m := range members {
		if !merge.Admits(prim, m.Value) {
			return false
		}
	}
	return true
}

// enumMemberForm classifies one lowered value as an Enum member: the PrimKind
// its kind declares and the literal text that names the member. ok=false means
// the kind has no place in an Enum, and the caller degrades the whole enum to a
// Union of Literals with a diagnostic.
//
// Its switch has one arm per ir.ValueKind, as ir.EnumMemberKey's does; the
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
	case ir.ValueNull, ir.ValueList, ir.ValueObject, ir.ValueRefKind, ir.ValueCtor,
		ir.ValueInf, ir.ValueNegInf, ir.ValueNaN:
		// Composite, reference and null values are not enum members: none has a
		// primitive to declare or a literal text form to name a member by. Nor
		// is a non-finite number, which no OpenAPI value is.
		return "", "", false
	default:
		return "", "", false
	}
}

// enumRepeats reports each entry of the enum at pointer whose value an earlier
// entry already holds, at the repeat's own pointer. It reads the list as written,
// so a repeat is reported whether the set lowers to an Enum or a Union, and a
// repeated null though no member holds one. Values compare by valueKey, so a
// repeat in an Enum is a member whose ID ends in its occurrence number.
func enumRepeats(c lowering.Ctx, entries []enumEntry, pointer jsontext.Pointer) []ir.Diagnostic {
	firstAt := make(map[string]int, len(entries))
	var diags []ir.Diagnostic
	for _, e := range entries {
		key, keyed := e.key()
		if !keyed {
			continue
		}
		first, repeated := firstAt[key]
		if !repeated {
			firstAt[key] = e.index
			continue
		}
		diags = append(diags, c.DiagAt(ir.SeverityWarning, diag.DuplicateEnumValue,
			pointer+ids.Ptr("enum", strconv.Itoa(e.index)),
			"enum value repeats the entry at index %d; every occurrence is kept", first))
	}
	return diags
}

// distinctEntries reports that no instance equals two of entries: no value
// repeats, and each has a key, since one with none may lower to the top type.
func distinctEntries(entries []enumEntry) bool {
	seen := make(map[string]bool, len(entries))
	for _, e := range entries {
		key, keyed := e.key()
		if !keyed || seen[key] {
			return false
		}
		seen[key] = true
	}
	return true
}

// key returns the key e is compared by (valueKey), or false for an entry with
// none: one value.FromNode cannot read, or one it reads to a value no key is
// built for.
func (e enumEntry) key() (string, bool) {
	if e.err != nil {
		return "", false
	}
	return valueKey(e.val, 0)
}

// maxValueKeyDepth bounds valueKey's descent into nested lists and objects. A
// value is read from the source no deeper than value.FromNode's own cap, so a
// value this deep is not one an enum holds.
const maxValueKeyDepth = 128

// valueKey returns the key an enum entry is compared by. A scalar's is its
// ir.EnumMemberKey. A list's and an object's are built from their elements'
// keys, each prefixed with its length (lengthPrefixed), and an object's members
// are sorted first, since JSON Schema compares instances regardless of member
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
		return ir.EnumMemberKey(v)
	case ir.ValueList:
		return listKey(v.List, depth)
	case ir.ValueObject:
		return objectKey(v.Object, depth)
	case ir.ValueRefKind, ir.ValueCtor, ir.ValueInf, ir.ValueNegInf, ir.ValueNaN:
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
