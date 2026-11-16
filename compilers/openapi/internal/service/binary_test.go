// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package service_test

import (
	"testing"

	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/stretchr/testify/assert"

	"github.com/dexpace/morphic/compilers/openapi/internal/load"
	"github.com/dexpace/morphic/compilers/openapi/internal/service"
)

// binaryCtx builds a context carrying nothing but the binary policy.
func binaryCtx(policy service.BinaryInference) service.Ctx {
	return service.New(0, &load.Document{Doc: &soa.OpenAPI{}}, service.Policy{BinaryInference: policy})
}

// TestMediaTypeBinary_AnswersFromThePolicy pins every answer the policy gives:
// the default list, a type wildcard and what it leaves to a suffix, a caller's
// list replacing the default, the off switch, and the normalization that makes
// one media type written two ways match once. application/octet-stream is not
// the policy's to answer for.
func TestMediaTypeBinary_AnswersFromThePolicy(t *testing.T) {
	t.Parallel()
	acme := service.BinaryInference{MediaTypes: []string{"Application/VND.acme.blob", "+Zip", "model/*"}}
	tests := []struct {
		name      string
		policy    service.BinaryInference
		mediaType string
		want      bool
	}{
		{"default list", service.BinaryInference{}, "application/pdf", true},
		{"default type wildcard", service.BinaryInference{}, "image/png", true},
		{"a wildcard media type", service.BinaryInference{}, "image/*", true},
		{"a vendor subtype under a wildcard", service.BinaryInference{}, "audio/vnd.acme.sound", true},
		{"a suffix the wildcard leaves out", service.BinaryInference{}, "image/svg+xml", false},
		{"parameters, case and space ignored", service.BinaryInference{}, "  Image/PNG; q=1 ", true},
		{"an ordinary type", service.BinaryInference{}, "application/json", false},
		{"a text type", service.BinaryInference{}, "text/plain", false},
		{"octet-stream is no entry", service.BinaryInference{}, "application/octet-stream", false},
		{"no media type", service.BinaryInference{}, "", false},
		{"a type with no subtype", service.BinaryInference{}, "image", false},
		{"disabled", service.BinaryInference{Disabled: true}, "application/pdf", false},
		{"disabled, wildcard", service.BinaryInference{Disabled: true}, "image/png", false},
		{"caller's type is honoured", acme, "application/vnd.acme.blob", true},
		{"caller's suffix is honoured", acme, "application/vnd.acme+zip", true},
		{"caller's wildcard is honoured", acme, "model/gltf-binary", true},
		{"caller's list replaces the default", acme, "application/pdf", false},
		{"caller's list replaces the default wildcard", acme, "image/png", false},
		{"a suffix the caller does not name", acme, "model/gltf+json", false},
		{
			"a blank entry names no media type",
			service.BinaryInference{MediaTypes: []string{"  ", "application/pdf"}},
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
	first := service.DefaultBinaryMediaTypes()
	first[0] = "text/plain"
	assert.NotEqual(t, first[0], service.DefaultBinaryMediaTypes()[0])
	assert.True(t, binaryCtx(service.BinaryInference{}).MediaTypeBinary("application/gzip"))
}
