// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package stream_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/ir/irverify"
)

const (
	// sentinelKey is the key the default mapping reads a terminator from.
	sentinelKey = "x-speakeasy-sse-sentinel"
	// unpromoted is the code of a key that states nothing usable.
	unpromoted = "openapi/unpromoted-extension"
	// whenKey is the key a test maps to the condition target.
	whenKey = "x-stream-when"
	// contentAt is where a response's content named by media type sits.
	contentAt = "/paths/~1chat/post/responses/200/content/"
)

// jsonBody is the response content that does not stream.
const jsonBody = `            application/json:
              schema: {type: object, properties: {text: {type: string}}}
`

// framed is the response content of a streaming media type whose key, when
// keyLine is not empty, is the line stating it.
func framed(media, keyLine string) string {
	body := "            " + media + ":\n"
	if keyLine != "" {
		body += "              " + keyLine + "\n"
	}
	return body + "              schema: {type: object, properties: {delta: {type: string}}}\n"
}

// sentinel is the line stating text under the default key.
func sentinel(text string) string { return sentinelKey + ": " + quoted(text) }

// quoted is text as a double-quoted YAML scalar.
func quoted(text string) string { return `"` + text + `"` }

// chat is a document of one operation, chat, whose request body has a boolean
// stream member and a string prompt, whose own keys are opKeys, a block of
// lines indented as the operation's, and whose success response holds
// contents.
func chat(opKeys string, contents ...string) string {
	return `openapi: 3.1.0
info: {title: t, version: "1"}
paths:
  /chat:
    post:
      operationId: chat
` + opKeys + `      requestBody:
        content:
          application/json:
            schema: {type: object, properties: {stream: {type: boolean}, prompt: {type: string}}}
      responses:
        "200":
          description: ok
          content:
` + strings.Join(contents, "")
}

// when is the operation's line stating the condition's input.
func when(expression string) string { return "      " + whenKey + ": " + expression + "\n" }

// both maps the terminator to the default key and to x-stream-terminator, and
// the condition to x-stream-when.
func both() openapi.Options {
	return openapi.Options{Promotions: openapi.ExtensionPromotions{
		Targets: map[openapi.ExtensionTarget][]openapi.ExtensionSource{
			openapi.TargetStreamTerminator: {{Key: sentinelKey}, {Key: "x-stream-terminator"}},
			openapi.TargetStreamCondition:  {{Key: whenKey}},
		},
	}}
}

// compile runs the whole compiler over src under opts and returns the chat
// operation, the document and every diagnostic.
func compile(t *testing.T, src string, opts openapi.Options) (ir.Operation, *ir.Document, []ir.Diagnostic) {
	t.Helper()
	doc, diags, err := openapi.New().Compile(t.Context(), []compilers.Source{openapitest.SourceOf(src)},
		compilers.Options{FormatOptions: opts})
	require.NoError(t, err)
	require.NotNil(t, doc)
	openapitest.RequireNoErrorDiags(t, diags)
	return openapitest.FindOp(t, doc, "chat"), doc, diags
}

// findings returns the pointers of the unpromoted-extension reports among
// diags, each required to be info.
func findings(t *testing.T, diags []ir.Diagnostic) []string {
	t.Helper()
	var out []string
	for _, d := range diags {
		if d.Code == unpromoted {
			assert.Equal(t, ir.SeverityInfo, d.Severity)
			out = append(out, string(d.Provenance.Pointer))
		}
	}
	return out
}

// contents returns the contents of op's one success response.
func contents(t *testing.T, op ir.Operation) []ir.Content {
	t.Helper()
	require.Len(t, op.Responses, 1)
	require.NotNil(t, op.Responses[0].Payload)
	return op.Responses[0].Payload.Contents
}

// terminator returns what op's response stream ends on, or nil.
func terminator(op ir.Operation) *string {
	if op.ResponseStream == nil {
		return nil
	}
	return op.ResponseStream.Terminator
}

// markers splits a marker list into its names.
func markers(m ir.Inferred) []string {
	if m == "" {
		return nil
	}
	return strings.Split(string(m), ",")
}

// TestPromote_FillsTheTerminator pins the default reading of a streaming
// content's key: the text is the stream's terminator, recorded on the content
// the key sat on and not on the operation, whose marker for how it streams is
// unchanged, and the key is kept. With promotion off the same document fills
// nothing and keeps every key.
func TestPromote_FillsTheTerminator(t *testing.T) {
	t.Parallel()
	src := chat(when("$request.stream"), jsonBody, framed("text/event-stream", sentinel("[DONE]")))

	op, _, diags := compile(t, src, openapi.Options{})
	require.NotNil(t, terminator(op))
	assert.Equal(t, "[DONE]", *terminator(op))
	assert.Nil(t, op.ResponseStream.Condition, "no key is mapped to the condition by default")
	cs := contents(t, op)
	require.Len(t, cs, 2)
	assert.Empty(t, cs[0].Inferred)
	assert.Equal(t, ir.Inferred("extension-promotion"), cs[1].Inferred)
	require.NotNil(t, cs[1].Stream)
	assert.Equal(t, ir.Inferred("streaming-inference"), cs[1].Stream.Inferred, "how it streams is unchanged")
	assert.Equal(t, ir.Inferred("streaming-inference"), op.Provenance.Inferred, "the operation read no key")
	assert.Contains(t, cs[1].Unmodeled, "openapi:"+sentinelKey)
	assert.Empty(t, diags)

	off, _, offDiags := compile(t, src, openapi.Options{Promotions: openapi.ExtensionPromotions{Disabled: true}})
	assert.Nil(t, terminator(off), "promotion off fills no terminator")
	offContents := contents(t, off)
	assert.Empty(t, offContents[1].Inferred)
	assert.Contains(t, offContents[1].Unmodeled, "openapi:"+sentinelKey, "the key stays as written")
	assert.Contains(t, off.Unmodeled, "openapi:"+whenKey)
	assert.Equal(t, ir.Inferred("streaming-inference"), off.Provenance.Inferred)
	assert.Empty(t, offDiags)
}

// TestPromote_FillsTheTerminatorOfEveryStreamingContent pins that contents
// stating the one text agree on it, and that the operation's own key, when it
// states one, is the terminator and is recorded on the operation.
func TestPromote_FillsTheTerminatorOfEveryStreamingContent(t *testing.T) {
	t.Parallel()
	op, _, diags := compile(t, chat("", jsonBody, framed("text/event-stream", sentinel("[DONE]")),
		framed("application/x-ndjson", sentinel("[DONE]"))), openapi.Options{})
	require.NotNil(t, terminator(op))
	assert.Equal(t, "[DONE]", *terminator(op))
	cs := contents(t, op)
	assert.Empty(t, cs[0].Inferred)
	assert.Equal(t, ir.Inferred("extension-promotion"), cs[1].Inferred)
	assert.Equal(t, ir.Inferred("extension-promotion"), cs[2].Inferred)
	assert.Empty(t, findings(t, diags))

	op, _, diags = compile(t, chat("      x-stream-terminator: \"[DONE]\"\n", jsonBody, framed("text/event-stream", "")), both())
	require.NotNil(t, terminator(op))
	assert.Equal(t, "[DONE]", *terminator(op))
	assert.Contains(t, markers(op.Provenance.Inferred), "extension-promotion")
	assert.Empty(t, contents(t, op)[1].Inferred, "the key sat on the operation")
	assert.Empty(t, diags)
}

// TestPromote_TerminatorIsAllOrNone pins that a terminator is filled only
// where every key stating one states the same text and the document has a
// stream to end: any other state fills nothing and is reported once, at the
// key that disagrees or states nothing, the later by pointer where two do, in
// whichever order the contents are declared.
func TestPromote_TerminatorIsAllOrNone(t *testing.T) {
	t.Parallel()
	sse, ndjson := contentAt+"text~1event-stream/"+sentinelKey, contentAt+"application~1x-ndjson/"+sentinelKey
	later := slices.Max([]string{sse, ndjson})
	for _, tc := range []struct {
		name string
		src  string
		opts openapi.Options
		want []string
	}{
		{"a streaming content stating none beside one that does",
			chat("", jsonBody, framed("text/event-stream", sentinel("[DONE]")), framed("application/x-ndjson", "")),
			openapi.Options{}, []string{sse}},
		{"two streaming contents stating two texts",
			chat("", jsonBody, framed("text/event-stream", sentinel("[DONE]")), framed("application/x-ndjson", sentinel("[END]"))),
			openapi.Options{}, []string{later}},
		{"the same two, declared in the other order",
			chat("", jsonBody, framed("application/x-ndjson", sentinel("[END]")), framed("text/event-stream", sentinel("[DONE]"))),
			openapi.Options{}, []string{later}},
		{"an empty text", chat("", jsonBody, framed("text/event-stream", sentinel(""))), openapi.Options{}, []string{sse}},
		{"a key on an operation with no streaming content",
			chat("      x-stream-terminator: \"[DONE]\"\n", jsonBody), both(),
			[]string{"/paths/~1chat/post/x-stream-terminator"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			op, doc, diags := compile(t, tc.src, tc.opts)
			assert.Nil(t, terminator(op), "a terminator is filled all or none")
			assert.Equal(t, tc.want, findings(t, diags))
			assert.Empty(t, irverify.Verify(doc))
			for _, c := range contents(t, op) {
				assert.Empty(t, c.Inferred, "a content records only a key it filled")
			}
		})
	}
}

// TestPromote_OperationAndContentsMustAgree pins that an operation stating a
// terminator is the terminator, but a streaming content stating another is
// reported and nothing is filled, while one stating the same fills it and one
// stating none is passed over.
func TestPromote_OperationAndContentsMustAgree(t *testing.T) {
	t.Parallel()
	op, _, diags := compile(t, chat("      x-stream-terminator: \"[DONE]\"\n", jsonBody,
		framed("text/event-stream", sentinel("[END]"))), both())
	assert.Nil(t, terminator(op))
	require.Len(t, findings(t, diags), 1)

	for _, key := range []string{sentinel("[DONE]"), ""} {
		op, _, diags = compile(t, chat("      x-stream-terminator: \"[DONE]\"\n", jsonBody,
			framed("text/event-stream", key)), both())
		require.NotNil(t, terminator(op), "content key %q", key)
		assert.Equal(t, "[DONE]", *terminator(op))
		assert.Empty(t, findings(t, diags))
	}
}

// TestPromote_FillsTheCondition pins that the key mapped to the condition,
// naming an input of the request, fills the condition where that input is a
// boolean and the success response holds a content that streams beside one
// that does not, recorded on the operation. An input of another type, an
// input the request does not have and a response with no content to choose
// between fill nothing and are reported at the key.
func TestPromote_FillsTheCondition(t *testing.T) {
	t.Parallel()
	const stream = "p/openapi/paths/~1chat/post/requestBody/content/application~1json/schema/properties/stream"
	src := chat(when("$request.stream"), jsonBody, framed("text/event-stream", sentinel("[DONE]")))

	op, _, diags := compile(t, src, both())
	require.NotNil(t, op.ResponseStream)
	require.NotNil(t, op.ResponseStream.Condition)
	cond := op.ResponseStream.Condition
	assert.Nil(t, cond.Input.Param, "the input is a member of the request body")
	assert.Equal(t, []ir.PropID{stream}, cond.Input.Segments)
	assert.Equal(t, ir.ValueBool, cond.Value.Kind)
	assert.True(t, cond.Value.Bool, "the stream is asked for when the input is true")
	assert.ElementsMatch(t, []string{"streaming-inference", "extension-promotion"}, markers(op.Provenance.Inferred))
	assert.Contains(t, op.Unmodeled, "openapi:"+whenKey)
	assert.Empty(t, diags)

	at := "/paths/~1chat/post/" + whenKey
	for _, tc := range []struct {
		name string
		src  string
	}{
		{"a string input", chat(when("$request.prompt"), jsonBody, framed("text/event-stream", sentinel("[DONE]")))},
		{"an input the request lacks", chat(when("$request.nothing"), jsonBody, framed("text/event-stream", ""))},
		{"nothing to choose between", chat(when("$request.stream"), framed("text/event-stream", sentinel("[DONE]")))},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			op, doc, diags := compile(t, tc.src, both())
			if op.ResponseStream != nil {
				assert.Nil(t, op.ResponseStream.Condition)
			}
			assert.Equal(t, []string{at}, findings(t, diags))
			assert.NotContains(t, markers(op.Provenance.Inferred), "extension-promotion")
			assert.Empty(t, irverify.Verify(doc))
		})
	}

	off, _, _ := compile(t, src, openapi.Options{Promotions: openapi.ExtensionPromotions{Disabled: true}})
	require.NotNil(t, off.ResponseStream)
	assert.Nil(t, off.ResponseStream.Condition, "promotion off fills no condition")
	assert.Contains(t, off.Unmodeled, "openapi:"+whenKey, "the key stays as written")
}
