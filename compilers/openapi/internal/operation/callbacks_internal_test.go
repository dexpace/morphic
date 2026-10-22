package operation

import (
	"slices"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/ids"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
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
