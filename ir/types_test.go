// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package ir_test

import (
	"maps"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
)

// typeDefZeroShapeTests is the Class A table for every concrete TypeDef kind:
// each must marshal its zero value with the "kind" tag adjacent and correct,
// followed by TypeCommon's shared fields and then its own fields in
// declaration order (json.go's kinded wrapper).
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
			want: `{"kind":"primitive","id":"","name":{},"anonymous":false,"docs":{},"sensitive":false,"provenance":{"source":0},"primitiveKind":""}`,
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
			want: `{"kind":"union","id":"","name":{},"anonymous":false,"docs":{},"sensitive":false,"provenance":{"source":0},"exclusive":false,"wireTagged":false,"closed":false,"inputOnly":false}`,
		},
		{
			name: "enum",
			td:   &ir.Enum{},
			want: `{"kind":"enum","id":"","name":{},"anonymous":false,"docs":{},"sensitive":false,"provenance":{"source":0},"valueType":"","closed":false,"flags":false}`,
		},
		{
			name: "list",
			td:   &ir.List{},
			want: `{"kind":"list","id":"","name":{},"anonymous":false,"docs":{},"sensitive":false,"provenance":{"source":0},"elementType":{"target":"","nullable":false}}`,
		},
		{
			name: "map",
			td:   &ir.MapT{},
			want: `{"kind":"map","id":"","name":{},"anonymous":false,"docs":{},"sensitive":false,"provenance":{"source":0},"value":{"target":"","nullable":false}}`,
		},
		{
			name: "tuple",
			td:   &ir.Tuple{},
			want: `{"kind":"tuple","id":"","name":{},"anonymous":false,"docs":{},"sensitive":false,"provenance":{"source":0}}`,
		},
		{
			name: "literal",
			td:   &ir.Literal{},
			want: `{"kind":"literal","id":"","name":{},"anonymous":false,"docs":{},"sensitive":false,"provenance":{"source":0},"value":{"kind":""}}`,
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

// TestPrimitive_PopulatedRoundTrip pins that a fully populated Primitive
// (TypeCommon plus PrimitiveKind) round-trips.
func TestPrimitive_PopulatedRoundTrip(t *testing.T) {
	t.Parallel()
	want := &ir.Primitive{
		TypeCommon:    populatedTypeCommon("type/primitive/string"),
		PrimitiveKind: ir.PrimitiveString,
	}
	assertTypeDefRoundTrip(t, want)
}

// TestScalar_PopulatedRoundTrip pins that a fully populated Scalar — base
// chain, constraints, and encoding override — round-trips.
func TestScalar_PopulatedRoundTrip(t *testing.T) {
	t.Parallel()
	want := &ir.Scalar{
		TypeCommon:  populatedTypeCommon("type/scalar/positive-int"),
		Base:        &ir.TypeRef{Target: "type/primitive/int32"},
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
		TypeCommon: populatedTypeCommon("type/model/User"),
		Properties: []ir.Property{populatedProperty()},
		Base:       &ir.TypeRef{Target: "type/model/Base"},
		Implements: []ir.TypeRef{{Target: "type/iface/A"}, {Target: "type/iface/B"}},
		Mixins:     []ir.TypeRef{{Target: "type/mixin/M1"}},
		Requires:   []string{"createdAt"},
		AdditionalProperties: &ir.AdditionalProperties{
			Value: new(populatedTypeRef()),
			Key:   &ir.TypeRef{Target: "type/primitive/string"},
			Patterns: []ir.PatternProperties{
				{Pattern: "^x-", Value: populatedTypeRef()},
			},
		},
		Additional:      ir.AdditionalClosedAfterComposition,
		Constraints:     populatedConstraints(),
		Abstract:        true,
		Positional:      true,
		ExtensionRanges: []ir.WireIDRange{{From: 100, To: 199}},
		Discriminator: &ir.Discriminator{
			Property: "property/kind",
			Mapping:  map[string]ir.TypeID{"dog": "type/Dog", "cat": "type/Cat"},
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
		TypeCommon: populatedTypeCommon("type/union/Pet"),
		Variants: []ir.Variant{
			{Name: ir.Naming{Source: "dog"}, Type: ir.TypeRef{Target: "type/Dog"}},
			{Name: ir.Naming{Source: "cat"}, Type: ir.TypeRef{Target: "type/Cat"}},
		},
		Exclusive:  true,
		WireTagged: true,
		Closed:     true,
		InputOnly:  true,
		Discriminator: &ir.Discriminator{
			PropertyName: "petType",
			Mapping:      map[string]ir.TypeID{"dog": "type/Dog"},
		},
		Constraints: &ir.Constraints{MinProperties: new(int64(1))},
	}
	assertTypeDefRoundTrip(t, want)
}

// TestEnum_PopulatedRoundTrip pins that a fully populated Enum round-trips.
func TestEnum_PopulatedRoundTrip(t *testing.T) {
	t.Parallel()
	want := &ir.Enum{
		TypeCommon: populatedTypeCommon("type/enum/Status"),
		ValueType:  ir.PrimitiveString,
		Members: []ir.EnumMember{
			{Name: ir.Naming{Source: "ACTIVE"}, Value: ir.Value{Kind: ir.ValueString, String: "active"}},
			{Name: ir.Naming{Source: "INACTIVE"}, Value: ir.Value{Kind: ir.ValueString, String: "inactive"}},
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
		TypeCommon:  populatedTypeCommon("type/list/Tags"),
		ElementType: populatedTypeRef(),
		Constraints: populatedConstraints(),
		Encoding:    populatedEncoding(),
	}
	assertTypeDefRoundTrip(t, want)
}

// TestMapT_PopulatedRoundTrip pins that a fully populated MapT round-trips.
func TestMapT_PopulatedRoundTrip(t *testing.T) {
	t.Parallel()
	want := &ir.MapT{
		TypeCommon:  populatedTypeCommon("type/map/Attrs"),
		Key:         &ir.TypeRef{Target: "type/primitive/int64"},
		Value:       populatedTypeRef(),
		Constraints: populatedConstraints(),
	}
	assertTypeDefRoundTrip(t, want)
}

// TestTuple_PopulatedRoundTrip pins that a fully populated Tuple round-trips
// with its element order preserved.
func TestTuple_PopulatedRoundTrip(t *testing.T) {
	t.Parallel()
	want := &ir.Tuple{
		TypeCommon:   populatedTypeCommon("type/tuple/Pair"),
		ElementTypes: []ir.TypeRef{{Target: "type/primitive/string"}, {Target: "type/primitive/int32"}},
		Rest:         &ir.TypeRef{Target: "type/primitive/bool", Nullable: true},
		Constraints:  populatedConstraints(),
	}
	assertTypeDefRoundTrip(t, want)
}

// TestLiteral_PopulatedRoundTrip pins that a fully populated Literal
// round-trips.
func TestLiteral_PopulatedRoundTrip(t *testing.T) {
	t.Parallel()
	want := &ir.Literal{
		TypeCommon: populatedTypeCommon("type/literal/Fixed"),
		Value:      ir.Value{Kind: ir.ValueString, String: "fixed"},
		ValueType:  ir.PrimitiveString,
	}
	assertTypeDefRoundTrip(t, want)
}

// TestExternal_PopulatedRoundTrip pins that a fully populated External
// round-trips.
func TestExternal_PopulatedRoundTrip(t *testing.T) {
	t.Parallel()
	want := &ir.External{
		TypeCommon: populatedTypeCommon("type/external/Pid"),
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
	want := &ir.Any{TypeCommon: populatedTypeCommon("type/any/Freeform")}
	assertTypeDefRoundTrip(t, want)
}

// primitiveKindSpellings is the whole primitive vocabulary and its on-disk
// spelling. It is a package-level fixture rather than a literal inside the test
// below because the ID tests walk the same vocabulary, and a second list of the
// kinds would be one more thing to keep in step with ir.PrimitiveKind's
// constants.
var primitiveKindSpellings = map[ir.PrimitiveKind]string{
	ir.PrimitiveBool:           "bool",
	ir.PrimitiveString:         "string",
	ir.PrimitiveBytes:          "bytes",
	ir.PrimitiveInt8:           "int8",
	ir.PrimitiveInt16:          "int16",
	ir.PrimitiveInt32:          "int32",
	ir.PrimitiveInt64:          "int64",
	ir.PrimitiveUint8:          "uint8",
	ir.PrimitiveUint16:         "uint16",
	ir.PrimitiveUint32:         "uint32",
	ir.PrimitiveUint64:         "uint64",
	ir.PrimitiveInteger:        "integer",
	ir.PrimitiveFloat32:        "float32",
	ir.PrimitiveFloat64:        "float64",
	ir.PrimitiveFloat:          "float",
	ir.PrimitiveNumber:         "number",
	ir.PrimitiveDecimal:        "decimal",
	ir.PrimitiveDecimal128:     "decimal128",
	ir.PrimitiveDate:           "date",
	ir.PrimitiveTime:           "time",
	ir.PrimitiveDatetime:       "datetime",
	ir.PrimitiveDatetimeOffset: "datetime_offset",
	ir.PrimitiveDuration:       "duration",
	ir.PrimitiveURL:            "url",
	ir.PrimitiveUUID:           "uuid",
	ir.PrimitiveAny:            "any",
}

// TestPrimitiveKind_Constants pins the on-disk spelling of every PrimitiveKind
// value. These strings are the primitive-scalar vocabulary written into every
// golden IR snapshot; a typo fix later would be a silent breaking change.
func TestPrimitiveKind_Constants(t *testing.T) {
	t.Parallel()
	assertConstantSpellings(t, primitiveKindSpellings, "unspecified")
}

// TestPrimitiveKind_TiesToConstBlock keeps PrimitiveKind.Valid tied to the
// const block. Nothing rejects an invented kind on deserialization and
// ir.PrimitiveTypeID derives an ID from any string, so irverify has Valid and
// nothing else to test against. The tie is made by reading the ir package's
// declarations, as typedef_completeness_test.go does for TypeKind.
// primitiveKindSpellings and the declared constants must name the same set, and
// every declared kind must be Valid. The converse, a case added to Valid for a
// kind no constant declares, cannot be enforced here: a switch body is not
// enumerable at run time.
func TestPrimitiveKind_TiesToConstBlock(t *testing.T) {
	t.Parallel()
	declared := declaredConstsOfType(t, "PrimitiveKind")
	require.NotEmpty(t, declared, "the ir sources must declare PrimitiveKind constants")

	values := make([]string, 0, len(declared))
	for _, declaredConst := range declared {
		values = append(values, declaredConst.Value)
		assert.True(t, ir.PrimitiveKind(declaredConst.Value).Valid(), "declared kind %q must be Valid", declaredConst.Value)
	}

	spelled := slices.Collect(maps.Values(primitiveKindSpellings))
	require.ElementsMatch(t, spelled, values)
}

// TestPrimitiveKind_UnknownIsInvalid pins the other direction: Valid must
// reject a kind no const declares, including the zero value a compiler leaves
// behind when it forgets the field, and a re-cased spelling of a real one.
func TestPrimitiveKind_UnknownIsInvalid(t *testing.T) {
	t.Parallel()
	assert.False(t, ir.PrimitiveKind("").Valid())
	assert.False(t, ir.PrimitiveKind("flt").Valid())
	assert.False(t, ir.PrimitiveKind("Float64").Valid())
	assert.False(t, ir.PrimitiveKind("datetimeOffset").Valid())
}

// domainEdges is every immediate containment, written out once so the edge
// test holds DomainParents to it in both directions.
var domainEdges = map[ir.PrimitiveKind][]ir.PrimitiveKind{
	ir.PrimitiveInt8:       {ir.PrimitiveInt16},
	ir.PrimitiveInt16:      {ir.PrimitiveInt32, ir.PrimitiveFloat32},
	ir.PrimitiveInt32:      {ir.PrimitiveInt64, ir.PrimitiveFloat64},
	ir.PrimitiveInt64:      {ir.PrimitiveInteger, ir.PrimitiveDecimal128},
	ir.PrimitiveUint8:      {ir.PrimitiveUint16, ir.PrimitiveInt16},
	ir.PrimitiveUint16:     {ir.PrimitiveUint32, ir.PrimitiveInt32, ir.PrimitiveFloat32},
	ir.PrimitiveUint32:     {ir.PrimitiveUint64, ir.PrimitiveInt64, ir.PrimitiveFloat64},
	ir.PrimitiveUint64:     {ir.PrimitiveInteger, ir.PrimitiveDecimal128},
	ir.PrimitiveInteger:    {ir.PrimitiveFloat},
	ir.PrimitiveFloat32:    {ir.PrimitiveFloat64},
	ir.PrimitiveFloat64:    {ir.PrimitiveFloat},
	ir.PrimitiveFloat:      {ir.PrimitiveDecimal},
	ir.PrimitiveDecimal128: {ir.PrimitiveDecimal},
	ir.PrimitiveDecimal:    {ir.PrimitiveNumber},
}

// domainRoots are the kinds with no immediate supertype: they sit directly
// under PrimitiveAny, which is a rule of StrictlyWithin and not an edge.
var domainRoots = []ir.PrimitiveKind{
	ir.PrimitiveBool, ir.PrimitiveString, ir.PrimitiveBytes, ir.PrimitiveNumber,
	ir.PrimitiveDate, ir.PrimitiveTime, ir.PrimitiveDatetime, ir.PrimitiveDatetimeOffset, ir.PrimitiveDuration,
	ir.PrimitiveURL, ir.PrimitiveUUID, ir.PrimitiveAny,
}

// declaredPrimitiveKinds returns every PrimitiveKind the ir sources declare.
func declaredPrimitiveKinds(t *testing.T) []ir.PrimitiveKind {
	t.Helper()
	consts := declaredConstsOfType(t, "PrimitiveKind")
	kinds := make([]ir.PrimitiveKind, 0, len(consts))
	for _, declaredConst := range consts {
		kinds = append(kinds, ir.PrimitiveKind(declaredConst.Value))
	}
	require.NotEmpty(t, kinds)
	return kinds
}

// TestPrimitiveKind_DomainParentsAreTheContainmentEdges holds the whole edge
// table, strictly in both directions: every edge listed is returned, and
// nothing else.
func TestPrimitiveKind_DomainParentsAreTheContainmentEdges(t *testing.T) {
	t.Parallel()
	for _, primitiveKind := range declaredPrimitiveKinds(t) {
		assert.ElementsMatch(t, domainEdges[primitiveKind], primitiveKind.DomainParents(), "parents of %s", primitiveKind)
	}
	assert.Empty(t, ir.PrimitiveKind("flt").DomainParents())
	got := ir.PrimitiveInt16.DomainParents()
	got[0] = ir.PrimitiveBool
	assert.Equal(t, ir.PrimitiveInt32, ir.PrimitiveInt16.DomainParents()[0], "the result must be fresh")
}

// TestPrimitiveKind_StrictlyWithinIsAStrictOrder checks irreflexivity,
// antisymmetry and transitivity over every pair of declared kinds, and that
// any is the top.
func TestPrimitiveKind_StrictlyWithinIsAStrictOrder(t *testing.T) {
	t.Parallel()
	kinds := declaredPrimitiveKinds(t)
	for _, a := range kinds {
		assert.False(t, a.StrictlyWithin(a), "%s within itself", a)
		assert.Equal(t, a != ir.PrimitiveAny, a.StrictlyWithin(ir.PrimitiveAny), "%s within any", a)
		assert.False(t, ir.PrimitiveAny.StrictlyWithin(a), "any within %s", a)
		for _, b := range kinds {
			if a.StrictlyWithin(b) {
				assert.False(t, b.StrictlyWithin(a), "%s and %s within each other", a, b)
			}
			for _, primitiveKind := range kinds {
				if a.StrictlyWithin(b) && b.StrictlyWithin(primitiveKind) {
					assert.True(t, a.StrictlyWithin(primitiveKind), "%s within %s within %s", a, b, primitiveKind)
				}
			}
		}
	}
	assert.True(t, ir.PrimitiveInt8.StrictlyWithin(ir.PrimitiveNumber))
	assert.True(t, ir.PrimitiveUint8.StrictlyWithin(ir.PrimitiveInt16))
	for _, primitiveKind := range []ir.PrimitiveKind{ir.PrimitiveFloat32, ir.PrimitiveFloat64, ir.PrimitiveFloat, ir.PrimitiveInteger} {
		assert.True(t, primitiveKind.StrictlyWithin(ir.PrimitiveDecimal), "%s within decimal", primitiveKind)
	}
	assert.True(t, ir.PrimitiveFloat.StrictlyWithin(ir.PrimitiveNumber))
	assert.False(t, ir.PrimitiveNumber.StrictlyWithin(ir.PrimitiveFloat32))
	assert.False(t, ir.PrimitiveInt64.StrictlyWithin(ir.PrimitiveFloat64))
	assert.False(t, ir.PrimitiveInt32.StrictlyWithin(ir.PrimitiveFloat32))
	assert.False(t, ir.PrimitiveInt32.StrictlyWithin(ir.PrimitiveUint32))
	assert.False(t, ir.PrimitiveUUID.StrictlyWithin(ir.PrimitiveString))
	assert.False(t, ir.PrimitiveKind("flt").StrictlyWithin(ir.PrimitiveAny))
	assert.False(t, ir.PrimitiveInt8.StrictlyWithin(ir.PrimitiveKind("flt")))
}

// TestPrimitiveKind_EveryDeclaredKindHasADomainParentOrIsARoot holds
// domainRoots to the declared set, so a kind added without an edge reddens
// here.
func TestPrimitiveKind_EveryDeclaredKindHasADomainParentOrIsARoot(t *testing.T) {
	t.Parallel()
	for _, primitiveKind := range declaredPrimitiveKinds(t) {
		isRoot := slices.Contains(domainRoots, primitiveKind)
		assert.Equal(t, !isRoot, len(primitiveKind.DomainParents()) > 0, "%s must be a root or have a parent, not both", primitiveKind)
	}
	for _, primitiveKind := range domainRoots {
		assert.True(t, primitiveKind.Valid(), "root %q must be a declared kind", primitiveKind)
	}
}

// TestPrimitiveKind_LongestDomainChainFitsTheCap reads the walk's cap from the
// source and holds the longest chain DomainParents makes to it.
func TestPrimitiveKind_LongestDomainChainFitsTheCap(t *testing.T) {
	t.Parallel()
	limit, err := irDecls(t).IntConst("maxDomainDepth")
	require.NoError(t, err)

	var longest func(primitiveKind ir.PrimitiveKind, depth int) int
	longest = func(primitiveKind ir.PrimitiveKind, depth int) int {
		require.LessOrEqual(t, int64(depth), limit, "a chain past maxDomainDepth: a cycle or a cap too low")
		best := 0
		for _, primitiveKindValue := range primitiveKind.DomainParents() {
			best = max(best, 1+longest(primitiveKindValue, depth+1))
		}
		return best
	}
	chain := 0
	for _, primitiveKind := range declaredPrimitiveKinds(t) {
		chain = max(chain, longest(primitiveKind, 0))
	}
	assert.LessOrEqual(t, int64(chain), limit)
	assert.Equal(t, 7, chain, "int8 up to number through float and decimal")
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

// TestAdditionalProperties_JSONContract pins AdditionalProperties' contract
// that Value carries no omitempty (a catch-all always has a value schema) and
// that a fully populated AdditionalProperties — key schema and pattern-scoped
// value schemas — round-trips.
func TestAdditionalProperties_JSONContract(t *testing.T) {
	t.Parallel()
	assertJSONContract(t, ir.AdditionalProperties{}, `{}`,
		ir.AdditionalProperties{
			Value: new(populatedTypeRef()),
			Key:   &ir.TypeRef{Target: "type/primitive/string"},
			Patterns: []ir.PatternProperties{
				{Pattern: "^x-", Value: populatedTypeRef()},
				{Pattern: "^y-", Value: ir.TypeRef{Target: "type/primitive/int32"}},
			},
		})
}

// TestPatternProperties_JSONContract pins PatternProperties' contract that both
// fields carry no omitempty and that a populated PatternProperties round-trips.
func TestPatternProperties_JSONContract(t *testing.T) {
	t.Parallel()
	assertJSONContract(t, ir.PatternProperties{}, `{"pattern":"","value":{"target":"","nullable":false}}`,
		ir.PatternProperties{Pattern: "^x-", Value: populatedTypeRef()})
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
	assertJSONContract(t, ir.Discriminator{}, `{"inferred":false}`, ir.Discriminator{
		Property:          "property/kind",
		PropertyName:      "kind",
		TupleElementIndex: new(0),
		Mapping:           map[string]ir.TypeID{"dog": "type/Dog", "cat": "type/Cat"},
		Default:           "type/Unknown",
		Envelope:          "object",
		EnvelopeValueName: "value",
		Inferred:          true,
	})
}

// TestDiscriminator_MappingDeterministic pins Class C for Discriminator's map
// field: Mapping must marshal with keys in sorted order on every run.
func TestDiscriminator_MappingDeterministic(t *testing.T) {
	t.Parallel()
	discriminator := ir.Discriminator{
		Mapping: map[string]ir.TypeID{
			"z-tag": "type/Z", "m-tag": "type/M", "a-tag": "type/A", "q-tag": "type/Q", "b-tag": "type/B",
		},
	}
	got := assertDeterministicMarshal(t, discriminator)
	assert.Contains(t, got,
		`"mapping":{"a-tag":"type/A","b-tag":"type/B","m-tag":"type/M","q-tag":"type/Q","z-tag":"type/Z"}`)
}

// TestVariant_JSONContract pins Variant's omitempty contract (Name, Type, and
// Docs carry no omitempty; every other field is optional) and that a fully
// populated Variant round-trips.
func TestVariant_JSONContract(t *testing.T) {
	t.Parallel()
	assertJSONContract(t, ir.Variant{}, `{"name":{},"type":{"target":"","nullable":false},"docs":{}}`,
		ir.Variant{
			Name:         populatedNaming(),
			Type:         populatedTypeRef(),
			WireName:     "dog_variant",
			WireID:       new(3),
			XML:          populatedXMLHints(),
			Event:        &ir.EventInfo{ContentType: "application/json", Terminal: true},
			Docs:         populatedDocs(),
			Deprecation:  populatedDeprecation(),
			Availability: populatedAvailability(),
			Examples: []ir.Example{
				{Name: "ex1", Value: &ir.Value{Kind: ir.ValueString, String: "v1"}},
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

// TestEnumMember_JSONContract pins EnumMember's omitempty contract (ID, Name,
// Value, and Docs carry no omitempty; every other field is optional) and that
// a fully populated EnumMember round-trips.
func TestEnumMember_JSONContract(t *testing.T) {
	t.Parallel()
	assertJSONContract(t, ir.EnumMember{},
		`{"id":"","name":{},"value":{"kind":""},"docs":{}}`,
		ir.EnumMember{
			ID:           "enum-member/openapi/components/schemas/Status/string:active",
			Name:         populatedNaming(),
			Value:        ir.Value{Kind: ir.ValueString, String: "active"},
			WireName:     "ACTIVE",
			Docs:         populatedDocs(),
			Deprecation:  populatedDeprecation(),
			Availability: populatedAvailability(),
			Examples: []ir.Example{
				{Name: "ex1", Value: &ir.Value{Kind: ir.ValueString, String: "active"}},
			},
			Unmodeled: populatedUnmodeled(),
		})
}
