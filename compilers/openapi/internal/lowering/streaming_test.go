package lowering_test

import (
	"testing"

	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/compilers/openapi/internal/overlay"
	"github.com/dexpace/morphic/ir"
)

// streamingCtx builds a context carrying nothing but the streaming policy.
func streamingCtx(policy lowering.StreamingMedia) lowering.Ctx {
	return lowering.New(0, &soa.OpenAPI{}, ir.SourceInfo{}, "", lowering.Limits{}, policy, overlay.Origin{})
}

// TestMediaTypeStreams_AnswersFromThePolicy pins every answer the policy gives,
// because each is a different decision: the default list, a caller's list
// replacing it rather than extending it, the off switch, and the normalization
// that makes one media type written two ways match once.
func TestMediaTypeStreams_AnswersFromThePolicy(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		policy    lowering.StreamingMedia
		mediaType string
		want      bool
	}{
		{"default list", lowering.StreamingMedia{}, "text/event-stream", true},
		{"default list, ordinary type", lowering.StreamingMedia{}, "application/json", false},
		{"parameters ignored", lowering.StreamingMedia{}, "text/event-stream; charset=utf-8", true},
		{"case ignored", lowering.StreamingMedia{}, "TEXT/Event-Stream", true},
		{"surrounding space ignored", lowering.StreamingMedia{}, "  text/event-stream  ", true},
		{"disabled", lowering.StreamingMedia{Disabled: true}, "text/event-stream", false},
		{
			"caller's list replaces the default",
			lowering.StreamingMedia{MediaTypes: []string{"application/vnd.acme.frames"}},
			"text/event-stream", false,
		},
		{
			"caller's list is honoured",
			lowering.StreamingMedia{MediaTypes: []string{"Application/VND.acme.frames"}},
			"application/vnd.acme.frames", true,
		},
		{
			"a blank entry names no media type",
			lowering.StreamingMedia{MediaTypes: []string{"  ", "text/event-stream"}},
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
	c := streamingCtx(lowering.StreamingMedia{})
	for _, mediaType := range defaults {
		assert.True(t, c.MediaTypeStreams(mediaType), "default media type %q is not recognized", mediaType)
	}
}
