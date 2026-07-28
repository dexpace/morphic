package ir_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dexpace/morphic/ir"
)

// TestPreserved_JSONRoundTrip pins that Preserved round-trips through
// assertRoundTrip's byte-level cmp.Diff. RawValue is json.RawMessage, and
// json.Marshal re-emits it verbatim rather than re-compacting it, so this
// only works because both fixture values below are already written compact
// (no incidental whitespace) — a non-compact fixture would round-trip to a
// semantically equal but byte-different value and fail the diff.
func TestPreserved_JSONRoundTrip(t *testing.T) {
	t.Parallel()
	assertRoundTrip(t, ir.Preserved{
		"openapi:x-rate-limit": {
			Reason:     ir.ReasonVendorExtension,
			Value:      ir.RawValue(`{"limit":100}`),
			Provenance: populatedProvenance(),
		},
		"smithy:aws.api#arn": {
			Reason:     ir.ReasonVendorExtension,
			Value:      ir.RawValue(`"arn:aws:s3"`),
			Provenance: populatedProvenance(),
		},
	})
}

func TestPreserved_DeterministicKeyOrder(t *testing.T) {
	t.Parallel()
	p := ir.Preserved{
		"graphql:@key":         {Reason: ir.ReasonVendorExtension, Value: ir.RawValue(`"id"`)},
		"openapi:x-rate-limit": {Reason: ir.ReasonVendorExtension, Value: ir.RawValue(`1`)},
		"erlang:opaque":        {Reason: ir.ReasonDegradedLowering, Value: ir.RawValue(`true`)},
	}
	got := assertDeterministicMarshal(t, p)
	assert.Equal(t,
		`{"erlang:opaque":{"reason":"degraded_lowering","value":true,"provenance":{"source":0}},`+
			`"graphql:@key":{"reason":"vendor_extension","value":"id","provenance":{"source":0}},`+
			`"openapi:x-rate-limit":{"reason":"vendor_extension","value":1,"provenance":{"source":0}}}`,
		got,
	)
}

// TestRawConfig_JSONRoundTrip pins the same contract for RawConfig, which is
// declared protocol configuration rather than an escape hatch and so carries
// no per-entry envelope of its own.
func TestRawConfig_JSONRoundTrip(t *testing.T) {
	t.Parallel()
	assertRoundTrip(t, ir.RawConfig{
		"groupId":  ir.RawValue(`"orders"`),
		"clientId": ir.RawValue(`"sdk"`),
	})
}

func TestRawConfig_DeterministicKeyOrder(t *testing.T) {
	t.Parallel()
	got := assertDeterministicMarshal(t, ir.RawConfig{
		"z": ir.RawValue(`1`),
		"m": ir.RawValue(`2`),
		"a": ir.RawValue(`3`),
	})
	assert.Equal(t, `{"a":3,"m":2,"z":1}`, got)
}
