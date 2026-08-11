// This file holds the corpus rows for media-type streaming and the public
// option surface that switches it, which no single source file owns: the policy
// lives on the compiler's options, the media types are read where content is
// lowered, and the result is written onto the operation core.
package openapi_test // external test package — exercises only the public API

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/ir"
)

// eventStreamSpec is one GET whose single 200 response declares mediaType with
// an inline object schema.
func eventStreamSpec(mediaType string) string {
	return `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /events:
    get:
      operationId: getEvents
      responses:
        "200":
          description: stream
          content:
            ` + mediaType + `:
              schema: {type: object, properties: {msg: {type: string}}}
`
}

// compileStreamingSpec compiles src with opts and requires no error diagnostic.
func compileStreamingSpec(t *testing.T, src string, opts openapi.Options) *ir.Document {
	t.Helper()
	doc, diags, err := openapi.New().Compile(t.Context(),
		[]compilers.Source{{Path: "spec.yaml", Data: []byte(src)}},
		compilers.Options{FormatOptions: opts})
	require.NoError(t, err)
	require.NotNil(t, doc)
	assertNoErrorDiags(t, diags)
	return doc
}

// TestStreaming_PolicyDisabledLeavesOnlyWhatIsDeclared pins the off switch
// invariant 6 requires, through the option a caller actually sets. Disabled
// means the media-type reading stops entirely, while a 3.2 itemSchema — a
// declaration, not a guess — still populates the fields it always did.
func TestStreaming_PolicyDisabledLeavesOnlyWhatIsDeclared(t *testing.T) {
	t.Parallel()
	off := openapi.Options{StreamingMedia: openapi.StreamingMedia{Disabled: true}}

	doc := compileStreamingSpec(t, eventStreamSpec("text/event-stream"), off)
	op, ok := opByName(doc, "getEvents")
	require.True(t, ok)
	assert.Empty(t, string(op.Streaming), "the media-type reading is off")
	assert.Nil(t, op.ResponseStream)
	assert.Empty(t, op.Provenance.Inferred)

	declared := `openapi: 3.2.0
info: {title: T, version: "1"}
paths:
  /events:
    get:
      operationId: getEvents
      responses:
        "200":
          description: stream
          content:
            text/event-stream:
              itemSchema: {type: object, properties: {msg: {type: string}}}
`
	doc = compileStreamingSpec(t, declared, off)
	op, ok = opByName(doc, "getEvents")
	require.True(t, ok)
	assert.Equal(t, ir.StreamingServer, op.Streaming, "disabling a heuristic does not disable a declaration")
	assert.NotNil(t, op.ResponseStream)
}

// TestStreaming_PolicyMediaTypesReplaceTheDefaults pins that a stated list is
// the whole list. A caller who names their own spelling gets it and does not
// silently keep the defaults beside it — which is the difference between a
// default and a standard.
func TestStreaming_PolicyMediaTypesReplaceTheDefaults(t *testing.T) {
	t.Parallel()
	own := openapi.Options{StreamingMedia: openapi.StreamingMedia{
		MediaTypes: []string{"application/vnd.acme.frames"},
	}}
	tests := []struct {
		mediaType string
		want      ir.StreamingMode
	}{
		{"application/vnd.acme.frames", ir.StreamingServer},
		{"text/event-stream", ""},
	}
	for _, tc := range tests {
		t.Run(tc.mediaType, func(t *testing.T) {
			t.Parallel()
			doc := compileStreamingSpec(t, eventStreamSpec(tc.mediaType), own)
			op, ok := opByName(doc, "getEvents")
			require.True(t, ok)
			assert.Equal(t, tc.want, op.Streaming)
		})
	}
}

// TestStreaming_DefaultMediaTypesAreTheOnesApplied holds the list a caller can
// start from to the list the compiler actually applies. It is asserted through
// a compile rather than against the policy's own set, because the exported
// accessor and the set the lowering reads are two things a caller has no way to
// tell apart until one of them goes stale.
func TestStreaming_DefaultMediaTypesAreTheOnesApplied(t *testing.T) {
	t.Parallel()
	defaults := openapi.DefaultStreamingMediaTypes()
	require.NotEmpty(t, defaults, "an empty list would make this vacuous")
	for _, mediaType := range defaults {
		doc := compileStreamingSpec(t, eventStreamSpec(mediaType), openapi.Options{})
		op, ok := opByName(doc, "getEvents")
		require.True(t, ok)
		assert.Equal(t, ir.StreamingServer, op.Streaming, "default media type %q does not stream", mediaType)
	}
}

// assertStreamingMedia30 is the corpus row for a 3.0 document that says it
// streams only by naming a media type — the version the IR carried no streaming
// signal for at all (GitHub #250).
func assertStreamingMedia30(t *testing.T, doc *ir.Document, _ []ir.Diagnostic) {
	op, ok := opByName(doc, "streamEvents")
	require.True(t, ok)
	assert.Equal(t, ir.StreamingServer, op.Streaming)
	require.NotNil(t, op.ResponseStream)
	require.NotNil(t, op.ResponseStream.Events)
	content := firstContent(t, op)
	assert.Empty(t, cmp.Diff(content.Type, *op.ResponseStream.Events))
	assert.Equal(t, "text/event-stream", content.MediaType, "the media type itself is still kept as declared")
	assert.Equal(t, "streaming-media-type", op.Provenance.Inferred)
}

// assertStreamingMedia31 is the corpus row for the three shapes 3.0 cannot show
// on one operation: a request body that streams as well as its response, a
// response whose two streaming contents name different element types, where
// none is elected, and one whose two name the same, where it is.
//
// The last two are the pair that says what the refusal is for. Refusing on the
// count alone would pass the middle one and fail the last, and content
// negotiation over a single frame — the same schema as text/event-stream and as
// application/x-ndjson — is the commoner shape of the two.
func assertStreamingMedia31(t *testing.T, doc *ir.Document, diags []ir.Diagnostic) {
	both, ok := opByName(doc, "ingestRows")
	require.True(t, ok)
	assert.Equal(t, ir.StreamingBidi, both.Streaming)
	require.NotNil(t, both.RequestStream)
	require.NotNil(t, both.RequestStream.Events)
	require.NotNil(t, both.Request)
	assert.Empty(t, cmp.Diff(both.Request.Contents[0].Type, *both.RequestStream.Events))
	require.NotNil(t, both.ResponseStream)
	assert.NotNil(t, both.ResponseStream.Events, "a charset parameter does not defeat the match")
	assert.Equal(t, "streaming-media-type", both.Provenance.Inferred)

	either, ok := opByName(doc, "streamEither")
	require.True(t, ok)
	assert.Equal(t, ir.StreamingServer, either.Streaming)
	require.NotNil(t, either.ResponseStream)
	assert.Nil(t, either.ResponseStream.Events, "two streaming contents elect no element type")
	require.NotNil(t, either.Responses[0].Payload)
	assert.Len(t, either.Responses[0].Payload.Contents, 2, "both contents are still kept")
	assert.True(t, openapitest.HasDiag(diags, "openapi/degraded-construct"),
		"the unelected element type is reported; got %+v", diags)

	negotiated, ok := opByName(doc, "streamNegotiated")
	require.True(t, ok)
	assert.Equal(t, ir.StreamingServer, negotiated.Streaming)
	require.NotNil(t, negotiated.ResponseStream)
	require.NotNil(t, negotiated.ResponseStream.Events,
		"two contents naming one element type elect it: there is nothing to choose between")
	assert.Equal(t, ir.TypeID("t/openapi/components/schemas/Frame"),
		negotiated.ResponseStream.Events.Target)
	require.NotNil(t, negotiated.Responses[0].Payload)
	assert.Len(t, negotiated.Responses[0].Payload.Contents, 2,
		"and both media types are still kept, as they are for the unelected case")
}
