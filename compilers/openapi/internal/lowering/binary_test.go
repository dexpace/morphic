// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package lowering_test

import (
	"testing"

	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/stretchr/testify/assert"

	"github.com/dexpace/morphic/compilers/openapi/internal/load"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
)

// binaryCtx builds a context carrying nothing but the binary policy.
func binaryCtx(policy lowering.BinaryMedia) lowering.Ctx {
	return lowering.New(0, &load.Document{Doc: &soa.OpenAPI{}}, lowering.Policy{Binary: policy})
}

// TestMediaTypeBinary_AnswersFromThePolicy pins every answer the policy gives:
// the default list, a type wildcard and what it leaves to a suffix, a caller's
// list replacing the default, the off switch, and the normalization that makes
// one media type written two ways match once. application/octet-stream is not
// the policy's to answer for.
func TestMediaTypeBinary_AnswersFromThePolicy(t *testing.T) {
	t.Parallel()
	acme := lowering.BinaryMedia{MediaTypes: []string{"Application/VND.acme.blob", "+Zip", "model/*"}}
	tests := []struct {
		name      string
		policy    lowering.BinaryMedia
		mediaType string
		want      bool
	}{
		{"default list", lowering.BinaryMedia{}, "application/pdf", true},
		{"default type wildcard", lowering.BinaryMedia{}, "image/png", true},
		{"a wildcard media type", lowering.BinaryMedia{}, "image/*", true},
		{"a vendor subtype under a wildcard", lowering.BinaryMedia{}, "audio/vnd.acme.sound", true},
		{"a suffix the wildcard leaves out", lowering.BinaryMedia{}, "image/svg+xml", false},
		{"parameters, case and space ignored", lowering.BinaryMedia{}, "  Image/PNG; q=1 ", true},
		{"an ordinary type", lowering.BinaryMedia{}, "application/json", false},
		{"a text type", lowering.BinaryMedia{}, "text/plain", false},
		{"octet-stream is no entry", lowering.BinaryMedia{}, "application/octet-stream", false},
		{"no media type", lowering.BinaryMedia{}, "", false},
		{"a type with no subtype", lowering.BinaryMedia{}, "image", false},
		{"disabled", lowering.BinaryMedia{Disabled: true}, "application/pdf", false},
		{"disabled, wildcard", lowering.BinaryMedia{Disabled: true}, "image/png", false},
		{"caller's type is honoured", acme, "application/vnd.acme.blob", true},
		{"caller's suffix is honoured", acme, "application/vnd.acme+zip", true},
		{"caller's wildcard is honoured", acme, "model/gltf-binary", true},
		{"caller's list replaces the default", acme, "application/pdf", false},
		{"caller's list replaces the default wildcard", acme, "image/png", false},
		{"a suffix the caller does not name", acme, "model/gltf+json", false},
		{
			"a blank entry names no media type",
			lowering.BinaryMedia{MediaTypes: []string{"  ", "application/pdf"}},
			"", false,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, binaryCtx(tc.policy).MediaTypeBinary(tc.mediaType))
		})
	}
}

// TestDefaultBinaryMediaTypes_ReturnsAFreshList holds the default to a copy per
// call, so a caller extending it cannot change what the next policy reads.
func TestDefaultBinaryMediaTypes_ReturnsAFreshList(t *testing.T) {
	t.Parallel()
	first := lowering.DefaultBinaryMediaTypes()
	first[0] = "text/plain"
	assert.NotEqual(t, first[0], lowering.DefaultBinaryMediaTypes()[0])
	assert.True(t, binaryCtx(lowering.BinaryMedia{}).MediaTypeBinary("application/gzip"))
}
