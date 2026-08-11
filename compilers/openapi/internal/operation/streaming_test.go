package operation_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/ir"
)

// streamingSpec compiles src under a chosen streaming policy. It routes through
// the compiler's public options because the policy is a caller's choice and the
// projection onto the lowering context is what carries it here.
func streamingSpec(t *testing.T, src string, opts openapi.Options) (*ir.Document, []ir.Diagnostic) {
	t.Helper()
	doc, diags, err := openapi.New().Compile(t.Context(), []compilers.Source{openapitest.SourceOf(src)},
		compilers.Options{FormatOptions: opts})
	require.NoError(t, err)
	require.NotNil(t, doc)
	openapitest.RequireNoErrorDiags(t, diags)
	return doc, diags
}

// eventStreamSpec is one GET whose single 200 response declares mediaType with
// an inline object schema.
func eventStreamSpec(version, mediaType string) string {
	return openapitest.PathsSpecVer(version, `  /events:
    get:
      operationId: getEvents
      responses:
        "200":
          description: stream
          content:
            `+mediaType+`:
              schema: {type: object, properties: {msg: {type: string}}}
`)
}

// firstResponseContent returns the operation's single success-response content.
func firstResponseContent(t *testing.T, op ir.Operation) ir.Content {
	t.Helper()
	require.Len(t, op.Responses, 1)
	require.NotNil(t, op.Responses[0].Payload)
	require.NotEmpty(t, op.Responses[0].Payload.Contents)
	return op.Responses[0].Payload.Contents[0]
}

// TestStreaming_ResponseMediaTypeImpliesServerStream is the probe from
// GitHub #250: a response whose only media type is one of the streaming
// spellings carries a streaming signal, at every version rather than only at
// 3.2, and the element type is the schema under the media type — for a frame
// format that schema describes one frame, not the whole body.
func TestStreaming_ResponseMediaTypeImpliesServerStream(t *testing.T) {
	t.Parallel()
	for _, version := range []string{"3.0.3", "3.1.0", "3.2.0"} {
		for _, mediaType := range openapi.DefaultStreamingMediaTypes() {
			t.Run(version+" "+mediaType, func(t *testing.T) {
				t.Parallel()
				doc, _ := streamingSpec(t, eventStreamSpec(version, mediaType), openapi.Options{})
				op := openapitest.FindOp(t, doc, "getEvents")

				assert.Equal(t, ir.StreamingServer, op.Streaming)
				require.NotNil(t, op.ResponseStream, "a streaming media type populates the response direction")
				assert.Nil(t, op.RequestStream, "nothing streams towards the server")

				content := firstResponseContent(t, op)
				require.NotNil(t, op.ResponseStream.Events, "the frame schema is the stream element type")
				assert.Empty(t, cmp.Diff(content.Type, *op.ResponseStream.Events))
				assert.Equal(t, mediaType, content.MediaType, "the media type is still kept as declared")
				assert.Equal(t, "streaming-media-type", op.Provenance.Inferred,
					"classifying a media type is a heuristic and says so")
			})
		}
	}
}

// TestStreaming_MediaTypeParametersDoNotDefeatTheMatch pins that the match is
// against the media type itself: a charset is a parameter of the same type, and
// a document that writes one still declares a stream.
func TestStreaming_MediaTypeParametersDoNotDefeatTheMatch(t *testing.T) {
	t.Parallel()
	doc, _ := streamingSpec(t, eventStreamSpec("3.1.0", "text/event-stream; charset=utf-8"), openapi.Options{})
	op := openapitest.FindOp(t, doc, "getEvents")
	assert.Equal(t, ir.StreamingServer, op.Streaming)
	assert.NotNil(t, op.ResponseStream)
}

// TestStreaming_RequestMediaTypeImpliesClientStream pins the other direction,
// and the two together pin bidi: the summary is derived from both rather than
// from whichever was noticed first.
func TestStreaming_RequestMediaTypeImpliesClientStream(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name         string
		responseType string
		want         ir.StreamingMode
		wantResponse bool
	}{
		{"client only", "application/json", ir.StreamingClient, false},
		{"both directions", "text/event-stream", ir.StreamingBidi, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			spec := openapitest.PathsSpec(`  /ingest:
    post:
      operationId: ingest
      requestBody:
        content:
          application/x-ndjson:
            schema: {type: object, properties: {row: {type: string}}}
      responses:
        "200":
          description: ok
          content:
            ` + tc.responseType + `:
              schema: {type: object, properties: {msg: {type: string}}}
`)
			doc, _ := streamingSpec(t, spec, openapi.Options{})
			op := openapitest.FindOp(t, doc, "ingest")
			assert.Equal(t, tc.want, op.Streaming)
			assert.Equal(t, tc.wantResponse, op.ResponseStream != nil, "response direction")
			require.NotNil(t, op.RequestStream, "the request body's media type streams")
			require.NotNil(t, op.RequestStream.Events)
			require.NotNil(t, op.Request)
			assert.Empty(t, cmp.Diff(op.Request.Contents[0].Type, *op.RequestStream.Events))
		})
	}
}

// TestStreaming_ItemSchemaDeclaresTheStream pins that a 3.2 itemSchema is a
// declaration and the media-type list is a guess, so the two cannot both fire:
// the declared element type is the one Events names, and no heuristic is
// stamped on an operation that declared what it does.
func TestStreaming_ItemSchemaDeclaresTheStream(t *testing.T) {
	t.Parallel()
	spec := openapitest.PathsSpecVer("3.2.0", `  /events:
    get:
      operationId: getEvents
      responses:
        "200":
          description: stream
          content:
            text/event-stream:
              schema: {type: object, properties: {envelope: {type: string}}}
              itemSchema: {type: object, properties: {msg: {type: string}}}
`)
	doc, _ := streamingSpec(t, spec, openapi.Options{})
	op := openapitest.FindOp(t, doc, "getEvents")
	assert.Equal(t, ir.StreamingServer, op.Streaming)
	require.NotNil(t, op.ResponseStream)
	require.NotNil(t, op.ResponseStream.Events)

	content := firstResponseContent(t, op)
	require.NotNil(t, content.Item)
	assert.Empty(t, cmp.Diff(*content.Item, *op.ResponseStream.Events),
		"the declared item schema is the element type, not the media type's own schema")
	assert.Empty(t, op.Provenance.Inferred, "a declared stream is not a heuristic")
}

// TestStreaming_SeveralStreamingContentsLeaveTheElementUnnamed pins the one
// place this classification refuses to answer, in both directions.
// StreamDetail holds one element type per direction while Payload keeps every
// media type, so naming one of two streaming contents would be the
// primary-content selection invariant 2 forbids a compiler to make. The
// direction still streams, and the refusal is reported rather than silent.
func TestStreaming_SeveralStreamingContentsLeaveTheElementUnnamed(t *testing.T) {
	t.Parallel()
	spec := openapitest.PathsSpec(`  /events:
    post:
      operationId: exchange
      requestBody:
        required: true
        content:
          application/x-ndjson:
            schema: {type: object, properties: {a: {type: string}}}
          application/jsonl:
            schema: {type: object, properties: {b: {type: string}}}
      responses:
        "200":
          description: stream
          content:
            text/event-stream:
              schema: {type: object, properties: {c: {type: string}}}
            application/x-ndjson:
              schema: {type: object, properties: {d: {type: string}}}
`)
	doc, diags := streamingSpec(t, spec, openapi.Options{})
	op := openapitest.FindOp(t, doc, "exchange")
	assert.Equal(t, ir.StreamingBidi, op.Streaming)
	require.NotNil(t, op.RequestStream)
	require.NotNil(t, op.ResponseStream)
	assert.Nil(t, op.RequestStream.Events, "two candidate element types, so none is elected")
	assert.Nil(t, op.ResponseStream.Events, "two candidate element types, so none is elected")
	require.NotNil(t, op.Request)
	assert.Len(t, op.Request.Contents, 2, "every content is still kept")

	assert.Equal(t, 2, openapitest.CountDiagsAt(diags, diag.DegradedConstruct, ir.SeverityInfo),
		"one report per direction; got %+v", diags)
	for _, pointer := range []string{"/paths/~1events/post/requestBody", "/paths/~1events/post/responses"} {
		assert.True(t, openapitest.HasDiagCodeAt(diags, diag.DegradedConstruct, pointer),
			"the refusal names the direction it was made in: %s", pointer)
	}
}

// TestStreaming_OrdinaryContentDoesNotStream is the negative half: without a
// streaming media type the operation core keeps its zero values, so every spec
// that does not stream is untouched by this classification.
func TestStreaming_OrdinaryContentDoesNotStream(t *testing.T) {
	t.Parallel()
	doc, _ := streamingSpec(t, eventStreamSpec("3.1.0", "application/json"), openapi.Options{})
	op := openapitest.FindOp(t, doc, "getEvents")
	assert.Empty(t, string(op.Streaming))
	assert.Nil(t, op.RequestStream)
	assert.Nil(t, op.ResponseStream)
	assert.Empty(t, op.Provenance.Inferred)
}

// TestStreaming_ContentOrderDoesNotChooseAnElement is the two-order oracle for
// the one place a choice was available. Compiled with the two streaming media
// types declared either way round, the operation's streaming fields have to
// agree — electing the first candidate would pass a single-order test and
// produce a different element type here.
func TestStreaming_ContentOrderDoesNotChooseAnElement(t *testing.T) {
	t.Parallel()
	spec := func(first, second string) string {
		return openapitest.PathsSpec(`  /events:
    get:
      operationId: getEvents
      responses:
        "200":
          description: stream
          content:
            ` + first + `:
              schema: {type: object, properties: {a: {type: string}}}
            ` + second + `:
              schema: {type: object, properties: {b: {type: string}}}
`)
	}
	forward, _ := streamingSpec(t, spec("text/event-stream", "application/x-ndjson"), openapi.Options{})
	reverse, _ := streamingSpec(t, spec("application/x-ndjson", "text/event-stream"), openapi.Options{})

	first := openapitest.FindOp(t, forward, "getEvents")
	second := openapitest.FindOp(t, reverse, "getEvents")
	assert.Equal(t, first.Streaming, second.Streaming)
	assert.Empty(t, cmp.Diff(first.RequestStream, second.RequestStream))
	assert.Empty(t, cmp.Diff(first.ResponseStream, second.ResponseStream),
		"the element type must not depend on which media type was written first")
}

// TestStreaming_MarkerListsEveryHeuristicThatApplied pins that two heuristics
// reaching one operation both survive. Provenance.Inferred holds one string, so
// the streaming marker overwriting the grouping one — or being dropped by it —
// would leave an audit reading the IR believing only one guess was made.
func TestStreaming_MarkerListsEveryHeuristicThatApplied(t *testing.T) {
	t.Parallel()
	byPrefix := openapi.Options{Grouping: openapi.GroupByPathPrefix}
	doc, _ := streamingSpec(t, eventStreamSpec("3.1.0", "text/event-stream"), byPrefix)
	op := openapitest.FindOp(t, doc, "getEvents")
	assert.Equal(t, "group-path-prefix,streaming-media-type", op.Provenance.Inferred)
}
