// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package operation_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/compile"
	"github.com/dexpace/morphic/compilers/openapi/internal/auth"
	"github.com/dexpace/morphic/compilers/openapi/internal/budget"
	"github.com/dexpace/morphic/compilers/openapi/internal/load"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/compilers/openapi/internal/operation"
	"github.com/dexpace/morphic/compilers/openapi/internal/overlay"
	"github.com/dexpace/morphic/compilers/openapi/internal/servers"
	"github.com/dexpace/morphic/ir"
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
		lowering.Limits{}, lowering.StreamingMedia{}, lowering.BinaryMedia{}, lowering.ExtensionPromotions{}, lowering.NullableWidening{}, overlay.Origin{}).
		WithLoaded(loadedDoc)

	schemes, _ := auth.LowerSecuritySchemes(c)

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	svc, _, diags := operation.LowerService(ctx, c, compile.NewTypes(), servers.NewRegistry(), schemes)

	assert.Empty(t, diags)
	assert.Empty(t, svc.Groups, "neither the path loop nor the webhook loop lowered an operation")
}

// lowerUnder lowers spec's service with what path items copy held to limit,
// and returns the operations it mounted, in order, and whether the run is
// refused.
func lowerUnder(t *testing.T, spec string, limit budget.Limit) ([]ir.Operation, bool) {
	t.Helper()
	loadedDoc, _, err := load.Load(t.Context(), 0, openapitest.SourceOf(spec), load.Options{})
	require.NoError(t, err)
	require.NotNil(t, loadedDoc)
	c := lowering.New(0, loadedDoc.Doc, loadedDoc.Source, lowering.GroupByTags,
		lowering.Limits{MaxReferenceExpansion: limit}, lowering.StreamingMedia{}, lowering.BinaryMedia{},
		lowering.ExtensionPromotions{}, lowering.NullableWidening{}, overlay.Origin{}).WithLoaded(loadedDoc)
	schemes, _ := auth.LowerSecuritySchemes(c)
	svc, _, _ := operation.LowerService(t.Context(), c, compile.NewTypes(), servers.NewRegistry(), schemes)
	ops := make([]ir.Operation, 0, 3)
	for _, g := range svc.Groups {
		ops = append(ops, g.Operations...)
	}
	_, refused := c.ExpansionRefusal()
	return ops, refused
}

// names returns the source names of ops, in order.
func names(ops []ir.Operation) []string {
	out := make([]string, 0, len(ops))
	for _, op := range ops {
		out = append(out, op.Name.Source)
	}
	return out
}

// copyingItem is a 3.2 path item keeping an extension, x-k: v, which weighs
// 3+1+3, naming a server and writing a parameter weighing 25, beneath a get,
// a LINK writing what link gives it, and an UNLINK.
func copyingItem(link string) string {
	return `openapi: 3.2.0
info: {title: T, version: "1"}
paths:
  /p:
    x-k: v
    servers: [{url: 'https://a.test'}]
    parameters: [{name: q, in: query, schema: {type: string}}]
    get: {operationId: g, responses: {"200": {description: ok}}}
    additionalOperations:
      LINK: {operationId: link, responses: {"200": {description: ok}}` + link + `}
      UNLINK: {operationId: unlink, responses: {"200": {description: ok}}}
`
}

// TestLowerService_APathItemsCopiesStopAtTheBudget pins where a mount's copies
// cross the budget, LINK being the first operation charged: on the entries it
// keeps and the server it takes, before its parameter, or on the parameter, the
// rest within the budget. The mount lowers no operation after the one crossing
// it, and the run is refused. An operation naming servers of its own is charged
// none of its item's.
func TestLowerService_APathItemsCopiesStopAtTheBudget(t *testing.T) {
	t.Parallel()
	const kept, param = 3 + 1 + 3, 25
	all, refused := lowerUnder(t, copyingItem(""), budget.Unbounded())
	require.Equal(t, []string{"g", "link", "unlink"}, names(all))
	require.False(t, refused)
	require.Len(t, all[1].Servers, 1)
	server := len(all[1].Servers[0])

	ops, refused := lowerUnder(t, copyingItem(""), budget.AtMost(kept+server-1))
	assert.Equal(t, []string{"g", "link"}, names(ops), "crossing on what LINK keeps and the server it takes")
	assert.True(t, refused)

	ops, refused = lowerUnder(t, copyingItem(""), budget.AtMost(kept+server))
	assert.Equal(t, []string{"g", "link"}, names(ops), "crossing on LINK's parameter")
	assert.True(t, refused)

	own := ", servers: [{url: 'https://b.test'}]"
	ops, refused = lowerUnder(t, copyingItem(own), budget.AtMost(kept+param+kept+server+param))
	assert.Equal(t, []string{"g", "link", "unlink"}, names(ops), "LINK takes no server of its item's")
	assert.False(t, refused)
	_, refused = lowerUnder(t, copyingItem(own), budget.AtMost(kept+param+kept+server+param-1))
	assert.True(t, refused)
}
