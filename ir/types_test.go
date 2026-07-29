package ir_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
)

// TestTypeCommon_JSONContract pins TypeCommon's omitempty contract. ID, Name,
// Anonymous, Docs, Sensitive, and Provenance carry no omitempty because every
// type-graph node has an identity, a naming, an anonymity flag, a docs
// object, a sensitivity flag, and provenance; every other field is optional.
// TypeCommon is embedded and inlined into all 11 concrete kinds, so this test
// pins the shared portion of every one of their zero-value encodings. It also
// pins that a fully populated TypeCommon round-trips.
func TestTypeCommon_JSONContract(t *testing.T) {
	t.Parallel()
	assertJSONContract(t, ir.TypeCommon{},
		`{"id":"","name":{},"anonymous":false,"docs":{},"sensitive":false,"provenance":{"source":0}}`,
		ir.TypeCommon{
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
			Unmodeled:  populatedUnmodeled(),
			Provenance: populatedProvenance(),
		})
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
// catch-all, property-set cardinality, extension ranges, and discriminator —
// round-trips.
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
		Constraints:     populatedConstraints(),
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

// TestTypeDef_CommonIsAnAliasNotACopy checks that Common() returns a pointer
// into the concrete struct's own TypeCommon, not a copy or a value shared
// across kinds. A copying or singleton implementation would pass every other
// test in this package (including the JSON round-trip tests above, which
// never call Common()) while silently breaking any pass that mutates shared
// metadata (Usage flags, Availability, …) via the TypeDef interface.
//
// The read-back goes through json.Marshal of the whole td rather than
// td.Common().ID again, since re-reading via Common() would trivially pass a
// shared-singleton mutant.
func TestTypeDef_CommonIsAnAliasNotACopy(t *testing.T) {
	t.Parallel()
	for _, k := range allKinds {
		t.Run(string(k), func(t *testing.T) {
			t.Parallel()
			td, ok := ir.NewTypeDef(k)
			require.True(t, ok, "no concrete type registered for kind %q", k)
			td.Common().ID = "t/y"
			raw, err := json.Marshal(td)
			require.NoError(t, err)
			assert.Contains(t, string(raw), `"id":"t/y"`)
		})
	}
}

// TestPrimKind_Constants pins the on-disk spelling of every PrimKind value.
// These strings are the primitive-scalar vocabulary written into every
// golden IR snapshot; a typo fix later would be a silent breaking change.
func TestPrimKind_Constants(t *testing.T) {
	t.Parallel()
	assertConstantSpellings(t, map[ir.PrimKind]string{
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
	}, "unspecified")
}

// TestAdditionalMode_Constants pins the on-disk spelling of every
// AdditionalMode value, including the empty-string "unspecified" state.
func TestAdditionalMode_Constants(t *testing.T) {
	t.Parallel()
	assertConstantSpellings(t, map[ir.AdditionalMode]string{
		ir.AdditionalUnspecified:            "",
		ir.AdditionalClosed:                 "closed",
		ir.AdditionalClosedAfterComposition: "closed_after_composition",
	}, "unspecified")
}

// TestWireIDRange_JSONContract pins WireIDRange's contract that both bounds
// carry no omitempty and that a populated WireIDRange round-trips.
func TestWireIDRange_JSONContract(t *testing.T) {
	t.Parallel()
	assertJSONContract(t, ir.WireIDRange{}, `{"from":0,"to":0}`, ir.WireIDRange{From: 100, To: 199})
}

// TestAdditionalProps_JSONContract pins AdditionalProps' contract that Value
// carries no omitempty (a catch-all always has a value schema) and that a
// fully populated AdditionalProps — key schema and pattern-scoped value
// schemas — round-trips.
func TestAdditionalProps_JSONContract(t *testing.T) {
	t.Parallel()
	assertJSONContract(t, ir.AdditionalProps{}, `{"value":{"target":"","nullable":false}}`,
		ir.AdditionalProps{
			Value: populatedTypeRef(),
			Key:   &ir.TypeRef{Target: "t/prim/string"},
			Patterns: []ir.PatternProps{
				{Pattern: "^x-", Value: populatedTypeRef()},
				{Pattern: "^y-", Value: ir.TypeRef{Target: "t/prim/int32"}},
			},
		})
}

// TestPatternProps_JSONContract pins PatternProps' contract that both fields
// carry no omitempty and that a populated PatternProps round-trips.
func TestPatternProps_JSONContract(t *testing.T) {
	t.Parallel()
	assertJSONContract(t, ir.PatternProps{}, `{"pattern":"","value":{"target":"","nullable":false}}`,
		ir.PatternProps{Pattern: "^x-", Value: populatedTypeRef()})
}

// TestDiscriminator_JSONContract pins Discriminator's contract that Inferred
// carries no omitempty ("not heuristically inferred" is a declared fact, not
// an absence — the same reasoning invariant #8 applies elsewhere in the
// package), while Default and every locator field stay optional, and that a
// fully populated Discriminator — property-based, index-based, and
// mapping/default/envelope fields simultaneously populated purely to
// exercise every field — round-trips.
func TestDiscriminator_JSONContract(t *testing.T) {
	t.Parallel()
	idx := 0
	assertJSONContract(t, ir.Discriminator{}, `{"inferred":false}`, ir.Discriminator{
		Property:          "p/kind",
		PropertyName:      "kind",
		Index:             &idx,
		Mapping:           map[string]ir.TypeID{"dog": "t/Dog", "cat": "t/Cat"},
		Default:           "t/Unknown",
		Envelope:          "object",
		EnvelopeValueName: "value",
		Inferred:          true,
	})
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

// TestVariant_JSONContract pins Variant's omitempty contract (Name, Type, and
// Docs carry no omitempty; every other field is optional) and that a fully
// populated Variant round-trips.
func TestVariant_JSONContract(t *testing.T) {
	t.Parallel()
	wireID := 3
	assertJSONContract(t, ir.Variant{}, `{"name":{},"type":{"target":"","nullable":false},"docs":{}}`,
		ir.Variant{
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
			Unmodeled: populatedUnmodeled(),
		})
}

// TestEventInfo_JSONContract pins EventInfo's contract that Terminal carries
// no omitempty and that a populated EventInfo round-trips.
func TestEventInfo_JSONContract(t *testing.T) {
	t.Parallel()
	assertJSONContract(t, ir.EventInfo{}, `{"terminal":false}`,
		ir.EventInfo{ContentType: "application/json", Terminal: true})
}

// TestEnumMember_JSONContract pins EnumMember's omitempty contract (Name,
// Value, and Docs carry no omitempty; every other field is optional) and that
// a fully populated EnumMember round-trips.
func TestEnumMember_JSONContract(t *testing.T) {
	t.Parallel()
	assertJSONContract(t, ir.EnumMember{},
		`{"name":{},"value":{"kind":"","bytes":null,"list":null,"object":null},"docs":{}}`,
		ir.EnumMember{
			Name:         populatedNaming(),
			Value:        ir.Value{Kind: ir.ValueString, Str: "active"},
			WireName:     "ACTIVE",
			Docs:         populatedDocs(),
			Deprecation:  populatedDeprecation(),
			Availability: populatedAvailability(),
			Examples: []ir.Example{
				{Name: "ex1", Value: &ir.Value{Kind: ir.ValueString, Str: "active"}},
			},
			Unmodeled: populatedUnmodeled(),
		})
}

// TestTemplateInstantiation_JSONContract pins TemplateInstantiation's
// omitempty contract (both fields are optional) and that a fully populated
// TemplateInstantiation round-trips.
func TestTemplateInstantiation_JSONContract(t *testing.T) {
	t.Parallel()
	assertJSONContract(t, ir.TemplateInstantiation{}, `{}`, ir.TemplateInstantiation{
		Template: "Page",
		Args: []ir.TemplateArg{
			{Type: &ir.TypeRef{Target: "t/item"}},
			{Value: &ir.Value{Kind: ir.ValueNumber, Num: ir.BigVal("10")}},
		},
	})
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
