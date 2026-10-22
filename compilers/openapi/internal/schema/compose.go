package schema

import (
	"cmp"
	"encoding/base64"
	"encoding/json/jsontext"
	"net/url"
	"strconv"
	"strings"
	"unicode/utf8"

	oas3 "github.com/speakeasy-api/openapi/jsonschema/oas3"
	"github.com/speakeasy-api/openapi/values"

	"github.com/dexpace/morphic/compilers/compile"
	"github.com/dexpace/morphic/compilers/openapi/internal/bind/typed"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/ids"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/compilers/openapi/internal/merge"
	"github.com/dexpace/morphic/compilers/openapi/internal/resolve"
	"github.com/dexpace/morphic/compilers/openapi/internal/value"
	"github.com/dexpace/morphic/ir"
)

// requireMembers lands each name of required on m, once m holds every own
// property: as Required on the property of that wire name, never cleared once
// set, and otherwise in m.Requires, once, in the order required names it. A
// name no own property declares is one a Base or Mixin declares, or one only
// the catch-all governs, and JSON Schema requires it all the same (ir-design
// §4.3). Each object lowering calls it once, on a model that lists none yet.
func requireMembers(m *ir.Model, required []string) {
	byWire := merge.WireNameIndex(m.Properties)
	listed := make(map[string]bool, len(required))
	for _, name := range required {
		if i, ok := byWire[name]; ok {
			m.Properties[i].Required = true
			continue
		}
		if !listed[name] {
			listed[name] = true
			m.Requires = append(m.Requires, name)
		}
	}
}

// onLoop reports whether the $ref at b lies on a loop of $refs, which the load
// phase reports once and the lowering drops to any without a report of its
// own.
func onLoop(c lowering.Ctx, b *oas3.JSONSchema[oas3.Referenceable]) bool {
	e, ok := c.Bindings().Entry(b)
	return ok && e.Chain.Member
}

// branchHint derives a composition branch's naming hint from its $ref target's
// name, falling back to a positional hint for inline branches. It names both a
// union variant and an allOf branch, which reach it as the same question.
//
// A $ref branch's answer is also subSchemaHint's, and must stay so: an outside
// $ref can name the branch pointer, and hoistSubSchema names it by
// subSchemaHint where no lowering of the composition recorded this hint, so
// the branch takes one name whether or not its composition is lowered.
func branchHint(refs *typed.Bindings, b *oas3.JSONSchema[oas3.Referenceable], i int) string {
	return branchHintAt(refs, b, strconv.Itoa(i))
}

// branchHintAt is branchHint for a branch whose index is already a token, as the
// last one of its pointer is.
func branchHintAt(refs *typed.Bindings, b *oas3.JSONSchema[oas3.Referenceable], index string) string {
	// Only a true reference carries a target name: IsReference() is precisely
	// GetSchema().Ref != "" for a non-bool branch, so a schema whose Ref pointer is
	// set but empty (IsReference() false) suggests no name.
	if b != nil && b.IsReference() {
		if name := targetHint(refs, b); name != "" {
			return name
		}
	}
	return positionalBranchHint(index)
}

// compositionKeywords are the schema keywords whose numbered children are
// composition branches.
var compositionKeywords = map[string]bool{"allOf": true, "oneOf": true, "anyOf": true}

// positionalBranchHint names an inline composition branch by its position, which
// is all there is to name it by: it has no $ref target to take a name from.
func positionalBranchHint(index string) string { return "variant_" + index }

// isDecimalIndex reports whether s is a non-empty run of ASCII digits — the
// shape a composition branch's pointer segment takes.
func isDecimalIndex(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// maxTargetHintHops bounds targetHint's walk along composition branches that
// hold a $ref: a loop of them, and any longer chain, stops there. An acyclic
// chain longer than the cap is legal, though no real document writes one: past
// it the walk keeps the name it has reached, so a union variant naming the
// chain's first branch can be named apart from the node it holds.
const maxTargetHintHops = 64

// targetHint returns the hint of the node a $ref resolves to, so a union
// variant or allOf branch is named as the type it holds is (GitHub #521).
//
// Each position on the way is read as subSchemaHint names it (ownHint); a
// composition branch holding a $ref moves the walk on to its target, keeping
// the last name found. The fragment is percent-decoded, then RFC 6901
// unescaped, so `#/components/schemas/Cat~1Dog` suggests "Cat/Dog" (GitHub
// #505); one spelling no pointer ends the walk with refHint's name. A position
// under /paths keeps ownHint's guess, which the declaration replaces on the
// node (GitHub #729).
func targetHint(refs *typed.Bindings, b *oas3.JSONSchema[oas3.Referenceable]) string {
	hint := ""
	for range maxTargetHintHops {
		ref := b.GetRef().String()
		pointer, ok := resolve.FragmentPointer(ref)
		if !ok {
			return cmp.Or(refHint(ref), hint)
		}
		decl := refs.Declared(b)
		own, follow := ownHint(decl, pointer)
		hint = cmp.Or(own, hint)
		if !follow {
			return hint
		}
		b = decl
	}
	return hint
}

// refHint returns the name a reference suggests when its fragment spells no
// pointer — a whole-document URI, a $anchor, a fragment that decodes to bytes
// that are not UTF-8 (GitHub #520): what follows its last '/', percent-decoded
// when that decodes to UTF-8, so `./Fish%2DTank.yaml` suggests "Fish-Tank.yaml".
func refHint(ref string) string {
	name := ref
	if _, after, ok := strings.CutLast(ref, "/"); ok {
		name = after
	}
	if decoded, err := url.PathUnescape(name); err == nil && utf8.ValidString(decoded) {
		return decoded
	}
	return name
}

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
