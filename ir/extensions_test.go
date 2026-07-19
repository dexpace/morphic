package ir_test

import (
	"encoding/json"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
)

func TestExtensions_JSONRoundTrip(t *testing.T) {
	t.Parallel()
	ext := ir.Extensions{
		"openapi:x-rate-limit": ir.RawValue(`{"limit":100}`),
		"smithy:aws.api#arn":   ir.RawValue(`"arn:aws:s3"`),
	}

	raw, err := json.Marshal(ext)
	require.NoError(t, err)

	var back ir.Extensions
	require.NoError(t, json.Unmarshal(raw, &back))

	assert.JSONEq(t, string(ext["openapi:x-rate-limit"]), string(back["openapi:x-rate-limit"]))
	assert.JSONEq(t, string(ext["smithy:aws.api#arn"]), string(back["smithy:aws.api#arn"]))
	assert.ElementsMatch(t, keysOf(ext), keysOf(back))
}

func TestExtensions_DeterministicKeyOrder(t *testing.T) {
	t.Parallel()
	ext := ir.Extensions{
		"graphql:@key":         ir.RawValue(`"id"`),
		"openapi:x-rate-limit": ir.RawValue(`1`),
		"erlang:opaque":        ir.RawValue(`true`),
	}

	first, err := json.Marshal(ext)
	require.NoError(t, err)
	second, err := json.Marshal(ext)
	require.NoError(t, err)

	assert.Empty(t, cmp.Diff(string(first), string(second)))
	assert.Equal(t,
		`{"erlang:opaque":true,"graphql:@key":"id","openapi:x-rate-limit":1}`,
		string(first),
	)
}

func keysOf(ext ir.Extensions) []string {
	out := make([]string, 0, len(ext))
	for k := range ext {
		out = append(out, k)
	}
	return out
}
