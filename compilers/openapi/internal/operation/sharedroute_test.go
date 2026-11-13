// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package operation_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/ir"
)

// sharedRouteSpec declares GET /bearer three times, twice by a fragment, and
// one route under two expression names, beside a method and a route no other
// operation sends, and a callback and a webhook, which no client routes.
const sharedRouteSpec = `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /bearer#oauth2:
    get: {responses: {"200": {description: ok}}}
    post: {responses: {"200": {description: ok}}}
  /bearer#apiKey:
    get: {responses: {"200": {description: ok}}}
  /bearer:
    get:
      responses: {"200": {description: ok}}
      callbacks:
        cb:
          /bearer:
            get: {responses: {"200": {description: ok}}}
  /a/{x}#one:
    parameters: [{name: x, in: path, required: true, schema: {type: string}}]
    get: {responses: {"200": {description: ok}}}
  /a/{y}#two:
    parameters: [{name: y, in: path, required: true, schema: {type: string}}]
    get: {responses: {"200": {description: ok}}}
  /other:
    get: {responses: {"200": {description: ok}}}
webhooks:
  /bearer:
    get: {responses: {"200": {description: ok}}}
`

// TestSharedRoute_MarksEveryOperationSendingARouteAnotherSends pins
// SharedRoute: each path operation whose method and route, its expressions'
// names aside, another sends is marked, and no other, a callback's and a
// webhook's included. An operation with no operationId keeps a name hint of
// its own, from the whole key, fragment included.
func TestSharedRoute_MarksEveryOperationSendingARouteAnotherSends(t *testing.T) {
	t.Parallel()
	_, svc, diags := compileService(t, sharedRouteSpec)
	openapitest.RequireNoErrorDiags(t, diags)

	type binding struct {
		hint, template string
		shared         bool
	}
	got := map[ir.OpID]binding{}
	for _, g := range svc.Groups {
		for _, op := range g.Operations {
			hb := op.Bindings.HTTP[0]
			got[op.ID] = binding{hint: op.Name.Hint, template: hb.URITemplate + hb.WebhookName, shared: hb.SharedRoute}
		}
	}
	const p = "op/openapi/paths/"
	assert.Equal(t, map[ir.OpID]binding{
		p + "~1bearer#oauth2/get":                    {hint: "get_bearer_oauth_2", template: "/bearer", shared: true},
		p + "~1bearer#oauth2/post":                   {hint: "post_bearer_oauth_2", template: "/bearer"},
		p + "~1bearer#apiKey/get":                    {hint: "get_bearer_api_key", template: "/bearer", shared: true},
		p + "~1bearer/get":                           {hint: "get_bearer", template: "/bearer", shared: true},
		p + "~1bearer/get/callbacks/cb/~1bearer/get": {hint: "get_bearer", template: "/bearer"},
		p + "~1a~1{x}#one/get":                       {hint: "get_a_x_one", template: "/a/{x}", shared: true},
		p + "~1a~1{y}#two/get":                       {hint: "get_a_y_two", template: "/a/{y}", shared: true},
		p + "~1other/get":                            {hint: "get_other", template: "/other"},
		"op/openapi/webhooks/~1bearer/get":           {hint: "get_bearer", template: "/bearer"},
	}, got)
}

// TestSharedRoute_PathPrefixGroupsTheRoute pins that grouping by path prefix
// reads a key's route, so a fragment names no group of its own.
func TestSharedRoute_PathPrefixGroupsTheRoute(t *testing.T) {
	t.Parallel()
	svc, diags := serviceWithGrouping(t, openapitest.PathsSpec(`  /bearer#oauth2:
    get: {operationId: a, responses: {"200": {description: ok}}}
  /bearer/x#apiKey:
    get: {operationId: b, responses: {"200": {description: ok}}}
  /#root:
    get: {operationId: c, responses: {"200": {description: ok}}}
`), lowering.GroupByPathPrefix)
	openapitest.RequireNoErrorDiags(t, diags)
	assert.Equal(t, map[ir.GroupID][]ir.OpID{
		"g/path-prefix/bearer": {"op/openapi/paths/~1bearer#oauth2/get", "op/openapi/paths/~1bearer~1x#apiKey/get"},
		"g/path-prefix":        {"op/openapi/paths/~1#root/get"},
	}, opsByGroup(t, svc.Groups))
}
