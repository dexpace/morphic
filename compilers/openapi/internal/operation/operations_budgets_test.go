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
	lowered, diags := operation.LowerService(ctx, c, compile.NewTypes(), schemes)

	assert.Empty(t, diags)
	assert.Empty(t, lowered.Service.Groups, "neither the path loop nor the webhook loop lowered an operation")
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
	lowered, _ := operation.LowerService(t.Context(), c, compile.NewTypes(), schemes)
	ops := make([]ir.Operation, 0, 3)
	for _, g := range lowered.Service.Groups {
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

// TestLowerService_NoAdditionalOperationLowersPastTheBudget pins that once what
// path items copy crosses the budget, at /p's LINK, a later item's fixed methods
// still lower, as they cost nothing, while its additional operation, which
// would copy past the budget, does not.
func TestLowerService_NoAdditionalOperationLowersPastTheBudget(t *testing.T) {
	t.Parallel()
	const spec = `openapi: 3.2.0
info: {title: T, version: "1"}
paths:
  /p:
    x-k: v
    get: {operationId: g, responses: {"200": {description: ok}}}
    additionalOperations:
      LINK: {operationId: link, responses: {"200": {description: ok}}}
  /q:
    x-k: v
    get: {operationId: g2, responses: {"200": {description: ok}}}
    put: {operationId: p2, responses: {"200": {description: ok}}}
    additionalOperations:
      LINK: {operationId: link2, responses: {"200": {description: ok}}}
`
	ops, refused := lowerUnder(t, spec, budget.AtMost(1))
	assert.Equal(t, []string{"g", "link", "g2", "p2"}, names(ops))
	assert.True(t, refused)
}

// leastBudget is the least budget within which lowerUnder admits spec, which
// 4,096 must admit.
func leastBudget(t *testing.T, spec string) int {
	t.Helper()
	const most = 1 << 12
	_, refused := lowerUnder(t, spec, budget.AtMost(most))
	require.False(t, refused, "admitted within %d", most)
	lo, hi := -1, most
	for hi-lo > 1 {
		mid := lo + (hi-lo)/2
		if _, refused := lowerUnder(t, spec, budget.AtMost(mid)); refused {
			lo = mid
		} else {
			hi = mid
		}
	}
	return hi
}

// TestLowerService_AnItemsParametersAreChargedOnceACopy pins what an item's
// parameters add to the least budget it lowers within, beside a get and a
// LINK: a $ref'd one is charged at each operation, by Declared, and not again,
// and one written beside the item's $ref, a layer of its own, is charged at
// LINK alone, as it weighs: 25, as a parameter of this shape does.
func TestLowerService_AnItemsParametersAreChargedOnceACopy(t *testing.T) {
	t.Parallel()
	const param = 25
	const head = "openapi: 3.2.0\ninfo: {title: T, version: \"1\"}\npaths:\n  /p:\n"
	const ops = `    get: {operationId: g, responses: {"200": {description: ok}}}
    additionalOperations:
      LINK: {operationId: link, responses: {"200": {description: ok}}}
`
	const components = "components:\n  parameters:\n    P: {name: q, in: query, schema: {type: string}}\n"
	plain := leastBudget(t, head+ops+components)
	referenced := leastBudget(t, head+"    parameters: [{$ref: '#/components/parameters/P'}]\n"+ops+components)
	assert.Equal(t, plain+2*param, referenced, "a $ref'd parameter is charged at each operation, once")

	layered := func(params string) string {
		return head + "    $ref: '#/components/pathItems/I'\n" + params + components + "  pathItems:\n    I:\n" +
			`      get: {operationId: g, responses: {"200": {description: ok}}}
      additionalOperations:
        LINK: {operationId: link, responses: {"200": {description: ok}}}
`
	}
	without := leastBudget(t, layered(""))
	with := leastBudget(t, layered("    parameters: [{name: q, in: query, schema: {type: string}}]\n"))
	assert.Equal(t, without+param, with, "a parameter beside the item's $ref is charged at LINK alone")
}
