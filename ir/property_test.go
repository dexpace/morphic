package ir_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
)

// TestVisibility_ZeroValueShape pins Visibility's contract that None carries
// no omitempty: the zero value means "visible in all lifecycles", and None
// marks a distinct fourth state ("visible in none", TypeSpec @invisible) that
// must be observable even when Only is also empty. Collapsing None into
// omitempty would make the zero value and "explicitly invisible with no
// lifecycle list" indistinguishable on the wire.
func TestVisibility_ZeroValueShape(t *testing.T) {
	t.Parallel()
	assertZeroValueShape(t, ir.Visibility{}, `{"none":false}`)
}

// TestVisibility_PopulatedRoundTrip pins that a populated Visibility (a
// non-empty Only list, and separately None set true) round-trips.
func TestVisibility_PopulatedRoundTrip(t *testing.T) {
	t.Parallel()
	tests := map[string]ir.Visibility{
		"only":     {Only: []ir.Lifecycle{ir.LifecycleCreate, ir.LifecycleUpdate}},
		"none":     {None: true},
		"combined": {Only: []ir.Lifecycle{ir.LifecycleRead}, None: false},
	}
	for name, want := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assertRoundTrip(t, want)
		})
	}
}

// TestProperty_ZeroValueShape pins Property's omitempty contract field by
// field, per the test specification's derived lists. The fields that MUST be
// present on the zero value (id, type, required, clientOptional,
// defaultAdded, visibility, flatten, eventHeader, eventPayload, secret, docs,
// provenance, name) carry no omitempty tag; every other field is optional.
// This is the test that would fail the instant an omitempty is added to, say,
// Required — see the Verification section of the test spec for the exact
// regression this guards against.
func TestProperty_ZeroValueShape(t *testing.T) {
	t.Parallel()
	assertZeroValueShape(t, ir.Property{},
		`{"id":"","name":{},"type":{"target":"","nullable":false},"required":false,`+
			`"clientOptional":false,"defaultAdded":false,"visibility":{"none":false},`+
			`"flatten":false,"eventHeader":false,"eventPayload":false,"secret":false,`+
			`"docs":{},"provenance":{"source":0}}`)
}

// TestProperty_PopulatedRoundTrip pins that a fully populated Property — every
// field non-zero, including nested Constraints, Encoding, Args, XML, and
// Availability — survives a JSON round trip.
func TestProperty_PopulatedRoundTrip(t *testing.T) {
	t.Parallel()
	assertRoundTrip(t, populatedProperty())
}

// TestProperty_WireNameByFormatDeterministic pins Class C for Property's one
// map field: WireNameByFormat must marshal with keys in sorted order and
// identically on every run, since Documents are compared and cached as
// serialized bytes (invariant #7).
func TestProperty_WireNameByFormatDeterministic(t *testing.T) {
	t.Parallel()
	prop := ir.Property{
		ID: "p/x",
		WireNameByFormat: map[string]string{
			"z-format": "Z",
			"m-format": "M",
			"a-format": "A",
			"q-format": "Q",
			"b-format": "B",
		},
	}
	got := assertDeterministicMarshal(t, prop)
	wantSubstr := `"wireNameByFormat":{"a-format":"A","b-format":"B","m-format":"M","q-format":"Q","z-format":"Z"}`
	assert.Contains(t, got, wantSubstr, "map keys must serialize in sorted order")
}

// TestProperty_RequiredNullableFourStates is the non-negotiable invariant #8
// test: Property.Required (wire presence) and Type.Nullable (this usage
// admits null) are orthogonal, yielding four distinct, independently
// meaningful states. All four must marshal to distinct JSON, round-trip back
// to an equal value, and carry both "required:" and "nullable:" literally on
// the wire in every case. Adding `,omitempty` to either Property.Required or
// TypeRef.Nullable would collapse two of these four rows onto each other with
// no compile-time or test signal short of this table.
func TestProperty_RequiredNullableFourStates(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		required bool
		nullable bool
	}{
		{name: "must be present, may not be null", required: true, nullable: false},
		{name: "must be present, may be null", required: true, nullable: true},
		{name: "may be absent, may not be null", required: false, nullable: false},
		{name: "may be absent, may be null", required: false, nullable: true},
	}

	// The distinctness check needs every row's encoding in hand, so it is
	// computed synchronously up front, before any parallel subtest runs (a
	// subtest's t.Parallel() pauses it until this function returns, so
	// collecting encodings only inside the subtests would race this
	// assertion against still-empty results).
	encodings := make(map[string]bool, len(tests))
	for _, tt := range tests {
		prop := ir.Property{
			ID:       "p/x",
			Required: tt.required,
			Type:     ir.TypeRef{Target: "t/x", Nullable: tt.nullable},
		}
		raw, err := json.Marshal(prop)
		require.NoError(t, err)
		encodings[string(raw)] = true
	}
	assert.Len(t, encodings, len(tests), "all four required x nullable combinations must marshal to distinct JSON")

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			prop := ir.Property{
				ID:       "p/x",
				Required: tt.required,
				Type:     ir.TypeRef{Target: "t/x", Nullable: tt.nullable},
			}

			raw, err := json.Marshal(prop)
			require.NoError(t, err)
			got := string(raw)
			assert.Contains(t, got, `"required":`, "invariant #8: required must appear literally")
			assert.Contains(t, got, `"nullable":`, "invariant #8: nullable must appear literally")

			var back ir.Property
			require.NoError(t, json.Unmarshal(raw, &back))
			assert.Equal(t, prop, back)
		})
	}
}
