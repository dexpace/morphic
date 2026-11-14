// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package lowering_test

import (
	"testing"

	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/load"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
)

// streamingCtx builds a context carrying nothing but the streaming policy.
func streamingCtx(policy lowering.StreamingInference) lowering.Ctx {
	return lowering.New(0, &load.Document{Doc: &soa.OpenAPI{}}, lowering.Policy{Streaming: policy})
}

// TestMediaTypeStreams_AnswersFromThePolicy pins every answer the policy gives,
// because each is a different decision: the default list, a caller's list
// replacing it rather than extending it, the off switch, and the normalization
// that makes one media type written two ways match once.
func TestMediaTypeStreams_AnswersFromThePolicy(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		policy    lowering.StreamingInference
		mediaType string
		want      bool
	}{
		{"default list", lowering.StreamingInference{}, "text/event-stream", true},
		{"default list, ordinary type", lowering.StreamingInference{}, "application/json", false},
		{"parameters ignored", lowering.StreamingInference{}, "text/event-stream; charset=utf-8", true},
		{"case ignored", lowering.StreamingInference{}, "TEXT/Event-Stream", true},
		{"surrounding space ignored", lowering.StreamingInference{}, "  text/event-stream  ", true},
		{"default list, json-seq", lowering.StreamingInference{}, "application/json-seq", true},
		{"default suffix", lowering.StreamingInference{}, "application/geo+json-seq", true},
		{"default suffix, parameters and case ignored", lowering.StreamingInference{}, "Application/GEO+JSON-SEQ; charset=utf-8", true},
		{"the last suffix decides", lowering.StreamingInference{}, "application/a+json-seq+zip", false},
		{"and an earlier one does not hide it", lowering.StreamingInference{}, "application/a+b+json-seq", true},
		{"a suffix is not a substring", lowering.StreamingInference{}, "application/x+json-sequence", false},
		{"another suffix", lowering.StreamingInference{}, "application/problem+json", false},
		{"disabled", lowering.StreamingInference{Disabled: true}, "text/event-stream", false},
		{"disabled, suffix", lowering.StreamingInference{Disabled: true}, "application/geo+json-seq", false},
		{
			"caller's suffix is honoured",
			lowering.StreamingInference{MediaTypes: []string{"+Frames"}},
			"application/vnd.acme+frames", true,
		},
		{
			"caller's list replaces the default suffix",
			lowering.StreamingInference{MediaTypes: []string{"+frames"}},
			"application/geo+json-seq", false,
		},
		{
			"caller's list replaces the default",
			lowering.StreamingInference{MediaTypes: []string{"application/vnd.acme.frames"}},
			"text/event-stream", false,
		},
		{
			"caller's list is honoured",
			lowering.StreamingInference{MediaTypes: []string{"Application/VND.acme.frames"}},
			"application/vnd.acme.frames", true,
		},
		{
			"a blank entry names no media type",
			lowering.StreamingInference{MediaTypes: []string{"  ", "text/event-stream"}},
			"", false,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, streamingCtx(tc.policy).MediaTypeStreams(tc.mediaType))
		})
	}
}

// TestDefaultStreamingMediaTypes_AreAllRecognized holds the exported default
// list to the set the policy actually applies. Two transcriptions of one set is
// one of them going stale unnoticed, and the exported one is what a caller
// extending the list starts from.
func TestDefaultStreamingMediaTypes_AreAllRecognized(t *testing.T) {
	t.Parallel()
	defaults := lowering.DefaultStreamingMediaTypes()
	require.NotEmpty(t, defaults, "an empty default list would make every case below vacuous")
	c := streamingCtx(lowering.StreamingInference{})
	for _, mediaType := range defaults {
		assert.True(t, c.MediaTypeStreams(mediaType), "default media type %q is not recognized", mediaType)
	}
}
