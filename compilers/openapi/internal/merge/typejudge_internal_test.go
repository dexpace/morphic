// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package merge

import (
	"encoding/json/jsontext"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
)

func stubResolve(reg map[ir.TypeID]ir.TypeDef) func(ir.TypeID) (ir.TypeDef, bool) {
	return func(id ir.TypeID) (ir.TypeDef, bool) { td, ok := reg[id]; return td, ok }
}

// judgeRegistry is narrowingRegistry plus one node of every kind the arms
// compare against a bare primitive, and the container pairs.
func judgeRegistry() map[ir.TypeID]ir.TypeDef {
	reg := narrowingRegistry()
	i32, integer := primitiveRef(ir.PrimitiveInt32), primitiveRef(ir.PrimitiveInteger)
	nullable := func(typeRef ir.TypeRef) ir.TypeRef { typeRef.Nullable = true; return typeRef }
	minItems := int64(1)
	zero := int64(0)
	reg["type/enum-int"] = &ir.Enum{ID: "type/enum-int", ValueType: ir.PrimitiveInteger, Members: []ir.EnumMember{{Value: number("1")}}}
	reg["type/enum-big"] = &ir.Enum{ID: "type/enum-big", ValueType: ir.PrimitiveInteger, Members: []ir.EnumMember{{Value: number("5000000000")}}}
	reg["type/enum-none"] = &ir.Enum{ID: "type/enum-none", ValueType: ir.PrimitiveInteger}
	reg["type/enum-bytes"] = &ir.Enum{ID: "type/enum-bytes", ValueType: ir.PrimitiveBytes, Members: []ir.EnumMember{{Value: ir.Value{Kind: ir.ValueBytes, Bytes: []byte("hi")}}}}
	reg["type/enum-str"] = &ir.Enum{ID: "type/enum-str", ValueType: ir.PrimitiveString, Members: []ir.EnumMember{{Value: stringValue("a")}}}
	reg["type/lit-a"] = &ir.Literal{ID: "type/lit-a", Value: stringValue("a")}
	reg["type/list-enum-str-min"] = &ir.List{ID: "type/list-enum-str-min", ElementType: ir.TypeRef{Target: "type/enum-str"},
		Constraints: &ir.Constraints{MinItems: &minItems}}
	reg["type/list-enum-str"] = &ir.List{ID: "type/list-enum-str", ElementType: ir.TypeRef{Target: "type/enum-str"}}
	reg["type/list-enum-int"] = &ir.List{ID: "type/list-enum-int", ElementType: ir.TypeRef{Target: "type/enum-int"}}
	reg["type/lit-a-twin"] = &ir.Literal{ID: "type/lit-a-twin", Value: stringValue("a")}
	reg["type/enum-str-twin"] = &ir.Enum{ID: "type/enum-str-twin", ValueType: ir.PrimitiveString, Members: []ir.EnumMember{{ID: "twin/a", Value: stringValue("a")}}}
	reg["type/scalar-str-twin"] = &ir.Scalar{ID: "type/scalar-str-twin", Base: &ir.TypeRef{Target: "type/primitive/string"}}
	for _, id := range []ir.TypeID{"type/lit-a-h1", "type/lit-a-h2"} {
		reg[id] = &ir.Literal{ID: id, Anonymous: true, Value: stringValue("a")}
	}
	reg["type/enum-str-h"] = &ir.Enum{ID: "type/enum-str-h", Anonymous: true,
		ValueType: ir.PrimitiveString, Members: []ir.EnumMember{{ID: "h/a", Value: stringValue("a")}}}
	reg["type/lit-x"] = &ir.Literal{ID: "type/lit-x", Value: stringValue("x")}
	reg["type/lit-local"] = &ir.Literal{ID: "type/lit-local", Value: stringValue("2024-01-01T00:00:00")}
	reg["type/lit-1.5"] = &ir.Literal{ID: "type/lit-1.5", Value: number("1.5")}
	reg["type/lit-null"] = &ir.Literal{ID: "type/lit-null", Value: ir.Value{Kind: ir.ValueNull}}
	reg["type/lit-1"] = &ir.Literal{ID: "type/lit-1", Value: number("1")}
	reg["type/scalar-str"] = &ir.Scalar{ID: "type/scalar-str", Base: &ir.TypeRef{Target: "type/primitive/string"}}
	reg["type/scalar-int"] = &ir.Scalar{ID: "type/scalar-int", Base: &integer}
	reg["type/scalar-any"] = &ir.Scalar{ID: "type/scalar-any", Base: &ir.TypeRef{Target: "type/primitive/any"}}
	reg["type/scalar-any-doc"] = &ir.Scalar{Base: &ir.TypeRef{Target: "type/primitive/any"},
		ID: "type/scalar-any-doc", Docs: ir.Docs{Description: "d"}, Deprecation: &ir.Deprecation{}, Sensitive: true}
	reg["type/scalar-any-ex"] = &ir.Scalar{Base: &ir.TypeRef{Target: "type/primitive/any"},
		ID: "type/scalar-any-ex", Examples: []ir.Example{{Value: new(number("1"))}}}
	reg["type/alias-scalar-any-doc"] = &ir.Scalar{ID: "type/alias-scalar-any-doc", Base: &ir.TypeRef{Target: "type/scalar-any-doc"}}
	reg["type/alias-doc-scalar-any"] = &ir.Scalar{Base: &ir.TypeRef{Target: "type/scalar-any"},
		ID: "type/alias-doc-scalar-any", Docs: ir.Docs{Description: "d"}}
	reg["type/list-scalar-any"] = &ir.List{ID: "type/list-scalar-any", ElementType: ir.TypeRef{Target: "type/scalar-any"}}
	untyped := func(id ir.TypeID, td ir.TypeDef) {
		typeCommon := td.Common()
		typeCommon.ID, typeCommon.Provenance = id, ir.Provenance{Pointer: "/untyped/" + jsontext.Pointer(id)}
		reg[id] = td
	}
	untyped("type/model-untyped", &ir.Model{Properties: []ir.Property{{WireName: "own"}}})
	untyped("type/model-untyped-doc", &ir.Model{Properties: []ir.Property{{WireName: "own"}},
		Docs: ir.Docs{Description: "d"}})
	untyped("type/model-untyped-ex", &ir.Model{Properties: []ir.Property{{WireName: "own"}},
		Examples: []ir.Example{{}}})
	untyped("type/model-untyped-doc-inline", &ir.Model{Properties: []ir.Property{{WireName: "own"}},
		Anonymous: true, Docs: ir.Docs{Description: "d"}})
	untyped("type/alias-untyped", &ir.Scalar{Base: &ir.TypeRef{Target: "type/model-untyped"}})
	untyped("type/alias-doc-untyped", &ir.Scalar{Base: &ir.TypeRef{Target: "type/alias-untyped"},
		Docs: ir.Docs{Description: "d"}})
	untyped("type/alias-of-untyped-doc", &ir.Scalar{Base: &ir.TypeRef{Target: "type/model-untyped-doc"}})
	untyped("type/list-untyped", &ir.List{ElementType: i32})
	untyped("type/map-untyped", &ir.MapT{Value: i32})
	reg["type/alias-typed-untyped"] = &ir.Scalar{ID: "type/alias-typed-untyped", Base: &ir.TypeRef{Target: "type/model-untyped"}}
	reg["type/list-model-untyped"] = &ir.List{ID: "type/list-model-untyped", ElementType: ir.TypeRef{Target: "type/model-untyped"}}
	reg["type/list-model-untyped-doc"] = &ir.List{ID: "type/list-model-untyped-doc", ElementType: ir.TypeRef{Target: "type/model-untyped-doc"}}
	reg["type/list-scalar-any-doc"] = &ir.List{ID: "type/list-scalar-any-doc", ElementType: ir.TypeRef{Target: "type/scalar-any-doc"}}
	reg["type/scalar-enc"] = &ir.Scalar{ID: "type/scalar-enc", Base: &integer, Encoding: &ir.Encoding{Name: "x"}}
	reg["type/scalar-opaque"] = &ir.Scalar{ID: "type/scalar-opaque"}
	formatted := func(id ir.TypeID, enc ir.Encoding) *ir.Scalar {
		return &ir.Scalar{ID: id, Base: &ir.TypeRef{Target: "type/primitive/string"}, Encoding: &enc}
	}
	reg["type/scalar-time"] = formatted("type/scalar-time", ir.Encoding{Name: "time"})
	timeMax := formatted("type/scalar-time-max", ir.Encoding{Name: "time"})
	timeMax.Constraints = &ir.Constraints{MaxLength: new(int64(5))}
	reg["type/scalar-time-max"] = timeMax
	timeRoomy := formatted("type/scalar-time-roomy", ir.Encoding{Name: "time"})
	timeRoomy.Constraints = &ir.Constraints{MaxLength: new(int64(20))}
	reg["type/scalar-time-roomy"] = timeRoomy
	reg["type/scalar-time-wire"] = formatted("type/scalar-time-wire", ir.Encoding{Name: "time", WireType: &ir.TypeRef{Target: "type/primitive/string"}})
	reg["type/scalar-time-schema"] = formatted("type/scalar-time-schema", ir.Encoding{Name: "time", Schema: &ir.TypeRef{Target: "type/primitive/string"}})
	timeDoc := formatted("type/scalar-time-doc", ir.Encoding{Name: "time"})
	timeDoc.Docs = ir.Docs{Description: "d"}
	reg["type/scalar-time-doc"] = timeDoc
	reg["type/scalar-time-media"] = formatted("type/scalar-time-media", ir.Encoding{Name: "time", MediaType: "text/plain"})
	reg["type/scalar-email"] = formatted("type/scalar-email", ir.Encoding{Name: "email"})
	reg["type/alias-time"] = &ir.Scalar{ID: "type/alias-time", Base: &ir.TypeRef{Target: "type/scalar-time"}}
	reg["type/alias-encoded-time"] = &ir.Scalar{ID: "type/alias-encoded-time", Base: &ir.TypeRef{Target: "type/scalar-time"},
		Encoding: &ir.Encoding{Name: "base64"}}
	reg["type/scalar-time-int"] = &ir.Scalar{ID: "type/scalar-time-int", Base: &integer, Encoding: &ir.Encoding{Name: "time"}}
	reg["type/alias-offset"] = &ir.Scalar{ID: "type/alias-offset", Base: &ir.TypeRef{Target: "type/primitive/datetime_offset"}}
	reg["type/lit-time"] = &ir.Literal{ID: "type/lit-time", Value: stringValue("12:00:00Z")}
	reg["type/enum-time"] = &ir.Enum{ID: "type/enum-time", ValueType: ir.PrimitiveString, Members: []ir.EnumMember{{Value: stringValue("12:00:00Z")}}}
	reg["type/any"] = &ir.Any{ID: "type/any"}
	reg["type/model"] = &ir.Model{ID: "type/model"}
	reg["type/list-int-null"] = &ir.List{ID: "type/list-int-null", ElementType: nullable(integer)}
	reg["type/list-int"] = &ir.List{ID: "type/list-int", ElementType: integer}
	reg["type/list-i32"] = &ir.List{ID: "type/list-i32", ElementType: i32}
	reg["type/list-i32-null"] = &ir.List{ID: "type/list-i32-null", ElementType: nullable(i32)}
	reg["type/list-u32"] = &ir.List{ID: "type/list-u32", ElementType: primitiveRef(ir.PrimitiveUint32)}
	reg["type/lit-a-doc"] = &ir.Literal{ID: "type/lit-a-doc", Docs: ir.Docs{Description: "d"}, Value: stringValue("a")}
	reg["type/list-i32-kept-other"] = &ir.List{ID: "type/list-i32-kept-other", ElementType: i32,
		Unmodeled: ir.Unmodeled{"openapi:x-a": {Value: ir.RawValue("2")}}}
	reg["type/list-int-kept"] = &ir.List{ID: "type/list-int-kept",
		Unmodeled: ir.Unmodeled{"openapi:x-a": {Value: ir.RawValue("1")}}, ElementType: integer}
	reg["type/list-i32-h-described"] = &ir.List{ID: "type/list-i32-h-described", Anonymous: true,
		Docs: ir.Docs{Description: "d"}, ElementType: i32}
	reg["type/scalar-opaque-doc"] = &ir.Scalar{ID: "type/scalar-opaque-doc", Docs: ir.Docs{Description: "d"}}
	reg["type/enum-ab-doc"] = &ir.Enum{ID: "type/enum-ab-doc", ValueType: ir.PrimitiveString,
		Members: []ir.EnumMember{{Value: stringValue("a"), Docs: ir.Docs{Description: "x"}}, {Value: stringValue("b")}}}
	reg["type/enum-ab"] = &ir.Enum{ID: "type/enum-ab", ValueType: ir.PrimitiveString,
		Members: []ir.EnumMember{{Value: stringValue("a")}, {Value: stringValue("b")}}}
	reg["type/list-int-exampled"] = &ir.List{ID: "type/list-int-exampled",
		Examples: []ir.Example{{Value: &ir.Value{Kind: ir.ValueList}}}, ElementType: integer}
	bound := func(lo, hi int64) *ir.Constraints {
		constraints := &ir.Constraints{}
		if lo > 0 {
			constraints.MinItems = &lo
		}
		if hi > 0 {
			constraints.MaxItems = &hi
		}
		return constraints
	}
	reg["type/list-i32-min2"] = &ir.List{ID: "type/list-i32-min2", ElementType: i32, Constraints: bound(2, 0)}
	reg["type/list-i32-max"] = &ir.List{ID: "type/list-i32-max", ElementType: i32, Constraints: bound(0, 5)}
	reg["type/list-int-min1"] = &ir.List{ID: "type/list-int-min1", ElementType: integer, Constraints: bound(1, 0)}
	reg["type/list-int-min2"] = &ir.List{ID: "type/list-int-min2", ElementType: integer, Constraints: bound(2, 0)}
	reg["type/list-int-max"] = &ir.List{ID: "type/list-int-max", ElementType: integer, Constraints: bound(0, 5)}
	reg["type/list-i32-described"] = &ir.List{ID: "type/list-i32-described", ElementType: i32, Docs: ir.Docs{Description: "d"}}
	reg["type/list-int-described"] = &ir.List{ID: "type/list-int-described", ElementType: integer, Docs: ir.Docs{Description: "d"}}
	reg["type/lists-min"] = &ir.List{ID: "type/lists-min", ElementType: ref("type/list-i32-min")}
	reg["type/lists-min2"] = &ir.List{ID: "type/lists-min2", ElementType: ref("type/list-i32-min2")}
	reg["type/lists-max"] = &ir.List{ID: "type/lists-max", ElementType: ref("type/list-i32-max")}
	reg["type/map-bare"] = &ir.MapT{ID: "type/map-bare", Value: i32}
	reg["type/map-min"] = &ir.MapT{ID: "type/map-min", Value: i32, Constraints: &ir.Constraints{MinProperties: new(int64(1))}}
	reg["type/map-max"] = &ir.MapT{ID: "type/map-max", Value: i32, Constraints: &ir.Constraints{MaxProperties: new(int64(5))}}
	encoded := func(id ir.TypeID, constraints *ir.Constraints) *ir.Scalar {
		return &ir.Scalar{ID: id, Base: &integer, Encoding: &ir.Encoding{Name: "x"}, Constraints: constraints}
	}
	reg["type/scalar-enc-min"] = encoded("type/scalar-enc-min", &ir.Constraints{Min: new(ir.BigVal("1"))})
	reg["type/scalar-enc-max"] = encoded("type/scalar-enc-max", &ir.Constraints{Max: new(ir.BigVal("5"))})
	reg["type/scalar-enc-a"] = encoded("type/scalar-enc-a", &ir.Constraints{Pattern: "^a$"})
	reg["type/scalar-enc-b"] = encoded("type/scalar-enc-b", &ir.Constraints{Pattern: "^b$"})
	tuple := func(id ir.TypeID, constraints *ir.Constraints, rest *ir.TypeRef, elements ...ir.TypeRef) *ir.Tuple {
		return &ir.Tuple{ID: id, ElementTypes: elements, Rest: rest, Constraints: constraints}
	}
	reg["type/tuple"] = tuple("type/tuple", nil, nil, i32)
	reg["type/tuple-min"] = tuple("type/tuple-min", bound(1, 0), nil, i32)
	reg["type/tuple-max"] = tuple("type/tuple-max", bound(0, 3), nil, i32)
	reg["type/tuple-long"] = tuple("type/tuple-long", nil, nil, i32, i32)
	reg["type/tuple-int"] = tuple("type/tuple-int", nil, nil, integer)
	reg["type/tuple-null"] = tuple("type/tuple-null", nil, nil, nullable(i32))
	reg["type/tuple-rest"] = tuple("type/tuple-rest", nil, &i32, i32)
	reg["type/tuple-rest-int"] = tuple("type/tuple-rest-int", nil, &integer, i32)
	stringType := primitiveRef(ir.PrimitiveString)
	reg["type/tuple-rest-str"] = tuple("type/tuple-rest-str", nil, &stringType, i32)
	reg["type/list-i32-min"] = &ir.List{ID: "type/list-i32-min", ElementType: i32, Constraints: &ir.Constraints{MinItems: &minItems}}
	reg["type/list-int-min"] = &ir.List{ID: "type/list-int-min", ElementType: integer, Constraints: &ir.Constraints{MinItems: &minItems}}
	reg["type/list-i32-twin"] = &ir.List{ID: "type/list-i32-twin", ElementType: i32}
	for _, id := range []ir.TypeID{"type/list-i32-h1", "type/list-i32-h2"} {
		reg[id] = &ir.List{ID: id, Anonymous: true, ElementType: i32}
	}
	reg["type/list-i32-other"] = &ir.List{ID: "type/list-i32-other", Anonymous: true, ElementType: i32,
		Provenance: ir.Provenance{Source: 1}}
	reg["type/list-int-min0"] = &ir.List{ID: "type/list-int-min0", ElementType: integer, Constraints: &ir.Constraints{MinItems: &zero}}
	reg["type/list-str-null"] = &ir.List{ID: "type/list-str-null", ElementType: nullable(primitiveRef(ir.PrimitiveString))}
	reg["type/list-int-null-min"] = &ir.List{ID: "type/list-int-null-min", ElementType: nullable(integer),
		Constraints: &ir.Constraints{MinItems: &minItems}}
	reg["type/list-str"] = &ir.List{ID: "type/list-str", ElementType: primitiveRef(ir.PrimitiveString)}
	reg["type/list-list-int"] = &ir.List{ID: "type/list-list-int", ElementType: ir.TypeRef{Target: "type/list-int"}}
	reg["type/list-list-i32"] = &ir.List{ID: "type/list-list-i32", ElementType: ir.TypeRef{Target: "type/list-i32"}}
	reg["type/map-int"] = &ir.MapT{ID: "type/map-int", Value: integer}
	reg["type/map-int-h"] = &ir.MapT{ID: "type/map-int-h", Anonymous: true, Value: integer}
	reg["type/map-str"] = &ir.MapT{ID: "type/map-str", Value: primitiveRef(ir.PrimitiveString)}
	reg["type/map-int-min"] = &ir.MapT{ID: "type/map-int-min", Value: integer, Constraints: &ir.Constraints{MinProperties: &minItems}}
	reg["type/map-i32"] = &ir.MapT{ID: "type/map-i32", Value: i32}
	reg["type/map-i32-keyed"] = &ir.MapT{ID: "type/map-i32-keyed", Key: &ir.TypeRef{Target: "type/primitive/uuid"}, Value: i32}
	reg["type/cycle"] = &ir.List{ID: "type/cycle", ElementType: ir.TypeRef{Target: "type/cycle2"}}
	reg["type/cycle2"] = &ir.List{ID: "type/cycle2", ElementType: ir.TypeRef{Target: "type/cycle"}}
	reg["type/cycle-b"] = &ir.List{ID: "type/cycle-b", ElementType: ir.TypeRef{Target: "type/cycle2-b"}}
	reg["type/cycle2-b"] = &ir.List{ID: "type/cycle2-b", ElementType: ir.TypeRef{Target: "type/cycle-b"}}
	valueTypedEnums(reg)
	aliasesOverNodes(reg)
	return reg
}

// valueTypedEnums adds enums whose value types differ: t/enum-<kind>-<values>,
// their values joined.
func valueTypedEnums(reg map[ir.TypeID]ir.TypeDef) {
	for _, e := range []struct {
		kind   ir.PrimitiveKind
		values []string
	}{
		{ir.PrimitiveNumber, []string{"1", "2"}}, {ir.PrimitiveNumber, []string{"2", "1"}}, {ir.PrimitiveNumber, []string{"1"}},
		{ir.PrimitiveNumber, []string{"1", "2.5"}}, {ir.PrimitiveNumber, []string{"2.5"}},
		{ir.PrimitiveInteger, []string{"1"}}, {ir.PrimitiveInteger, []string{"1", "2"}}, {ir.PrimitiveInteger, []string{"1", "3"}},
		{ir.PrimitiveInt32, []string{"1", "2"}}, {ir.PrimitiveInt32, []string{"1"}},
		{ir.PrimitiveUint32, []string{"1", "2"}}, {ir.PrimitiveUint32, []string{"2"}},
	} {
		id := ir.TypeID("type/enum-" + string(e.kind) + "-" + strings.Join(e.values, ""))
		members := make([]ir.EnumMember, len(e.values))
		for k, v := range e.values {
			members[k] = ir.EnumMember{Value: number(v)}
		}
		reg[id] = &ir.Enum{ID: id, ValueType: e.kind, Members: members}
	}
}

// aliasesOverNodes adds aliases over the registry's enums, consts, scalars and
// lists, and the nodes they meet.
func aliasesOverNodes(reg map[ir.TypeID]ir.TypeDef) {
	alias := func(id, base ir.TypeID, bounds *ir.Constraints) {
		reg[id] = &ir.Scalar{ID: id, Base: &ir.TypeRef{Target: base}, Constraints: bounds}
	}
	maxLength := &ir.Constraints{MaxLength: new(int64(5))}
	alias("type/alias-enum-str-max", "type/enum-str", maxLength)
	alias("type/alias-lit-a-max", "type/lit-a", maxLength)
	alias("type/alias-enum-big-max", "type/enum-big", &ir.Constraints{Max: new(ir.BigVal("6000000000"))})
	alias("type/alias-enum-ab", "type/enum-ab", nil)
	alias("type/alias-list-i32-min", "type/list-i32-min", nil)
	alias("type/alias-scalar-time", "type/scalar-time", &ir.Constraints{MinLength: new(int64(1))})
	alias("type/alias-lit-time-doc", "type/lit-time", nil)
	alias("type/alias-list-i32-min-doc", "type/list-i32-min", nil)
	alias("type/alias-time-ref", "type/scalar-time", nil)
	reg["type/alias-time-ref"].Common().Unmodeled = ir.Unmodeled{"openapi:not": {Value: ir.RawValue(`{"$ref":"n.json"}`)}}
	reg["type/alias-enum-str-null"] = &ir.Scalar{ID: "type/alias-enum-str-null",
		Base: &ir.TypeRef{Target: "type/enum-str", Nullable: true}}
	reg["type/enum-str-closed"] = &ir.Enum{ID: "type/enum-str-closed", ValueType: ir.PrimitiveString, Closed: true,
		Members: []ir.EnumMember{{Value: stringValue("a")}}}
	reg["type/alias-list-i32-min-doc"].Common().Docs = ir.Docs{Description: "d"}
	reg["type/alias-lit-time-doc"].Common().Docs = ir.Docs{Description: "x"}
	reg["type/enum-bc"] = &ir.Enum{ID: "type/enum-bc", ValueType: ir.PrimitiveString,
		Members: []ir.EnumMember{{Value: stringValue("b")}, {Value: stringValue("c")}}}
	reg["type/scalar-i32-doc"] = &ir.Scalar{ID: "type/scalar-i32-doc", Base: new(primitiveRef(ir.PrimitiveInt32)),
		Docs: ir.Docs{Description: "d"}}
}

func ref(id ir.TypeID) ir.TypeRef { return ir.TypeRef{Target: id} }

// relate is relateFrom for a field's own types.
func (j typeJudge) relate(a, b ir.TypeRef) relation { return j.relateFrom(a, b, 0) }

func TestTypeJudge_RelateArms(t *testing.T) {
	t.Parallel()
	typeJudgeValue := typeJudge{resolve: stubResolve(judgeRegistry()), sites: atRoot(&stubWalk{})}
	primitive := func(primitiveKind ir.PrimitiveKind) ir.TypeID { return primitiveRef(primitiveKind).Target }

	for _, tc := range []struct {
		name string
		a, b ir.TypeID
		want relation
	}{
		{"one target", "type/lit-a", "type/lit-a", relSame},
		{"a dangling target", "type/absent", "type/primitive/integer", relUnrelated},
		{"a dangling second", "type/primitive/integer", "type/absent", relUnrelated},
		{"the top loses to a bare kind", primitive(ir.PrimitiveAny), primitive(ir.PrimitiveInt32), secondTypeIsNarrower},
		{"a degraded any node is not the top", "type/model", "type/any", relUnrelated},
		{"`{}` is unrelated to a degraded any", primitive(ir.PrimitiveAny), "type/any", relUnrelated},
		{"bare narrower", primitive(ir.PrimitiveInt32), primitive(ir.PrimitiveInteger), firstTypeIsNarrower},
		{"bare broader", primitive(ir.PrimitiveInteger), primitive(ir.PrimitiveInt32), secondTypeIsNarrower},
		{"bare overlap", primitive(ir.PrimitiveInt32), primitive(ir.PrimitiveUint32), relOverlap},
		{"bare disjoint", primitive(ir.PrimitiveString), primitive(ir.PrimitiveInteger), relDisjoint},
		{"an enum of integer against bare int32", "type/enum-int", primitive(ir.PrimitiveInt32), relOverlap},
		{"bare int32 against an enum of integer", primitive(ir.PrimitiveInt32), "type/enum-int", relOverlap},
		{"an enum no member of which the kind admits", "type/enum-big", primitive(ir.PrimitiveInt32), relDisjoint},
		{"bare int32 against an enum none of whose members it admits", primitive(ir.PrimitiveInt32), "type/enum-big", relDisjoint},
		{"an enum with no members against an overlapping kind", "type/enum-none", primitive(ir.PrimitiveInt32), relDisjoint},
		{"an enum against a kind with no judge", "type/enum-str", primitive(ir.PrimitiveBytes), relOverlap},
		{"an enum of bytes against a format no member meets", "type/enum-bytes", primitive(ir.PrimitiveUUID), relDisjoint},
		{"an enum of string over bare string", "type/enum-str", primitive(ir.PrimitiveString), firstTypeIsNarrower},
		{"bare string under an enum of string", primitive(ir.PrimitiveString), "type/enum-str", secondTypeIsNarrower},
		{"an enum against another family", "type/enum-str", primitive(ir.PrimitiveInteger), relDisjoint},
		{"a scalar over its own kind", "type/scalar-str", primitive(ir.PrimitiveString), firstTypeIsNarrower},
		{"a scalar over a narrower kind", "type/scalar-int", primitive(ir.PrimitiveNumber), firstTypeIsNarrower},
		{"a scalar over a broader kind", "type/scalar-int", primitive(ir.PrimitiveInt32), relOverlap},
		{"an alias over the top type", "type/scalar-any", primitive(ir.PrimitiveInt32), secondTypeIsNarrower},
		{"an alias over the top type holding what a reference carries down", "type/scalar-any-doc", primitive(ir.PrimitiveInt32), secondTypeIsNarrower},
		{"an alias over the top type holding examples", "type/scalar-any-ex", primitive(ir.PrimitiveInt32), relOverlap},
		{"an alias of an alias holding docs over the top type", "type/alias-scalar-any-doc", "type/model", relUnrelated},
		{"an alias holding docs of an alias over the top type", "type/alias-doc-scalar-any", "type/model", secondTypeIsNarrower},
		{"a list of an alias over the top type", "type/list-scalar-any", "type/list-i32", secondTypeIsNarrower},
		{"a list of an alias holding docs over the top type", "type/list-scalar-any-doc", "type/list-i32", relOverlap},
		{"an opaque scalar", "type/scalar-opaque", primitive(ir.PrimitiveInt32), relUnrelated},
		{"a literal the kind admits", "type/lit-1", primitive(ir.PrimitiveInteger), firstTypeIsNarrower},
		{"a literal string against integer", "type/lit-a", primitive(ir.PrimitiveInteger), relDisjoint},
		{"a literal 1.5 against integer", "type/lit-1.5", primitive(ir.PrimitiveInteger), relDisjoint},
		{"a literal against a kind with no judge", "type/lit-x", primitive(ir.PrimitiveBytes), relOverlap},
		{"a literal the local date-time kind admits", "type/lit-local", primitive(ir.PrimitiveDatetime), firstTypeIsNarrower},
		{"a literal the local date-time kind refuses", "type/lit-x", primitive(ir.PrimitiveDatetime), relDisjoint},
		{"a time scalar within string", "type/scalar-time", primitive(ir.PrimitiveString), firstTypeIsNarrower},
		{"a time scalar against a date-time", "type/scalar-time", primitive(ir.PrimitiveDatetimeOffset), relDisjoint},
		{"a time scalar against a local time", "type/scalar-time", primitive(ir.PrimitiveTime), relDisjoint},
		{"a time scalar against bytes", "type/scalar-time", primitive(ir.PrimitiveBytes), relOverlap},
		{"a time scalar against an integer", "type/scalar-time", primitive(ir.PrimitiveInteger), relDisjoint},
		{"an alias of a time scalar against a date-time", "type/alias-time", primitive(ir.PrimitiveDatetimeOffset), relDisjoint},
		{"a time scalar whose encoding says more", "type/scalar-time-media", primitive(ir.PrimitiveDatetimeOffset), relOverlap},
		{"a scalar of a format with no grammar here", "type/scalar-email", primitive(ir.PrimitiveDatetimeOffset), relOverlap},
		{"a time a time scalar admits", "type/lit-time", "type/scalar-time", firstTypeIsNarrower},
		{"a string a time scalar refuses", "type/lit-x", "type/scalar-time", relDisjoint},
		{"a time a time scalar's bound refuses", "type/lit-time", "type/scalar-time-max", relDisjoint},
		{"a time a time scalar's bound admits", "type/lit-time", "type/scalar-time-roomy", firstTypeIsNarrower},
		{"a literal against an alias of a kind it is not", "type/lit-a", "type/scalar-int", relOverlap},
		{"a time against a time scalar holding detail", "type/lit-time", "type/scalar-time-doc", relMeet},
		{"a time against an alias of a time scalar", "type/lit-time", "type/alias-time", firstTypeIsNarrower},
		{"a time against a format with no grammar here", "type/lit-time", "type/scalar-email", relOverlap},
		{"an enum no member of which is a time", "type/enum-str", "type/scalar-time", relDisjoint},
		{"an enum with a time member", "type/enum-time", "type/scalar-time", relOverlap},
		{"two time scalars declared apart", "type/scalar-time", "type/scalar-time-doc", relOverlap},
		{"a time scalar against an alias of a date-time", "type/scalar-time", "type/alias-offset", relDisjoint},
		{"a time scalar against a format with no grammar here", "type/scalar-time", "type/scalar-email", relOverlap},
		{"a null literal against a bare integer", "type/lit-null", primitive(ir.PrimitiveInteger), relDisjoint},
		{"a model against a bare kind", "type/model", primitive(ir.PrimitiveInteger), relDisjoint},
		{"a model stating no instance type against a bare kind", "type/model-untyped", primitive(ir.PrimitiveInteger), secondTypeIsNarrower},
		{"one holding docs a reference carries down", "type/model-untyped-doc", primitive(ir.PrimitiveInteger), secondTypeIsNarrower},
		{"one holding examples", "type/model-untyped-ex", primitive(ir.PrimitiveInteger), relOverlap},
		{"one holding docs written at the declaration", "type/model-untyped-doc-inline", primitive(ir.PrimitiveInteger), relOverlap},
		{"an enum against a model stating no instance type", "type/enum-str", "type/model-untyped", firstTypeIsNarrower},
		{"a literal against a model stating no instance type", "type/lit-a", "type/model-untyped", firstTypeIsNarrower},
		{"a list against a model stating no instance type", "type/list-i32", "type/model-untyped", firstTypeIsNarrower},
		{"an alias stating none of a model stating none", "type/alias-untyped", primitive(ir.PrimitiveInteger), secondTypeIsNarrower},
		{"an alias holding docs a reference carries down of one", "type/alias-doc-untyped", primitive(ir.PrimitiveInteger), secondTypeIsNarrower},
		{"an alias of one holding docs", "type/alias-of-untyped-doc", primitive(ir.PrimitiveInteger), relOverlap},
		{"an alias stating a type of a model stating none", "type/alias-typed-untyped", primitive(ir.PrimitiveInteger), relDisjoint},
		{"two structures each stating no instance type", "type/list-untyped", "type/model-untyped", relOverlap},
		{"a list stating none against a model", "type/list-untyped", "type/model", secondTypeIsNarrower},
		{"a map stating none against a list", "type/map-untyped", "type/list-i32", secondTypeIsNarrower},
		{"a list of a model stating none against a list of strings", "type/list-model-untyped", "type/list-str", secondTypeIsNarrower},
		{"a list of one holding docs against a list of strings", "type/list-model-untyped-doc", "type/list-str", relOverlap},
		{"the top type spelled as an alias against a model", "type/scalar-any", "type/model", secondTypeIsNarrower},
		{"an alias over the top type holding docs against a model", "type/scalar-any-doc", "type/model", secondTypeIsNarrower},
		{"an alias over the top type holding examples against a model", "type/scalar-any-ex", "type/model", relUnrelated},
		{"two different literals", "type/lit-a", "type/lit-x", relDisjoint},
		{"two identical hoisted literals", "type/lit-a-h1", "type/lit-a-h2", relSame},
		{"a named enum against a hoisted copy", "type/enum-str", "type/enum-str-h", firstTypeIsNarrower},
		{"two identical named literals", "type/lit-a", "type/lit-a-twin", relOverlap},
		{"two identical named enums", "type/enum-str", "type/enum-str-twin", relOverlap},
		{"two identical named scalars", "type/scalar-str", "type/scalar-str-twin", relOverlap},
		{"a literal against an enum", "type/lit-a", "type/enum-str", relOverlap},
		{"a scalar against an enum", "type/scalar-str", "type/enum-str", relOverlap},
		{"a scalar against a literal", "type/scalar-str", "type/lit-a", relOverlap},
		{"a narrower element", "type/list-int-null", "type/list-i32", secondTypeIsNarrower},
		{"a narrower element, the other way", "type/list-i32", "type/list-int-null", firstTypeIsNarrower},
		{"a narrower element that admits null the broader does not", "type/list-int", "type/list-i32-null", relMeet},
		{"one element admitting null and the other not", "type/list-i32-null", "type/list-i32", secondTypeIsNarrower},
		{"a loser-only constraint", "type/list-int-min", "type/list-i32", relMeet},
		{"a winner-only constraint", "type/list-int", "type/list-i32-min", secondTypeIsNarrower},
		{"the same constraint on both", "type/list-int-min", "type/list-i32-min", secondTypeIsNarrower},
		{"two lists alike, declared apart", "type/list-i32", "type/list-i32-twin", relOverlap},
		{"two hoisted copies of one list", "type/list-i32-h1", "type/list-i32-h2", relSame},
		{"a list declared apart against a hoisted copy", "type/list-i32", "type/list-i32-h1", firstTypeIsNarrower},
		{"a map declared apart against a hoisted copy", "type/map-int", "type/map-int-h", firstTypeIsNarrower},
		{"a list another document hoists at the declaration's pointer", "type/list-i32-other", "type/list-i32-h1", firstTypeIsNarrower},
		{"two lists of one element, one constrained", "type/list-i32-min", "type/list-i32", firstTypeIsNarrower},
		{"two lists of one element, one bound tighter", "type/list-i32-min2", "type/list-i32-min", firstTypeIsNarrower},
		{"two lists of one element, each adding a bound", "type/list-i32-min", "type/list-i32-max", relMeet},
		{"a narrower element and bounds that say all the other's do", "type/list-i32-min2", "type/list-int-min1", firstTypeIsNarrower},
		{"a narrower element and looser bounds", "type/list-i32-min", "type/list-int-min2", relMeet},
		{"a narrower element and a bound only the broader says", "type/list-i32-min", "type/list-int-max", relMeet},
		{"two lists of one element differing beside a bound", "type/list-i32-described", "type/list-i32-max", relMeet},
		{"a narrower element holding detail the broader lacks", "type/list-i32-described", "type/list-int", firstTypeIsNarrower},
		{"a narrower element holding the detail the broader holds", "type/list-i32-described", "type/list-int-described", firstTypeIsNarrower},
		{"a narrower element lacking detail the broader holds", "type/list-i32", "type/list-int-described", relMeet},
		{"a narrower element lacking examples the broader gives", "type/list-i32", "type/list-int-exampled", relOverlap},
		{"two lists of one element, one described", "type/list-i32", "type/list-i32-described", relOverlap},
		{"two hoisted lists of one element, one described", "type/list-i32-h1", "type/list-i32-h-described", relOverlap},
		{"a narrower element lacking what the broader keeps", "type/list-i32", "type/list-int-kept", relMeet},
		{"two opaque scalars, one described", "type/scalar-opaque", "type/scalar-opaque-doc", relUnrelated},
		{"an enum within another", "type/enum-str", "type/enum-ab", firstTypeIsNarrower},
		{"an enum within another saying its shared member otherwise", "type/enum-str", "type/enum-ab-doc", relOverlap},
		{"two lists of lists whose elements meet", "type/lists-min", "type/lists-max", relMeet},
		{"two lists of lists, one element tighter", "type/lists-min2", "type/lists-min", firstTypeIsNarrower},
		{"two maps of one value, each adding a bound", "type/map-min", "type/map-max", relMeet},
		{"two maps of one value, one constrained", "type/map-min", "type/map-bare", firstTypeIsNarrower},
		{"two scalars alike but for a bound", "type/scalar-enc-min", "type/scalar-enc", firstTypeIsNarrower},
		{"two scalars, each adding a bound", "type/scalar-enc-min", "type/scalar-enc-max", relMeet},
		{"two scalars differing in a keyword with no order", "type/scalar-enc-a", "type/scalar-enc-b", relMeet},
		{"two scalars differing beside their bounds", "type/scalar-enc-min", "type/scalar-int", relOverlap},
		{"two tuples, each adding a bound", "type/tuple-min", "type/tuple-max", relMeet},
		{"two tuples, one constrained", "type/tuple-min", "type/tuple", firstTypeIsNarrower},
		{"two tuples of different lengths", "type/tuple", "type/tuple-long", relUnrelated},
		{"two tuples, one position narrower", "type/tuple", "type/tuple-int", firstTypeIsNarrower},
		{"two tuples, one position admitting null", "type/tuple", "type/tuple-null", firstTypeIsNarrower},
		{"a tuple with a rest against one with none", "type/tuple", "type/tuple-rest", relUnrelated},
		{"two tuples, one rest narrower", "type/tuple-rest", "type/tuple-rest-int", firstTypeIsNarrower},
		{"two tuples whose rests share no value", "type/tuple-rest", "type/tuple-rest-str", relUnrelated},
		{"two literals of one value, one described", "type/lit-a", "type/lit-a-doc", relOverlap},
		{"a narrower element keeping a key the broader keeps otherwise", "type/list-i32-kept-other", "type/list-int-kept", relOverlap},
		{"a tuple against a list", "type/tuple", "type/list-i32", relUnrelated},
		{"disjoint elements overlap in the empty list", "type/list-str", "type/list-i32", relOverlap},
		{"disjoint elements where one list is never empty", "type/list-str", "type/list-int-min", relDisjoint},
		{"a minItems of zero admits the empty list", "type/list-str", "type/list-int-min0", relOverlap},
		{"disjoint values where one map is never empty", "type/map-str", "type/map-int-min", relDisjoint},
		{"disjoint elements that both admit null overlap", "type/list-str-null", "type/list-int-null-min", relOverlap},
		{"disjoint elements where only one admits null", "type/list-str-null", "type/list-int-min", relDisjoint},
		{"nested lists", "type/list-list-int", "type/list-list-i32", secondTypeIsNarrower},
		{"a list against a map", "type/list-int", "type/map-int", relDisjoint},
		{"enum members sharing no value, one list never empty", "type/list-enum-str-min", "type/list-enum-int", relDisjoint},
		{"enum members sharing no value, both lists admitting the empty one", "type/list-enum-str", "type/list-enum-int", relOverlap},
		{"a map value", "type/map-int", "type/map-i32", secondTypeIsNarrower},
		{"maps with different keys", "type/map-i32", "type/map-i32-keyed", relUnrelated},
		{"nesting past the cap", "type/cycle", "type/cycle-b", relUnrelated},
		{"an enum within another of a broader value type", "type/enum-integer-1", "type/enum-number-12", firstTypeIsNarrower},
		{"one set of values of a narrower value type", "type/enum-integer-12", "type/enum-number-12", firstTypeIsNarrower},
		{"one set of values of another type in another order", "type/enum-integer-12", "type/enum-number-21", relOverlap},
		{"an enum within another of a narrower value type", "type/enum-number-1", "type/enum-integer-12", relMeet},
		{"enums of different value types sharing some values", "type/enum-number-12.5", "type/enum-integer-13", relMeet},
		{"enums of different value types sharing none", "type/enum-number-2.5", "type/enum-integer-13", relDisjoint},
		{"enums of overlapping value types sharing values", "type/enum-int32-12", "type/enum-uint32-12", relOverlap},
		{"enums of overlapping value types sharing none", "type/enum-int32-1", "type/enum-uint32-2", relDisjoint},
		{"an alias of an enum no member of which a kind admits", "type/alias-enum-str-max", primitive(ir.PrimitiveUUID), relDisjoint},
		{"an alias of a const a kind refuses", "type/alias-lit-a-max", primitive(ir.PrimitiveInteger), relDisjoint},
		{"an alias of a const a kind admits", "type/alias-lit-a-max", primitive(ir.PrimitiveString), firstTypeIsNarrower},
		{"an alias of an enum against a node of a kind no member meets", "type/alias-enum-big-max", "type/scalar-i32-doc", relDisjoint},
		{"an alias of an enum against the enum", "type/alias-enum-ab", "type/enum-ab", firstTypeIsNarrower},
		{"an alias of an enum against one it meets", "type/alias-enum-ab", "type/enum-bc", relMeet},
		{"an alias of a list against one it meets", "type/alias-list-i32-min", "type/list-i32-max", relMeet},
		{"an alias of a time scalar holding a bound a time meets", "type/lit-time", "type/alias-scalar-time", firstTypeIsNarrower},
		{"an alias of a time holding docs a time scalar gives otherwise", "type/alias-lit-time-doc", "type/scalar-time-doc", relOverlap},
		{"two aliases of one list saying it otherwise, judged as two scalars", "type/alias-list-i32-min", "type/alias-list-i32-min-doc", relUnrelated},
		{"a time against an alias of a time scalar the view cannot read", "type/lit-time", "type/alias-time-ref", relOverlap},
		{"two enums differing in closedness", "type/enum-str", "type/enum-str-closed", relOverlap},
		{"an alias of the top type against the top it stands for", "type/alias-doc-scalar-any", "type/scalar-any", firstTypeIsNarrower},
		{"an alias adding null to an enum against the enum", "type/alias-enum-str-null", "type/enum-str", secondTypeIsNarrower},
		{"an alias adding null to an enum against a kind holding it", "type/alias-enum-str-null", primitive(ir.PrimitiveString), relOverlap},
	} {
		want := tc.want
		assert.Equal(t, want, typeJudgeValue.relate(ref(tc.a), ref(tc.b)), tc.name)
		assert.Equal(t, want.flipped(), typeJudgeValue.relate(ref(tc.b), ref(tc.a)), "%s, swapped", tc.name)
	}

	nullTwin := ref("type/lit-a-h2")
	nullTwin.Nullable = true
	assert.Equal(t, secondTypeIsNarrower, typeJudgeValue.relate(nullTwin, ref("type/lit-a-h1")), "identical hoisted nodes order by null")
	assert.Equal(t, firstTypeIsNarrower, typeJudgeValue.relate(ref("type/lit-a-h1"), nullTwin), "in either order")

	nullInt := primitiveRef(ir.PrimitiveInteger)
	nullInt.Nullable = true
	assert.Equal(t, firstTypeIsNarrower, typeJudgeValue.relate(ref("type/lit-null"), nullInt), "a null literal against a nullable integer")
	assert.Equal(t, secondTypeIsNarrower, typeJudgeValue.relate(nullInt, ref("type/lit-null")), "a nullable integer against a null literal")

	nullString := primitiveRef(ir.PrimitiveString)
	nullString.Nullable = true
	assert.Equal(t, firstTypeIsNarrower, typeJudgeValue.relate(ref("type/alias-enum-str-null"), nullString),
		"an alias admitting null within a kind whose reference admits null too")

	nullTime := ref("type/scalar-time")
	nullTime.Nullable = true
	assert.Equal(t, firstTypeIsNarrower, typeJudgeValue.relate(ref("type/lit-null"), nullTime), "a null literal against a time scalar admitting null")
	assert.Equal(t, relDisjoint, typeJudgeValue.relate(ref("type/lit-null"), ref("type/scalar-time")), "and against one that does not")
}

// TestRelateKinds_ReadsAHeldFormatAsAStringFormat pins formatAgainst on the
// kinds no lowered pair reaches: the top, which a field's judgment settles
// before reading kinds, and a second held format, which only time is today.
func TestRelateKinds_ReadsAHeldFormatAsAStringFormat(t *testing.T) {
	t.Parallel()
	held := valueKind{primitive: ir.PrimitiveString, format: "time"}
	for _, tc := range []struct {
		name string
		k    valueKind
		want relation
	}{
		{"the top", valueKind{primitive: ir.PrimitiveAny}, firstTypeIsNarrower},
		{"itself", held, relSame},
		{"another held format", valueKind{primitive: ir.PrimitiveString, format: "other"}, relDisjoint},
		{"string", valueKind{primitive: ir.PrimitiveString}, firstTypeIsNarrower},
		{"a string format", valueKind{primitive: ir.PrimitiveDate}, relDisjoint},
	} {
		assert.Equal(t, tc.want, relateKinds(held, tc.k), tc.name)
		assert.Equal(t, tc.want.flipped(), relateKinds(tc.k, held), "%s, swapped", tc.name)
	}
}

// TestKindOf_TheFirstEncodingDecidesTheFormat pins which Encoding on a Base
// chain names the held format: the first met, read only over a chain ending in
// string, so an alias encoding a time otherwise, or a time format over another
// kind, holds none.
func TestKindOf_TheFirstEncodingDecidesTheFormat(t *testing.T) {
	t.Parallel()
	typeJudgeValue := typeJudge{resolve: stubResolve(judgeRegistry()), sites: atRoot(&stubWalk{})}
	for _, tc := range []struct {
		id   ir.TypeID
		want valueKind
	}{
		{"type/scalar-time", valueKind{primitive: ir.PrimitiveString, format: "time"}},
		{"type/alias-time", valueKind{primitive: ir.PrimitiveString, format: "time"}},
		{"type/alias-encoded-time", valueKind{primitive: ir.PrimitiveString}},
		{"type/scalar-time-int", valueKind{primitive: ir.PrimitiveInteger}},
		{"type/scalar-time-media", valueKind{primitive: ir.PrimitiveString}},
		{"type/scalar-time-wire", valueKind{primitive: ir.PrimitiveString}},
		{"type/scalar-time-schema", valueKind{primitive: ir.PrimitiveString}},
		{"type/scalar-email", valueKind{primitive: ir.PrimitiveString}},
	} {
		got, ok := typeJudgeValue.kindOf(ref(tc.id))
		require.True(t, ok, "%s", tc.id)
		assert.Equal(t, tc.want.primitive, got.primitive, "%s", tc.id)
		assert.Equal(t, tc.want.format, got.format, "%s: the held format", tc.id)
	}
}

// encodedRegistry is narrowingRegistry plus a Scalar for each way an Encoding
// writes a value: under a WireType its base is or is not spelled in, under a
// name with no WireType, and under a WireType or base that is no primitive.
func encodedRegistry() map[ir.TypeID]ir.TypeDef {
	reg := narrowingRegistry()
	scalar := func(id ir.TypeID, base ir.TypeID, enc ir.Encoding) {
		reg[id] = &ir.Scalar{ID: id, Base: &ir.TypeRef{Target: base}, Encoding: &enc}
	}
	wire := func(primitiveKind ir.PrimitiveKind) *ir.TypeRef {
		typeRef := primitiveRef(primitiveKind)
		return &typeRef
	}
	offset, bytes := primitiveRef(ir.PrimitiveDatetimeOffset).Target, primitiveRef(ir.PrimitiveBytes).Target
	scalar("type/unixtime-int", offset, ir.Encoding{Name: "unixTimestamp", WireType: wire(ir.PrimitiveInteger)})
	scalar("type/unixtime-num", offset, ir.Encoding{Name: "unixTimestamp", WireType: wire(ir.PrimitiveNumber)})
	scalar("type/unixtime-str", offset, ir.Encoding{Name: "unixTimestamp", WireType: wire(ir.PrimitiveString)})
	scalar("type/http-date", offset, ir.Encoding{Name: "http-date"})
	scalar("type/offset-base64", offset, ir.Encoding{Name: "base64"})
	scalar("type/offset-media", offset, ir.Encoding{MediaType: "text/plain"})
	scalar("type/offset-time", offset, ir.Encoding{Name: "time"})
	scalar("type/byte", bytes, ir.Encoding{Name: "base64", WireType: wire(ir.PrimitiveString)})
	scalar("type/base64url", bytes, ir.Encoding{Name: "base64url", WireType: wire(ir.PrimitiveString)})
	scalar("type/number-text", primitiveRef(ir.PrimitiveInt64).Target, ir.Encoding{Name: "string", WireType: wire(ir.PrimitiveString)})
	scalar("type/string-email", primitiveRef(ir.PrimitiveString).Target, ir.Encoding{Name: "email"})
	scalar("type/int-weird", primitiveRef(ir.PrimitiveInteger).Target, ir.Encoding{Name: "weird"})
	scalar("type/wire-gone", offset, ir.Encoding{Name: "x", WireType: &ir.TypeRef{Target: "type/absent"}})
	scalar("type/wire-node", offset, ir.Encoding{Name: "x", WireType: &ir.TypeRef{Target: "type/unixtime-int"}})
	scalar("type/alias-wired", "type/http-date", ir.Encoding{Name: "x", WireType: wire(ir.PrimitiveInteger)})
	reg["type/alias-unixtime-int"] = &ir.Scalar{ID: "type/alias-unixtime-int", Base: &ir.TypeRef{Target: "type/unixtime-int"}}
	reg["type/enum-http-date"] = &ir.Enum{ID: "type/enum-http-date", ValueType: ir.PrimitiveString,
		Members: []ir.EnumMember{{Value: stringValue("Sun, 06 Nov 1994 08:49:37 GMT")}}}
	reg["type/enum-base64"] = &ir.Enum{ID: "type/enum-base64", ValueType: ir.PrimitiveString,
		Members: []ir.EnumMember{{Value: stringValue("MjAyMA==")}}}
	reg["type/enum-seconds"] = &ir.Enum{ID: "type/enum-seconds", ValueType: ir.PrimitiveInteger,
		Members: []ir.EnumMember{{Value: number("1700000000")}}}
	return reg
}

// TestKindOf_AnEncodedScalarIsReadAsItsValuesTravel pins travelsAs and
// rewritesText: a Scalar whose WireType does not spell its base is read as
// that type, as seconds written as an integer are an integer, while a base
// its WireType spells keeps its kind, as bytes do under base64; and a string
// format whose text an encoding names otherwise is unjudged, as an http-date
// is no RFC 3339 text (docs/compilers/openapi.md#allof-redeclared-property).
// A WireType or base that is no primitive leaves the base read as it is.
func TestKindOf_AnEncodedScalarIsReadAsItsValuesTravel(t *testing.T) {
	t.Parallel()
	typeJudgeValue := typeJudge{resolve: stubResolve(encodedRegistry()), sites: atRoot(&stubWalk{})}
	for id, want := range map[ir.TypeID]valueKind{
		"type/unixtime-int":       {primitive: ir.PrimitiveInteger},
		"type/alias-unixtime-int": {primitive: ir.PrimitiveInteger},
		"type/unixtime-num":       {primitive: ir.PrimitiveNumber},
		"type/unixtime-str":       {primitive: ir.PrimitiveDatetimeOffset, unjudged: true},
		"type/http-date":          {primitive: ir.PrimitiveDatetimeOffset, unjudged: true},
		"type/offset-base64":      {primitive: ir.PrimitiveDatetimeOffset, unjudged: true},
		"type/offset-media":       {primitive: ir.PrimitiveDatetimeOffset},
		"type/offset-time":        {primitive: ir.PrimitiveDatetimeOffset, unjudged: true},
		"type/byte":               {primitive: ir.PrimitiveBytes, unjudged: true},
		"type/number-text":        {primitive: ir.PrimitiveString},
		"type/string-email":       {primitive: ir.PrimitiveString},
		"type/int-weird":          {primitive: ir.PrimitiveInteger},
		"type/wire-gone":          {primitive: ir.PrimitiveDatetimeOffset, unjudged: true},
		"type/wire-node":          {primitive: ir.PrimitiveDatetimeOffset, unjudged: true},
		"type/alias-wired":        {primitive: ir.PrimitiveDatetimeOffset, unjudged: true},
	} {
		got, ok := typeJudgeValue.kindOf(ref(id))
		require.True(t, ok, "%s", id)
		assert.Equal(t, want, got, "%s", id)
	}
}

// TestTypeJudge_AnEncodedScalarRelatesAsItsValues pins what those kinds
// decide: unixtime on an integer is narrower than integer and number, overlaps
// int64 and shares nothing with a string, as an integer would; an http-date is
// narrower than datetime_offset and string, as byte is than bytes; and an enum
// of text an encoding writes overlaps the scalar where the base's grammar
// would refuse the text, while a bare datetime_offset still refuses it.
func TestTypeJudge_AnEncodedScalarRelatesAsItsValues(t *testing.T) {
	t.Parallel()
	typeJudgeValue := typeJudge{resolve: stubResolve(encodedRegistry()), sites: atRoot(&stubWalk{})}
	primitive := func(primitiveKind ir.PrimitiveKind) ir.TypeID { return primitiveRef(primitiveKind).Target }
	for _, tc := range []struct {
		a, b ir.TypeID
		want relation
	}{
		{"type/unixtime-int", primitive(ir.PrimitiveInteger), firstTypeIsNarrower},
		{"type/unixtime-int", primitive(ir.PrimitiveNumber), firstTypeIsNarrower},
		{"type/unixtime-int", primitive(ir.PrimitiveInt64), relOverlap},
		{"type/unixtime-int", primitive(ir.PrimitiveString), relDisjoint},
		{"type/unixtime-int", primitive(ir.PrimitiveDatetimeOffset), relDisjoint},
		{"type/unixtime-int", "type/http-date", relDisjoint},
		{"type/unixtime-int", "type/enum-seconds", relOverlap},
		{"type/unixtime-num", primitive(ir.PrimitiveInteger), relOverlap},
		{"type/unixtime-str", primitive(ir.PrimitiveString), firstTypeIsNarrower},
		{"type/unixtime-str", primitive(ir.PrimitiveInteger), relDisjoint},
		{"type/http-date", primitive(ir.PrimitiveDatetimeOffset), firstTypeIsNarrower},
		{"type/http-date", primitive(ir.PrimitiveString), firstTypeIsNarrower},
		{"type/http-date", primitive(ir.PrimitiveInteger), relDisjoint},
		{"type/http-date", "type/enum-http-date", relOverlap},
		{"type/offset-base64", "type/enum-base64", relOverlap},
		{"type/byte", primitive(ir.PrimitiveBytes), firstTypeIsNarrower},
		{"type/byte", "type/base64url", relOverlap},
		{"type/enum-http-date", primitive(ir.PrimitiveDatetimeOffset), relDisjoint},
	} {
		assert.Equal(t, tc.want, typeJudgeValue.relate(ref(tc.a), ref(tc.b)), "%s against %s", tc.a, tc.b)
		assert.Equal(t, tc.want.flipped(), typeJudgeValue.relate(ref(tc.b), ref(tc.a)), "%s against %s", tc.b, tc.a)
	}
}

// TestValueKind_AnUnjudgedKindAdmitsNothing pins admits and literalAgainst on
// a kind an encoding leaves unjudged: no value is shown to be one of it, an
// RFC 3339 text no more than any, so a literal overlaps it rather than being
// narrower or disjoint, as against bytes.
func TestValueKind_AnUnjudgedKindAdmitsNothing(t *testing.T) {
	t.Parallel()
	offset := valueKind{primitive: ir.PrimitiveDatetimeOffset}
	unjudged := valueKind{primitive: ir.PrimitiveDatetimeOffset, unjudged: true}
	for _, value := range []ir.Value{stringValue("2020-01-01T00:00:00Z"), stringValue("Sun, 06 Nov 1994 08:49:37 GMT"), number("1")} {
		assert.False(t, unjudged.admits(value), "%v", value)
		assert.Equal(t, relOverlap, literalAgainst(value, unjudged, false), "%v", value)
	}
	assert.True(t, offset.admits(stringValue("2020-01-01T00:00:00Z")), "the judged kind reads its own text")
	assert.Equal(t, relDisjoint, literalAgainst(stringValue("Sun, 06 Nov 1994 08:49:37 GMT"), offset, false))
}

// TestKindOf_ANumberAsTextIsAString pins travelsAs: a Scalar whose Encoding
// carries a number as a string has a string's instances, so it relates to a
// string and not to its number, while bytes, which JSON spells as a string
// already, keeps its own kind however it is encoded, a kind with no judge
// (docs/compilers/openapi.md#string-formats).
func TestKindOf_ANumberAsTextIsAString(t *testing.T) {
	t.Parallel()
	wire := func() *ir.Encoding {
		typeRef := primitiveRef(ir.PrimitiveString)
		return &ir.Encoding{Name: "string", WireType: &typeRef}
	}
	reg := map[ir.TypeID]ir.TypeDef{}
	for _, primitiveKind := range []ir.PrimitiveKind{ir.PrimitiveString, ir.PrimitiveInt64, ir.PrimitiveBytes} {
		reg[primitiveRef(primitiveKind).Target] = &ir.Primitive{ID: primitiveRef(primitiveKind).Target, PrimitiveKind: primitiveKind}
	}
	i64, b := primitiveRef(ir.PrimitiveInt64), primitiveRef(ir.PrimitiveBytes)
	reg["type/num-text"] = &ir.Scalar{ID: "type/num-text", Base: &i64, Encoding: wire()}
	reg["type/alias-num-text"] = &ir.Scalar{ID: "type/alias-num-text", Base: &ir.TypeRef{Target: "type/num-text"}}
	reg["type/num"] = &ir.Scalar{ID: "type/num", Base: &i64, Encoding: &ir.Encoding{Name: "string"}}
	reg["type/bytes-b64"] = &ir.Scalar{ID: "type/bytes-b64", Base: &b, Encoding: wire()}
	typeJudgeValue := typeJudge{resolve: stubResolve(reg), sites: atRoot(&stubWalk{})}
	for id, want := range map[ir.TypeID]valueKind{
		"type/num-text": {primitive: ir.PrimitiveString}, "type/alias-num-text": {primitive: ir.PrimitiveString}, "type/num": {primitive: ir.PrimitiveInt64},
		"type/bytes-b64": {primitive: ir.PrimitiveBytes, unjudged: true},
	} {
		got, ok := typeJudgeValue.kindOf(ref(id))
		require.True(t, ok, "%s", id)
		assert.Equal(t, want, got, "%s", id)
	}
	stringValue, _ := typeJudgeValue.kindOf(primitiveRef(ir.PrimitiveString))
	number, _ := typeJudgeValue.kindOf(primitiveRef(ir.PrimitiveInt64))
	text, _ := typeJudgeValue.kindOf(ref("type/num-text"))
	assert.Equal(t, relSame, relateKinds(stringValue, text), "a string and a number as text share their instances")
	assert.Equal(t, relDisjoint, relateKinds(number, text), "a JSON number is no string")
}

func TestTypeJudge_AdmitsPerNode(t *testing.T) {
	t.Parallel()
	reg := judgeRegistry()
	reg["type/enum-1"] = &ir.Enum{ID: "type/enum-1", ValueType: ir.PrimitiveInteger,
		Members: []ir.EnumMember{{Value: number("1")}, {Value: number("2")}}}
	reg["type/list-1"] = &ir.List{ID: "type/list-1", ElementType: ref("type/enum-1")}
	reg["type/map-1"] = &ir.MapT{ID: "type/map-1", Value: ref("type/enum-1")}
	short, two := int64(3), int64(2)
	reg["type/scalar-short"] = &ir.Scalar{ID: "type/scalar-short", Base: &ir.TypeRef{Target: "type/primitive/string"},
		Constraints: &ir.Constraints{MaxLength: &short}}
	reg["type/list-pair"] = &ir.List{ID: "type/list-pair", ElementType: ref("type/enum-1"), Constraints: &ir.Constraints{MaxItems: &two}}
	reg["type/map-pair"] = &ir.MapT{ID: "type/map-pair", Value: ref("type/enum-1"), Constraints: &ir.Constraints{MaxProperties: &two}}
	typeJudgeValue := typeJudge{resolve: stubResolve(reg), sites: atRoot(&stubWalk{})}
	list := func(vs ...ir.Value) ir.Value { return ir.Value{Kind: ir.ValueList, List: vs} }
	obj := func(value ir.Value) ir.Value {
		return ir.Value{Kind: ir.ValueObject, Object: []ir.Field{{Name: "k", Value: value}}}
	}

	assert.True(t, typeJudgeValue.admits(ref("type/enum-1"), number("1.0")), "an enum member by magnitude")
	assert.False(t, typeJudgeValue.admits(ref("type/enum-1"), number("3")))
	assert.True(t, typeJudgeValue.admits(ref("type/lit-a"), stringValue("a")))
	assert.False(t, typeJudgeValue.admits(ref("type/lit-a"), stringValue("b")))
	assert.True(t, typeJudgeValue.admits(ref("type/scalar-int"), number("3")))
	assert.False(t, typeJudgeValue.admits(ref("type/scalar-enc"), number("3")), "an encoded scalar admits nothing")
	assert.False(t, typeJudgeValue.admits(ref("type/scalar-opaque"), number("3")), "an opaque scalar admits nothing")
	assert.True(t, typeJudgeValue.admits(ref("type/scalar-time"), stringValue("12:00:00Z")), "a held format admits what its grammar does")
	assert.False(t, typeJudgeValue.admits(ref("type/scalar-time"), stringValue("12:00:00")), "and refuses what it does not")
	assert.False(t, typeJudgeValue.admits(ref("type/scalar-time-max"), stringValue("12:00:00Z")), "a held format's bounds are judged too")
	assert.True(t, typeJudgeValue.admits(ref("type/alias-time"), stringValue("12:00:00Z")), "an alias of it admits what it does")
	assert.False(t, typeJudgeValue.admits(ref("type/scalar-time-media"), stringValue("12:00:00Z")), "an encoding saying more admits nothing")
	assert.False(t, typeJudgeValue.admits(ref("type/scalar-email"), stringValue("a@b.c")), "a format with no grammar here admits nothing")
	assert.False(t, typeJudgeValue.admits(ref("type/scalar-time-int"), stringValue("12:00:00Z")), "nor does a time format over an integer")
	assert.True(t, typeJudgeValue.admits(ref("type/any"), stringValue("a")))
	assert.False(t, typeJudgeValue.admits(ref("type/model"), stringValue("a")))
	assert.False(t, typeJudgeValue.admits(ref("type/absent"), stringValue("a")))
	assert.True(t, typeJudgeValue.admits(ref("type/list-1"), list(number("1"), number("2"))))
	assert.False(t, typeJudgeValue.admits(ref("type/list-1"), list(number("1"), number("3"))))
	assert.False(t, typeJudgeValue.admits(ref("type/list-1"), stringValue("a")))
	assert.True(t, typeJudgeValue.admits(ref("type/map-1"), obj(number("2"))))
	assert.False(t, typeJudgeValue.admits(ref("type/map-1"), obj(number("3"))))
	assert.False(t, typeJudgeValue.admits(ref("type/map-1"), list(number("1"))))
	assert.True(t, typeJudgeValue.admits(ref("type/scalar-short"), stringValue("abc")), "a scalar's constraints admit what meets them")
	assert.False(t, typeJudgeValue.admits(ref("type/scalar-short"), stringValue("abcd")), "and refuse what does not")
	assert.False(t, typeJudgeValue.admits(ref("type/list-pair"), list(number("1"), number("2"), number("1"))), "a list's constraints are judged")
	pairs := ir.Value{Kind: ir.ValueObject, Object: []ir.Field{{Name: "a", Value: number("1")}, {Name: "b", Value: number("1")},
		{Name: "c", Value: number("2")}}}
	assert.False(t, typeJudgeValue.admits(ref("type/map-pair"), pairs), "and so are a map's")
	assert.False(t, typeJudgeValue.admits(ref("type/cycle"), list(list(list(list(list(list(list(list(list(list())))))))))),
		"a list nested past the cap is refused")
}

// TestRegistry_RefusesAtWhatAJudgeRefuses pins refusesAt to what admitsAt
// refuses for a reason: a value of another kind, one outside an enum or a
// literal, one a bound, a held format or a type below refuses, each of a list's
// items and a map's values among them, and a null where none is admitted. What
// admitsAt refuses for want of a judge, it does not: a pattern, a precision or
// a scale, the text of bytes or of a format not held, a model, a union, a
// tuple, an opaque scalar, a type the registry does not hold and a cycle past
// maxJudgeDepth.
func TestRegistry_RefusesAtWhatAJudgeRefuses(t *testing.T) {
	t.Parallel()
	reg := judgeRegistry()
	scalar := func(id, base ir.TypeID, encoding *ir.Encoding, constraints ir.Constraints) {
		reg[id] = &ir.Scalar{ID: id, Base: &ir.TypeRef{Target: base}, Encoding: encoding, Constraints: &constraints}
	}
	reg["type/union"] = &ir.Union{ID: "type/union", Closed: true}
	reg["type/tuple"] = &ir.Tuple{ID: "type/tuple", ElementTypes: []ir.TypeRef{primitiveRef(ir.PrimitiveInteger)}}
	scalar("type/patterned", "type/primitive/string", nil, ir.Constraints{Pattern: "b$", MaxLength: new(int64(2))})
	scalar("type/precise", "type/primitive/number", nil, ir.Constraints{Precision: new(int64(1)), Scale: new(int64(0))})
	scalar("type/email", "type/primitive/string", &ir.Encoding{Name: "email"}, ir.Constraints{MaxLength: new(int64(2))})
	scalar("type/http-date", "type/primitive/datetime_offset", &ir.Encoding{Name: "http-date"}, ir.Constraints{})
	scalar("type/scalar-cycle", "type/scalar-cycle", nil, ir.Constraints{})
	reg["type/map-cycle"] = &ir.MapT{ID: "type/map-cycle", Value: ref("type/map-cycle")}
	scalar("type/scalar-model", "type/model", nil, ir.Constraints{})
	for id, element := range map[ir.TypeID]ir.TypeID{"type/patterns": "type/patterned", "type/models": "type/model"} {
		reg[id] = &ir.List{ID: id, ElementType: ref(element), Constraints: &ir.Constraints{MaxItems: new(int64(1))}}
		reg[id+"-map"] = &ir.MapT{ID: id + "-map", Value: ref(element), Constraints: &ir.Constraints{MaxProperties: new(int64(1))}}
	}
	registryValue := registry{resolve: stubResolve(reg)}
	text := func(v string) ir.Value { return ir.Value{Kind: ir.ValueString, String: v} }
	list := func(vs ...ir.Value) ir.Value { return ir.Value{Kind: ir.ValueList, List: vs} }
	object := func(vs ...ir.Value) ir.Value {
		out := ir.Value{Kind: ir.ValueObject}
		for i, value := range vs {
			out.Object = append(out.Object, ir.Field{Name: fmt.Sprint(i), Value: value})
		}
		return out
	}
	nested := func(depth int) ir.Value {
		value := number("1")
		for range depth {
			value = list(value)
		}
		return value
	}
	nestedMap := func(depth int) ir.Value {
		value := number("1")
		for range depth {
			value = object(value)
		}
		return value
	}
	null := ir.Value{Kind: ir.ValueNull}
	for _, tc := range []struct {
		id      ir.TypeID
		value   ir.Value
		refuses bool
	}{
		{"type/primitive/integer", number("1"), false},
		{"type/primitive/integer", text("a"), true},
		{"type/primitive/integer", null, true},
		{"type/primitive/bytes", text("a"), false},
		{"type/primitive/bytes", number("1"), true},
		{"type/primitive/any", null, false},
		{"type/any", null, true},
		{"type/any", number("1"), false},
		{"type/enum-int", text("a"), true},
		{"type/lit-a", text("a"), false},
		{"type/lit-a", text("b"), true},
		{"type/patterned", text("a"), false},
		{"type/patterned", text("aaa"), true},
		{"type/patterned", number("1"), true},
		{"type/precise", number("1.25"), false},
		{"type/precise", text("1"), true},
		{"type/email", text("a"), false},
		{"type/email", text("aaa"), true},
		{"type/email", number("1"), true},
		{"type/http-date", text("Sun, 06 Nov 1994"), false},
		{"type/http-date", number("1"), true},
		{"type/scalar-time", text("10:00:00Z"), false},
		{"type/scalar-time", text("a"), true},
		{"type/scalar-opaque", number("1"), false},
		{"type/scalar-model", number("1"), false},
		{"type/patterns", list(text("a")), false},
		{"type/patterns", list(text("aaa")), true},
		{"type/patterns", list(text("a"), text("a")), true},
		{"type/patterns", text("a"), true},
		{"type/patterns-map", object(text("a")), false},
		{"type/patterns-map", object(text("aaa")), true},
		{"type/patterns-map", object(text("a"), text("a")), true},
		{"type/patterns-map", list(), true},
		{"type/models", list(number("1")), false},
		{"type/models-map", object(number("1")), false},
		{"type/model", number("1"), false},
		{"type/union", number("1"), false},
		{"type/tuple", number("1"), false},
		{"type/absent", number("1"), false},
		{"type/scalar-cycle", number("1"), false},
		{"type/map-cycle", nestedMap(maxJudgeDepth), true},
		{"type/map-cycle", nestedMap(maxJudgeDepth + 1), false},
		{"type/cycle", nested(maxJudgeDepth), true},
		{"type/cycle", nested(maxJudgeDepth + 1), false},
	} {
		assert.Equal(t, tc.refuses, registryValue.refusesAt(ref(tc.id), tc.value, 0), "%s of %s", tc.id, tc.value.Kind)
	}
	nullable := ref("type/primitive/integer")
	nullable.Nullable = true
	assert.False(t, registryValue.refusesAt(nullable, null, 0))
}

func TestRelation_Flipped(t *testing.T) {
	t.Parallel()
	for _, relationValue := range []relation{relUnrelated, relDisjoint, relOverlap, relSame, relMeet} {
		assert.Equal(t, relationValue, relationValue.flipped())
	}
	assert.Equal(t, secondTypeIsNarrower, firstTypeIsNarrower.flipped())
	assert.Equal(t, firstTypeIsNarrower, secondTypeIsNarrower.flipped())
}

func TestTypeJudge_NullIsJudgedByNullability(t *testing.T) {
	t.Parallel()
	null := ir.Value{Kind: ir.ValueNull}
	typeJudgeValue := typeJudge{resolve: stubResolve(narrowingRegistry()), sites: atRoot(&stubWalk{})}
	nullable := func(primitiveKind ir.PrimitiveKind) ir.TypeRef {
		typeRef := primitiveRef(primitiveKind)
		typeRef.Nullable = true
		return typeRef
	}

	assert.True(t, typeJudgeValue.admits(nullable(ir.PrimitiveInteger), null))
	assert.False(t, typeJudgeValue.admits(primitiveRef(ir.PrimitiveInteger), null))
	assert.False(t, typeJudgeValue.admits(nullable(ir.PrimitiveInteger), stringValue("x")))
	assert.True(t, typeJudgeValue.admits(primitiveRef(ir.PrimitiveInteger), number("1")))
	assert.True(t, typeJudgeValue.admits(primitiveRef(ir.PrimitiveAny), null), "the top type admits null whatever the bit says")
}

// TestClassify_ComparesResolvedKinds pins the last-resort comparison classify
// makes when neither side ends in a primitive kind. Two structures of one
// instance type are not provably contradictory, and neither is anything against
// a union or an opaque scalar, which say nothing of their values: only two
// structures of different instance types are disjoint. A list and a tuple are
// one instance type, as a model and a map are, a literal holding an object or a
// list is that instance type, and an alias is judged as the type it stands for.
func TestClassify_ComparesResolvedKinds(t *testing.T) {
	t.Parallel()
	stubbed, _ := stubMerger(map[ir.TypeID]ir.TypeDef{
		"type/model":       declaring("type/model"),
		"type/other":       declaring("type/other"),
		"type/derived":     &ir.Model{ID: "type/derived", Base: &ir.TypeRef{Target: "type/model"}},
		"type/grandchild":  &ir.Model{ID: "type/grandchild", Base: &ir.TypeRef{Target: "type/derived"}},
		"type/mixed":       &ir.Model{ID: "type/mixed", Mixins: []ir.TypeRef{{Target: "type/other"}, {Target: "type/model"}}},
		"type/via-alias":   &ir.Model{ID: "type/via-alias", Base: &ir.TypeRef{Target: "type/alias-model"}},
		"type/dangling":    &ir.Model{ID: "type/dangling", Base: &ir.TypeRef{Target: "type/absent"}},
		"type/partly-gone": &ir.Model{ID: "type/partly-gone", Mixins: []ir.TypeRef{{Target: "type/derived"}, {Target: "type/absent"}}},
		"type/scalar":      &ir.Scalar{ID: "type/scalar", Base: &ir.TypeRef{Target: "type/primitive/string"}},
		"type/scalar-on":   &ir.Scalar{ID: "type/scalar-on", Base: &ir.TypeRef{Target: "type/scalar"}},
		"type/cycle-a":     &ir.Model{ID: "type/cycle-a", Base: &ir.TypeRef{Target: "type/cycle-b"}},
		"type/cycle-b":     &ir.Model{ID: "type/cycle-b", Base: &ir.TypeRef{Target: "type/cycle-a"}},
		"type/bare":        &ir.Model{ID: "type/bare"},
		"type/bare-twin":   &ir.Model{ID: "type/bare-twin"},
		"type/bare-titled": &ir.Model{ID: "type/bare-titled", Docs: ir.Docs{Summary: "s"}},
		"type/alias-bare":  &ir.Scalar{ID: "type/alias-bare", Base: &ir.TypeRef{Target: "type/bare"}},
		"type/map":         &ir.MapT{ID: "type/map"},
		"type/list":        &ir.List{ID: "type/list", ElementType: ir.TypeRef{Target: "type/other"}},
		"type/tuple":       &ir.Tuple{ID: "type/tuple"},
		"type/union":       &ir.Union{ID: "type/union", Closed: true},
		"type/holds":       &ir.Union{ID: "type/holds", Variants: variantsOf("type/list", "type/model")},
		"type/one-holds":   &ir.Union{ID: "type/one-holds", Exclusive: true, Variants: variantsOf("type/list", "type/model")},
		"type/two-hold":    &ir.Union{ID: "type/two-hold", Exclusive: true, Variants: variantsOf("type/model", "type/bare")},
		"type/may-hold":    &ir.Union{ID: "type/may-hold", Exclusive: true, Variants: variantsOf("type/model", "type/other")},
		"type/routes":      &ir.Union{ID: "type/routes", Variants: variantsOf("type/model"), Discriminator: &ir.Discriminator{PropertyName: "k"}},
		"type/keyed":       &ir.Union{ID: "type/keyed", Variants: variantsOf("type/model"), WireTagged: true},
		"type/of-models":   &ir.Union{ID: "type/of-models", Closed: true, Variants: variantsOf("type/model", "type/derived")},
		"type/closed-mix":  &ir.Union{ID: "type/closed-mix", Closed: true, Variants: variantsOf("type/model", "type/list")},
		"type/open-models": &ir.Union{ID: "type/open-models", Variants: variantsOf("type/model", "type/derived")},
		"type/routed":      &ir.Union{ID: "type/routed", Closed: true, Variants: variantsOf("type/model", "type/derived"), Discriminator: &ir.Discriminator{PropertyName: "k"}},
		"type/bounded":     &ir.Union{ID: "type/bounded", Variants: variantsOf("type/list", "type/model"), Constraints: &ir.Constraints{MinProperties: new(int64(1))}},
		"type/bounded-of-models": &ir.Union{ID: "type/bounded-of-models", Closed: true, Variants: variantsOf("type/model", "type/derived"),
			Constraints: &ir.Constraints{MinProperties: new(int64(1))}},
		"type/holds-alias":      &ir.Scalar{ID: "type/holds-alias", Base: &ir.TypeRef{Target: "type/holds"}, Constraints: &ir.Constraints{MinItems: new(int64(1))}},
		"type/plain-alias":      &ir.Scalar{ID: "type/plain-alias", Base: &ir.TypeRef{Target: "type/holds"}},
		"type/external":         &ir.External{ID: "type/external"},
		"type/opaque":           &ir.Scalar{ID: "type/opaque"},
		"type/literal":          &ir.Literal{ID: "type/literal", Value: ir.Value{Kind: ir.ValueString, String: "a"}},
		"type/lit-object":       &ir.Literal{ID: "type/lit-object", Value: ir.Value{Kind: ir.ValueObject}},
		"type/lit-list":         &ir.Literal{ID: "type/lit-list", Value: ir.Value{Kind: ir.ValueList}},
		"type/alias-model":      &ir.Scalar{ID: "type/alias-model", Base: &ir.TypeRef{Target: "type/model"}},
		"type/alias-chain":      &ir.Scalar{ID: "type/alias-chain", Base: &ir.TypeRef{Target: "type/alias-model"}},
		"type/alias-list":       &ir.Scalar{ID: "type/alias-list", Base: &ir.TypeRef{Target: "type/list"}},
		"type/alias-gone":       &ir.Scalar{ID: "type/alias-gone", Base: &ir.TypeRef{Target: "type/absent"}},
		"type/alias-loop":       &ir.Scalar{ID: "type/alias-loop", Base: &ir.TypeRef{Target: "type/alias-loop"}},
		"type/primitive/string": &ir.Primitive{ID: "type/primitive/string", PrimitiveKind: ir.PrimitiveString},
	})
	for _, tc := range []struct {
		name string
		a, b ir.TypeID
		want relation
	}{
		{"two models", "type/model", "type/other", relUnrelated},
		{"a model and a map are one instance type", "type/model", "type/map", relUnrelated},
		{"a list and a tuple are one instance type", "type/list", "type/tuple", relUnrelated},
		{"a model and a list", "type/model", "type/list", relDisjoint},
		{"a map and a tuple", "type/map", "type/tuple", relDisjoint},
		{"a model and a union that may hold it", "type/model", "type/union", relUnrelated},
		{"a model and an inclusive union one variant of which holds it", "type/model", "type/holds", firstTypeIsNarrower},
		{"a model and an exclusive union one variant of which alone holds it", "type/model", "type/one-holds", firstTypeIsNarrower},
		{"a model and an exclusive union two variants of which hold it", "type/model", "type/two-hold", relUnrelated},
		{"a model and an exclusive union another variant of which may hold it", "type/model", "type/may-hold", relUnrelated},
		{"a model and a discriminated union holding it", "type/model", "type/routes", relUnrelated},
		{"a model and a key-tagged union holding it", "type/model", "type/keyed", relUnrelated},
		{"a list and a union holding it", "type/list", "type/holds", firstTypeIsNarrower},
		{"two unions", "type/holds", "type/one-holds", relUnrelated},
		{"a union every variant of which is within a bare object", "type/of-models", "type/bare", firstTypeIsNarrower},
		{"an open union every variant it lists of which is", "type/open-models", "type/bare", relUnrelated},
		{"a model and an open union one variant of which holds it", "type/model", "type/open-models", firstTypeIsNarrower},
		{"a union some variant of which is not", "type/holds", "type/bare", relUnrelated},
		{"a closed union some variant of which is not", "type/closed-mix", "type/bare", relUnrelated},
		{"a discriminated union every variant of which is", "type/routed", "type/bare", relUnrelated},
		{"a union of no variant", "type/union", "type/bare", relUnrelated},
		{"a model and a union one variant of which holds it, bounded beside", "type/model", "type/bounded", relUnrelated},
		{"a model and a bounded alias of a union holding it", "type/model", "type/holds-alias", relUnrelated},
		{"a model and an alias of a union holding it", "type/model", "type/plain-alias", firstTypeIsNarrower},
		{"a bounded union every variant of which is within a bare object", "type/bounded-of-models", "type/bare", firstTypeIsNarrower},
		{"a list and a union", "type/list", "type/union", relUnrelated},
		{"a model and an external", "type/model", "type/external", relUnrelated},
		{"a model and an opaque scalar", "type/model", "type/opaque", relUnrelated},
		{"an opaque scalar and a union", "type/opaque", "type/union", relUnrelated},
		{"a literal and a union that may hold it", "type/literal", "type/union", relUnrelated},
		{"a literal and a structure", "type/literal", "type/model", relDisjoint},
		{"an object literal and a model", "type/lit-object", "type/model", relUnrelated},
		{"an object literal and a map", "type/lit-object", "type/map", relUnrelated},
		{"an object literal and a list", "type/lit-object", "type/list", relDisjoint},
		{"a list literal and a tuple", "type/lit-list", "type/tuple", relUnrelated},
		{"a list literal and a list", "type/lit-list", "type/list", relUnrelated},
		{"a list literal and a model", "type/lit-list", "type/model", relDisjoint},
		{"a model and an alias of another", "type/other", "type/alias-model", relUnrelated},
		{"a model and an alias of itself", "type/model", "type/alias-model", secondTypeIsNarrower},
		{"a chain of aliases and the model at its end", "type/alias-chain", "type/model", firstTypeIsNarrower},
		{"two links of one chain", "type/alias-chain", "type/alias-model", firstTypeIsNarrower},
		{"a list and an alias of itself", "type/list", "type/alias-list", secondTypeIsNarrower},
		{"a model and its Base", "type/derived", "type/model", firstTypeIsNarrower},
		{"a model and its Base's Base", "type/grandchild", "type/model", firstTypeIsNarrower},
		{"a model and a mixin", "type/mixed", "type/model", firstTypeIsNarrower},
		{"a model and the model an alias Base stands for", "type/via-alias", "type/model", firstTypeIsNarrower},
		{"a Base the registry does not hold", "type/dangling", "type/model", relUnrelated},
		{"a part the registry does not hold beside one that leads there", "type/partly-gone", "type/model", firstTypeIsNarrower},
		{"an alias of a scalar is within the scalar it stands for", "type/scalar-on", "type/scalar", firstTypeIsNarrower},
		{"models composing each other", "type/cycle-a", "type/cycle-b", relOverlap},
		{"a model and a bare object", "type/model", "type/bare", firstTypeIsNarrower},
		{"a map and a bare object", "type/map", "type/bare", firstTypeIsNarrower},
		{"an object literal and a bare object", "type/lit-object", "type/bare", firstTypeIsNarrower},
		{"an alias of a model and a bare object", "type/alias-model", "type/bare", firstTypeIsNarrower},
		{"a list and a bare object", "type/list", "type/bare", relDisjoint},
		{"a list literal and a bare object", "type/lit-list", "type/bare", relDisjoint},
		{"a union and a bare object", "type/union", "type/bare", relUnrelated},
		{"two bare objects declared apart", "type/bare", "type/bare-twin", relOverlap},
		{"an alias of a bare object and that object", "type/alias-bare", "type/bare", firstTypeIsNarrower},
		{"a model and an alias of a bare object, which may say more", "type/model", "type/alias-bare", relUnrelated},
		{"a model and an object with a title", "type/model", "type/bare-titled", relUnrelated},
		{"a map and an object with a title", "type/map", "type/bare-titled", relUnrelated},
		{"an object with a title and a bare object", "type/bare-titled", "type/bare", firstTypeIsNarrower},
		{"a model and an alias of a list", "type/model", "type/alias-list", relDisjoint},
		{"an alias chain is read to its end", "type/list", "type/alias-chain", relDisjoint},
		{"an alias of a model against a kind", "type/alias-model", "type/primitive/string", relDisjoint},
		{"a kind and a bare object", "type/primitive/string", "type/bare", relDisjoint},
		{"an alias with nothing at its end stays opaque", "type/list", "type/alias-gone", relUnrelated},
		{"a cyclic alias stays opaque", "type/list", "type/alias-loop", relUnrelated},
	} {
		got := judge(stubbed).relate(ir.TypeRef{Target: tc.a}, ir.TypeRef{Target: tc.b})
		assert.Equal(t, tc.want, got, tc.name)
		assert.Equal(t, tc.want.flipped(), judge(stubbed).relate(ir.TypeRef{Target: tc.b}, ir.TypeRef{Target: tc.a}), "%s, flipped", tc.name)
	}

	gone, ok := stubbed.resolve("type/alias-gone")
	require.True(t, ok)
	assert.Equal(t, gone, judge(stubbed).unaliased(gone), "an alias with nothing at its end stands for itself, never for no type")
}

// TestUnaliased_ReadsAChainUpToItsBound pins maxTypeResolveDepth on an alias
// chain: that many aliases are read to the type they stand for, and one more
// stops at an alias, so a chain too long to read is judged as an opaque scalar.
func TestUnaliased_ReadsAChainUpToItsBound(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name      string
		aliases   int
		wantModel bool
	}{
		{"at the bound", maxTypeResolveDepth, true},
		{"past the bound", maxTypeResolveDepth + 1, false},
	} {
		reg := map[ir.TypeID]ir.TypeDef{"type/end": declaring("type/end")}
		next := ir.TypeID("type/end")
		for i := range tc.aliases {
			id := ir.TypeID(fmt.Sprintf("type/alias-%d", i))
			reg[id] = &ir.Scalar{ID: id, Base: &ir.TypeRef{Target: next}}
			next = id
		}
		stubbed, _ := stubMerger(reg)

		_, isModel := judge(stubbed).unaliased(reg[next]).(*ir.Model)

		assert.Equal(t, tc.wantModel, isModel, tc.name)
	}
}

// TestComposes_ReadsAChainUpToItsBound pins maxComposedParts: a part that many
// links away is found, and one more is not, so a hierarchy too deep to read
// stays two types the field composes.
func TestComposes_ReadsAChainUpToItsBound(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name  string
		links int
		want  bool
	}{
		{"at the bound", maxComposedParts, true},
		{"past the bound", maxComposedParts + 1, false},
	} {
		reg := map[ir.TypeID]ir.TypeDef{"type/end": declaring("type/end")}
		next := ir.TypeID("type/end")
		for i := range tc.links {
			id := ir.TypeID(fmt.Sprintf("type/link-%d", i))
			reg[id] = &ir.Model{ID: id, Base: &ir.TypeRef{Target: next}}
			next = id
		}
		stubbed, _ := stubMerger(reg)

		assert.Equal(t, tc.want, judge(stubbed).composes(next, "type/end"), tc.name)
	}
}

// TestComposes_ReadsEachPartOnce pins that a diamond and a cycle both end: a
// part two chains reach is read once, and a walk back to its start stops there.
func TestComposes_ReadsEachPartOnce(t *testing.T) {
	t.Parallel()
	stubbed, _ := stubMerger(map[ir.TypeID]ir.TypeDef{
		"type/root":   &ir.Model{ID: "type/root", Mixins: []ir.TypeRef{{Target: "type/left"}, {Target: "type/right"}}},
		"type/left":   &ir.Model{ID: "type/left", Base: &ir.TypeRef{Target: "type/shared"}},
		"type/right":  &ir.Model{ID: "type/right", Base: &ir.TypeRef{Target: "type/shared"}},
		"type/shared": &ir.Model{ID: "type/shared", Base: &ir.TypeRef{Target: "type/root"}},
		"type/alone":  declaring("type/alone"),
	})

	assert.True(t, judge(stubbed).composes("type/root", "type/shared"))
	assert.False(t, judge(stubbed).composes("type/root", "type/alone"))
}

// TestSaysOnlyObject_ReadsEveryFieldThatSaysSomething pins what a bare object is:
// a model with nothing to lose by being narrowed. Where it was declared says
// nothing, and anything else on the model does, docs and examples among them,
// and a field the IR adds later, since the comparison reads the whole struct.
func TestSaysOnlyObject_ReadsEveryFieldThatSaysSomething(t *testing.T) {
	t.Parallel()
	ref := &ir.TypeRef{Target: "type/x"}
	for _, tc := range []struct {
		name string
		td   ir.TypeDef
		want bool
	}{
		{"nothing at all", &ir.Model{}, true},
		{"where it was declared", &ir.Model{
			ID: "type/m", Name: ir.Naming{Hint: "m"}, Anonymous: true, Provenance: ir.Provenance{Pointer: "/m"},
		}, true},
		{"docs", &ir.Model{Docs: ir.Docs{Summary: "s"}}, false},
		{"a deprecation", &ir.Model{Deprecation: &ir.Deprecation{}}, false},
		{"sensitive", &ir.Model{Sensitive: true}, false},
		{"an XML hint", &ir.Model{XML: &ir.XMLHints{}}, false},
		{"an example", &ir.Model{Examples: []ir.Example{{}}}, false},
		{"a property", declaring("type/m"), false},
		{"a required name", &ir.Model{Requires: []string{"x"}}, false},
		{"a Base", &ir.Model{Base: ref}, false},
		{"a mixin", &ir.Model{Mixins: []ir.TypeRef{*ref}}, false},
		{"a catch-all", &ir.Model{AdditionalProperties: &ir.AdditionalProperties{}}, false},
		{"closed", &ir.Model{Additional: ir.AdditionalClosed}, false},
		{"constraints", &ir.Model{Constraints: &ir.Constraints{}}, false},
		{"a discriminator", &ir.Model{Discriminator: &ir.Discriminator{}}, false},
		{"something kept verbatim", &ir.Model{Unmodeled: ir.Unmodeled{"k": {}}}, false},
		{"no model", &ir.MapT{}, false},
	} {
		assert.Equal(t, tc.want, saysOnlyObject(tc.td), tc.name)
	}
}

// TestTypeJudge_UnrelatedPrimitivesAreDisjoint pins the pairs the relation
// calls disjoint, and the enum against a bare kind of another family.
func TestTypeJudge_UnrelatedPrimitivesAreDisjoint(t *testing.T) {
	t.Parallel()
	reg := narrowingRegistry()
	reg["type/enm"] = &ir.Enum{ID: "type/enm", ValueType: ir.PrimitiveString}
	stubbed, _ := stubMerger(reg)

	assert.Equal(t, relDisjoint, judge(stubbed).relate(primitiveRef(ir.PrimitiveUUID), primitiveRef(ir.PrimitiveDatetimeOffset)))
	assert.Equal(t, relDisjoint, judge(stubbed).relate(ir.TypeRef{Target: "type/enm"}, primitiveRef(ir.PrimitiveInteger)))
	assert.Equal(t, relOverlap, judge(stubbed).relate(primitiveRef(ir.PrimitiveInt32), primitiveRef(ir.PrimitiveUint32)),
		"overlapping kinds are no conflict")
}

// TestSameValue_ComparesJSONValues pins the value equality the fold and the
// enum judgment share: one JSON value spelled two ways is one, and a value
// with no JSON instance, which canonicalValue cannot spell, compares deeply.
func TestSameValue_ComparesJSONValues(t *testing.T) {
	t.Parallel()
	obj := func(fs ...ir.Field) ir.Value { return ir.Value{Kind: ir.ValueObject, Object: fs} }
	sym := func(s string) ir.Value { return ir.Value{Kind: ir.ValueSymbol, String: s} }
	for _, tc := range []struct {
		name string
		a, b ir.Value
		same bool
	}{
		{"one number, two spellings", number("1"), number("1.0"), true},
		{"two numbers", number("1"), number("2"), false},
		{"one object, two member orders", obj(ir.Field{Name: "a", Value: number("1")}, ir.Field{Name: "b", Value: stringValue("x")}),
			obj(ir.Field{Name: "b", Value: stringValue("x")}, ir.Field{Name: "a", Value: number("1e0")}), true},
		{"a number and a string", number("1"), stringValue("1"), false},
		{"one symbol, compared deeply", sym("s"), sym("s"), true},
		{"two symbols, compared deeply", sym("s"), sym("t"), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.same, sameValue(tc.a, tc.b))
			assert.Equal(t, tc.same, sameValue(tc.b, tc.a))
		})
	}
}

// TestTypeJudge_ComparesReferentsNotReferences pins that the judgment reads
// the nodes two references name, not the references.
func TestTypeJudge_ComparesReferentsNotReferences(t *testing.T) {
	t.Parallel()
	stubbed, _ := stubMerger(map[ir.TypeID]ir.TypeDef{
		"type/a":   &ir.Primitive{ID: "type/a", PrimitiveKind: ir.PrimitiveString},
		"type/b":   &ir.Primitive{ID: "type/b", PrimitiveKind: ir.PrimitiveString},
		"type/i":   &ir.Primitive{ID: "type/i", PrimitiveKind: ir.PrimitiveInt32},
		"type/any": &ir.Any{ID: "type/any"},
		"type/m":   &ir.Model{ID: "type/m"},
	})
	ref := func(id ir.TypeID) ir.TypeRef { return ir.TypeRef{Target: id} }

	assert.Equal(t, relSame, judge(stubbed).relate(ref("type/a"), ref("type/b")),
		"two distinct nodes of one primitive kind are compared by kind")
	assert.Equal(t, relDisjoint, judge(stubbed).relate(ref("type/a"), ref("type/i")),
		"string and integer cannot both be satisfied")
	assert.Equal(t, relUnrelated, judge(stubbed).relate(ref("type/any"), ref("type/i")),
		"a degraded node stands for a declaration the judgment cannot see")
	assert.Equal(t, relDisjoint, judge(stubbed).relate(ref("type/m"), ref("type/a")),
		"a structural type and a primitive are different kinds")
}

// TestTypeJudge_CopiesUnderAnotherBaseOverlap pins that a hoisted copy keeping
// a reference is one type with a node held alike only where the two read it
// against one base (FieldDeclarationContext.Alike): elsewhere they are two
// types declared apart, which overlap, whether the other is a copy or a node
// declared elsewhere. Copies keeping none say the same under any base.
func TestTypeJudge_CopiesUnderAnotherBaseOverlap(t *testing.T) {
	t.Parallel()
	reg := judgeRegistry()
	not := ir.Unmodeled{"openapi:not": {Value: ir.RawValue(`{"$ref":"n.json"}`)}}
	for _, id := range []ir.TypeID{"type/lit-a-ref1", "type/lit-a-ref2"} {
		reg[id] = &ir.Literal{ID: id, Anonymous: true, Value: stringValue("a"), Unmodeled: not}
	}
	reg["type/lit-a-ref"] = &ir.Literal{ID: "type/lit-a-ref", Value: stringValue("a"), Unmodeled: not}
	absolute := ir.Unmodeled{"openapi:not": {Value: ir.RawValue(`{"$ref":"https://ex.test/n.json"}`)}}
	for _, id := range []ir.TypeID{"type/lit-a-abs1", "type/lit-a-abs2"} {
		reg[id] = &ir.Literal{ID: id, Anonymous: true, Value: stringValue("a"), Unmodeled: absolute}
	}
	typeJudgeValue := typeJudge{resolve: stubResolve(reg), sites: atRoot(&stubWalk{apart: true})}
	assert.Equal(t, relOverlap, typeJudgeValue.relate(ref("type/lit-a-ref1"), ref("type/lit-a-ref2")))
	assert.Equal(t, relOverlap, typeJudgeValue.relate(ref("type/lit-a-ref"), ref("type/lit-a-ref1")), "a node declared apart")
	assert.Equal(t, relSame, typeJudgeValue.relate(ref("type/lit-a-h1"), ref("type/lit-a-h2")), "copies keeping no reference")
	assert.Equal(t, relSame, typeJudgeValue.relate(ref("type/lit-a-abs1"), ref("type/lit-a-abs2")), "copies keeping an absolute one")
	assert.Equal(t, firstTypeIsNarrower, typeJudgeValue.relate(ref("type/enum-str"), ref("type/enum-str-h")),
		"a node declared apart still wins over a hoisted copy")
}

// variantsOf returns one variant naming each of targets, in order.
func variantsOf(targets ...ir.TypeID) []ir.Variant {
	vs := make([]ir.Variant, 0, len(targets))
	for _, typeID := range targets {
		vs = append(vs, ir.Variant{Type: ir.TypeRef{Target: typeID}})
	}
	return vs
}

// TestTypeJudge_UnionVariantsStopAtTheirBudget pins maxVariantReads from both
// sides: a type a union's last variant holds is within it where the union lists
// that many variants, and unrelated to it past them.
func TestTypeJudge_UnionVariantsStopAtTheirBudget(t *testing.T) {
	t.Parallel()
	for _, n := range []int{maxVariantReads, maxVariantReads + 1} {
		reg := ir.TypeRegistry{"type/model": declaring("type/model"), "type/list": &ir.List{ID: "type/list", ElementType: ir.TypeRef{Target: "type/model"}}}
		targets := make([]ir.TypeID, n)
		for i := range targets {
			targets[i] = "type/list"
		}
		targets[n-1] = "type/model"
		reg["type/u"] = &ir.Union{ID: "type/u", Variants: variantsOf(targets...)}
		stubbed, _ := stubMerger(reg)
		want := firstTypeIsNarrower
		if n > maxVariantReads {
			want = relUnrelated
		}
		assert.Equal(t, want, judge(stubbed).relate(ir.TypeRef{Target: "type/model"}, ir.TypeRef{Target: "type/u"}), "%d variants", n)
	}
}

// TestTypeJudge_AUnionWithinATypeStopsAtTheBudget pins maxVariantReads for a
// closed union every variant of which is within a type: one judgment reads
// each variant once to ask whether the type is within it, and again to ask
// whether it is within the type, so a union of half the budget's variants is
// narrower and one more is unrelated.
func TestTypeJudge_AUnionWithinATypeStopsAtTheBudget(t *testing.T) {
	t.Parallel()
	for _, n := range []int{maxVariantReads / 2, maxVariantReads/2 + 1} {
		targets := make([]ir.TypeID, n)
		for i := range targets {
			targets[i] = "type/model"
		}
		stubbed, _ := stubMerger(ir.TypeRegistry{
			"type/model": declaring("type/model"),
			"type/bare":  &ir.Model{ID: "type/bare"},
			"type/u":     &ir.Union{ID: "type/u", Closed: true, Variants: variantsOf(targets...)},
		})
		want := firstTypeIsNarrower
		if n > maxVariantReads/2 {
			want = relUnrelated
		}
		assert.Equal(t, want, judge(stubbed).relate(ir.TypeRef{Target: "type/u"}, ir.TypeRef{Target: "type/bare"}), "%d variants", n)
	}
}

// TestJSONInstanceKind_NamesTheKindOfAValue pins the kind of JSON instance the
// other side of a judgment is asked of a structure's source as
// (FieldDeclarationContext.Admits): a primitive by its JSON spelling, an
// integer apart from a number with a fraction, and a literal or a structure by
// its value.
func TestJSONInstanceKind_NamesTheKindOfAValue(t *testing.T) {
	t.Parallel()
	for primitiveKind, want := range map[ir.PrimitiveKind]JSONInstanceKind{
		ir.PrimitiveBool: InstanceBoolean, ir.PrimitiveInt32: InstanceInteger, ir.PrimitiveInteger: InstanceInteger,
		ir.PrimitiveUint64: InstanceInteger, ir.PrimitiveFloat64: InstanceNumber, ir.PrimitiveNumber: InstanceNumber,
		ir.PrimitiveDecimal: InstanceNumber, ir.PrimitiveString: InstanceString, ir.PrimitiveBytes: InstanceString,
		ir.PrimitiveDatetime: InstanceString,
	} {
		assert.Equal(t, want, primitiveInstance(primitiveKind), primitiveKind)
	}
	for _, tc := range []struct {
		td   ir.TypeDef
		want JSONInstanceKind
	}{
		{&ir.Model{}, InstanceObject},
		{&ir.MapT{}, InstanceObject},
		{&ir.List{}, InstanceArray},
		{&ir.Tuple{}, InstanceArray},
		{&ir.Literal{Value: ir.Value{Kind: ir.ValueObject}}, InstanceObject},
		{&ir.Literal{Value: ir.Value{Kind: ir.ValueList}}, InstanceArray},
		{&ir.Literal{Value: ir.Value{Kind: ir.ValueBool}}, InstanceBoolean},
		{&ir.Literal{Value: ir.Value{Kind: ir.ValueNull}}, InstanceNull},
		{&ir.Literal{Value: ir.Value{Kind: ir.ValueNumber, Number: "2"}}, InstanceInteger},
		{&ir.Literal{Value: ir.Value{Kind: ir.ValueNumber, Number: "2.5"}}, InstanceNumber},
		{&ir.Literal{Value: ir.Value{Kind: ir.ValueNumber, Number: "1e3"}}, InstanceNumber},
		{&ir.Literal{Value: ir.Value{Kind: ir.ValueString, String: "a"}}, InstanceString},
		{&ir.Enum{}, 0},
	} {
		assert.Equal(t, tc.want, nodeInstance(tc.td), "%T %+v", tc.td, tc.td)
	}
}
