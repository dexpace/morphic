package operation_test

import (
	"encoding/json/jsontext"
	"maps"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/nodeview"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/ir"
)

// TestResponses_ErrorSplitAndRanges pins how a responses map splits: a status
// below 400 is a Response and one from 400 an ErrorCase, faulted by its class
// as RFC 9110 gives it, and the default, which classes nothing, is
// Operation.Default with the catch-all condition, in neither list (GitHub
// #608).
func TestResponses_ErrorSplitAndRanges(t *testing.T) {
	t.Parallel()
	spec := openapitest.PathsSpec(`  /w:
    get:
      operationId: w
      responses:
        "200": {description: ok}
        "404":
          description: missing
          content: {application/json: {schema: {type: object, properties: {msg: {type: string}}}}}
        "5XX": {description: server oops}
        default: {description: anything else}
`)
	_, svc, diags := compileService(t, spec)
	openapitest.RequireNoErrorDiags(t, diags)
	op := openapitest.FirstOp(t, svc)
	require.Len(t, op.Responses, 1)
	assert.Equal(t, []ir.StatusRange{{From: 200, To: 200}}, op.Responses[0].Conditions.StatusCodes)
	require.Len(t, op.Errors, 2)
	assert.Equal(t, []ir.StatusRange{{From: 404, To: 404}}, op.Errors[0].Conditions.StatusCodes)
	assert.Equal(t, "client", op.Errors[0].Fault)
	require.NotNil(t, op.Errors[0].Payload)
	require.Len(t, op.Errors[0].Payload.Contents, 1)
	assert.NotEmpty(t, op.Errors[0].Payload.Contents[0].Type.Target, "404 error model lowered and referenced")
	assert.Equal(t, []ir.StatusRange{{From: 500, To: 599}}, op.Errors[1].Conditions.StatusCodes)
	assert.Equal(t, "server", op.Errors[1].Fault)
	require.NotNil(t, op.Default, "the default is the operation's catch-all")
	assert.Equal(t, []ir.StatusRange{{From: 0, To: 0}}, op.Default.Conditions.StatusCodes)
	assert.Equal(t, "default", op.Default.Name.Hint)
	assert.Equal(t, "anything else", op.Default.Docs.Description)
}

// TestResponses_ALoneDefaultIsNeitherClass pins the operation #608 is about:
// its only response is `default`, which OpenAPI says answers every status, so
// the operation's one description of success is it. It lowers to Default whole,
// with its body, and Responses and Errors stay empty, so a reader is not told
// the operation has no success response or only an error.
func TestResponses_ALoneDefaultIsNeitherClass(t *testing.T) {
	t.Parallel()
	spec := openapitest.PathsSpec(`  /w:
    get:
      operationId: w
      responses:
        default:
          description: result
          headers: {X-Rate: {schema: {type: integer}}}
          content: {application/json: {schema: {type: object, properties: {v: {type: string}}}}}
`)
	_, svc, diags := compileService(t, spec)
	openapitest.RequireNoErrorDiags(t, diags)
	op := openapitest.FirstOp(t, svc)

	assert.Empty(t, op.Responses)
	assert.Empty(t, op.Errors)
	require.NotNil(t, op.Default)
	assert.Equal(t, []ir.StatusRange{{}}, op.Default.Conditions.StatusCodes)
	require.NotNil(t, op.Default.Payload)
	require.Len(t, op.Default.Payload.Contents, 1)
	assert.Equal(t, ir.TypeID("t/anon/paths/~1w/get/responses/default/content/application~1json/schema"),
		op.Default.Payload.Contents[0].Type.Target)
	require.Len(t, op.Default.Headers, 1)
	assert.Equal(t, "X-Rate", op.Default.Headers[0].WireName)
	assert.Equal(t, "result", op.Default.Docs.Description)
}

// TestResponses_NamedByStatusKey pins the only naming OpenAPI gives a response.
// It declares no name and keys responses by status code, so Response.Name
// carries the hint ir-design §7.2 calls for, derived from that key; without it
// an emitter has nothing to name a per-response result type from (GitHub #259).
//
// The error half of the map and the default are named the same way, by the
// same function (GitHub #422), as "404" and "default" assert: the spelling of
// a wildcard or catch-all key is recorded nowhere else, since StatusRange
// renders {500,599} and {0,0} without it.
func TestResponses_NamedByStatusKey(t *testing.T) {
	t.Parallel()
	spec := openapitest.PathsSpec(`  /w:
    get:
      operationId: w
      responses:
        "200": {description: ok}
        "2XX": {description: any success}
        "": {description: no status code at all}
        "404": {description: missing}
        default: {description: anything else}
`)
	_, svc, diags := compileService(t, spec)
	openapitest.RequireNoErrorDiags(t, diags)
	op := openapitest.FirstOp(t, svc)

	require.Len(t, op.Responses, 3)
	hints := make([]string, 0, len(op.Responses))
	for _, r := range op.Responses {
		assert.Empty(t, r.Name.Source, "OpenAPI declares no response name, so Source stays empty")
		hints = append(hints, r.Name.Hint)
	}
	assert.Equal(t, []string{"200", "2_xx", "empty"}, hints,
		"the key as declared, neutralized; a key with no word in it takes the mint")

	require.Len(t, op.Errors, 1)
	assert.Empty(t, op.Errors[0].Name.Source, "OpenAPI declares no error name either, so Source stays empty")
	assert.Equal(t, "404", op.Errors[0].Name.Hint)
	require.NotNil(t, op.Default)
	assert.Empty(t, op.Default.Name.Source)
	assert.Equal(t, "default", op.Default.Name.Hint, "the default records the key its range cannot state")
}

// TestResponses_InvalidStatusKeyIsReported is the whole of GitHub #262 at the
// lowering level: a key naming no status used to be folded into {0,0} — the
// range "default" denotes — with no diagnostic, so a typo and a declared default
// arrived as the same shape. Both are declared here, and they must not.
func TestResponses_InvalidStatusKeyIsReported(t *testing.T) {
	t.Parallel()
	spec := openapitest.PathsSpec(`  /w:
    get:
      operationId: w
      responses:
        "200": {description: ok}
        "wat":
          description: names no status
          content: {application/json: {schema: {type: object, properties: {a: {type: string}}}}}
        default: {description: anything else}
`)
	_, svc, diags := compileService(t, spec)
	openapitest.RequireNoErrorDiags(t, diags)
	op := openapitest.FirstOp(t, svc)

	msg := openapitest.DiagMessageAt(t, diags, diag.InvalidStatusKey, ir.SeverityWarning,
		"/paths/~1w/get/responses/wat")
	assert.Contains(t, msg, `"wat"`, "the message names the key that could not be read")

	require.Len(t, op.Responses, 2)
	byHint := openapitest.IndexBy(op.Responses, func(r ir.Response) string { return r.Name.Hint })
	ok200, found := byHint["200"]
	require.True(t, found)
	assert.Equal(t, []ir.StatusRange{{From: 200, To: 200}}, ok200.Conditions.StatusCodes)

	unreadable, found := byHint["wat"]
	require.True(t, found, "the response is kept, and its key survives as the name hint")
	assert.Empty(t, unreadable.Conditions.StatusCodes,
		"no status could be read, so none is claimed — {0,0} would be default's own range")
	require.NotNil(t, unreadable.Payload, "the body is real and is not dropped over a bad key")
	assert.NotEmpty(t, unreadable.Payload.Contents)

	assert.Empty(t, op.Errors)
	require.NotNil(t, op.Default, "the declared default is still the only catch-all")
	assert.Equal(t, []ir.StatusRange{{From: 0, To: 0}}, op.Default.Conditions.StatusCodes)
}

// TestResponses_ValidStatusKeysAreNotReported is the overreach guard: a check
// that rejected anything unusual would satisfy the test above while warning on
// documents that are entirely correct.
func TestResponses_ValidStatusKeysAreNotReported(t *testing.T) {
	t.Parallel()
	spec := openapitest.PathsSpec(`  /w:
    get:
      operationId: w
      responses:
        "100": {description: continue}
        "200": {description: ok}
        "2XX": {description: any success}
        "599": {description: the top of what HTTP defines}
        "4xx": {description: lowercase wildcard}
        default: {description: anything else}
`)
	_, _, diags := compileService(t, spec)
	openapitest.RequireNoErrorDiags(t, diags)
	assert.False(t, openapitest.HasDiag(diags, diag.InvalidStatusKey),
		"every key here names a status; got %+v", diags)
}

// TestResponses_ErrorHeadersAreStructural pins the header half of GitHub #422.
// A 429's Retry-After and the rate-limit family live on precisely the status
// class that had no typed home for them: they were kept verbatim under
// ErrorCase.Unmodeled with an info diagnostic, so a consumer reading
// Response.Headers saw headers on a 200 and none on a 429. They are now lowered
// by lowerHeaders, exactly as the success side's are, and nothing is kept.
func TestResponses_ErrorHeadersAreStructural(t *testing.T) {
	t.Parallel()
	spec := openapitest.PathsSpec(`  /w:
    get:
      operationId: w
      responses:
        "200": {description: ok}
        "429":
          description: slow down
          headers:
            Retry-After: {schema: {type: integer}}
            X-RateLimit-Remaining: {schema: {type: integer}}
`)
	_, svc, diags := compileService(t, spec)
	openapitest.RequireNoErrorDiags(t, diags)
	op := openapitest.FirstOp(t, svc)
	require.Len(t, op.Errors, 1)
	ec := op.Errors[0]

	require.Len(t, ec.Headers, 2, "both headers lowered structurally")
	wire := make([]string, 0, len(ec.Headers))
	for _, h := range ec.Headers {
		wire = append(wire, h.WireName)
		assert.NotEmpty(t, h.Type.Target, "each header carries its lowered type")
	}
	assert.Equal(t, []string{"Retry-After", "X-RateLimit-Remaining"}, wire)

	assert.NotContains(t, ec.Unmodeled, "openapi:headers",
		"a typed home means nothing is kept verbatim beside it")
	for _, d := range diags {
		assert.NotContains(t, d.Message, "error response headers",
			"nothing is degraded, so nothing announces a degradation")
	}
}

func TestResponses_HeadersLowered(t *testing.T) {
	t.Parallel()
	spec := openapitest.PathsSpec(`  /h:
    get:
      operationId: h
      responses:
        "200":
          description: ok
          headers:
            X-Rate-Limit: {required: true, schema: {type: integer}}
`)
	_, svc, diags := compileService(t, spec)
	openapitest.RequireNoErrorDiags(t, diags)
	op := openapitest.FirstOp(t, svc)
	require.Len(t, op.Responses, 1)
	require.Len(t, op.Responses[0].Headers, 1)
	h := op.Responses[0].Headers[0]
	assert.Equal(t, "X-Rate-Limit", h.WireName)
	assert.True(t, h.Required)
	assert.Equal(t, ir.TypeID("t/prim/integer"), h.Type.Target)
	assert.Equal(t, ir.PropID("p/openapi/paths/~1h/get/responses/200/headers/X-Rate-Limit"), h.ID)
}

func TestResponses_LinksPreserved(t *testing.T) {
	t.Parallel()
	spec := openapitest.PathsSpec(`  /l:
    get:
      operationId: l
      responses:
        "200":
          description: ok
          links:
            GetUserByUserId: {operationId: getUser}
`)
	_, svc, diags := compileService(t, spec)
	openapitest.RequireNoErrorDiags(t, diags)
	op := openapitest.FirstOp(t, svc)
	require.Len(t, op.Responses, 1)
	raw, ok := op.Responses[0].Unmodeled["openapi:links"]
	require.True(t, ok, "response links preserved raw for later promotion")
	assert.Contains(t, string(raw.Value), "GetUserByUserId")
	assert.Equal(t, ir.ReasonNoIRHome, raw.Reason)
}

const componentResponseRefSpec = `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /a:
    get:
      operationId: getA
      responses:
        "200": {$ref: '#/components/responses/OK'}
  /b:
    get:
      operationId: getB
      responses:
        "200": {$ref: '#/components/responses/OK'}
components:
  responses:
    OK:
      description: ok
      content:
        application/json:
          schema: {type: object, properties: {n: {type: string}}}
`

// TestResponses_ComponentRefSharedAcrossOperationsInternsOnce is the fix's
// core scenario for a referenced response component (issue #107): two
// operations $ref'ing one #/components/responses/OK must intern its content
// schema once, at the component's own declaration pointer.
func TestResponses_ComponentRefSharedAcrossOperationsInternsOnce(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, componentResponseRefSpec)
	openapitest.RequireNoErrorDiags(t, diags)
	getA := openapitest.FindOp(t, doc, "getA")
	getB := openapitest.FindOp(t, doc, "getB")
	require.Len(t, getA.Responses, 1)
	require.Len(t, getB.Responses, 1)

	wantID := ir.TypeID("t/anon/components/responses/OK/content/application~1json/schema")
	assert.Equal(t, wantID, getA.Responses[0].Payload.Contents[0].Type.Target)
	assert.Equal(t, wantID, getB.Responses[0].Payload.Contents[0].Type.Target,
		"both operations resolve the same shared response schema")

	_, fabricatedA := doc.Types[ir.TypeID("t/anon/paths/~1a/get/responses/200/content/application~1json/schema")]
	_, fabricatedB := doc.Types[ir.TypeID("t/anon/paths/~1b/get/responses/200/content/application~1json/schema")]
	assert.False(t, fabricatedA, "no fabricated per-operation ID for /a")
	assert.False(t, fabricatedB, "no fabricated per-operation ID for /b")
}

const refdErrorResponseSpec = `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /a:
    get:
      operationId: getA
      responses:
        "404": {$ref: '#/components/responses/NotFound'}
        default: {$ref: '#/components/responses/Fallback'}
  /b:
    get:
      operationId: getB
      responses:
        "404": {$ref: '#/components/responses/NotFound'}
        default: {$ref: '#/components/responses/Fallback'}
components:
  responses:
    NotFound:
      description: nope
      content:
        application/json:
          schema: {type: object, properties: {code: {type: string}}}
    Fallback:
      description: fallback
      content:
        application/json:
          schema: {type: object, properties: {msg: {type: string}}}
`

// TestResponses_RefdErrorAndDefaultInternAtDeclaration covers the arms of
// lowerResponses the success-response test does not reach: a $ref'd 4XX
// response (the isErrorRange branch) and a $ref'd default response, which
// resolves outside the status loop entirely. Both must intern their models at
// the component's own pointer, once for the two operations.
func TestResponses_RefdErrorAndDefaultInternAtDeclaration(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, refdErrorResponseSpec)
	openapitest.RequireNoErrorDiags(t, diags)
	getA := openapitest.FindOp(t, doc, "getA")
	getB := openapitest.FindOp(t, doc, "getB")
	require.Len(t, getA.Errors, 1)
	require.Len(t, getB.Errors, 1)

	byFault := func(op ir.Operation) map[string]ir.ErrorCase {
		return openapitest.IndexBy(op.Errors, func(ec ir.ErrorCase) string { return ec.Fault })
	}
	aErrs, bErrs := byFault(getA), byFault(getB)

	errType := func(ec ir.ErrorCase) ir.TypeID {
		require.NotNil(t, ec.Payload)
		require.Len(t, ec.Payload.Contents, 1)
		return ec.Payload.Contents[0].Type.Target
	}
	wantNotFound := ir.TypeID("t/anon/components/responses/NotFound/content/application~1json/schema")
	assert.Equal(t, wantNotFound, errType(aErrs["client"]))
	assert.Equal(t, wantNotFound, errType(bErrs["client"]), "the shared 4XX error model interns once")

	defType := func(op ir.Operation) ir.TypeID {
		require.NotNil(t, op.Default)
		require.NotNil(t, op.Default.Payload)
		require.Len(t, op.Default.Payload.Contents, 1)
		return op.Default.Payload.Contents[0].Type.Target
	}
	wantFallback := ir.TypeID("t/anon/components/responses/Fallback/content/application~1json/schema")
	assert.Equal(t, wantFallback, defType(getA))
	assert.Equal(t, wantFallback, defType(getB), "the shared default model interns once")

	for id := range doc.Types {
		assert.NotContains(t, string(id), "/responses/404", "no fabricated per-operation error ID")
		assert.NotContains(t, string(id), "/responses/default", "no fabricated per-operation default ID")
	}
}

// TestErrorCase_EveryMediaTypeIsAContent pins the content half of GitHub #422:
// an error response lowers to ErrorCase.Payload.Contents, one entry per media
// type, in the same shape and by the same function as a success response's. A
// 404 declaring only application/problem+json must be distinguishable from one
// declaring application/json, and a 400 declaring both must keep both schemas.
//
// The 409 is the control: an error declaring no content still gets no payload,
// so a Contents entry marks a declaration rather than appearing on every case.
func TestErrorCase_EveryMediaTypeIsAContent(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, openapitest.PathsSpec(`  /x:
    get:
      operationId: getX
      responses:
        "200": {description: ok}
        "400":
          description: bad
          content:
            application/json:
              schema: {type: object, properties: {a: {type: string}}}
            application/problem+json:
              schema: {type: object, properties: {b: {type: string}}}
        "404":
          description: gone
          content:
            application/problem+json:
              schema: {type: object}
        "409": {description: conflict}
`))
	openapitest.RequireNoErrorDiags(t, diags)
	errs := openapitest.IndexBy(openapitest.FindOp(t, doc, "getX").Errors,
		func(ec ir.ErrorCase) int { return ec.Conditions.StatusCodes[0].From })

	multi := errs[400]
	require.NotNil(t, multi.Payload)
	require.Len(t, multi.Payload.Contents, 2, "every media type is kept, none elected")
	assert.Equal(t, []string{"application/json", "application/problem+json"},
		[]string{multi.Payload.Contents[0].MediaType, multi.Payload.Contents[1].MediaType})
	assert.NotEqual(t, multi.Payload.Contents[0].Type.Target, multi.Payload.Contents[1].Type.Target,
		"the second media type keeps its own schema rather than the first's")

	single := errs[404]
	require.NotNil(t, single.Payload)
	require.Len(t, single.Payload.Contents, 1)
	assert.Equal(t, "application/problem+json", single.Payload.Contents[0].MediaType,
		"the sole media type's own key is what used to be lost")

	for status, ec := range errs {
		assert.NotContains(t, ec.Unmodeled, "openapi:content",
			"%d keeps no content map beside a typed payload", status)
	}
	for _, d := range diags {
		assert.NotContains(t, d.Message, "ErrorCase home",
			"nothing is degraded, so nothing announces a degradation")
	}

	assert.Nil(t, errs[409].Payload, "an error response declaring no content gets no payload")
}

// TestResponses_TwoKeysForOneRangeAreReported pins the collision the neutralized
// hint cannot show. "4XX" and "4xx" name one range, and the key reaches the IR
// neutralized, so both responses arrive with hint "4_xx" and identical
// conditions. An ErrorCase carries no ID, so name and conditions are the whole
// of what tells one from another — two indistinguishable cases, compiled with
// exit 0 and nothing said. Source cannot hold the difference: a responses-map
// key is not a name the document declared, which TestResponses_NamedByStatusKey
// pins. So the collision is reported where the keys are read.
func TestResponses_TwoKeysForOneRangeAreReported(t *testing.T) {
	t.Parallel()
	spec := openapitest.PathsSpec(`  /w:
    get:
      operationId: w
      responses:
        "200": {description: ok}
        "4XX": {description: upper}
        "4xx": {description: lower}
`)
	_, svc, diags := compileService(t, spec)
	op := openapitest.FirstOp(t, svc)

	require.Len(t, op.Errors, 2, "both are kept: neither key is wrong on its own")
	assert.True(t, openapitest.HasDiag(diags, diag.DuplicateStatusKey),
		"two keys resolving to one range is reported; got %v", diags)
}

// TestResponses_ACollisionIsReportedTheSameInEitherOrder is the two-order diff
// for that diagnostic. Reported at the second key to arrive, it named a
// different key — and sat at a different pointer — when the two were declared
// the other way round, though a responses map has no order to mean anything
// by. One diagnostic per colliding range, at the map, naming every key in
// sorted order, reads the same from either spelling; and three keys on one
// range are one collision, not two.
func TestResponses_ACollisionIsReportedTheSameInEitherOrder(t *testing.T) {
	t.Parallel()
	upper, lower, mixed := `        "4XX": {description: upper}
`, `        "4xx": {description: lower}
`, `        "4Xx": {description: mixed}
`
	diagsFor := func(keys string) []ir.Diagnostic {
		t.Helper()
		spec := openapitest.PathsSpec("  /w:\n    get:\n      operationId: w\n      responses:\n" + keys)
		_, _, diags := compileService(t, spec)
		var found []ir.Diagnostic
		for _, d := range diags {
			if d.Code == diag.DuplicateStatusKey {
				found = append(found, d)
			}
		}
		return found
	}

	asWritten := diagsFor(upper + lower + mixed)
	reversed := diagsFor(mixed + lower + upper)
	require.Len(t, asWritten, 1, "three keys on one range are one collision; got %v", asWritten)
	assert.Equal(t, asWritten, reversed, "the report must not depend on which key was written first")
	assert.Equal(t, jsontext.Pointer("/paths/~1w/get/responses"), asWritten[0].Provenance.Pointer)
	assert.Contains(t, asWritten[0].Message, `"4XX", "4Xx", "4xx"`, "every colliding key, in sorted order")
}

// TestResponses_ASharedBodyIsNamedAlikeFromEitherStatusClass is the two-order
// diff for the payload hint. A response $ref'd across operations by its path
// pointer, mounted once as a success and once as an error, interns one type at
// that pointer; the hint it carries was whichever side lowered first, because
// the two sides fell back to different words where the pointer named no
// component. Zero diagnostics either way, and nothing in the corpus reaches
// the order-invariance oracle with this shape, so the swap is made here.
func TestResponses_ASharedBodyIsNamedAlikeFromEitherStatusClass(t *testing.T) {
	t.Parallel()
	success := `  /a:
    get:
      operationId: getA
      responses:
        "200":
          description: ok
          content: {application/json: {schema: {type: object, properties: {a: {type: string}}}}}
`
	failure := `  /b:
    get:
      operationId: getB
      responses:
        "400": {$ref: '#/paths/~1a/get/responses/200'}
`
	hintOf := func(paths string) string {
		t.Helper()
		doc, _, diags := compileService(t, openapitest.PathsSpec(paths))
		openapitest.RequireNoErrorDiags(t, diags)
		td, ok := doc.Types["t/anon/paths/~1a/get/responses/200/content/application~1json/schema"]
		require.True(t, ok, "the shared body is interned once at its declaration pointer; got %v", slices.Sorted(maps.Keys(doc.Types)))
		return td.Common().Name.Hint
	}

	successFirst := hintOf(success + failure)
	failureFirst := hintOf(failure + success)
	assert.Equal(t, successFirst, failureFirst, "the hint depends on which mount lowered first")
}

// TestResponses_StatusKeyDiagsAreSitedAtTheOperation pins where the two
// responses-map key diagnostics land. The key is a fact about the operation's
// own map, so the diagnostic belongs at the operation's entry for it — not at
// whatever the entry resolved to. Sited at the resolved pointer, a $ref'd
// response put a warning about the operation on the shared component, naming
// neither operation; and because the diagnostic stream dedups on full identity,
// the second operation's warning vanished with it.
func TestResponses_StatusKeyDiagsAreSitedAtTheOperation(t *testing.T) {
	t.Parallel()
	spec := `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /a:
    get:
      operationId: getA
      responses:
        "200": {description: ok}
        "4XX": {$ref: '#/components/responses/NF'}
        "4xx": {$ref: '#/components/responses/NF'}
        "wat": {$ref: '#/components/responses/NF'}
  /b:
    get:
      operationId: getB
      responses:
        "200": {description: ok}
        "4XX": {$ref: '#/components/responses/NF'}
        "4xx": {$ref: '#/components/responses/NF'}
        "wat": {$ref: '#/components/responses/NF'}
components:
  responses:
    NF: {description: not found}
`
	_, _, diags := compileService(t, spec)

	for _, op := range []string{"~1a", "~1b"} {
		responses := "/paths/" + op + "/get/responses"
		msg := openapitest.DiagMessageAt(t, diags, diag.DuplicateStatusKey, ir.SeverityWarning, responses)
		assert.Contains(t, msg, `"4XX", "4xx"`, "the message names every key that collided")
		msg = openapitest.DiagMessageAt(t, diags, diag.InvalidStatusKey, ir.SeverityWarning, responses+"/wat")
		assert.Contains(t, msg, `"wat"`, "the message names the key that could not be read")
	}
	for _, d := range diags {
		assert.NotEqual(t, jsontext.Pointer("/components/responses/NF"), d.Provenance.Pointer,
			"a key of the operation's map is no fault of the component it resolved to: %v", d)
	}
}

// TestResponses_RefSiteDocsOverrideTheDeclaration pins the response half of
// GitHub #610 at both status classes, and the per-mount rule: one component
// mounted three times — a success with a sibling description, an error with a
// different one, and a success with none — must give each mount its own docs
// and leave the declaration's description for the mount that writes none.
func TestResponses_RefSiteDocsOverrideTheDeclaration(t *testing.T) {
	t.Parallel()
	spec := `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /a:
    get:
      operationId: a
      responses:
        "200": {$ref: '#/components/responses/Ok', description: as a returns it}
  /b:
    get:
      operationId: b
      responses:
        "404": {$ref: '#/components/responses/Ok', description: as b fails}
  /c:
    get:
      operationId: c
      responses:
        "200": {$ref: '#/components/responses/Ok'}
components:
  responses:
    Ok:
      description: declared
      content:
        application/json: {schema: {type: object, properties: {n: {type: string}}}}
`
	doc, _, diags := compileService(t, spec)
	openapitest.RequireNoErrorDiags(t, diags)

	a := openapitest.FindOp(t, doc, "a")
	require.Len(t, a.Responses, 1)
	assert.Equal(t, "as a returns it", a.Responses[0].Docs.Description)

	b := openapitest.FindOp(t, doc, "b")
	require.Len(t, b.Errors, 1)
	assert.Equal(t, "as b fails", b.Errors[0].Docs.Description,
		"an error case is lowered by the same shared function, so it folds too")

	c := openapitest.FindOp(t, doc, "c")
	require.Len(t, c.Responses, 1)
	assert.Equal(t, "declared", c.Responses[0].Docs.Description,
		"a mount with no siblings keeps the declaration's description")
}

// TestResponses_SummaryIsReadAtEveryVersion pins GitHub #615's
// Response.summary: the 3.2 field reaches Docs.Summary and is not also kept
// verbatim or called undefined, and below 3.2 it reads the same while load adds
// that the field is a later version's (GitHub #864).
func TestResponses_SummaryIsReadAtEveryVersion(t *testing.T) {
	t.Parallel()
	const body = `  /a:
    get:
      operationId: a
      responses:
        "204":
          summary: The item was deleted
          description: Long form.
`
	for _, version := range []string{"3.2.0", "3.1.0", "3.0.3"} {
		t.Run(version, func(t *testing.T) {
			t.Parallel()
			doc, _, diags := compileService(t, "openapi: "+version+"\ninfo: {title: T, version: \"1\"}\npaths:\n"+body)
			openapitest.RequireNoErrorDiags(t, diags)
			resp := openapitest.FindOp(t, doc, "a").Responses[0]
			assert.Equal(t, "The item was deleted", resp.Docs.Summary)
			assert.Equal(t, "Long form.", resp.Docs.Description)
			assert.NotContains(t, resp.Unmodeled, "openapi:summary",
				"a key the reader took is read, not also kept verbatim")
			assert.False(t, openapitest.HasDiag(diags, diag.UnknownObjectKey),
				"the census must not call the field undefined")
			assert.Equal(t, version != "3.2.0", openapitest.HasDiagCodeAt(diags, diag.LaterVersionConstruct,
				"/paths/~1a/get/responses/204/summary"))
		})
	}
}

// TestResponses_SummaryNoReaderTakesIsStillReported pins the 3.2 summaries the
// reader does not place in Docs.Summary, which must not vanish because the
// census was told the key is the dialect's: one that is not a string is kept
// verbatim with a warning, and one merged in from past
// nodeview.MergeDepthLimit keeps the census's report that it reached the IR in
// no form. One merged in through `<<` is read as a written one is.
func TestResponses_SummaryNoReaderTakesIsStillReported(t *testing.T) {
	t.Parallel()
	doc, _, diags := compileService(t, openapitest.PathsSpecVer("3.2.0", `  /a:
    get:
      operationId: a
      responses:
        "200": {description: ok, summary: [not, a, string]}
        "201":
          <<: {summary: Merged}
          description: ok
        "202": `+openapitest.NestedMerges(nodeview.MergeDepthLimit+1, "{summary: Deep, description: ok}")+`
`))
	responses := openapitest.FindOp(t, doc, "a").Responses
	require.Len(t, responses, 3)
	assert.Empty(t, responses[0].Docs.Summary)
	kept, ok := responses[0].Unmodeled["openapi:summary"]
	require.True(t, ok, "a summary that is not a string is kept verbatim")
	assert.JSONEq(t, `["not","a","string"]`, string(kept.Value))
	assert.True(t, openapitest.HasDiagCodeAt(diags, diag.DegradedConstruct, "/paths/~1a/get/responses/200/summary"))
	assert.Equal(t, "Merged", responses[1].Docs.Summary, "a merged-in summary is read")
	assert.True(t, openapitest.HasDiagCodeAt(diags, diag.UnknownKeyUnreachable, "/paths/~1a/get/responses/202/summary"),
		"one past the bound is still reported; got %v", diags)
}

// TestResponses_SummaryUseSiteBeatsTheDeclaration holds the 3.2 field to the
// same precedence the rest of a response's docs take: a summary written beside
// the $ref describes this mount, so it wins over the declaration's own.
func TestResponses_SummaryUseSiteBeatsTheDeclaration(t *testing.T) {
	t.Parallel()
	doc, _, diags := compileService(t, `openapi: 3.2.0
info: {title: T, version: "1"}
paths:
  /a:
    get:
      operationId: a
      responses:
        "200": {$ref: '#/components/responses/Ok', summary: As a sees it}
components:
  responses:
    Ok:
      summary: As declared
      description: ok
      content:
        application/json: {schema: {type: string}}
`)
	openapitest.RequireNoErrorDiags(t, diags)
	assert.Equal(t, "As a sees it", openapitest.FindOp(t, doc, "a").Responses[0].Docs.Summary)
}
