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
		TypeCommon:  populatedTypeCommon("t/map/Attrs"),
		Key:         &ir.TypeRef{Target: "t/prim/int64"},
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
		TypeCommon:  populatedTypeCommon("t/tuple/Pair"),
		Elems:       []ir.TypeRef{{Target: "t/prim/string"}, {Target: "t/prim/int32"}},
		Rest:        &ir.TypeRef{Target: "t/prim/bool", Nullable: true},
		Constraints: populatedConstraints(),
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

// primKindSpellings is the whole primitive vocabulary and its on-disk spelling.
// It is a package-level fixture rather than a literal inside the test below
// because the ID tests walk the same vocabulary, and a second list of the kinds
// would be one more thing to keep in step with ir.PrimKind's constants.
var primKindSpellings = map[ir.PrimKind]string{
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

// TestPrimKind_Constants pins the on-disk spelling of every PrimKind value.
// These strings are the primitive-scalar vocabulary written into every
// golden IR snapshot; a typo fix later would be a silent breaking change.
func TestPrimKind_Constants(t *testing.T) {
	t.Parallel()
	assertConstantSpellings(t, primKindSpellings, "unspecified")
}

// TestPrimKind_TiesToConstBlock keeps PrimKind.Valid tied to the const block.
// Nothing rejects an invented kind on deserialization and ir.PrimTypeID derives
// an ID from any string, so irverify has Valid and nothing else to test
// against. The tie is made by reading the ir package's declarations, as
// typedef_completeness_test.go does for TypeKind. primKindSpellings and the
// declared constants must name the same set, and every declared kind must be
// Valid. The converse, a case added to Valid for a kind no constant declares,
// cannot be enforced here: a switch body is not enumerable at run time.
func TestPrimKind_TiesToConstBlock(t *testing.T) {
	t.Parallel()
	declared := declaredConstsOfType(t, "PrimKind")
	require.NotEmpty(t, declared, "the ir sources must declare PrimKind constants")

	values := make([]string, 0, len(declared))
	for _, c := range declared {
		values = append(values, c.Value)
		assert.True(t, ir.PrimKind(c.Value).Valid(), "declared kind %q must be Valid", c.Value)
	}

	spelled := slices.Collect(maps.Values(primKindSpellings))
	require.ElementsMatch(t, spelled, values)
}

// TestPrimKind_UnknownIsInvalid pins the other direction: Valid must reject a
// kind no const declares, including the zero value a compiler leaves behind when
// it forgets the field, and a re-cased spelling of a real one.
func TestPrimKind_UnknownIsInvalid(t *testing.T) {
	t.Parallel()
	assert.False(t, ir.PrimKind("").Valid())
	assert.False(t, ir.PrimKind("flt").Valid())
	assert.False(t, ir.PrimKind("Float64").Valid())
	assert.False(t, ir.PrimKind("datetimeOffset").Valid())
}

// domainEdges is every immediate containment, written out once so the edge
// test holds DomainParents to it in both directions.
var domainEdges = map[ir.PrimKind][]ir.PrimKind{
	ir.PrimInt8:       {ir.PrimInt16},
	ir.PrimInt16:      {ir.PrimInt32, ir.PrimFloat32},
	ir.PrimInt32:      {ir.PrimInt64, ir.PrimFloat64},
	ir.PrimInt64:      {ir.PrimInteger, ir.PrimDecimal128},
	ir.PrimUint8:      {ir.PrimUint16, ir.PrimInt16},
	ir.PrimUint16:     {ir.PrimUint32, ir.PrimInt32, ir.PrimFloat32},
	ir.PrimUint32:     {ir.PrimUint64, ir.PrimInt64, ir.PrimFloat64},
	ir.PrimUint64:     {ir.PrimInteger, ir.PrimDecimal128},
	ir.PrimInteger:    {ir.PrimFloat},
	ir.PrimFloat32:    {ir.PrimFloat64},
	ir.PrimFloat64:    {ir.PrimFloat},
	ir.PrimFloat:      {ir.PrimDecimal},
	ir.PrimDecimal128: {ir.PrimDecimal},
	ir.PrimDecimal:    {ir.PrimNumber},
}

// domainRoots are the kinds with no immediate supertype: they sit directly
// under PrimAny, which is a rule of StrictlyWithin and not an edge.
var domainRoots = []ir.PrimKind{
	ir.PrimBool, ir.PrimString, ir.PrimBytes, ir.PrimNumber,
	ir.PrimDate, ir.PrimTime, ir.PrimDatetime, ir.PrimDatetimeOffset, ir.PrimDuration,
	ir.PrimURL, ir.PrimUUID, ir.PrimAny,
}

// declaredPrimKinds returns every PrimKind the ir sources declare.
func declaredPrimKinds(t *testing.T) []ir.PrimKind {
	t.Helper()
	consts := declaredConstsOfType(t, "PrimKind")
	kinds := make([]ir.PrimKind, 0, len(consts))
	for _, c := range consts {
		kinds = append(kinds, ir.PrimKind(c.Value))
	}
	require.NotEmpty(t, kinds)
	return kinds
}

// TestPrimKind_DomainParentsAreTheContainmentEdges holds the whole edge table,
// strictly in both directions: every edge listed is returned, and nothing else.
func TestPrimKind_DomainParentsAreTheContainmentEdges(t *testing.T) {
	t.Parallel()
	for _, k := range declaredPrimKinds(t) {
		assert.ElementsMatch(t, domainEdges[k], k.DomainParents(), "parents of %s", k)
	}
	assert.Empty(t, ir.PrimKind("flt").DomainParents())
	got := ir.PrimInt16.DomainParents()
	got[0] = ir.PrimBool
	assert.Equal(t, ir.PrimInt32, ir.PrimInt16.DomainParents()[0], "the result must be fresh")
}

// TestPrimKind_StrictlyWithinIsAStrictOrder checks irreflexivity,
// antisymmetry and transitivity over every pair of declared kinds, and that
// any is the top.
func TestPrimKind_StrictlyWithinIsAStrictOrder(t *testing.T) {
	t.Parallel()
	kinds := declaredPrimKinds(t)
	for _, a := range kinds {
		assert.False(t, a.StrictlyWithin(a), "%s within itself", a)
		assert.Equal(t, a != ir.PrimAny, a.StrictlyWithin(ir.PrimAny), "%s within any", a)
		assert.False(t, ir.PrimAny.StrictlyWithin(a), "any within %s", a)
		for _, b := range kinds {
			if a.StrictlyWithin(b) {
				assert.False(t, b.StrictlyWithin(a), "%s and %s within each other", a, b)
			}
			for _, c := range kinds {
				if a.StrictlyWithin(b) && b.StrictlyWithin(c) {
					assert.True(t, a.StrictlyWithin(c), "%s within %s within %s", a, b, c)
				}
			}
		}
	}
	assert.True(t, ir.PrimInt8.StrictlyWithin(ir.PrimNumber))
	assert.True(t, ir.PrimUint8.StrictlyWithin(ir.PrimInt16))
	for _, k := range []ir.PrimKind{ir.PrimFloat32, ir.PrimFloat64, ir.PrimFloat, ir.PrimInteger} {
		assert.True(t, k.StrictlyWithin(ir.PrimDecimal), "%s within decimal", k)
	}
	assert.True(t, ir.PrimFloat.StrictlyWithin(ir.PrimNumber))
	assert.False(t, ir.PrimNumber.StrictlyWithin(ir.PrimFloat32))
	assert.False(t, ir.PrimInt64.StrictlyWithin(ir.PrimFloat64))
	assert.False(t, ir.PrimInt32.StrictlyWithin(ir.PrimFloat32))
	assert.False(t, ir.PrimInt32.StrictlyWithin(ir.PrimUint32))
	assert.False(t, ir.PrimUUID.StrictlyWithin(ir.PrimString))
	assert.False(t, ir.PrimKind("flt").StrictlyWithin(ir.PrimAny))
	assert.False(t, ir.PrimInt8.StrictlyWithin(ir.PrimKind("flt")))
}

// TestPrimKind_EveryDeclaredKindHasADomainParentOrIsARoot holds domainRoots to
// the declared set, so a kind added without an edge reddens here.
func TestPrimKind_EveryDeclaredKindHasADomainParentOrIsARoot(t *testing.T) {
	t.Parallel()
	for _, k := range declaredPrimKinds(t) {
		isRoot := slices.Contains(domainRoots, k)
		assert.Equal(t, !isRoot, len(k.DomainParents()) > 0, "%s must be a root or have a parent, not both", k)
	}
	for _, r := range domainRoots {
		assert.True(t, r.Valid(), "root %q must be a declared kind", r)
	}
}

// TestPrimKind_LongestDomainChainFitsTheCap reads the walk's cap from the
// source and holds the longest chain DomainParents makes to it.
func TestPrimKind_LongestDomainChainFitsTheCap(t *testing.T) {
	t.Parallel()
	limit, err := irDecls(t).IntConst("maxDomainDepth")
	require.NoError(t, err)

	var longest func(k ir.PrimKind, depth int) int
	longest = func(k ir.PrimKind, depth int) int {
		require.LessOrEqual(t, int64(depth), limit, "a chain past maxDomainDepth: a cycle or a cap too low")
		best := 0
		for _, p := range k.DomainParents() {
			best = max(best, 1+longest(p, depth+1))
		}
		return best
	}
	chain := 0
	for _, k := range declaredPrimKinds(t) {
		chain = max(chain, longest(k, 0))
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
	assertJSONContract(t, ir.Discriminator{}, `{"inferred":false}`, ir.Discriminator{
		Property:          "p/kind",
		PropertyName:      "kind",
		Index:             new(0),
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

// TestEnumMember_JSONContract pins EnumMember's omitempty contract (ID, Name,
// Value, and Docs carry no omitempty; every other field is optional) and that
// a fully populated EnumMember round-trips.
func TestEnumMember_JSONContract(t *testing.T) {
	t.Parallel()
	assertJSONContract(t, ir.EnumMember{},
		`{"id":"","name":{},"value":{"kind":""},"docs":{}}`,
		ir.EnumMember{
			ID:           "e/openapi/components/schemas/Status/s:active",
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
