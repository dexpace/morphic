package ir_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
)

// TestTypeCommon_ZeroValueShape pins TypeCommon's omitempty contract. ID,
// Name, Anonymous, Docs, Sensitive, and Provenance carry no omitempty because
// every type-graph node has an identity, a naming, an anonymity flag, a docs
// object, a sensitivity flag, and provenance; every other field is optional.
// TypeCommon is embedded and inlined into all 11 concrete kinds, so this test
// pins the shared portion of every one of their zero-value encodings.
func TestTypeCommon_ZeroValueShape(t *testing.T) {
	t.Parallel()
	assertZeroValueShape(t, ir.TypeCommon{},
		`{"id":"","name":{},"anonymous":false,"docs":{},"sensitive":false,"provenance":{"source":0}}`)
}

// TestTypeCommon_PopulatedRoundTrip pins that a fully populated TypeCommon
// round-trips.
func TestTypeCommon_PopulatedRoundTrip(t *testing.T) {
	t.Parallel()
	want := ir.TypeCommon{
		ID:               "t/openapi/components/schemas/User",
		Name:             populatedNaming(),
		Namespace:        []string{"com", "example"},
		Anonymous:        true,
		Docs:             populatedDocs(),
		Tags:             []string{"users", "public"},
		Sensitive:        true,
		Access:           "internal",
		Deprecation:      populatedDeprecation(),
		Availability:     populatedAvailability(),
		Usage:            ir.UsageInput | ir.UsageOutput,
		WireNameByFormat: map[string]string{"xml": "User", "json": "user"},
		MediaTypeHint:    "application/json",
		XML:              populatedXMLHints(),
		Examples: []ir.Example{
			{Name: "ex1", Value: &ir.Value{Kind: ir.ValueString, Str: "v1"}},
		},
		Instantiation: &ir.TemplateInstantiation{
			Template: "Page",
			Args: []ir.TemplateArg{
				{Type: &ir.TypeRef{Target: "t/item"}},
				{Value: &ir.Value{Kind: ir.ValueNumber, Num: ir.BigVal("10")}},
			},
		},
		Extensions: populatedExtensions(),
		Provenance: populatedProvenance(),
	}
	assertRoundTrip(t, want)
}

// TestTypeCommon_WireNameByFormatDeterministic pins Class C for TypeCommon's
// map field.
func TestTypeCommon_WireNameByFormatDeterministic(t *testing.T) {
	t.Parallel()
	tc := ir.TypeCommon{
		WireNameByFormat: map[string]string{
			"z-format": "Z", "m-format": "M", "a-format": "A", "q-format": "Q", "b-format": "B",
		},
	}
	got := assertDeterministicMarshal(t, tc)
	assert.Contains(t, got,
		`"wireNameByFormat":{"a-format":"A","b-format":"B","m-format":"M","q-format":"Q","z-format":"Z"}`)
}

// typeDefZeroShapeTests is the Class A table for every concrete TypeDef kind:
// each must marshal its zero value with the "kind" tag adjacent and correct,
// followed by TypeCommon's shared fields and then its own fields in
// declaration order (json.go's marshalWithKind contract).
func typeDefZeroShapeTests() []struct {
	name string
	td   ir.TypeDef
	want string
} {
	return []struct {
		name string
		td   ir.TypeDef
		want string
	}{
		{
			name: "primitive",
			td:   &ir.Primitive{},
			want: `{"kind":"primitive","id":"","name":{},"anonymous":false,"docs":{},"sensitive":false,"provenance":{"source":0},"prim":""}`,
		},
		{
			name: "scalar",
			td:   &ir.Scalar{},
			want: `{"kind":"scalar","id":"","name":{},"anonymous":false,"docs":{},"sensitive":false,"provenance":{"source":0}}`,
		},
		{
			name: "model",
			td:   &ir.Model{},
			want: `{"kind":"model","id":"","name":{},"anonymous":false,"docs":{},"sensitive":false,"provenance":{"source":0},"abstract":false,"positional":false,"inputOnly":false}`,
		},
		{
			name: "union",
			td:   &ir.Union{},
			want: `{"kind":"union","id":"","name":{},"anonymous":false,"docs":{},"sensitive":false,"provenance":{"source":0},"exclusive":false,"wireTagged":false}`,
		},
		{
			name: "enum",
			td:   &ir.Enum{},
			want: `{"kind":"enum","id":"","name":{},"anonymous":false,"docs":{},"sensitive":false,"provenance":{"source":0},"valueType":"","closed":false,"flags":false}`,
		},
		{
			name: "list",
			td:   &ir.List{},
			want: `{"kind":"list","id":"","name":{},"anonymous":false,"docs":{},"sensitive":false,"provenance":{"source":0},"elem":{"target":"","nullable":false}}`,
		},
		{
			name: "map",
			td:   &ir.MapT{},
			want: `{"kind":"map","id":"","name":{},"anonymous":false,"docs":{},"sensitive":false,"provenance":{"source":0},"key":{"target":"","nullable":false},"value":{"target":"","nullable":false}}`,
		},
		{
			name: "tuple",
			td:   &ir.Tuple{},
			want: `{"kind":"tuple","id":"","name":{},"anonymous":false,"docs":{},"sensitive":false,"provenance":{"source":0}}`,
		},
		{
			name: "literal",
			td:   &ir.Literal{},
			want: `{"kind":"literal","id":"","name":{},"anonymous":false,"docs":{},"sensitive":false,"provenance":{"source":0},"value":{"kind":"","bytes":null,"list":null,"object":null}}`,
		},
		{
			name: "external",
			td:   &ir.External{},
			want: `{"kind":"external","id":"","name":{},"anonymous":false,"docs":{},"sensitive":false,"provenance":{"source":0},"identity":""}`,
		},
		{
			name: "any",
			td:   &ir.Any{},
			want: `{"kind":"any","id":"","name":{},"anonymous":false,"docs":{},"sensitive":false,"provenance":{"source":0}}`,
		},
	}
}

// TestTypeDef_ZeroValueShapeWithKindTag pins the zero-value marshal shape of
// every concrete TypeDef kind, including the adjacent "kind" discriminator
// (invariant #7 / the sealed-sum JSON encoding). This is the Class A test
// json_internal_test.go does not cover: that file drives marshalWithKind's
// error and formatting branches directly, but never asserts the exact
// zero-value payload for each real kind.
func TestTypeDef_ZeroValueShapeWithKindTag(t *testing.T) {
	t.Parallel()
	for _, tt := range typeDefZeroShapeTests() {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			raw, err := json.Marshal(tt.td)
			require.NoError(t, err)
			assert.Equal(t, tt.want, string(raw))
		})
	}
}

// TestPrimitive_PopulatedRoundTrip pins that a fully populated Primitive
// (TypeCommon plus Prim) round-trips.
func TestPrimitive_PopulatedRoundTrip(t *testing.T) {
	t.Parallel()
	want := &ir.Primitive{
		TypeCommon: populatedTypeCommon("t/prim/string"),
		Prim:       ir.PrimString,
	}
	assertTypeDefRoundTrip(t, want)
}

// TestScalar_PopulatedRoundTrip pins that a fully populated Scalar — base
// chain, constraints, and encoding override — round-trips.
func TestScalar_PopulatedRoundTrip(t *testing.T) {
	t.Parallel()
	want := &ir.Scalar{
		TypeCommon:  populatedTypeCommon("t/scalar/positive-int"),
		Base:        &ir.TypeRef{Target: "t/prim/int32"},
		Constraints: populatedConstraints(),
		Encoding:    populatedEncoding(),
	}
	assertTypeDefRoundTrip(t, want)
}

// TestModel_PopulatedRoundTrip pins that a fully populated Model — own
// properties, inheritance, interface conformance, mixins, additional-props
// catch-all, extension ranges, and discriminator — round-trips.
func TestModel_PopulatedRoundTrip(t *testing.T) {
	t.Parallel()
	want := &ir.Model{
		TypeCommon: populatedTypeCommon("t/model/User"),
		Properties: []ir.Property{populatedProperty()},
		Base:       &ir.TypeRef{Target: "t/model/Base"},
		Implements: []ir.TypeRef{{Target: "t/iface/A"}, {Target: "t/iface/B"}},
		Mixins:     []ir.TypeRef{{Target: "t/mixin/M1"}},
		AdditionalProps: &ir.AdditionalProps{
			Value: populatedTypeRef(),
			Key:   &ir.TypeRef{Target: "t/prim/string"},
			Patterns: []ir.PatternProps{
				{Pattern: "^x-", Value: populatedTypeRef()},
			},
		},
		Additional:      ir.AdditionalClosedAfterComposition,
		Abstract:        true,
		Positional:      true,
		ExtensionRanges: []ir.WireIDRange{{From: 100, To: 199}},
		Discriminator: &ir.Discriminator{
			Property: "p/kind",
			Mapping:  map[string]ir.TypeID{"dog": "t/Dog", "cat": "t/Cat"},
		},
		DiscriminatorValue: "user",
		InputOnly:          true,
	}
	assertTypeDefRoundTrip(t, want)
}

// TestUnion_PopulatedRoundTrip pins that a fully populated Union — variants
// and a discriminator — round-trips.
func TestUnion_PopulatedRoundTrip(t *testing.T) {
	t.Parallel()
	want := &ir.Union{
		TypeCommon: populatedTypeCommon("t/union/Pet"),
		Variants: []ir.Variant{
			{Name: ir.Naming{Source: "dog"}, Type: ir.TypeRef{Target: "t/Dog"}},
			{Name: ir.Naming{Source: "cat"}, Type: ir.TypeRef{Target: "t/Cat"}},
		},
		Exclusive:  true,
		WireTagged: true,
		Discriminator: &ir.Discriminator{
			PropertyName: "petType",
			Mapping:      map[string]ir.TypeID{"dog": "t/Dog"},
		},
	}
	assertTypeDefRoundTrip(t, want)
}

// TestEnum_PopulatedRoundTrip pins that a fully populated Enum round-trips.
func TestEnum_PopulatedRoundTrip(t *testing.T) {
	t.Parallel()
	want := &ir.Enum{
		TypeCommon: populatedTypeCommon("t/enum/Status"),
		ValueType:  ir.PrimString,
		Members: []ir.EnumMember{
			{Name: ir.Naming{Source: "ACTIVE"}, Value: ir.Value{Kind: ir.ValueString, Str: "active"}},
			{Name: ir.Naming{Source: "INACTIVE"}, Value: ir.Value{Kind: ir.ValueString, Str: "inactive"}},
		},
		Closed:         true,
		Flags:          true,
		FallbackMember: "UNKNOWN",
	}
	assertTypeDefRoundTrip(t, want)
}

// TestList_PopulatedRoundTrip pins that a fully populated List round-trips.
func TestList_PopulatedRoundTrip(t *testing.T) {
	t.Parallel()
	want := &ir.List{
		TypeCommon:  populatedTypeCommon("t/list/Tags"),
		Elem:        populatedTypeRef(),
		Constraints: populatedConstraints(),
		Encoding:    populatedEncoding(),
	}
	assertTypeDefRoundTrip(t, want)
}

// TestMapT_PopulatedRoundTrip pins that a fully populated MapT round-trips.
func TestMapT_PopulatedRoundTrip(t *testing.T) {
	t.Parallel()
	want := &ir.MapT{
		TypeCommon: populatedTypeCommon("t/map/Attrs"),
		Key:        ir.TypeRef{Target: "t/prim/string"},
		Value:      populatedTypeRef(),
	}
	assertTypeDefRoundTrip(t, want)
}

// TestTuple_PopulatedRoundTrip pins that a fully populated Tuple round-trips
// with its element order preserved.
func TestTuple_PopulatedRoundTrip(t *testing.T) {
	t.Parallel()
	want := &ir.Tuple{
		TypeCommon: populatedTypeCommon("t/tuple/Pair"),
		Elems:      []ir.TypeRef{{Target: "t/prim/string"}, {Target: "t/prim/int32"}},
	}
	assertTypeDefRoundTrip(t, want)
}

// TestLiteral_PopulatedRoundTrip pins that a fully populated Literal
// round-trips.
func TestLiteral_PopulatedRoundTrip(t *testing.T) {
	t.Parallel()
	want := &ir.Literal{
		TypeCommon: populatedTypeCommon("t/literal/Fixed"),
		Value:      ir.Value{Kind: ir.ValueString, Str: "fixed"},
	}
	assertTypeDefRoundTrip(t, want)
}

// TestExternal_PopulatedRoundTrip pins that a fully populated External
// round-trips.
func TestExternal_PopulatedRoundTrip(t *testing.T) {
	t.Parallel()
	want := &ir.External{
		TypeCommon: populatedTypeCommon("t/external/Pid"),
		Identity:   "erlang:pid",
		Package:    "erlang",
		MinVersion: "24.0",
	}
	assertTypeDefRoundTrip(t, want)
}

// TestAny_PopulatedRoundTrip pins that a fully populated Any (TypeCommon only)
// round-trips.
func TestAny_PopulatedRoundTrip(t *testing.T) {
	t.Parallel()
	want := &ir.Any{TypeCommon: populatedTypeCommon("t/any/Freeform")}
	assertTypeDefRoundTrip(t, want)
}

// TestTypeDef_CommonIsAnAliasNotACopy is the highest-value new test in this
// file: Common() must return a pointer into the concrete struct's own
// TypeCommon, not a pointer to a copy. A copying implementation would compile
// and pass every other test in this package (including the JSON round-trip
// tests above, which never call Common()), but would silently break any pass
// that mutates a type's shared metadata (Usage flags, Availability, …) via
// the TypeDef interface — exactly the pattern passes use.
func TestTypeDef_CommonIsAnAliasNotACopy(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		check func(t *testing.T)
	}{
		{"primitive", func(t *testing.T) {
			td := &ir.Primitive{TypeCommon: ir.TypeCommon{ID: "t/x"}}
			td.Common().ID = "t/y"
			assert.Equal(t, ir.TypeID("t/y"), td.ID)
		}},
		{"scalar", func(t *testing.T) {
			td := &ir.Scalar{TypeCommon: ir.TypeCommon{ID: "t/x"}}
			td.Common().ID = "t/y"
			assert.Equal(t, ir.TypeID("t/y"), td.ID)
		}},
		{"model", func(t *testing.T) {
			td := &ir.Model{TypeCommon: ir.TypeCommon{ID: "t/x"}}
			td.Common().ID = "t/y"
			assert.Equal(t, ir.TypeID("t/y"), td.ID)
		}},
		{"union", func(t *testing.T) {
			td := &ir.Union{TypeCommon: ir.TypeCommon{ID: "t/x"}}
			td.Common().ID = "t/y"
			assert.Equal(t, ir.TypeID("t/y"), td.ID)
		}},
		{"enum", func(t *testing.T) {
			td := &ir.Enum{TypeCommon: ir.TypeCommon{ID: "t/x"}}
			td.Common().ID = "t/y"
			assert.Equal(t, ir.TypeID("t/y"), td.ID)
		}},
		{"list", func(t *testing.T) {
			td := &ir.List{TypeCommon: ir.TypeCommon{ID: "t/x"}}
			td.Common().ID = "t/y"
			assert.Equal(t, ir.TypeID("t/y"), td.ID)
		}},
		{"map", func(t *testing.T) {
			td := &ir.MapT{TypeCommon: ir.TypeCommon{ID: "t/x"}}
			td.Common().ID = "t/y"
			assert.Equal(t, ir.TypeID("t/y"), td.ID)
		}},
		{"tuple", func(t *testing.T) {
			td := &ir.Tuple{TypeCommon: ir.TypeCommon{ID: "t/x"}}
			td.Common().ID = "t/y"
			assert.Equal(t, ir.TypeID("t/y"), td.ID)
		}},
		{"literal", func(t *testing.T) {
			td := &ir.Literal{TypeCommon: ir.TypeCommon{ID: "t/x"}}
			td.Common().ID = "t/y"
			assert.Equal(t, ir.TypeID("t/y"), td.ID)
		}},
		{"external", func(t *testing.T) {
			td := &ir.External{TypeCommon: ir.TypeCommon{ID: "t/x"}}
			td.Common().ID = "t/y"
			assert.Equal(t, ir.TypeID("t/y"), td.ID)
		}},
		{"any", func(t *testing.T) {
			td := &ir.Any{TypeCommon: ir.TypeCommon{ID: "t/x"}}
			td.Common().ID = "t/y"
			assert.Equal(t, ir.TypeID("t/y"), td.ID)
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			tt.check(t)
		})
	}
}

// TestPrimKind_Constants pins the on-disk spelling of every PrimKind value.
// These strings are the primitive-scalar vocabulary written into every
// golden IR snapshot; a typo fix later would be a silent breaking change.
func TestPrimKind_Constants(t *testing.T) {
	t.Parallel()
	tests := map[ir.PrimKind]string{
		ir.PrimBool:           "bool",
		ir.PrimString:         "string",
		ir.PrimBytes:          "bytes",
		ir.PrimInt8:           "int8",
		ir.PrimInt16:          "int16",
		ir.PrimInt32:          "int32",
		ir.PrimInt64:          "int64",
		ir.PrimUint8:          "uint8",
		ir.PrimUint16:         "uint16",
		ir.PrimUint32:         "uint32",
		ir.PrimUint64:         "uint64",
		ir.PrimInteger:        "integer",
		ir.PrimFloat32:        "float32",
		ir.PrimFloat64:        "float64",
		ir.PrimFloat:          "float",
		ir.PrimNumber:         "number",
		ir.PrimDecimal:        "decimal",
		ir.PrimDecimal128:     "decimal128",
		ir.PrimDate:           "date",
		ir.PrimTime:           "time",
		ir.PrimDatetime:       "datetime",
		ir.PrimDatetimeOffset: "datetime_offset",
		ir.PrimDuration:       "duration",
		ir.PrimURL:            "url",
		ir.PrimUUID:           "uuid",
		ir.PrimAny:            "any",
	}
	for kind, want := range tests {
		t.Run(want, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, ir.PrimKind(want), kind)
		})
	}
}

// TestAdditionalMode_Constants pins the on-disk spelling of every
// AdditionalMode value, including the empty-string "unspecified" state.
func TestAdditionalMode_Constants(t *testing.T) {
	t.Parallel()
	tests := map[ir.AdditionalMode]string{
		ir.AdditionalUnspecified:            "",
		ir.AdditionalClosed:                 "closed",
		ir.AdditionalClosedAfterComposition: "closed_after_composition",
	}
	for mode, want := range tests {
		name := want
		if name == "" {
			name = "unspecified"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, ir.AdditionalMode(want), mode)
		})
	}
}

// TestWireIDRange_ZeroValueShape pins WireIDRange's contract that both bounds
// carry no omitempty.
func TestWireIDRange_ZeroValueShape(t *testing.T) {
	t.Parallel()
	assertZeroValueShape(t, ir.WireIDRange{}, `{"from":0,"to":0}`)
}

// TestWireIDRange_PopulatedRoundTrip pins that a populated WireIDRange
// round-trips.
func TestWireIDRange_PopulatedRoundTrip(t *testing.T) {
	t.Parallel()
	assertRoundTrip(t, ir.WireIDRange{From: 100, To: 199})
}

// TestAdditionalProps_ZeroValueShape pins AdditionalProps' contract that
// Value carries no omitempty: a catch-all always has a value schema.
func TestAdditionalProps_ZeroValueShape(t *testing.T) {
	t.Parallel()
	assertZeroValueShape(t, ir.AdditionalProps{}, `{"value":{"target":"","nullable":false}}`)
}

// TestAdditionalProps_PopulatedRoundTrip pins that a fully populated
// AdditionalProps — key schema and pattern-scoped value schemas — round-trips.
func TestAdditionalProps_PopulatedRoundTrip(t *testing.T) {
	t.Parallel()
	want := ir.AdditionalProps{
		Value: populatedTypeRef(),
		Key:   &ir.TypeRef{Target: "t/prim/string"},
		Patterns: []ir.PatternProps{
			{Pattern: "^x-", Value: populatedTypeRef()},
			{Pattern: "^y-", Value: ir.TypeRef{Target: "t/prim/int32"}},
		},
	}
	assertRoundTrip(t, want)
}

// TestPatternProps_ZeroValueShape pins PatternProps' contract that both
// fields carry no omitempty.
func TestPatternProps_ZeroValueShape(t *testing.T) {
	t.Parallel()
	assertZeroValueShape(t, ir.PatternProps{}, `{"pattern":"","value":{"target":"","nullable":false}}`)
}

// TestPatternProps_PopulatedRoundTrip pins that a populated PatternProps
// round-trips.
func TestPatternProps_PopulatedRoundTrip(t *testing.T) {
	t.Parallel()
	assertRoundTrip(t, ir.PatternProps{Pattern: "^x-", Value: populatedTypeRef()})
}

// TestDiscriminator_ZeroValueShape pins Discriminator's contract that
// Inferred carries no omitempty ("not heuristically inferred" is a declared
// fact, not an absence — the same reasoning invariant #8 applies elsewhere in
// the package), while Default and every locator field stay optional.
func TestDiscriminator_ZeroValueShape(t *testing.T) {
	t.Parallel()
	assertZeroValueShape(t, ir.Discriminator{}, `{"inferred":false}`)
}

// TestDiscriminator_PopulatedRoundTrip pins that a fully populated
// Discriminator — property-based, index-based, and mapping/default/envelope
// fields simultaneously populated purely to exercise every field — round-
// trips.
func TestDiscriminator_PopulatedRoundTrip(t *testing.T) {
	t.Parallel()
	idx := 0
	want := ir.Discriminator{
		Property:          "p/kind",
		PropertyName:      "kind",
		Index:             &idx,
		Mapping:           map[string]ir.TypeID{"dog": "t/Dog", "cat": "t/Cat"},
		Default:           "t/Unknown",
		Envelope:          "object",
		EnvelopeValueName: "value",
		Inferred:          true,
	}
	assertRoundTrip(t, want)
}

// TestDiscriminator_MappingDeterministic pins Class C for Discriminator's map
// field: Mapping must marshal with keys in sorted order on every run.
func TestDiscriminator_MappingDeterministic(t *testing.T) {
	t.Parallel()
	d := ir.Discriminator{
		Mapping: map[string]ir.TypeID{
			"z-tag": "t/Z", "m-tag": "t/M", "a-tag": "t/A", "q-tag": "t/Q", "b-tag": "t/B",
		},
	}
	got := assertDeterministicMarshal(t, d)
	assert.Contains(t, got,
		`"mapping":{"a-tag":"t/A","b-tag":"t/B","m-tag":"t/M","q-tag":"t/Q","z-tag":"t/Z"}`)
}

// TestVariant_ZeroValueShape pins Variant's omitempty contract: Name, Type,
// and Docs carry no omitempty; every other field is optional.
func TestVariant_ZeroValueShape(t *testing.T) {
	t.Parallel()
	assertZeroValueShape(t, ir.Variant{}, `{"name":{},"type":{"target":"","nullable":false},"docs":{}}`)
}

// TestVariant_PopulatedRoundTrip pins that a fully populated Variant round-
// trips.
func TestVariant_PopulatedRoundTrip(t *testing.T) {
	t.Parallel()
	wireID := 3
	want := ir.Variant{
		Name:         populatedNaming(),
		Type:         populatedTypeRef(),
		WireName:     "dog_variant",
		WireID:       &wireID,
		XML:          populatedXMLHints(),
		Event:        &ir.EventInfo{ContentType: "application/json", Terminal: true},
		Docs:         populatedDocs(),
		Deprecation:  populatedDeprecation(),
		Availability: populatedAvailability(),
		Examples: []ir.Example{
			{Name: "ex1", Value: &ir.Value{Kind: ir.ValueString, Str: "v1"}},
		},
		Extensions: populatedExtensions(),
	}
	assertRoundTrip(t, want)
}

// TestEventInfo_ZeroValueShape pins EventInfo's contract that Terminal
// carries no omitempty.
func TestEventInfo_ZeroValueShape(t *testing.T) {
	t.Parallel()
	assertZeroValueShape(t, ir.EventInfo{}, `{"terminal":false}`)
}

// TestEventInfo_PopulatedRoundTrip pins that a populated EventInfo round-
// trips.
func TestEventInfo_PopulatedRoundTrip(t *testing.T) {
	t.Parallel()
	assertRoundTrip(t, ir.EventInfo{ContentType: "application/json", Terminal: true})
}

// TestEnumMember_ZeroValueShape pins EnumMember's omitempty contract: Name,
// Value, and Docs carry no omitempty; every other field is optional.
func TestEnumMember_ZeroValueShape(t *testing.T) {
	t.Parallel()
	assertZeroValueShape(t, ir.EnumMember{},
		`{"name":{},"value":{"kind":"","bytes":null,"list":null,"object":null},"docs":{}}`)
}

// TestEnumMember_PopulatedRoundTrip pins that a fully populated EnumMember
// round-trips.
func TestEnumMember_PopulatedRoundTrip(t *testing.T) {
	t.Parallel()
	want := ir.EnumMember{
		Name:         populatedNaming(),
		Value:        ir.Value{Kind: ir.ValueString, Str: "active"},
		WireName:     "ACTIVE",
		Docs:         populatedDocs(),
		Deprecation:  populatedDeprecation(),
		Availability: populatedAvailability(),
		Examples: []ir.Example{
			{Name: "ex1", Value: &ir.Value{Kind: ir.ValueString, Str: "active"}},
		},
		Extensions: populatedExtensions(),
	}
	assertRoundTrip(t, want)
}

// TestTemplateInstantiation_ZeroValueShape pins TemplateInstantiation's
// omitempty contract: both fields are optional.
func TestTemplateInstantiation_ZeroValueShape(t *testing.T) {
	t.Parallel()
	assertZeroValueShape(t, ir.TemplateInstantiation{}, `{}`)
}

// TestTemplateInstantiation_PopulatedRoundTrip pins that a fully populated
// TemplateInstantiation round-trips.
func TestTemplateInstantiation_PopulatedRoundTrip(t *testing.T) {
	t.Parallel()
	want := ir.TemplateInstantiation{
		Template: "Page",
		Args: []ir.TemplateArg{
			{Type: &ir.TypeRef{Target: "t/item"}},
			{Value: &ir.Value{Kind: ir.ValueNumber, Num: ir.BigVal("10")}},
		},
	}
	assertRoundTrip(t, want)
}

// TestTemplateArg_ZeroValueShape pins TemplateArg's omitempty contract: both
// fields are optional, since exactly one is legally set at a time.
func TestTemplateArg_ZeroValueShape(t *testing.T) {
	t.Parallel()
	assertZeroValueShape(t, ir.TemplateArg{}, `{}`)
}

// TestTemplateArg_PopulatedRoundTrip pins that both arms of TemplateArg — the
// type-parameter arm and the valueof arm — round-trip.
func TestTemplateArg_PopulatedRoundTrip(t *testing.T) {
	t.Parallel()
	tests := map[string]ir.TemplateArg{
		"type":  {Type: &ir.TypeRef{Target: "t/item"}},
		"value": {Value: &ir.Value{Kind: ir.ValueNumber, Num: ir.BigVal("10")}},
	}
	for name, want := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assertRoundTrip(t, want)
		})
	}
}
