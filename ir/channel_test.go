package ir_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dexpace/morphic/ir"
)

// TestChannel_ZeroValueShape pins Channel's omitempty contract: Name, Docs,
// and Provenance carry no omitempty, since every channel has a naming, a
// possibly-empty docs object, and provenance, while Address is a nilable
// pointer (nil = unknown/runtime-assigned address) and every other field is
// optional.
func TestChannel_ZeroValueShape(t *testing.T) {
	t.Parallel()
	assertZeroValueShape(t, ir.Channel{}, `{"name":{},"docs":{},"provenance":{"source":0}}`)
}

// TestChannel_PopulatedRoundTrip pins that a fully populated Channel — a
// concrete address, params, messages, server indices, protocol bindings —
// round-trips.
func TestChannel_PopulatedRoundTrip(t *testing.T) {
	t.Parallel()
	addr := "users/{userId}/signedup"
	want := ir.Channel{
		ID:      "c/asyncapi/user-signup",
		Name:    populatedNaming(),
		Address: &addr,
		Docs:    populatedDocs(),
		Tags:    []string{"users", "events"},
		Params: []ir.Parameter{
			{Name: ir.Naming{Source: "userId"}, Type: populatedTypeRef(), Required: true},
			{Name: ir.Naming{Source: "region"}, Type: populatedTypeRef(), Required: false},
		},
		Messages: []ir.MessageID{"m/UserSignedUp", "m/UserSignupFailed"},
		Servers:  []int{0, 2},
		Bindings: map[string]ir.Extensions{
			"kafka": {"partitions": ir.RawValue(`3`)},
			"amqp":  {"exchange": ir.RawValue(`"events"`)},
		},
		Extensions: populatedExtensions(),
		Provenance: populatedProvenance(),
	}
	assertRoundTrip(t, want)
}

// TestChannel_BindingsDeterministic pins Class C for Channel's map field:
// Bindings must marshal with keys in sorted order on every run.
func TestChannel_BindingsDeterministic(t *testing.T) {
	t.Parallel()
	ch := ir.Channel{
		Bindings: map[string]ir.Extensions{
			"z-proto": {"k": ir.RawValue(`1`)},
			"m-proto": {"k": ir.RawValue(`2`)},
			"a-proto": {"k": ir.RawValue(`3`)},
			"q-proto": {"k": ir.RawValue(`4`)},
			"b-proto": {"k": ir.RawValue(`5`)},
		},
	}
	got := assertDeterministicMarshal(t, ch)
	assert.Contains(t, got,
		`"bindings":{"a-proto":{"k":3},"b-proto":{"k":5},"m-proto":{"k":2},"q-proto":{"k":4},"z-proto":{"k":1}}`)
}

// TestMessage_ZeroValueShape pins Message's omitempty contract: Name,
// Payload, Docs, and Provenance carry no omitempty, matching Channel's
// pattern for every-entity-has-one fields; Headers, CorrelationID, and the
// rest stay optional.
func TestMessage_ZeroValueShape(t *testing.T) {
	t.Parallel()
	assertZeroValueShape(t, ir.Message{},
		`{"name":{},"payload":{},"docs":{},"provenance":{"source":0}}`)
}

// TestMessage_PopulatedRoundTrip pins that a fully populated Message — headers
// type, correlation-id path, examples, and bindings — round-trips.
func TestMessage_PopulatedRoundTrip(t *testing.T) {
	t.Parallel()
	headers := populatedTypeRef()
	want := ir.Message{
		ID:      "m/asyncapi/UserSignedUp",
		Name:    populatedNaming(),
		Payload: ir.Payload{Contents: []ir.Content{{MediaType: "application/json", Type: populatedTypeRef()}}},
		Headers: &headers,
		CorrelationID: &ir.PropPath{
			In:       "header",
			Segments: []ir.PropID{"p/correlationId"},
		},
		ContentType: "application/json",
		Tags:        []string{"users", "events"},
		Docs:        populatedDocs(),
		Deprecation: populatedDeprecation(),
		Examples: []ir.Example{
			{Name: "ex1", Value: &ir.Value{Kind: ir.ValueString, Str: "v1"}, Headers: &ir.Value{Kind: ir.ValueString, Str: "h1"}},
			{Name: "ex2", Value: &ir.Value{Kind: ir.ValueString, Str: "v2"}},
		},
		Bindings: map[string]ir.Extensions{
			"kafka": {"key": ir.RawValue(`"userId"`)},
		},
		Extensions: populatedExtensions(),
		Provenance: populatedProvenance(),
	}
	assertRoundTrip(t, want)
}
