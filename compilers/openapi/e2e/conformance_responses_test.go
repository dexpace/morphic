// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package e2e_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/ir"
)

func assertPerStatusErrors(t *testing.T, doc *ir.Document, _ []ir.Diagnostic) {
	op, ok := opByName(doc, "getWidgets")
	require.True(t, ok)
	require.Len(t, op.Responses, 1, "the 2xx success response")
	faults := map[ir.StatusRange]string{}
	byRange := map[ir.StatusRange]ir.ErrorCase{}
	for _, ec := range op.Errors {
		require.Len(t, ec.Conditions.StatusCodes, 1)
		rng := ec.Conditions.StatusCodes[0]
		byRange[rng] = ec
		faults[rng] = ec.Fault
	}
	assert.Equal(t, map[ir.StatusRange]string{
		{From: 404, To: 404}: "client",
		{From: 429, To: 429}: "client",
		{From: 500, To: 599}: "server",
	}, faults, "each range classified from its own status")
	require.NotNil(t, op.Default, "the default is classed neither way, so it is no error case")
	assert.Equal(t, []ir.StatusRange{{}}, op.Default.Conditions.StatusCodes, "the catch-all alone")
	assert.Equal(t, "default", op.Default.Name.Hint)

	assertErrorCaseIsAResponse(t, byRange)
}

// assertErrorCaseIsAResponse holds the three fields ir.ErrorCase gained in
// GitHub #422 to the claim ir.Response already carried: every status spelling,
// every header and every media type survives, whatever the status class.
//
// The 5XX case doubles as the control: an error response declaring no headers
// and no content gets neither, so what the other two carry marks a declaration
// rather than appearing on every error case.
func assertErrorCaseIsAResponse(t *testing.T, byRange map[ir.StatusRange]ir.ErrorCase) {
	t.Helper()
	hints := map[ir.StatusRange]string{}
	for rng, ec := range byRange {
		hints[rng] = ec.Name.Hint
	}
	assert.Equal(t, map[ir.StatusRange]string{
		{From: 404, To: 404}: "404",
		{From: 429, To: 429}: "429",
		{From: 500, To: 599}: "5_xx",
	}, hints, "the key as declared, then neutralized")

	notFound, ok := byRange[ir.StatusRange{From: 404, To: 404}]
	require.True(t, ok)
	require.NotNil(t, notFound.Payload)
	require.Len(t, notFound.Payload.Contents, 1)
	assert.Equal(t, "application/json", notFound.Payload.Contents[0].MediaType,
		"a single-media error keeps the key it was written under")

	throttled, ok := byRange[ir.StatusRange{From: 429, To: 429}]
	require.True(t, ok)
	require.NotNil(t, throttled.Payload)
	require.Len(t, throttled.Payload.Contents, 2, "every media type is kept, none elected")
	assert.Equal(t, "t/openapi/components/schemas/Problem",
		string(throttled.Payload.Contents[1].Type.Target),
		"the second media type keeps its own schema rather than the first's")
	wire := make([]string, 0, len(throttled.Headers))
	for _, h := range throttled.Headers {
		wire = append(wire, h.WireName)
	}
	assert.Equal(t, []string{"Retry-After", "X-RateLimit-Remaining"}, wire,
		"the headers that only ever appear on an error status are structural")

	serverErr, ok := byRange[ir.StatusRange{From: 500, To: 599}]
	require.True(t, ok)
	assert.Nil(t, serverErr.Payload, "an error response declaring no content gets no payload")
	assert.Empty(t, serverErr.Headers, "an error response declaring no headers gets none")
	for rng, ec := range byRange {
		assert.NotContains(t, ec.Unmodeled, "openapi:content", "%v keeps no content map", rng)
		assert.NotContains(t, ec.Unmodeled, "openapi:headers", "%v keeps no headers map", rng)
	}
}

// assertDefaultResponse pins that a `default` response lowers to the operation's
// Default, whole, in the three shapes it takes: the operation's only response,
// the fallback beside a success, and a bare one with no body. In none is the
// catch-all among Errors, since `default` answers successes and failures alike
// and the compiler classes it as neither (docs/ir/responses.md). The control is
// the 200 beside the default, which stays a Response.
func assertDefaultResponse(t *testing.T, doc *ir.Document, _ []ir.Diagnostic) {
	catchAll := []ir.StatusRange{{From: 0, To: 0}}
	byOp := map[string]ir.Operation{}
	for _, name := range []string{"purgeCache", "getItem", "ping"} {
		op, ok := opByName(doc, name)
		require.True(t, ok, "operation %s", name)
		require.NotNil(t, op.Default, "%s: the default is kept as Default", name)
		assert.Empty(t, op.Errors, "%s: the catch-all is no error case", name)
		assert.Equal(t, catchAll, op.Default.Conditions.StatusCodes, "%s: the catch-all alone", name)
		assert.Equal(t, "default", op.Default.Name.Hint, "%s: the key as written", name)
		byOp[name] = op
	}

	purge := byOp["purgeCache"]
	assert.Empty(t, purge.Responses, "a lone default is the operation's whole description of the outcome")
	require.NotNil(t, purge.Default.Payload)
	require.Len(t, purge.Default.Payload.Contents, 1)
	assert.Equal(t, "application/json", purge.Default.Payload.Contents[0].MediaType)
	assert.Equal(t, "the cache was purged, or it was not", purge.Default.Docs.Description)

	item := byOp["getItem"]
	require.Len(t, item.Responses, 1, "the 200 stays a Response beside the default")
	assert.Equal(t, []ir.StatusRange{{From: 200, To: 200}}, item.Responses[0].Conditions.StatusCodes)
	require.NotNil(t, item.Default.Payload)
	require.Len(t, item.Default.Payload.Contents, 1)
	assert.Equal(t, componentID("Problem"), item.Default.Payload.Contents[0].Type.Target,
		"the default keeps its own body, apart from the success one")
	require.NotNil(t, item.Responses[0].Payload)
	assert.Equal(t, componentID("Item"), item.Responses[0].Payload.Contents[0].Type.Target)

	ping := byOp["ping"]
	assert.Empty(t, ping.Responses)
	assert.Nil(t, ping.Default.Payload, "a default declaring no content gets no payload")
	assert.Equal(t, "pong", ping.Default.Docs.Description)
}

// assertResponseSummary32 pins GitHub #615's Response.summary: the 3.2 field
// reaches Docs.Summary on both status classes, because it is read in the one
// shared lowering both use, and the document draws no unknown-key warning for a
// key its dialect defines.
func assertResponseSummary32(t *testing.T, doc *ir.Document, diags []ir.Diagnostic) {
	op, ok := opByName(doc, "deleteItem")
	require.True(t, ok)
	require.Len(t, op.Responses, 1)
	assert.Equal(t, "The item was deleted", op.Responses[0].Docs.Summary)
	assert.Equal(t, "The long form of the same fact.", op.Responses[0].Docs.Description)

	require.Len(t, op.Errors, 1)
	assert.Equal(t, "No such item", op.Errors[0].Docs.Summary,
		"an error case is lowered by the same function, so it reads the field too")
	assert.Empty(t, diagsAt(diags, diag.UnknownObjectKey, "/paths/~1items~1{id}/delete/responses/204/summary"),
		"a key 3.2 defines is not an undefined key")
}

// assertHTTPDateInference pins the HTTP-date policy on its case: the
// Last-Modified and Expires response headers, declared date-time inline and
// through a $ref, and the If-Unmodified-Since header parameter take the
// http-date encoding, marked and each reported once, the parameter's on its own
// Encoding and its type unchanged; a Date declared as text and another
// date-time field keep what they declare, and so does a parameter that is
// not a header. The policy switched off fills none of them.
func assertHTTPDateInference(t *testing.T, doc *ir.Document, diags []ir.Diagnostic) {
	op, ok := opByName(doc, "putFile")
	require.True(t, ok)
	require.Len(t, op.Responses, 1)
	headers := op.Responses[0].Headers
	for wire, encoded := range map[string]bool{"Last-Modified": true, "Expires": true, "Date": false, "X-Modified": false} {
		h, ok := headerByWire(headers, wire)
		require.True(t, ok, wire)
		if !encoded {
			assert.Nil(t, h.Encoding, wire)
			assert.Empty(t, h.Provenance.Inferred, wire)
			continue
		}
		assert.Equal(t, &ir.Encoding{Name: "http-date"}, h.Encoding, wire)
		assert.Equal(t, openapi.HTTPDateInferenceHeuristic, h.Provenance.Inferred, wire)
		assert.Equal(t, 1, countCode(diags, diag.InferredHTTPDate,
			"/paths/~1files~1{name}/put/responses/204/headers/"+wire), wire)
	}
	// A header whose format names its text carries that encoding on its type,
	// so the document has spoken for it (docs/compilers/openapi.md#string-formats).
	for wire, encoding := range map[string]string{"Sunset": "http-date", "Retry-After": "unixTimestamp"} {
		h, ok := headerByWire(headers, wire)
		require.True(t, ok, wire)
		assert.Nil(t, h.Encoding, wire)
		assert.Empty(t, h.Provenance.Inferred, wire)
		sc, ok := doc.Types[h.Type.Target].(*ir.Scalar)
		require.True(t, ok, "%s: the format hoists a Scalar", wire)
		require.NotNil(t, sc.Encoding, wire)
		assert.Equal(t, encoding, sc.Encoding.Name, wire)
	}
	require.Len(t, op.Params, 2)
	assert.Nil(t, op.Params[0].Encoding, "a path parameter is never read")
	assert.Empty(t, op.Params[0].Provenance.Inferred)
	since := op.Params[1]
	assert.Equal(t, "If-Unmodified-Since", since.Name.Source)
	assert.Equal(t, &ir.Encoding{Name: "http-date"}, since.Encoding, "the parameter's own text")
	assert.Equal(t, ir.PrimTypeID(ir.PrimDatetimeOffset), since.Type.Target, "its type is still the instant")
	assert.Equal(t, openapi.HTTPDateInferenceHeuristic, since.Provenance.Inferred)
	assert.Equal(t, 1, countCode(diags, diag.InferredHTTPDate, "/paths/~1files~1{name}/put/parameters/1"))
	reported := 0
	for _, d := range diags {
		if d.Code == diag.InferredHTTPDate {
			reported++
		}
	}
	assert.Equal(t, 3, reported, "nothing else is reported")

	off, offDiags := corpusWith(t, "http-date-inference",
		openapi.Options{HTTPDateInference: openapi.HTTPDateInference{Disabled: true}})
	offOp, ok := opByName(off, "putFile")
	require.True(t, ok)
	require.Len(t, offOp.Params, 2)
	assert.Nil(t, offOp.Params[1].Encoding, "the policy off leaves the parameter as declared")
	assert.Empty(t, offOp.Params[1].Provenance.Inferred)
	assert.Equal(t, ir.PrimTypeID(ir.PrimDatetimeOffset), offOp.Params[1].Type.Target)
	assert.Empty(t, offDiags)
}
