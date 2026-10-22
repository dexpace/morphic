package operation

import (
	"encoding/json/jsontext"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	yaml "gopkg.in/yaml.v3"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi/internal/annotation"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/ids"
	"github.com/dexpace/morphic/compilers/openapi/internal/load"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
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
	openapitest.RequireNoErrorDiags(t, diags)
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
	assert.Equal(t, "newPet", op.Bindings.HTTP[0].WebhookName, "the webhooks-map key names the event")
	assert.Empty(t, op.Bindings.HTTP[0].URITemplate, "a webhook carries no URI template")
}

// TestWebhooks_KeyIsTheEventName lowers a webhooks-map key as the event's name
// however it is spelled: braces in it are not template variables, and pointer
// escaping is the ID's business, not the name's. The binding carries no URI
// template, and an operation with no operationId takes its hint from the method
// and the key, which the empty template would have dropped.
func TestWebhooks_KeyIsTheEventName(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, key, operationID string
		wantID                 ir.OpID
		wantName               ir.Naming
	}{
		{"braces, with an operationId", "{tenant}.created", "tenantCreated",
			"op/openapi/webhooks/{tenant}.created/post", ir.Naming{Source: "tenantCreated", Canonical: "tenant_created"}},
		{"braces, named by the key", "{tenant}.created", "",
			"op/openapi/webhooks/{tenant}.created/post", ir.Naming{Hint: "post_tenant_created"}},
		{"pointer escapes, named by the key", "a/b~c", "",
			"op/openapi/webhooks/a~1b~0c/post", ir.Naming{Hint: "post_a_b_c"}},
		{"the empty key", "", "", "op/openapi/webhooks//post", ir.Naming{Hint: "post"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			operationID := ""
			if tc.operationID != "" {
				operationID = "\n      operationId: " + tc.operationID
			}
			spec := `openapi: 3.1.0
info: {title: T, version: "1"}
paths: {}
webhooks:
  ` + strconv.Quote(tc.key) + `:
    post:` + operationID + `
      responses: {"200": {description: ok}}
`
			svc, diags := lowerServiceSpec(t, spec)
			openapitest.RequireNoErrorDiags(t, diags)
			op := webhookOperation(t, svc)
			assert.Equal(t, tc.wantID, op.ID, "the mount pointer, not the event name, identifies the operation")
			assert.Equal(t, tc.wantName, op.Name)
			require.Len(t, op.Bindings.HTTP, 1)
			assert.True(t, op.Bindings.HTTP[0].IsWebhook)
			assert.Equal(t, tc.key, op.Bindings.HTTP[0].WebhookName, "the key, verbatim")
			assert.Empty(t, op.Bindings.HTTP[0].URITemplate, "a webhook has no URI template")
		})
	}
}

// webhookOperation returns the one operation of svc's webhooks group.
func webhookOperation(t *testing.T, svc ir.Service) ir.Operation {
	t.Helper()
	i := slices.IndexFunc(svc.Groups, func(g ir.OperationGroup) bool { return g.ID == ids.WebhookGroup() })
	require.GreaterOrEqual(t, i, 0, "the service has a webhooks group")
	require.Len(t, svc.Groups[i].Operations, 1, "the webhooks group holds exactly one operation")
	return svc.Groups[i].Operations[0]
}

// TestWebhooks_CallbacksLandInTheWebhooksGroup pins that a webhook operation's
// callbacks are lowered, as a path operation's are, and that their operations
// join it in the webhooks group.
func TestWebhooks_CallbacksLandInTheWebhooksGroup(t *testing.T) {
	t.Parallel()
	spec := `openapi: 3.1.0
info: {title: T, version: "1"}
paths: {}
webhooks:
  newPet:
    post:
      operationId: onNewPet
      callbacks:
        ack:
          '{$request.body#/ack}':
            post:
              operationId: ackPost
              responses: {"200": {description: ok}}
      responses: {"200": {description: ok}}
`
	svc, diags := lowerServiceSpec(t, spec)
	openapitest.RequireNoErrorDiags(t, diags)
	require.Len(t, svc.Groups, 1)
	group := svc.Groups[0]
	assert.Equal(t, "webhooks", group.Name.Hint)
	byName := openapitest.IndexBy(group.Operations, func(op ir.Operation) string { return op.Name.Source })
	hook, ok := byName["onNewPet"]
	require.True(t, ok)
	ack, ok := byName["ackPost"]
	require.True(t, ok, "the callback operation joins the webhook in its group")
	require.Len(t, hook.Bindings.HTTP, 1)
	require.Len(t, hook.Bindings.HTTP[0].Callbacks, 1)
	assert.Equal(t, []ir.OpID{ack.ID}, hook.Bindings.HTTP[0].Callbacks[0].Operations)
	require.Len(t, ack.Bindings.HTTP, 1)
	assert.False(t, ack.Bindings.HTTP[0].IsWebhook, "a callback is not a webhook, though a webhook declares it")
	assert.Empty(t, ack.Bindings.HTTP[0].WebhookName, "the webhook's name stays on the webhook")
	assert.Equal(t, "{$request.body#/ack}", ack.Bindings.HTTP[0].URITemplate)
}

func TestCallbacks_RegisteredAndBound(t *testing.T) {
	t.Parallel()
	spec := openapitest.PathsSpec(`  /subscribe:
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
	openapitest.RequireNoErrorDiags(t, diags)
	require.Len(t, svc.Groups, 1)
	group := svc.Groups[0]
	require.Len(t, group.Operations, 2, "parent op and callback op both registered")
	byName := openapitest.IndexBy(group.Operations, func(op ir.Operation) string { return op.Name.Source })
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
	spec := openapitest.PathsSpec(`  /users/{id}:
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
	merged := mergeParameters(appendSourced(nil, pi.GetParameters(), pathPtr, nil, nil), op.GetParameters(), opPtr)
	require.Len(t, merged, 2, "shared (name,in) collapses to one; op wins")
	assert.Same(t, op.GetParameters()[0], merged[0].ref, "operation parameter overrides the path-item one")
	assert.Equal(t, jsontext.Pointer("/paths/~1users~1{id}/get/parameters/0"), merged[0].pointer,
		"the op-level parameter keeps its own declaration pointer")

	byName := map[string]sourcedParam{}
	for _, sp := range merged {
		byName[resolve.Object[soa.Parameter](sp.ref).GetName()] = sp
	}
	_, hasID := byName["id"]
	_, hasTrace := byName["trace"]
	assert.True(t, hasID)
	assert.True(t, hasTrace)
	assert.Equal(t, jsontext.Pointer("/paths/~1users~1{id}/parameters/1"), byName["trace"].pointer,
		"the unshadowed path-level parameter keeps its own declaration pointer, at its own path-item index")
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

// TestParamKey_NilParameterIsNotAKey pins the guard on the merge key. A nil
// entry in a parameter list has no name and no location, so it cannot key
// anything — and answering with the zero key would silently merge every such
// entry onto one another.
func TestParamKey_NilParameterIsNotAKey(t *testing.T) {
	t.Parallel()
	_, ok := paramKey(nil)
	assert.False(t, ok)
}

// writtenFixture's one operation is written at /paths/~1z/get and reused every
// way a path can reuse it: /a aliases the path item, /m merges it in, /r
// references it, and /o aliases the operation itself. /s is a sequence, which
// no operation pointer passes through.
const writtenFixture = `paths:
  /z: &item
    get: &op {operationId: dup}
  /a: *item
  /m:
    <<: *item
  /r: {$ref: '#/paths/~1z'}
  /o:
    get: *op
  /s: [1, 2]
`

// writtenOp parses writtenFixture, returning its root and the node declaring
// its one operation.
func writtenOp(t *testing.T) (root, op *yaml.Node) {
	t.Helper()
	root = openapitest.YAMLNode(t, writtenFixture)
	op = annotation.RawChildNode(annotation.RawChildNode(annotation.RawChildNode(root, "paths"), "/z"), "get")
	require.NotNil(t, op, "the fixture writes its operation at /z")
	return root, op
}

// TestWrittenTree_HoldsANodeOnlyWhereTheTextWritesIt pins the walk declare
// reads a declaration's place from: the pointer the text writes the operation
// at holds it, and no pointer reaching it through an alias, a merge key or a
// $ref does, which is what tells a declaration from its reuses.
func TestWrittenTree_HoldsANodeOnlyWhereTheTextWritesIt(t *testing.T) {
	t.Parallel()
	root, op := writtenOp(t)
	w := newWrittenTree(root)

	assert.True(t, w.holds("/paths/~1z/get", op), "the text writes the operation at /z")
	for _, reuse := range []jsontext.Pointer{"/paths/~1a/get", "/paths/~1m/get", "/paths/~1r/get", "/paths/~1o/get"} {
		assert.False(t, w.holds(reuse, op), "%s reuses the operation rather than writing it", reuse)
	}
	assert.False(t, w.holds("/paths/~1s/0", op), "a walk reads mappings only")
	assert.False(t, newWrittenTree(nil).holds("/paths/~1z/get", op), "a tree with no text holds nothing")

	_, indexed := w.children[annotation.RawChildNode(root, "paths")]
	assert.True(t, indexed, "the paths mapping is indexed for the walks after the first")
}

// TestDeclare_ReadsWhereADeclarationIsWritten pins how declare places one
// declaration: at the pointer its text is at, with the mount there first, even
// where a reuse's pointer sorts before it. A declaration written nowhere the
// tree holds, as one in another document is, falls back to pointer order. Each
// case runs its claims in both orders.
func TestDeclare_ReadsWhereADeclarationIsWritten(t *testing.T) {
	t.Parallel()
	root, op := writtenOp(t)
	for _, tc := range []struct {
		name   string
		claims []operationIDClaim
		want   declaration
	}{
		{
			name: "an alias site sorting first",
			claims: []operationIDClaim{
				{node: op, ptrs: opPointers{mount: "/paths/~1a/get", decl: "/paths/~1a/get"}},
				{node: op, ptrs: opPointers{mount: "/paths/~1z/get", decl: "/paths/~1z/get"}},
			},
			want: declaration{at: "/paths/~1z/get", mounts: []jsontext.Pointer{"/paths/~1z/get", "/paths/~1a/get"}},
		},
		{
			name: "a ref sorting first",
			claims: []operationIDClaim{
				{node: op, ptrs: opPointers{mount: "/paths/~1r/get", decl: "/paths/~1z/get"}},
				{node: op, ptrs: opPointers{mount: "/paths/~1z/get", decl: "/paths/~1z/get"}},
			},
			want: declaration{at: "/paths/~1z/get", mounts: []jsontext.Pointer{"/paths/~1z/get", "/paths/~1r/get"}},
		},
		{
			name: "written where no claim is mounted",
			claims: []operationIDClaim{
				{node: op, ptrs: opPointers{mount: "/paths/~1y/get", decl: "/paths/~1z/get"}},
				{node: op, ptrs: opPointers{mount: "/paths/~1x/get", decl: "/paths/~1z/get"}},
			},
			want: declaration{at: "/paths/~1z/get", mounts: []jsontext.Pointer{"/paths/~1x/get", "/paths/~1y/get"}},
		},
		{
			name: "written nowhere the tree holds",
			claims: []operationIDClaim{
				{node: op, ptrs: opPointers{mount: "/paths/~1y/get", decl: "/paths/~1y/get"}},
				{node: op, ptrs: opPointers{mount: "/paths/~1x/get", decl: "/paths/~1x/get"}},
			},
			want: declaration{at: "/paths/~1x/get", mounts: []jsontext.Pointer{"/paths/~1x/get", "/paths/~1y/get"}},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			reversed := slices.Clone(tc.claims)
			slices.Reverse(reversed)
			for _, claims := range [][]operationIDClaim{tc.claims, reversed} {
				got := declare(claims, newWrittenTree(root))
				assert.Empty(t, cmp.Diff(tc.want, got, cmp.AllowUnexported(declaration{})))
			}
		})
	}
}

// TestByDeclaration_GroupsByNodeOrDeclarationPointer pins both ways claims
// mount one declaration, and that grouping follows a chain of them: a shares a
// node with b, and b a declaration pointer with c, so the three are one
// declaration. The c-first order is the one that splits them when each claim
// joins the first group it matches, and every order gives one answer.
func TestByDeclaration_GroupsByNodeOrDeclarationPointer(t *testing.T) {
	t.Parallel()
	shared, copied, other := &yaml.Node{}, &yaml.Node{}, &yaml.Node{}
	a := operationIDClaim{node: shared, ptrs: opPointers{mount: "/paths/~1a/get", decl: "/paths/~1a/get"}}
	b := operationIDClaim{node: shared, ptrs: opPointers{mount: "/paths/~1b/get", decl: "/components/pathItems/S/get"}}
	c := operationIDClaim{node: copied, ptrs: opPointers{mount: "/paths/~1c/get", decl: "/components/pathItems/S/get"}}
	d := operationIDClaim{node: other, ptrs: opPointers{mount: "/paths/~1d/get", decl: "/paths/~1d/get"}}
	want := [][]jsontext.Pointer{{"/paths/~1a/get", "/paths/~1b/get", "/paths/~1c/get"}, {"/paths/~1d/get"}}

	for _, claims := range [][]operationIDClaim{{a, b, c, d}, {c, a, d, b}, {d, c, b, a}} {
		var got [][]jsontext.Pointer
		for _, group := range byDeclaration(claims) {
			var mounts []jsontext.Pointer
			for _, cl := range group {
				mounts = append(mounts, cl.ptrs.mount)
			}
			slices.Sort(mounts)
			got = append(got, mounts)
		}
		slices.SortFunc(got, func(x, y []jsontext.Pointer) int { return strings.Compare(string(x[0]), string(y[0])) })
		assert.Empty(t, cmp.Diff(want, got))
	}
}

// TestByDeclaration_ANilNodeGroupsByDeclarationPointerAlone pins the stricter
// reading for a claim with no node: two such claims are one declaration only
// at one declaration pointer, never through the missing node they share.
func TestByDeclaration_ANilNodeGroupsByDeclarationPointerAlone(t *testing.T) {
	t.Parallel()
	apart := byDeclaration([]operationIDClaim{
		{ptrs: opPointers{mount: "/paths/~1a/get", decl: "/paths/~1a/get"}},
		{ptrs: opPointers{mount: "/paths/~1b/get", decl: "/paths/~1b/get"}},
	})
	assert.Len(t, apart, 2, "two claims with no node are two declarations")

	together := byDeclaration([]operationIDClaim{
		{ptrs: opPointers{mount: "/paths/~1a/get", decl: "/components/pathItems/S/get"}},
		{ptrs: opPointers{mount: "/paths/~1b/get", decl: "/components/pathItems/S/get"}},
	})
	assert.Len(t, together, 1, "unless they resolve to one declaration pointer")
}

// TestJudge_ReportsEveryDeclarationAndMountButTheFirst pins the findings judge
// makes of declarations already ordered: an error at each declaration but the
// first, naming where the first is written, and a warning at each mount but a
// declaration's first, naming that first mount.
func TestJudge_ReportsEveryDeclarationAndMountButTheFirst(t *testing.T) {
	t.Parallel()
	diags := judge(newRawLowerer(nil).ctx, "dup", []declaration{
		{at: "/paths/~1a/get", mounts: []jsontext.Pointer{"/paths/~1a/get", "/paths/~1b/get"}},
		{at: "/components/pathItems/S/get", mounts: []jsontext.Pointer{"/paths/~1c/get"}},
	})

	require.Len(t, diags, 2, "one remount and one second declaration: %+v", diags)
	assert.Equal(t, diag.DuplicateOperationID, diags[0].Code)
	assert.Equal(t, ir.SeverityWarning, diags[0].Severity)
	assert.Equal(t, jsontext.Pointer("/paths/~1b/get"), diags[0].Provenance.Pointer)
	assert.Contains(t, diags[0].Message, "/paths/~1a/get", "the warning names the mount it shares a declaration with")
	assert.Equal(t, diag.ConflictingOperationID, diags[1].Code)
	assert.Equal(t, ir.SeverityError, diags[1].Severity)
	assert.Equal(t, jsontext.Pointer("/components/pathItems/S/get"), diags[1].Provenance.Pointer,
		"the error is where the repeat is written")
	assert.Contains(t, diags[1].Message, "/paths/~1a/get", "and names where the first declaration is")
}

// TestDeclaringNode_FollowsAnAliasOneHop pins what an operation alias needs:
// `get: *op` builds the operation from the alias node, which declaringNode
// follows to the mapping it names. A node that is not an alias, and a missing
// one, are their own declaring node.
func TestDeclaringNode_FollowsAnAliasOneHop(t *testing.T) {
	t.Parallel()
	root := openapitest.YAMLNode(t, "a: &op {operationId: x}\nb: *op\n")
	anchored := annotation.RawChildNode(root, "a")
	alias := annotation.RawChildNode(root, "b")
	require.Equal(t, yaml.AliasNode, alias.Kind, "the fixture reuses the mapping through an alias")

	assert.Same(t, anchored, declaringNode(alias))
	assert.Same(t, anchored, declaringNode(anchored))
	assert.Nil(t, declaringNode(nil))
}

// TestOperationIDClaims_AddSkipsAnEmptyOperationID pins add's guard: an
// operation with no operationId claims nothing, because an emitter synthesizes
// its name from the method and path rather than reading one that was never
// declared.
func TestOperationIDClaims_AddSkipsAnEmptyOperationID(t *testing.T) {
	t.Parallel()
	claims := newOperationIDClaims()
	claims.add(&soa.Operation{}, opPointers{mount: "/paths/~1a/get", decl: "/paths/~1a/get"})

	assert.Empty(t, claims.names, "nothing was claimed, so nothing is queued to report")
	assert.Empty(t, claims.report(newRawLowerer(nil).ctx))
}
