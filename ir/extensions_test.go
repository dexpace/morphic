package ir_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dexpace/morphic/ir"
)

// TestExtensions_JSONRoundTrip pins that Extensions round-trips through
// assertRoundTrip's byte-level cmp.Diff. RawValue is json.RawMessage, and
// json.Marshal re-emits it verbatim rather than re-compacting it, so this
// only works because both fixture values below are already written compact
// (no incidental whitespace) — a non-compact fixture would round-trip to a
// semantically equal but byte-different value and fail the diff.
func TestExtensions_JSONRoundTrip(t *testing.T) {
	t.Parallel()
	assertRoundTrip(t, ir.Extensions{
		"openapi:x-rate-limit": ir.RawValue(`{"limit":100}`),
		"smithy:aws.api#arn":   ir.RawValue(`"arn:aws:s3"`),
	})
}

func TestExtensions_DeterministicKeyOrder(t *testing.T) {
	t.Parallel()
	ext := ir.Extensions{
		"graphql:@key":         ir.RawValue(`"id"`),
		"openapi:x-rate-limit": ir.RawValue(`1`),
		"erlang:opaque":        ir.RawValue(`true`),
	}
	got := assertDeterministicMarshal(t, ext)
	assert.Equal(t,
		`{"erlang:opaque":true,"graphql:@key":"id","openapi:x-rate-limit":1}`,
		got,
	)
}
