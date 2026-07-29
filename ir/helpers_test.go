// Package ir_test holds the per-source serialization-contract tests for the ir
// package. Shared fixtures live here so no single test file has to reconstruct
// a fully populated Docs, Naming, or Provenance from scratch.
package ir_test

import (
	"encoding/json"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
)

// assertRoundTrip marshals want, unmarshals into a fresh T, and asserts the
// result equals want via cmp.Diff (never reflect.DeepEqual, per CLAUDE.md).
// This is the Class B (populated round-trip) workhorse shared by every
// per-source test file.
func assertRoundTrip[T any](t *testing.T, want T) {
	t.Helper()
	raw, err := json.Marshal(want)
	require.NoError(t, err)
	var got T
	require.NoError(t, json.Unmarshal(raw, &got))
	assert.Empty(t, cmp.Diff(want, got), "JSON round-trip must reproduce the original value")
}

// assertZeroValueShape marshals zero and asserts the encoding is exactly
// wantJSON. This is the Class A workhorse: it is the test that fails the
// instant an omitempty tag is added or removed on a field that must always
// (or never) appear on the wire.
func assertZeroValueShape[T any](t *testing.T, zero T, wantJSON string) {
	t.Helper()
	raw, err := json.Marshal(zero)
	require.NoError(t, err)
	assert.Equal(t, wantJSON, string(raw))
}

// determinismTries is the number of repeated marshal calls
// assertDeterministicMarshal compares, per the test specification's Class C
// requirement ("marshal 20 times").
const determinismTries = 20

// assertDeterministicMarshal marshals v determinismTries times and asserts
// every encoding is byte-identical to the first (Class C / invariant #7: maps
// emitted in sorted-key order so golden snapshots and caching are stable).
func assertDeterministicMarshal(t *testing.T, v any) string {
	t.Helper()
	first, err := json.Marshal(v)
	require.NoError(t, err)
	for i := 1; i < determinismTries; i++ {
		next, err := json.Marshal(v)
		require.NoError(t, err)
		require.Equal(t, string(first), string(next), "marshal run %d diverged from the first", i)
	}
	return string(first)
}

// assertJSONContract merges a type's Class A (zero-value shape) and Class B
// (populated round-trip) checks into one call, running each half in its own
// t.Run subtest rather than inline — assertRoundTrip's require.NoError calls
// t.FailNow, so without the subtest boundary a failure in the zero half would
// abort the function before the round-trip half ever ran.
func assertJSONContract[T any](t *testing.T, zero T, wantZeroJSON string, populated T) {
	t.Helper()
	t.Run("zero", func(t *testing.T) {
		t.Parallel()
		assertZeroValueShape(t, zero, wantZeroJSON)
	})
	t.Run("roundtrip", func(t *testing.T) {
		t.Parallel()
		assertRoundTrip(t, populated)
	})
}

// assertConstantSpellings asserts each tests entry round-trips: the map value
// is the wire spelling, the map key the constant it must convert back to, one
// subtest per entry named after the spelling so one typo can't mask another.
// emptyName names the subtest for the empty-string member, since not every
// K's empty-string spelling is "unspecified" (IdempotencyKind's is
// "unknown"), so callers supply their own rather than the helper guessing.
func assertConstantSpellings[K ~string](t *testing.T, tests map[K]string, emptyName string) {
	t.Helper()
	for kind, want := range tests {
		name := want
		if name == "" {
			name = emptyName
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, K(want), kind)
		})
	}
}

// populatedNaming returns a Naming with every field non-zero, for embedding in
// Class B fixtures.
func populatedNaming() ir.Naming {
	return ir.Naming{
		Source:    "user_id",
		Canonical: "user_id",
		Hint:      "connection_domain",
		Aliases:   []string{"uid", "userId"},
	}
}

// populatedDocs returns a Docs with every field non-zero.
func populatedDocs() ir.Docs {
	return ir.Docs{
		Summary:     "short summary",
		Description: "long CommonMark description with a {t:t/x} cross-reference",
		ExternalDocs: []ir.Link{
			{URL: "https://example.com/a", Description: "doc a"},
			{URL: "https://example.com/b", Description: "doc b"},
		},
	}
}

// populatedProvenance returns a Provenance with every field non-zero.
func populatedProvenance() ir.Provenance {
	return ir.Provenance{
		Source:   3,
		Pointer:  "/paths/~1users/get",
		Inferred: "pagination-name-match",
	}
}

// populatedUnmodeled returns an Unmodeled map with several entries whose
// insertion order differs from sorted order, for Class C determinism checks.
func populatedUnmodeled() ir.Unmodeled {
	return ir.Unmodeled{
		"openapi:z-ext": {Reason: ir.ReasonVendorExtension, Value: ir.RawValue(`1`), Provenance: populatedProvenance()},
		"openapi:m-ext": {Reason: ir.ReasonVendorExtension, Value: ir.RawValue(`2`), Provenance: populatedProvenance()},
		"openapi:a-ext": {Reason: ir.ReasonVendorExtension, Value: ir.RawValue(`3`), Provenance: populatedProvenance()},
	}
}

// populatedRawConfig returns a RawConfig map with several entries whose
// insertion order differs from sorted order, for Class C determinism checks.
func populatedRawConfig() ir.RawConfig {
	return ir.RawConfig{
		"z-opt": ir.RawValue(`1`),
		"m-opt": ir.RawValue(`2`),
		"a-opt": ir.RawValue(`3`),
	}
}

// populatedDeprecation returns a Deprecation with every field non-zero.
func populatedDeprecation() *ir.Deprecation {
	return &ir.Deprecation{
		Message:        "use v2 instead",
		Since:          "1.2.0",
		RemovalVersion: "2.0.0",
	}
}

// populatedAvailability returns an Availability with every field non-zero.
func populatedAvailability() *ir.Availability {
	return &ir.Availability{
		Added:      []string{"v1", "v3"},
		Removed:    []string{"v2"},
		Deprecated: "v3",
		RenamedFrom: []ir.VersionedName{
			{Version: "v1", Name: "oldName"},
			{Version: "v2", Name: "olderName"},
		},
		TypeChangedFrom: []ir.VersionedType{
			{Version: "v1", Type: ir.TypeRef{Target: "t/old", Nullable: true}},
		},
		RequiredChanged: []ir.VersionedBool{
			{Version: "v1", WasRequired: false},
			{Version: "v2", WasRequired: true},
		},
	}
}

// populatedConstraints returns a Constraints with every field non-zero.
func populatedConstraints() *ir.Constraints {
	minV, err := ir.NewBigVal("1")
	if err != nil {
		panic(err)
	}
	maxV, err := ir.NewBigVal("100")
	if err != nil {
		panic(err)
	}
	multV, err := ir.NewBigVal("5")
	if err != nil {
		panic(err)
	}
	precision := int64(10)
	scale := int64(2)
	minLen := int64(1)
	maxLen := int64(255)
	minItems := int64(1)
	maxItems := int64(10)
	minProps := int64(1)
	maxProps := int64(20)
	return &ir.Constraints{
		Min:            &minV,
		Max:            &maxV,
		ExclusiveMin:   true,
		ExclusiveMax:   true,
		MultipleOf:     &multV,
		Precision:      &precision,
		Scale:          &scale,
		MinLength:      &minLen,
		MaxLength:      &maxLen,
		Pattern:        "^[a-z]+$",
		PatternMessage: "lowercase letters only",
		MinItems:       &minItems,
		MaxItems:       &maxItems,
		UniqueItems:    true,
		MinProps:       &minProps,
		MaxProps:       &maxProps,
	}
}

// populatedEncoding returns an Encoding with every field non-zero.
func populatedEncoding() *ir.Encoding {
	return &ir.Encoding{
		Name:      "rfc3339",
		WireType:  &ir.TypeRef{Target: "t/prim/string", Nullable: true},
		MediaType: "text/plain",
	}
}

// populatedXMLHints returns an XMLHints with every field non-zero.
func populatedXMLHints() *ir.XMLHints {
	return &ir.XMLHints{
		Name:      "Item",
		Namespace: "https://example.com/ns",
		Prefix:    "ex",
		NodeType:  "element",
		Wrapped:   true,
	}
}

// populatedTypeRef returns a non-zero TypeRef.
func populatedTypeRef() ir.TypeRef {
	return ir.TypeRef{Target: "t/openapi/components/schemas/User", Nullable: true}
}

// populatedValue returns a fully populated Value of ValueKind list, itself
// containing at least one member of every other ValueKind so a single
// fixture exercises every payload variant (ir-design §6).
func populatedValue() ir.Value {
	return ir.Value{
		Kind: ir.ValueList,
		List: []ir.Value{
			{Kind: ir.ValueNull},
			{Kind: ir.ValueBool, Bool: true},
			{Kind: ir.ValueString, Str: "hello"},
			{Kind: ir.ValueSymbol, Str: "ok"},
			{Kind: ir.ValueNumber, Num: ir.BigVal("3.14")},
			{Kind: ir.ValueBytes, Bytes: []byte{0x01, 0x02}},
			{Kind: ir.ValueObject, Object: []ir.Field{
				{Name: "b", Value: ir.Value{Kind: ir.ValueBool, Bool: false}},
				{Name: "a", Value: ir.Value{Kind: ir.ValueString, Str: "x"}},
			}},
			{Kind: ir.ValueRefKind, Ref: &ir.ValueRef{Type: "t/x", Member: "M"}},
			{Kind: ir.ValueCtor, Ctor: &ir.CtorValue{
				Scalar: "t/s",
				Name:   "fromISO",
				Args:   []ir.Value{{Kind: ir.ValueString, Str: "2024-05-06"}},
			}},
		},
	}
}

// populatedTypeCommon returns a TypeCommon with every field non-zero, for
// embedding in a concrete TypeDef kind's Class B fixture.
func populatedTypeCommon(id ir.TypeID) ir.TypeCommon {
	return ir.TypeCommon{
		ID:               id,
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
		WireNameByFormat: map[string]string{"xml": "Name", "json": "name"},
		MediaTypeHint:    "application/json",
		XML:              populatedXMLHints(),
		Examples: []ir.Example{
			{Name: "ex1", Value: &ir.Value{Kind: ir.ValueString, Str: "v1"}},
		},
		Instantiation: &ir.TemplateInstantiation{
			Template: "Page",
			Args:     []ir.TemplateArg{{Type: &ir.TypeRef{Target: "t/item"}}},
		},
		Unmodeled:  populatedUnmodeled(),
		Provenance: populatedProvenance(),
	}
}

// assertTypeDefRoundTrip marshals a concrete TypeDef pointer (exercising its
// MarshalJSON "kind" tag) and unmarshals into a fresh zero value of the same
// concrete type, then compares with cmp.Diff. Concrete TypeDef kinds have no
// custom UnmarshalJSON — the "kind" tag has no matching struct field, so the
// standard decoder simply ignores it, which is exactly the behavior
// TypeRegistry.UnmarshalJSON relies on after reading the tag separately
// (json.go).
func assertTypeDefRoundTrip[T any](t *testing.T, want *T) {
	t.Helper()
	raw, err := json.Marshal(want)
	require.NoError(t, err)
	got := new(T)
	require.NoError(t, json.Unmarshal(raw, got))
	assert.Empty(t, cmp.Diff(want, got), "JSON round-trip must reproduce the original value")
}

// outOfOrderProtocolBindings returns a map[string]RawConfig fixture whose
// insertion order is not sorted, shared by every Class C test pinning that a
// "namespace protocol -> raw config" bindings map marshals with sorted keys
// (Server.Bindings, Channel.Bindings, Message.Bindings, MessageBinding.Bindings
// all share this exact shape).
func outOfOrderProtocolBindings() map[string]ir.RawConfig {
	return map[string]ir.RawConfig{
		"z-proto": {"k": ir.RawValue(`1`)},
		"m-proto": {"k": ir.RawValue(`2`)},
		"a-proto": {"k": ir.RawValue(`3`)},
		"q-proto": {"k": ir.RawValue(`4`)},
		"b-proto": {"k": ir.RawValue(`5`)},
	}
}

// sortedProtocolBindingsJSON is outOfOrderProtocolBindings encoded with its
// keys in sorted order — the substring every protocol-bindings determinism
// test above asserts its marshaled fixture contains.
const sortedProtocolBindingsJSON = `"bindings":{"a-proto":{"k":3},"b-proto":{"k":5},"m-proto":{"k":2},"q-proto":{"k":4},"z-proto":{"k":1}}`

// populatedProperty returns a Property with every field non-zero, matching
// the shape ir-design §5.1 describes and the zero-value list in the test
// specification.
func populatedProperty() ir.Property {
	wireID := 4
	return ir.Property{
		ID:               "p/openapi/components/schemas/User/properties/id",
		Name:             populatedNaming(),
		WireName:         "id",
		WireNameByFormat: map[string]string{"xml": "Id", "json": "id"},
		WireID:           &wireID,
		ExtensionOf:      "pkg.ThirdParty",
		Type:             populatedTypeRef(),
		Required:         true,
		Presence:         ir.PresenceExplicit,
		ClientOptional:   true,
		DefaultAdded:     true,
		Visibility:       ir.Visibility{Only: []ir.Lifecycle{ir.LifecycleRead}, None: false},
		Default:          &ir.Value{Kind: ir.ValueString, Str: "default"},
		Constraints:      populatedConstraints(),
		Encoding:         populatedEncoding(),
		Args: []ir.Parameter{
			{Name: ir.Naming{Source: "limit"}, Type: populatedTypeRef(), Required: true},
			{Name: ir.Naming{Source: "offset"}, Type: populatedTypeRef(), Required: false},
		},
		Flatten:      true,
		EventHeader:  true,
		EventPayload: false,
		Secret:       true,
		XML:          populatedXMLHints(),
		Examples: []ir.Example{
			{Name: "ex1", Value: &ir.Value{Kind: ir.ValueString, Str: "v1"}},
			{Name: "ex2", Value: &ir.Value{Kind: ir.ValueString, Str: "v2"}},
		},
		Docs:         populatedDocs(),
		Deprecation:  populatedDeprecation(),
		Availability: populatedAvailability(),
		Unmodeled:    populatedUnmodeled(),
		Provenance:   populatedProvenance(),
	}
}
