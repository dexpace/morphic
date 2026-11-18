// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package schema

import (
	"encoding/base64"
	"encoding/json/jsontext"
	"slices"
	"strconv"
	"strings"

	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers/compile"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/ids"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/compilers/openapi/internal/merge"
	"github.com/dexpace/morphic/compilers/openapi/internal/spelling"
	"github.com/dexpace/morphic/compilers/openapi/internal/statement"
	"github.com/dexpace/morphic/compilers/openapi/internal/value"
	"github.com/dexpace/morphic/ir"
)

// lowerEnum hoists the `enum` site's schema writes as a closed Enum at the
// site's position (docs/compilers/openapi.md#enum-const). A heterogeneous or
// non-scalar member set becomes a Union of Literals over the members, with an
// info diagnostic; an empty list takes emptyEnum.
//
// A member count past the budget lowers as the top type with an error
// diagnostic, checked first because both paths cost per member, amplifying a
// small source (GitHub #75).
//
// A `null` entry is neither member nor variant but the reference's Nullable
// bit, which schemaAdmitsNull decides at every site (enumEntries), so a list
// of null alone is an Enum with no member.
func lowerEnum(c lowering.Ctx, ts *compile.Types, site valueSetSite) (ir.TypeID, []ir.Diagnostic) {
	s, written := site.set, site.written
	members := enumIn(s)
	var diags []ir.Diagnostic
	build := func(common ir.TypeCommon) ir.TypeDef {
		// The degenerate list first, then the budget. They cannot both hold — a
		// member count of zero exceeds no positive budget — so the order decides
		// nothing, but reading the empty case beside the members it lacks is
		// clearer than reading it after a bound on how many there may be.
		if len(members) == 0 {
			def, emptyDiags := emptyEnum(c, site, common)
			diags = append(diags, emptyDiags...)
			return def
		}
		if n := len(members); !c.Limits.MaxEnumMembers.Admits(n) {
			diags = append(diags, c.DiagAt(ir.SeverityError, diag.BudgetExceeded, written,
				"enum declares %d members, past the %d-member budget; lowered as any",
				n, c.Limits.MaxEnumMembers.Cap()))
			return &ir.Any{TypeCommon: common}
		}
		entries := readEnum(members)
		diags = append(diags, enumRepeats(c, entries, written)...)
		kept, keptDiags := enumEntries(c, s, entries, written)
		diags = append(diags, keptDiags...)
		members, memberPrim, ok := enumMembers(kept, common.ID)
		if !ok {
			def, enumDiags := enumAsUnion(c, ts, site, common, kept)
			diags = append(diags, enumDiags...)
			return def
		}
		return &ir.Enum{
			TypeCommon: common,
			ValueType:  site.enumType(c, func(p ir.PrimKind) bool { return enumMembersAre(members, p) }, memberPrim),
			Members:    members,
			Closed:     true,
		}
	}
	id := internNode(c, ts, site.pointer, func(common ir.TypeCommon) (ir.TypeDef, keySet) {
		td := build(common)
		return td, valueSetReads(site.set, td)
	}, site.finish)
	return id, diags
}

// enumEntry is one entry of an enum as written: its node, its index in the
// list, and the value it converts to, or why it converts to none.
type enumEntry struct {
	node  *yaml.Node
	err   error
	val   ir.Value
	index int
}

// readEnum converts each entry of an enum once, for every reader of the list.
func readEnum(nodes []*yaml.Node) []enumEntry {
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
func enumEntries(c lowering.Ctx, s *statement.Schema, entries []enumEntry, pointer jsontext.Pointer) ([]enumEntry, []ir.Diagnostic) {
	admitsNull := slices.ContainsFunc(entries, enumEntry.isNull) && schemaAdmitsNull(c, s)
	types := kindsIn(s)
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
func typesAdmit(types []statement.Kind, v ir.Value) bool {
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
func typeList(types []statement.Kind) string {
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
func emptyEnum(c lowering.Ctx, site valueSetSite, common ir.TypeCommon) (ir.TypeDef, []ir.Diagnostic) {
	diags := []ir.Diagnostic{c.DiagAt(ir.SeverityWarning, diag.EmptyEnum, site.written,
		"enum declares no member, so this position accepts no value; lowered as a closed enum with no members")}
	return &ir.Enum{
		TypeCommon: common,
		ValueType:  site.enumType(c, admitsEvery, ir.PrimAny),
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
		memberPrim, text, admissible := enumMemberOf(entries, i)
		if !admissible {
			return nil, "", false
		}
		key, keyed := ir.EnumMemberKey(e.val)
		occurrences[key]++
		id, placed := ir.EnumMemberIDFor(enum, key, occurrences[key])
		if !keyed || !placed {
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

// enumMemberOf classifies the entry at index i of entries as an Enum member, as
// enumMemberForm does its value, and refuses one converting to no value or of
// another kind than the first, which no Enum holds beside it.
func enumMemberOf(entries []enumEntry, i int) (ir.PrimKind, string, bool) {
	e := entries[i]
	if e.err != nil || i > 0 && e.val.Kind != entries[0].val.Kind {
		return "", "", false
	}
	return enumMemberForm(e.val)
}

// enumMemberPrim is the PrimKind enumMembers gives entries, and false where it
// refuses one (enumMemberOf), read with no member built: a value
// enumMemberForm admits always has a key, and a type's ID always places one. A
// position asking only its ValueType names no member (enumSetType).
func enumMemberPrim(entries []enumEntry) (ir.PrimKind, bool) {
	prim := ir.PrimAny
	for i := range entries {
		memberPrim, _, admissible := enumMemberOf(entries, i)
		if !admissible {
			return "", false
		}
		prim = memberPrim
	}
	return prim, true
}

// entriesAre reports whether every entry's value is a value of prim, as
// enumMembersAre asks of the members they lower to.
func entriesAre(entries []enumEntry, prim ir.PrimKind) bool {
	for _, e := range entries {
		if !merge.Admits(prim, e.val) {
			return false
		}
	}
	return true
}

// enumAsUnion lowers the entries of a heterogeneous or non-scalar enum that
// enumEntries kept to a Union of Literals, each hoisted at the pointer its
// entry is written at, emitting one info diagnostic. The Union is exclusive where those
// entries are distinct: otherwise an instance can match two variants.
func enumAsUnion(c lowering.Ctx, ts *compile.Types, site valueSetSite, common ir.TypeCommon, entries []enumEntry) (ir.TypeDef, []ir.Diagnostic) {
	diags := []ir.Diagnostic{c.DiagAt(ir.SeverityInfo, diag.DegradedConstruct, site.written,
		"heterogeneous or non-scalar enum lowered as a union of literals")}
	variants := make([]ir.Variant, 0, len(entries))
	for _, e := range entries {
		lptr := site.written + ids.Ptr("enum", strconv.Itoa(e.index))
		litID, litDiags := hoistLiteral(c, ts, site.memberSite(lptr), e.node)
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

// lowerConst hoists the const site's schema writes as a Literal of its value
// (hoistLiteral), but a null, which is no Literal's (nullConst), and reports,
// at the const, a value no type that schema declares admits, as enumEntries reports an enum member. The Literal
// holds the value as written, and the census keeps a type it does not fit
// verbatim.
func lowerConst(c lowering.Ctx, ts *compile.Types, site valueSetSite) (ir.TypeID, []ir.Diagnostic) {
	constant := constIn(site.set)
	val, err := value.FromNode(constant)
	if err == nil && val.Kind == ir.ValueNull {
		return nullConst(c, ts, site)
	}
	id, diags := hoistLiteral(c, ts, site, constant)
	if err != nil {
		return id, diags // hoistLiteral reported it
	}
	if types := kindsIn(site.set); !typesAdmit(types, val) {
		diags = append(diags, c.DiagAt(ir.SeverityWarning, diag.DegradedConstruct, site.written+ids.Ptr("const"),
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
func nullConst(c lowering.Ctx, ts *compile.Types, site valueSetSite) (ir.TypeID, []ir.Diagnostic) {
	id := lowerNullAlone(c, ts, site)
	if schemaAdmitsNull(c, site.set) {
		return id, nil
	}
	return id, []ir.Diagnostic{c.DiagAt(ir.SeverityWarning, diag.DegradedConstruct, site.written+ids.Ptr("const"),
		"const null admits no instance: a keyword beside the const excludes null, and JSON Schema "+
			"conjoins the two; lowered as an enum with no member, and the reference admits no null")}
}

// lowerNullAlone hoists at the site's position the node its schema, whose own
// keywords leave it no value but null, lowers to: a closed Enum with no member,
// the empty value space, which the nullable reference its position takes widens
// by null alone (docs/compilers/openapi.md#null-alone). Its ValueType is what
// that schema's type and format select, as an enum's is (enumValueType), the
// top type where they select none, narrowed as the site types it.
func lowerNullAlone(c lowering.Ctx, ts *compile.Types, site valueSetSite) ir.TypeID {
	return internNode(c, ts, site.pointer, func(common ir.TypeCommon) (ir.TypeDef, keySet) {
		td := &ir.Enum{TypeCommon: common, ValueType: site.enumType(c, admitsEvery, ir.PrimAny), Closed: true}
		return td, valueSetReads(site.set, td)
	}, site.finish)
}

// hoistLiteral hoists a single node as a Literal type at the site's position,
// or falls back to the schemaless top type when the node is structurally
// unconvertible — never a Literal whose Value lies about being null. It is
// the single entry point for lowering both a bare `const` schema (the position
// may be a top-level component, so internNode's ids.ForPointer keeps that
// component's stable named ID) and each individual member of a heterogeneous
// enum (enumAsUnion, always an anonymous sub-pointer).
//
// It returns the interned ID, plus the diagnostic an unconvertible node
// produces — none when the node converts.
func hoistLiteral(c lowering.Ctx, ts *compile.Types, site valueSetSite, node *yaml.Node) (ir.TypeID, []ir.Diagnostic) {
	// Captured from inside the build rather than reported around it, which keeps
	// the report tied to the node actually being constructed. Reporting eagerly
	// would be indistinguishable today — a second visit produces the identical
	// diagnostic, which Diags.Append drops — so nothing observable rests on this;
	// it is the honest place for it, not a load-bearing one.
	var diags []ir.Diagnostic
	id := internNode(c, ts, site.pointer, func(common ir.TypeCommon) (ir.TypeDef, keySet) {
		val, err := value.FromNode(node)
		if err != nil {
			diags = append(diags, c.DiagAt(ir.SeverityWarning, diag.DegradedConstruct, site.written,
				"unconvertible value lowered as the top type: %s", err.Error()))
			top := &ir.Any{TypeCommon: common}
			return top, valueSetReads(site.set, top)
		}
		td := &ir.Literal{TypeCommon: common, Value: val, ValueType: site.literalType(c, val)}
		return td, valueSetReads(site.set, td)
	}, site.finish)
	return id, diags
}

// literalValueType picks a Literal's ValueType as enumValueType picks an
// Enum's, for its one value: the primitive the declared type and format
// select, else the declared type's, where the value is one of it (declaredPrim);
// else the primitive the value's own kind declares, and none for a list or an
// object, which no primitive types. A refinement's value set is then typed
// by every conjunct (valueSetSite.valueType).
func literalValueType(s *statement.Schema, val ir.Value) ir.PrimKind {
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
func enumValueType(s *statement.Schema, admits func(ir.PrimKind) bool, memberPrim ir.PrimKind) ir.PrimKind {
	if prim, ok := declaredPrim(s, admits); ok {
		return prim
	}
	return memberPrim
}

// valueSetReads returns what the build of td, the node the value set set writes
// lowers to, read of set: the value set; the type where it says nothing td does
// not (valueSetTypeRead); and the format where td's ValueType is the primitive
// it pairs the type with, or a narrower one (valueTypeHoldsFormat).
func valueSetReads(set *statement.Schema, td ir.TypeDef) keySet {
	read := keySetOf("enum", "const")
	if valueSetTypeRead(set, td) {
		read |= keyBit("type")
	}
	switch n := td.(type) {
	case *ir.Enum:
		if valueTypeHoldsFormat(n.ValueType, set) {
			read |= keyBit("format")
		}
	case *ir.Literal:
		if valueTypeHoldsFormat(n.ValueType, set) {
			read |= keyBit("format")
		}
	default:
		// The top type and the union of literals hold no ValueType.
	}
	return read
}

// valueSetTypeRead reports whether the type set writes says nothing td, the
// node its value set lowers to, does not: no member is left to type; each
// member is a value of it, none naming an object or array (membersTyped); the
// Literal's value is one of it (literalTyped); or td is the top type, which no
// type contradicts. A union of literals holds none of a type naming a primitive.
func valueSetTypeRead(set *statement.Schema, td ir.TypeDef) bool {
	switch n := td.(type) {
	case *ir.Enum:
		return len(n.Members) == 0 || membersTyped(n.Members, set) && !slices.ContainsFunc(effectiveTypes(set), structural)
	case *ir.Literal:
		return literalTyped(n, set)
	case *ir.Any:
		return true
	default:
		return len(effectiveTypes(set)) == 0
	}
}

// structural reports whether st names an object or an array, which no
// primitive types.
func structural(st statement.Kind) bool {
	return st == statement.KindObject || st == statement.KindArray
}

// literalTyped reports whether lit holds the type s declares: its ValueType is
// the primitive that one type, or the type and format, select, which
// literalValueType took because the value is one of it, or a format of the
// type, as bytes is of string, or the type is the object or array a composite
// value already is. A type set naming no shape holds where it admits the
// value; one naming several, or a type the value is not, reached no field.
func literalTyped(lit *ir.Literal, s *statement.Schema) bool {
	types := effectiveTypes(s)
	if len(types) == 0 {
		return typesAdmit(kindsIn(s), lit.Value)
	}
	if len(types) > 1 {
		return false
	}
	if lit.Value.Kind == ir.ValueObject {
		return types[0] == statement.KindObject
	}
	if lit.Value.Kind == ir.ValueList {
		return types[0] == statement.KindArray
	}
	if prim, paired := formatPrimitive(s); paired && prim == lit.ValueType {
		return true
	}
	prim, known := spelling.Lookup(string(types[0]))
	return known && narrowsOrIs(lit.ValueType, prim)
}

// membersTyped reports whether every member of an Enum is a value of the type
// set s declares, which then says nothing the members do not. One a member is
// no value of is a type the Enum's ValueType does not take (enumValueType).
func membersTyped(members []ir.EnumMember, s *statement.Schema) bool {
	types := kindsIn(s)
	return !slices.ContainsFunc(members, func(m ir.EnumMember) bool { return !typesAdmit(types, m.Value) })
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
