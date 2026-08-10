package operation_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/compile"
	"github.com/dexpace/morphic/compilers/openapi/internal/load"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/compilers/openapi/internal/operation"
	"github.com/dexpace/morphic/compilers/openapi/internal/overlay"
	"github.com/dexpace/morphic/compilers/openapi/internal/schema"
)

// TestLowerService_CanceledContextLowersNoPathOrWebhook holds both walk loops to
// honouring ctx themselves. The phase boundary above would report the same
// cancellation with no check in either loop, so what is asserted is the work not
// done: a document mounting a path and a webhook must produce no group at all.
func TestLowerService_CanceledContextLowersNoPathOrWebhook(t *testing.T) {
	t.Parallel()
	const spec = `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /pets:
    get: {operationId: listPets, responses: {"200": {description: ok}}}
webhooks:
  petCreated:
    post: {operationId: petCreated, responses: {"200": {description: ok}}}
`
	loadedDoc, _, err := load.Load(t.Context(), 0, openapitest.SourceOf(spec), load.Options{})
	require.NoError(t, err)
	require.NotNil(t, loadedDoc)
	c := lowering.New(0, loadedDoc.Doc, loadedDoc.Source, lowering.GroupByTags,
		lowering.Limits{}, overlay.Origin{})
	var anchors schema.AnchorIndex

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	svc, _, diags := operation.LowerService(ctx, c, compile.NewTypes(0), &anchors, map[string]string{})

	assert.Empty(t, diags)
	assert.Empty(t, svc.Groups, "neither the path loop nor the webhook loop lowered an operation")
}
