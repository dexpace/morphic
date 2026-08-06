package operation

import (
	"strconv"
	"testing"

	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/speakeasy-api/openapi/sequencedmap"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/ids"
	"github.com/dexpace/morphic/compilers/openapi/internal/load"
	"github.com/dexpace/morphic/compilers/openapi/internal/resolve"
	"github.com/dexpace/morphic/ir"
)

func TestWebhooks_WebhookGroup(t *testing.T) {
	t.Parallel()
	spec := `openapi: 3.1.0
info: {title: T, version: "1"}
paths: {}
webhooks:
  newPet:
    post:
      operationId: onNewPet
      responses: {"200": {description: ok}}
`
	svc, diags := lowerServiceSpec(t, spec)
	requireNoErrorDiags(t, diags)
	var group ir.OperationGroup
	found := false
	for _, g := range svc.Groups {
		if g.Name.Hint == "webhooks" {
			group, found = g, true
		}
	}
	require.True(t, found, "webhook operations land in the webhooks group")
	require.Len(t, group.Operations, 1)
	op := group.Operations[0]
	assert.Equal(t, ir.OpID("op/openapi/webhooks/newPet/post"), op.ID)
	require.Len(t, op.Bindings.HTTP, 1)
	assert.True(t, op.Bindings.HTTP[0].IsWebhook)
}

func TestCallbacks_RegisteredAndBound(t *testing.T) {
	t.Parallel()
	spec := pathsSpec(`  /subscribe:
    post:
      operationId: sub
      callbacks:
        onEvent:
          '{$request.body#/cb}':
            post:
              operationId: cbPost
              responses: {"200": {description: ok}}
      responses: {"200": {description: ok}}
`)
	svc, diags := lowerServiceSpec(t, spec)
	requireNoErrorDiags(t, diags)
	require.Len(t, svc.Groups, 1)
	group := svc.Groups[0]
	require.Len(t, group.Operations, 2, "parent op and callback op both registered")
	byName := indexBy(group.Operations, func(op ir.Operation) string { return op.Name.Source })
	sub, ok := byName["sub"]
	require.True(t, ok)
	cb, ok := byName["cbPost"]
	require.True(t, ok)
	require.Len(t, sub.Bindings.HTTP, 1)
	require.Len(t, sub.Bindings.HTTP[0].Callbacks, 1)
	call := sub.Bindings.HTTP[0].Callbacks[0]
	assert.Equal(t, "{$request.body#/cb}", call.Expression)
	require.Len(t, call.Operations, 1)
	assert.Equal(t, cb.ID, call.Operations[0])
}

func TestParameters_PathItemMergeOverride(t *testing.T) {
	t.Parallel()
	spec := pathsSpec(`  /users/{id}:
    parameters:
      - {name: id, in: path, required: true, schema: {type: string}, description: path-level}
      - {name: trace, in: header, schema: {type: string}}
    get:
      operationId: g
      parameters:
        - {name: id, in: path, required: true, schema: {type: integer}, description: op-level}
      responses: {"200": {description: ok}}
`)
	loadedDoc, _, err := load.Load(t.Context(), 0, compilers.Source{Path: "spec.yaml", Data: []byte(spec)}, load.Options{})
	require.NoError(t, err)
	require.NotNil(t, loadedDoc)
	var pi *soa.PathItem
	for _, rp := range loadedDoc.Doc.GetPaths().All() {
		pi = resolve.Object[soa.PathItem](rp)
	}
	require.NotNil(t, pi)
	op := pi.Get()
	require.NotNil(t, op)
	pathPtr := ids.Ptr("paths", "/users/{id}")
	opPtr := pathPtr + ids.Ptr("get")
	merged := mergeParameters(pi.GetParameters(), op.GetParameters(), pathPtr, opPtr)
	require.Len(t, merged, 2, "shared (name,in) collapses to one; op wins")
	assert.Same(t, op.GetParameters()[0], merged[0].ref, "operation parameter overrides the path-item one")
	assert.Equal(t, "/paths/~1users~1{id}/get/parameters/0", merged[0].pointer,
		"the op-level parameter keeps its own declaration pointer")

	byName := map[string]sourcedParam{}
	for _, sp := range merged {
		byName[resolve.Object[soa.Parameter](sp.ref).GetName()] = sp
	}
	_, hasID := byName["id"]
	_, hasTrace := byName["trace"]
	assert.True(t, hasID)
	assert.True(t, hasTrace)
	assert.Equal(t, "/paths/~1users~1{id}/parameters/1", byName["trace"].pointer,
		"the unshadowed path-level parameter keeps its own declaration pointer, at its own path-item index")
}

func TestStatusRange_NamesAStatus(t *testing.T) {
	t.Parallel()
	cases := []struct {
		code     string
		from, to int
	}{
		{"default", 0, 0},
		{"200", 200, 200},
		{"100", 100, 100}, // the low bound of what HTTP defines
		{"599", 599, 599}, // and the high one
		{"4XX", 400, 499},
		{"5xx", 500, 599}, // lowercase is read too
		{"1XX", 100, 199},
	}
	for _, tc := range cases {
		t.Run(tc.code, func(t *testing.T) {
			t.Parallel()
			r, ok := statusRange(tc.code)
			require.True(t, ok, "%q names a status", tc.code)
			assert.Equal(t, tc.from, r.From)
			assert.Equal(t, tc.to, r.To)
		})
	}
}

// TestStatusRange_NamesNoStatus is the half that used to be silent. Every key
// here reached {0,0} or a range OpenAPI cannot declare, with no diagnostic and
// no way for a consumer to tell the result from a declared default (GitHub
// #262).
//
// The zero range beside the false is asserted because it is load-bearing, not
// because it is obvious: lowerResponses routes on isErrorRange without
// re-testing ok, so a false paired with anything from 400 up would send a key
// that names no status to lowerErrorCase, which would classify a fault from a
// range nothing derived and drop the key entirely — ErrorCase holds no naming.
// This is what holds that pairing.
func TestStatusRange_NamesNoStatus(t *testing.T) {
	t.Parallel()
	for _, code := range []string{
		"wat",  // not a number at all; used to reach {0,0}
		"20A",  // two digits and a letter
		"2X0",  // half a wildcard
		"1XY",  // the third character the wildcard test never read
		"9XX",  // a leading digit past the 1XX-5XX OpenAPI defines
		"6XX",  // the first one past it
		"0",    // Atoi read this as 0, which is default's own range
		"00",   // and this
		"000",  // and this, at the right width
		"600",  // three digits past what HTTP defines
		"099",  // and below it
		"+200", // Atoi accepts a sign; a status key has none
		"",     // no key at all
		"Default",
		"2XXX",
	} {
		// Quoted so the empty key names a subtest of its own rather than "#00".
		t.Run(strconv.Quote(code), func(t *testing.T) {
			t.Parallel()
			r, ok := statusRange(code)
			assert.False(t, ok, "%q names no status", code)
			assert.Equal(t, ir.StatusRange{}, r,
				"a false is paired with the zero range; lowerResponses routes on that")
		})
	}
}

// TestStatusConditions_RecordsNothingForAnUnreadableKey pins the disposition
// half: an unreadable key must not borrow default's {0,0} range, or the two
// become one shape in the IR.
func TestStatusConditions_RecordsNothingForAnUnreadableKey(t *testing.T) {
	t.Parallel()
	assert.Empty(t, statusConditions(ir.StatusRange{}, false).StatusCodes)
	assert.Equal(t,
		[]ir.StatusRange{{From: 0, To: 0}},
		statusConditions(ir.StatusRange{}, true).StatusCodes,
		"a declared default still records the catch-all it means")
}

func TestFaultFor(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "client", faultFor(ir.StatusRange{From: 404, To: 404}))
	assert.Equal(t, "server", faultFor(ir.StatusRange{From: 503, To: 503}))
	assert.Equal(t, "", faultFor(ir.StatusRange{}))
}

func TestPreserveErrorHeaders_WithoutRootNode(t *testing.T) {
	t.Parallel()
	l := newRawLowerer(&soa.OpenAPI{})
	headers := sequencedmap.New(
		sequencedmap.NewElem("X-H", &soa.ReferencedHeader{}),
	)
	ec := &ir.ErrorCase{}
	diags := preserveErrorHeaders(l.ctx, ec, &soa.Response{Headers: headers}, "/r")
	assert.Nil(t, ec.Unmodeled, "headers with no raw node are not preserved")
	require.Empty(t, diags)
}

func TestLowerResponses_NoResponses(t *testing.T) {
	t.Parallel()
	l := newRawLowerer(&soa.OpenAPI{})
	responses, errs, diags := lowerResponses(l.ctx, l.types, &l.anchors, &soa.Operation{}, "/op")
	assert.Nil(t, responses)
	assert.Nil(t, errs)
	assert.Empty(t, diags)
}

func TestFirstPathSegment_Empty(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "", firstPathSegment("/"))
	assert.Equal(t, "users", firstPathSegment("/users/{id}"))
}

func TestApplyPathServers_WithoutRootNode(t *testing.T) {
	t.Parallel()
	l := newRawLowerer(&soa.OpenAPI{})
	op := &ir.Operation{}
	diags := applyPathServers(l.ctx, op, &soa.PathItem{Servers: []*soa.Server{{URL: "https://x"}}}, "/paths/~1a")
	assert.Nil(t, op.Unmodeled, "servers with no raw node are not preserved")
	assert.Empty(t, diags)
}

// TestApplyOperationServers_WithoutRootNode is the operation half of the test
// above: a declared list whose source node cannot be read keeps nothing, and
// announces nothing it did not keep.
func TestApplyOperationServers_WithoutRootNode(t *testing.T) {
	t.Parallel()
	l := newRawLowerer(&soa.OpenAPI{})
	op := &ir.Operation{}
	diags := applyOperationServers(l.ctx, op, &soa.Operation{Servers: []*soa.Server{{URL: "https://x"}}}, "/paths/~1a/get")
	assert.Nil(t, op.Unmodeled, "servers with no raw node are not preserved")
	assert.Empty(t, diags)
}

func TestLowerTagDefs_NilEntrySkipped(t *testing.T) {
	t.Parallel()
	l := newRawLowerer(&soa.OpenAPI{Tags: []*soa.Tag{nil, {}}})
	assert.Len(t, lowerTagDefs(l.ctx), 1, "nil tag entry skipped")
}

// TestParamKey_NilParameterIsNotAKey pins the guard on the merge key. A nil
// entry in a parameter list has no name and no location, so it cannot key
// anything — and answering with the zero key would silently merge every such
// entry onto one another.
func TestParamKey_NilParameterIsNotAKey(t *testing.T) {
	t.Parallel()
	_, ok := paramKey(nil)
	assert.False(t, ok)
}

// TestCheckOperationIDUnique_ReportsTheSecondClaim pins what the check is for:
// the first claim on an operationId is recorded silently and the second names
// it. This used to cover a lazy map init instead, which is gone — the map is
// the caller's and is allocated where the lowering starts.
func TestCheckOperationIDUnique_ReportsTheSecondClaim(t *testing.T) {
	t.Parallel()
	l := newRawLowerer(nil)
	op := ir.Operation{Name: ir.Naming{Source: "dup"}}

	assert.Empty(t, checkOperationIDUnique(l.ctx, l.operationIDs, op, "/paths/~1a/get"),
		"the first claim is recorded without a word")

	diags := checkOperationIDUnique(l.ctx, l.operationIDs, op, "/paths/~1b/get")
	require.Len(t, diags, 1)
	assert.Equal(t, diag.DuplicateOperationID, diags[0].Code)
	assert.Contains(t, diags[0].Message, "/paths/~1a/get", "and it names the operation that claimed it first")
}

// TestFaultFor_ClassifiesAtTheClassBoundaries pins where one HTTP class ends and
// the next begins, which no fixture reaches: the corpus uses 400, 404, 429 and
// the 4XX/5XX wildcards, so both upper bounds are stated by the code and held by
// nothing. Narrowing either one by a single code leaves the whole suite green
// while a 499 stops being a client fault — and Fault is what an SDK emitter
// reads to decide which exception class it raises.
func TestFaultFor_ClassifiesAtTheClassBoundaries(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		from int
		want string
	}{
		{name: "the first client code", from: 400, want: "client"},
		{name: "the last client code", from: 499, want: "client"},
		{name: "the first server code", from: 500, want: "server"},
		{name: "the last server code", from: 599, want: "server"},
		{name: "a success is neither", from: 200, want: ""},
		{name: "past the server class", from: 600, want: ""},
		{name: "the catch-all default range is unclassified", from: 0, want: ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, faultFor(ir.StatusRange{From: tc.from, To: tc.from}))
		})
	}
}
