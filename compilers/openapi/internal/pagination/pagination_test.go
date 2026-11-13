// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package pagination_test

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi"
	"github.com/dexpace/morphic/compilers/openapi/internal/load"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/compilers/openapi/internal/pagination"
	"github.com/dexpace/morphic/ir"
)

// The query parameters and bodies the cases below combine.
const (
	pageParams = `[{name: q, in: query, schema: {type: string}}, {name: page, in: query, schema: {type: integer}},
        {name: per_page, in: query, schema: {type: number}}]`
	cursorParams = `[{name: cursor, in: query, schema: {type: string}},
        {name: per_page, in: query, schema: {type: integer}}]`
	stringList = `{type: array, items: {type: string}}`
	envelope   = `{type: object, properties: {
        errors: {type: array, items: {}}, messages: {type: array, items: {}},
        result: {type: array, items: {type: string}},
        result_info: {type: object, properties: {
          page: {type: integer}, total_count: {type: integer}, total_pages: {type: integer}}}}}`
)

// TestInfer_ReadsEachStrategy pins what the policy reads off each spelling it
// settles: the strategy a query parameter names, the page size beside it, and
// where the body holds the items, a cursor walk's next cursor and the totals
// (docs/compilers/openapi.md#pagination-policy).
func TestInfer_ReadsEachStrategy(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct{ src, want string }{
		"a page number beside a page size, in an envelope": {listing(pageParams, envelope),
			"page cursor=page limit=per_page items=/result next=- count=/result_info/total_count " +
				"pages=/result_info/total_pages stop=[]"},
		"a page number written as a string defaulting to one, its items the body whole": {
			listing(`[{name: page, in: query, schema: {type: string, default: "1"}}]`, stringList),
			"page cursor=page limit=- items=/ next=- count=- pages=- stop=[]"},
		"a page number written as a string, a number its example": {
			listing(`[{name: page, in: query, schema: {type: string, example: 2}}]`, stringList),
			"page cursor=page limit=- items=/ next=- count=- pages=- stop=[]"},
		"an offset written as a string, which no token is named": {
			listing(`[{name: offset, in: query, schema: {type: string}}]`, stringList),
			"offset cursor=offset limit=- items=/ next=- count=- pages=- stop=[]"},
		"an offset beside a limit, the total camel-cased": {listing(
			`[{name: offset, in: query, schema: {type: integer, format: int32}},
			  {name: limit, in: query, schema: {type: number, format: double}}]`,
			`{type: object, properties: {data: `+stringList+`, totalCount: {type: integer}}}`),
			"offset cursor=offset limit=limit items=/data next=- count=/totalCount pages=- stop=[]"},
		"a cursor the page's info hands back": {listing(cursorParams,
			`{type: object, properties: {result: `+stringList+`,
			  result_info: {type: object, properties: {cursor: {type: string}, count: {type: integer}}}}}`),
			"cursor cursor=cursor limit=per_page items=/result next=/result_info/cursor count=- pages=- stop=[no_next]"},
		"a token whose next value is named so beside the one sent": {listing(
			`[{name: pageToken, in: query, schema: {type: string}}, {name: pageSize, in: query, schema: {type: integer}}]`,
			`{type: object, properties: {pageToken: {type: string}, items: `+stringList+`,
			  nextPageToken: {type: string}}}`),
			"token cursor=pageToken limit=pageSize items=/items next=/nextPageToken count=- pages=- stop=[no_next]"},
		"a cursor beside no flag, one elsewhere in the page": {listing(cursorParams,
			`{type: object, properties: {success: {type: boolean}, result: `+stringList+`,
			  result_info: {type: object, properties: {cursor: {type: string}}}}}`),
			"cursor cursor=cursor limit=per_page items=/result next=/result_info/cursor count=- pages=- stop=[no_next]"},
		"a page number beside a flag, which a page walk reads no stop from": {listing(pageParams,
			`{type: object, properties: {result: `+stringList+`,
			  result_info: {type: object, properties: {has_more: {type: boolean}}}}}`),
			"page cursor=page limit=per_page items=/result next=- count=- pages=- stop=[]"},
		"items in the one list of an object named as holding them": {listing(pageParams,
			`{type: object, properties: {result: {type: object, properties: {note: {type: string}, reports: `+
				stringList+`}}, errors: `+stringList+`}}`),
			"page cursor=page limit=per_page items=/result/reports next=- count=- pages=- stop=[]"},
		"items in the body's one list, named otherwise": {listing(pageParams,
			`{type: object, properties: {tags: `+stringList+`,
			  pagination: {type: object, properties: {total_pages: {type: integer}}}}}`),
			"page cursor=page limit=per_page items=/tags next=- count=- pages=/pagination/total_pages stop=[]"},
		"a total named twice, which counts neither": {listing(pageParams,
			`{type: object, properties: {result: `+stringList+`,
			  a: {type: object, properties: {total_count: {type: integer}}},
			  b: {type: object, properties: {total_count: {type: integer}}}}}`),
			"page cursor=page limit=per_page items=/result next=- count=- pages=- stop=[]"},
		"a body composed over an envelope, read through references": {listing(pageParams,
			`{allOf: [{$ref: '#/components/schemas/Envelope'},
			  {type: object, properties: {result: {$ref: '#/components/schemas/Things', description: d}}}]}`) +
			"components:\n  schemas:\n    Things: " + stringList + "\n" +
			"    Envelope: {type: object, properties: {result: {}, errors: " + stringList + ", " +
			"result_info: {$ref: '#/components/schemas/Info'}}}\n" +
			"    Info: {type: object, properties: {total_count: {$ref: '#/components/schemas/Count'}}}\n" +
			"    Count: {type: integer, minimum: 0}\n",
			"page cursor=page limit=per_page items=/result next=- count=/result_info/total_count pages=- stop=[]"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			op, doc, diags := listOp(t, tc.src, openapi.Options{})
			openapitest.RequireNoErrorDiags(t, diags)
			assert.Equal(t, tc.want, describe(doc, op))
			assert.Empty(t, undecided(diags))
		})
	}
}

// TestInfer_MarksWhatItFills pins the marks an inferred Pagination carries:
// its own Inferred, and the heuristic's name in the operation's provenance,
// beside any it already named.
func TestInfer_MarksWhatItFills(t *testing.T) {
	t.Parallel()
	op, _, _ := listOp(t, listing(pageParams, envelope), openapi.Options{Grouping: openapi.GroupByPathPrefix})
	require.NotNil(t, op.Pagination)
	assert.True(t, op.Pagination.Inferred)
	assert.Equal(t, ir.JoinInferred("group-path-prefix", lowering.PaginationInferenceHeuristic), op.Provenance.Inferred)
}

// TestInfer_LeavesWhatItCannotSettle pins each reason an operation whose query
// parameters name a walk keeps no Pagination, and the info finding saying so
// at the operation.
func TestInfer_LeavesWhatItCannotSettle(t *testing.T) {
	t.Parallel()
	const pageOnly = `[{name: page, in: query, schema: {type: integer}}]`
	objectOf := func(members string) string { return `{type: object, properties: {` + members + `}}` }
	for name, tc := range map[string]struct{ src, want string }{
		"two parameters naming a continuation": {
			listing(`[{name: page, in: query, schema: {type: integer}}, {name: cursor, in: query, schema: {type: string}}]`, envelope),
			`query parameters "cursor" and "page" each name where a walk continues`},
		"two parameters naming the page size": {
			listing(`[{name: page, in: query, schema: {type: integer}}, {name: per_page, in: query, schema: {type: integer}},
			  {name: limit, in: query, schema: {type: integer}}]`, envelope),
			`query parameters "limit" and "per_page" each name the page size`},
		"a continuation of no type a walk sends": {
			listing(`[{name: page, in: query, schema: {type: string, enum: [p, q]}}]`, envelope),
			`query parameter "page" names where a walk continues, and its type is no plain number or string`},
		"a page written as a string no number shows it counts": {
			listing(`[{name: page, in: query, schema: {type: string, default: first, examples: [a1b2, "2.5", true]}}]`, envelope),
			`query parameter "page" names the page as a string with no whole number for its default or an example, ` +
				`so it may be a token`},
		"a page size of no type a walk sends": {
			listing(`[{name: page, in: query, schema: {type: integer}}, {name: per_page, in: query, schema: {type: boolean}}]`,
				envelope),
			`query parameter "per_page" names the page size, and its type is no plain number or string`},
		"a body neither an object nor a list": {listing(pageOnly, `{oneOf: [`+envelope+`, {type: string}]}`),
			`the application/json response body is neither an object nor a list`},
		"a list body for a cursor walk": {listing(cursorParams, stringList),
			`the application/json response body is a list, which holds no next cursor`},
		"two lists named as items": {listing(pageOnly, objectOf(`result: `+stringList+`, data: `+stringList)),
			`the application/json response body holds lists "data" and "result", each named as its items`},
		"two objects named as holding items": {
			listing(pageOnly, objectOf(`result: `+objectOf(`a: `+stringList)+`, data: `+objectOf(`b: `+stringList))),
			`the application/json response body holds objects "data" and "result", each named as holding its items`},
		"an object named as holding items that holds no list": {listing(pageOnly, objectOf(`result: `+objectOf(`n: {type: integer}`))),
			`the application/json response body holds "result", which holds no list of items`},
		"an object named as holding items that holds two lists": {
			listing(pageOnly, objectOf(`result: `+objectOf(`a: `+stringList+`, b: `+stringList))),
			`the application/json response body holds "result", which holds lists "a" and "b" and names none as its items`},
		"no list": {listing(pageOnly, objectOf(`count: {type: integer}`)),
			`the application/json response body holds no list of items`},
		"two lists named otherwise": {listing(pageOnly, objectOf(`tags: `+stringList+`, errors: `+stringList)),
			`the application/json response body holds lists "errors" and "tags" and names none as its items`},
		"three lists named otherwise": {listing(pageOnly, objectOf(`c: `+stringList+`, a: `+stringList+`, b: `+stringList)),
			`the application/json response body holds lists "a", "b" and "c" and names none as its items`},
		"five lists named otherwise, of which three are named": {listing(pageOnly,
			objectOf(`e: `+stringList+`, d: `+stringList+`, c: `+stringList+`, a: `+stringList+`, b: `+stringList)),
			`the application/json response body holds lists "a", "b", "c" and 2 more and names none as its items`},
		"no next cursor": {listing(cursorParams, objectOf(`result: `+stringList)),
			`the application/json response body holds no member "next_cursor" or "cursor" of the cursor's type`},
		"a next cursor of another type": {listing(cursorParams, objectOf(`result: `+stringList+`, cursor: {type: integer}`)),
			`the application/json response body holds no member "next_cursor" or "cursor" of the cursor's type`},
		"a flag beside the next cursor": {listing(cursorParams,
			objectOf(`result: `+stringList+`, next_cursor: {type: string}, has_more: {type: boolean}`)),
			`the application/json response body holds "has_more" beside its next cursor, a flag that can end ` +
				`the walk, which no stop condition reads`},
		"flags beside a next cursor an object holds": {listing(cursorParams, objectOf(`result: `+stringList+
			`, info: `+objectOf(`more: {type: boolean}, cursor: {type: string}, is_truncated: {type: boolean}`))),
			`the application/json response body holds "is_truncated" beside its next cursor, a flag that can ` +
				`end the walk, which no stop condition reads`},
		"two next cursors": {
			listing(cursorParams, objectOf(`result: `+stringList+`, cursor: {type: string}, info: `+objectOf(`cursor: {type: string}`))),
			`the application/json response body holds 2 members "cursor" of the cursor's type, each a next cursor`},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			op, doc, diags := listOp(t, tc.src, openapi.Options{})
			assert.Equal(t, "-", describe(doc, op))
			assert.Equal(t, []string{"no pagination is inferred: " + tc.want}, undecided(diags))
			got, ok := openapitest.FirstDiag(diags, "openapi/undecided-pagination")
			require.True(t, ok)
			assert.Equal(t, ir.SeverityInfo, got.Severity)
			assert.Equal(t, ir.Provenance{Pointer: "/paths/~1things/get"}, got.Provenance)
		})
	}
}

// TestInfer_ReadsEveryResponse holds a page to every content of every success
// response, a pager reading whichever arrives: two holding the page at
// different members settle none, and no success response, or none with a body,
// leaves nothing to read. One with no body ends a walk, and the default
// response is no success, so neither is read.
func TestInfer_ReadsEveryResponse(t *testing.T) {
	t.Parallel()
	head := `openapi: 3.1.0
info: {title: t, version: "1"}
paths:
  /things:
    get:
      operationId: listThings
      parameters: [{name: page, in: query, schema: {type: integer}}]
      responses:
`
	body := func(members string) string { return `{type: object, properties: {` + members + `}}` }
	for name, tc := range map[string]struct{ responses, want string }{
		"no success response": {`        default: {description: d}
`, `the operation declares no success response to read a page from`},
		"no success response with a body": {`        "200": {description: ok}
        "204": {description: none}
`, `no success response has a body to read a page from`},
		"two contents holding the page at different members": {`        "200":
          description: ok
          content:
            application/json: {schema: ` + body(`result: `+stringList) + `}
            application/xml: {schema: ` + body(`items: `+stringList) + `}
`, `the success responses' bodies hold their pages at different members`},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			op, doc, diags := listOp(t, head+tc.responses, openapi.Options{})
			assert.Equal(t, "-", describe(doc, op))
			assert.Equal(t, []string{"no pagination is inferred: " + tc.want}, undecided(diags))
		})
	}

	// Two bodies composing one envelope hold its items at one member, and
	// differ only in what each adds.
	composed := func(param, json, xml string) string {
		return `openapi: 3.1.0
info: {title: t, version: "1"}
paths:
  /things:
    get:
      operationId: listThings
      parameters: [` + param + `]
      responses:
        "200":
          description: ok
          content:
            application/json: {schema: {allOf: [{$ref: '#/components/schemas/Base'}, ` + body(json) + `]}}
            application/xml: {schema: {allOf: [{$ref: '#/components/schemas/Base'}, ` + body(xml) + `]}}
components:
  schemas:
    Base: ` + body(`result: `+stringList) + "\n"
	}
	const cursor = `{name: cursor, in: query, schema: {type: string}}`
	const page = `{name: page, in: query, schema: {type: integer}}`
	for name, src := range map[string]string{
		"a next cursor":    composed(cursor, `cursor: {type: string}`, `next_cursor: {type: string}`),
		"a count of items": composed(page, `total_count: {type: integer}`, `n: {type: object, properties: {total_count: {type: integer}}}`),
		"a count of pages": composed(page, `total_pages: {type: integer}`, `n: {type: object, properties: {total_pages: {type: integer}}}`),
		"a count and none": composed(page, `total_count: {type: integer}`, `n: {type: integer}`),
	} {
		t.Run("two contents holding "+name+" at different members", func(t *testing.T) {
			t.Parallel()
			op, doc, diags := listOp(t, src, openapi.Options{})
			assert.Equal(t, "-", describe(doc, op))
			assert.Equal(t, []string{"no pagination is inferred: the success responses' bodies hold their pages at " +
				"different members"}, undecided(diags))
		})
	}

	t.Run("a default response, which is read as no page", func(t *testing.T) {
		t.Parallel()
		src := head + `        "200": {description: ok, content: {application/json: {schema: ` + body(`result: `+stringList) + `}}}
        default: {description: failure, content: {application/json: {schema: ` + body(`message: {type: string}`) + `}}}
`
		op, doc, diags := listOp(t, src, openapi.Options{})
		assert.Equal(t, "page cursor=page limit=- items=/result next=- count=- pages=- stop=[]", describe(doc, op))
		assert.Empty(t, undecided(diags))
	})

	t.Run("a response with no body beside the page, which ends a walk", func(t *testing.T) {
		t.Parallel()
		src := head + `        "204": {description: none}
        "200": {description: ok, content: {application/json: {schema: ` + body(`result: `+stringList) + `}}}
        "304": {description: not modified}
`
		op, doc, diags := listOp(t, src, openapi.Options{})
		assert.Equal(t, "page cursor=page limit=- items=/result next=- count=- pages=- stop=[]", describe(doc, op))
		assert.Empty(t, undecided(diags))
	})

	t.Run("two responses holding one body", func(t *testing.T) {
		t.Parallel()
		src := head + `        "200": {description: ok, content: {application/json: {schema: {$ref: '#/components/schemas/Page'}}}}
        "206": {description: part, content: {application/json: {schema: {$ref: '#/components/schemas/Page'}}}}
components:
  schemas:
    Page: ` + body(`result: `+stringList) + "\n"
		op, doc, diags := listOp(t, src, openapi.Options{})
		assert.Equal(t, "page cursor=page limit=- items=/result next=- count=- pages=- stop=[]", describe(doc, op))
		assert.Empty(t, undecided(diags))
	})
}

// TestInfer_LeavesAnOperationNamingNoWalk pins what the policy neither fills
// nor reports: an operation naming a page size and no continuation, a
// continuation outside the query, an operation the API sends, and any under the
// policy turned off.
func TestInfer_LeavesAnOperationNamingNoWalk(t *testing.T) {
	t.Parallel()
	webhook := `openapi: 3.1.0
info: {title: t, version: "1"}
webhooks:
  thing:
    post:
      operationId: listThings
      parameters: [{name: page, in: query, schema: {type: integer}}]
      responses:
        "200": {description: ok, content: {application/json: {schema: ` + envelope + `}}}
`
	for name, tc := range map[string]struct {
		src  string
		opts openapi.Options
	}{
		"a page size alone": {listing(`[{name: limit, in: query, schema: {type: integer}}]`, envelope), openapi.Options{}},
		"a page number in a header": {listing(`[{name: page, in: header, schema: {type: integer}}]`, envelope),
			openapi.Options{}},
		"an operation the API sends": {webhook, openapi.Options{}},
		"the policy turned off": {listing(pageParams, envelope),
			openapi.Options{PaginationInference: openapi.PaginationInference{Disabled: true}}},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			op, doc, diags := listOp(t, tc.src, tc.opts)
			assert.Equal(t, "-", describe(doc, op))
			assert.Empty(t, undecided(diags))
			assert.Empty(t, op.Provenance.Inferred)
		})
	}
}

// TestInfer_ReadsOnlyAMethodSafeToSendAgain pins that a walk is read only off
// a GET or a QUERY, which a pager can send again for each page: a POST taking
// its path item's page parameters, as one creating a resource does, is
// reported and left with none, whatever its body holds.
func TestInfer_ReadsOnlyAMethodSafeToSendAgain(t *testing.T) {
	t.Parallel()
	page := `{type: object, properties: {result: ` + stringList + `}}`
	created := `{type: object, properties: {result: {type: object, properties: {tags: ` + stringList + `}}}}`
	src := `openapi: 3.2.0
info: {title: t, version: "1"}
paths:
  /things:
    parameters: [{name: page, in: query, schema: {type: integer}}, {name: per_page, in: query, schema: {type: integer}}]
    get:
      operationId: listThings
      responses: {"200": {description: ok, content: {application/json: {schema: ` + page + `}}}}
    query:
      operationId: queryThings
      responses: {"200": {description: ok, content: {application/json: {schema: ` + page + `}}}}
    post:
      operationId: createThing
      responses: {"200": {description: ok, content: {application/json: {schema: ` + created + `}}}}
    additionalOperations:
      SEARCH:
        operationId: searchThings
        responses: {"200": {description: ok, content: {application/json: {schema: ` + page + `}}}}
`
	doc, diags := compile(t, src, openapi.Options{})
	openapitest.RequireNoErrorDiags(t, diags)
	for name, want := range map[string]string{
		"listThings":   "page cursor=page limit=per_page items=/result next=- count=- pages=- stop=[]",
		"queryThings":  "page cursor=page limit=per_page items=/result next=- count=- pages=- stop=[]",
		"createThing":  "-",
		"searchThings": "-",
	} {
		assert.Equal(t, want, describe(doc, openapitest.FindOp(t, doc, name)), name)
	}
	got := undecided(diags)
	slices.Sort(got)
	assert.Equal(t, []string{
		"no pagination is inferred: the operation is a POST, and only a GET or a QUERY is safe to send again for each page",
		"no pagination is inferred: the operation is a SEARCH, and only a GET or a QUERY is safe to send again for each page",
	}, got)
}

// TestInfer_KeepsAPaginationTheOperationHolds pins that inference fills only an
// operation holding no Pagination and none its extensions speak of: one a step
// before it filled is kept whole, one the stated set names is left with none,
// and nothing is marked or reported for either.
func TestInfer_KeepsAPaginationTheOperationHolds(t *testing.T) {
	t.Parallel()
	declared := &ir.Pagination{Strategy: ir.PageStrategyNextLink}
	doc := &ir.Document{
		Types: ir.TypeRegistry{"t/list": &ir.List{ID: "t/list", Elem: ir.TypeRef{Target: "t/prim/string"}},
			"t/prim/integer": &ir.Primitive{ID: "t/prim/integer", Prim: ir.PrimInteger}},
		Services: []ir.Service{{Groups: []ir.OperationGroup{{Operations: []ir.Operation{{
			ID:     "op/list",
			Params: []ir.Parameter{{ID: "param/page", Name: ir.Naming{Source: "page", Canonical: "page"}, Type: ir.TypeRef{Target: "t/prim/integer"}}},
			Bindings: ir.OpBindings{HTTP: []ir.HTTPBinding{{Method: "GET", ParamBindings: []ir.HTTPParamBinding{
				{Param: "param/page", Location: ir.HTTPLocationQuery}}}}},
			Responses:  []ir.Response{{Payload: &ir.Payload{Contents: []ir.Content{{MediaType: "application/json", Type: ir.TypeRef{Target: "t/list"}}}}}},
			Pagination: declared,
		}}}}}},
	}
	c := lowering.New(0, &load.Document{Doc: &soa.OpenAPI{}}, lowering.Policy{})
	assert.Empty(t, pagination.Infer(c, doc, nil))
	op := doc.Services[0].Groups[0].Operations[0]
	assert.Same(t, declared, op.Pagination)
	assert.Equal(t, &ir.Pagination{Strategy: ir.PageStrategyNextLink}, op.Pagination)
	assert.Empty(t, op.Provenance.Inferred)

	doc.Services[0].Groups[0].Operations[0].Pagination = nil
	assert.Empty(t, pagination.Infer(c, doc, map[ir.OpID]bool{"op/list": true}))
	assert.Nil(t, doc.Services[0].Groups[0].Operations[0].Pagination, "an operation its extensions speak of")
	assert.Empty(t, doc.Services[0].Groups[0].Operations[0].Provenance.Inferred)

	assert.Empty(t, pagination.Infer(c, doc, map[ir.OpID]bool{"op/other": true}))
	inferred := doc.Services[0].Groups[0].Operations[0].Pagination
	require.NotNil(t, inferred, "the same operation holding none is inferred")
	assert.Equal(t, ir.PageStrategyPage, inferred.Strategy)
}

// TestInfer_LeavesWhatAnExtensionSpeaksOf compiles GETs taking parameters the
// names read as a walk, beside extensions that speak of a pagination, filling
// one, resolving to nothing, answering false or marking a parameter, a member,
// a header or a shared type, and two that speak of none. Inference fills and
// reports only where none speaks under the options in force, in either
// declaration order (docs/compilers/openapi.md#extension-pagination).
func TestInfer_LeavesWhatAnExtensionSpeaksOf(t *testing.T) {
	t.Parallel()
	const (
		pages = `{name: page, in: query, schema: {type: integer}}, {name: per_page, in: query, schema: {type: integer}}`
		data  = `{type: object, properties: {data: ` + stringList + `}}`
		fern  = `x-fern-pagination: `
		mark  = `x-stainless-pagination-property: {purpose: `
		next  = `{type: object, properties: {data: ` + stringList + `, next: `
	)
	// Each case's last three fields are what it compiles to with every policy
	// on, with promotion off or mapping no pagination target, and with
	// inference off: the strategy, the marker and the codes reported at its
	// path.
	cases := []struct{ name, ext, params, headers, body, on, noPromotion, noInference string }{
		{"filled", fern + `{cursor: $request.page_token, next_cursor: $response.next_page_token, results: $response.data}`,
			`{name: page_token, in: query, schema: {type: string}}, {name: per_page, in: query, schema: {type: integer}}`,
			"", `{type: object, properties: {data: ` + stringList + `, next_page_token: {type: string}}}`,
			"cursor extension-promotion []", "token pagination-inference []", "cursor extension-promotion []"},
		{"failed", fern + `{cursor: $request.cursor, next_cursor: $response.next_cursor, results: $response.data}`,
			pages, "", data, "- - [degraded-construct]", "page pagination-inference []", "- - [degraded-construct]"},
		{"answered", fern + `false`, pages, "", data,
			"- - [degraded-construct]", "page pagination-inference []", "- - [degraded-construct]"},
		{"answeredUnsettled", fern + `false`, pages + `, {name: cursor, in: query, schema: {type: string}}`, "", data,
			"- - [degraded-construct]", "- - [undecided-pagination]", "- - [degraded-construct]"},
		{"markedParam", "", pages + `, {name: after, in: query, schema: {type: string}, ` + mark + `next_cursor_param}}`,
			"", data, "- - [degraded-construct]", "page pagination-inference []", "- - [degraded-construct]"},
		{"markedMember", "", pages, "", next + `{type: string, ` + mark + `next_cursor_field}}}}`,
			"- - [degraded-construct]", "page pagination-inference []", "- - [degraded-construct]"},
		{"markedHeader", "", pages, `headers: {X-Next: {schema: {type: string}, ` + mark + `next_cursor_field}}}, `, data,
			"- - [degraded-construct]", "page pagination-inference []", "- - [degraded-construct]"},
		{"markedType", "", pages, "", next + `{$ref: '#/components/schemas/Cursor'}}}`,
			"- - [degraded-construct]", "page pagination-inference []", "- - [degraded-construct]"},
		{"markedTypeAgain", "", pages, "", next + `{$ref: '#/components/schemas/Cursor'}}}`,
			"- - [degraded-construct]", "page pagination-inference []", "- - [degraded-construct]"},
		{"markedOperation", mark + `next_cursor_param}`, pages, "", data,
			"page pagination-inference []", "page pagination-inference []", "- - []"},
		{"unmarked", "", pages, "", data, "page pagination-inference []", "page pagination-inference []", "- - []"},
	}
	policies := map[string]openapi.Options{
		"on":          {},
		"noPromotion": {Promotions: openapi.ExtensionPromotions{Disabled: true}},
		"noPageTarget": {Promotions: openapi.ExtensionPromotions{Targets: map[openapi.ExtensionTarget][]openapi.ExtensionSource{
			openapi.TargetDeprecation: {{Key: "x-deprecated"}}}}},
		"noInference": {PaginationInference: openapi.PaginationInference{Disabled: true}},
		"neither": {Promotions: openapi.ExtensionPromotions{Disabled: true},
			PaginationInference: openapi.PaginationInference{Disabled: true}},
	}
	for policy, opts := range policies {
		for _, reversed := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s reversed=%v", policy, reversed), func(t *testing.T) {
				t.Parallel()
				paths := make([]string, len(cases))
				for i, tc := range cases {
					paths[i] = getPath(tc.name, tc.ext, tc.params, tc.headers, tc.body)
				}
				if reversed {
					slices.Reverse(paths)
				}
				doc, diags := compile(t, "openapi: 3.1.0\ninfo: {title: t, version: \"1\"}\npaths:\n"+
					strings.Join(paths, "")+"components: {schemas: {Cursor: {type: string, "+mark+"next_cursor_field}}}}\n", opts)
				got := spoken(t, doc, diags)
				for _, tc := range cases {
					want := map[string]string{"on": tc.on, "noPromotion": tc.noPromotion, "noPageTarget": tc.noPromotion,
						"noInference": tc.noInference, "neither": "- - []"}[policy]
					assert.Equal(t, want, got[tc.name], tc.name)
				}
			})
		}
	}
}

// getPath is a path item /name holding one GET, name, carrying the operation
// entry ext, taking the query parameters params and answering 200 with the
// response entries headers beside a JSON body, each written in YAML flow form.
func getPath(name, ext, params, headers, body string) string {
	return `  /` + name + `:
    get:
      operationId: ` + name + `
      ` + ext + `
      parameters: [` + params + `]
      responses:
        "200": {description: ok, ` + headers + `content: {application/json: {schema: ` + body + `}}}
`
}

// spoken renders each GET of doc, by name, as its pagination's strategy, its
// Provenance.Inferred and the codes of diags reported under its path item,
// requiring every finding to sit under one.
func spoken(t *testing.T, doc *ir.Document, diags []ir.Diagnostic) map[string]string {
	t.Helper()
	codes := map[string][]string{}
	for _, d := range diags {
		at, ok := strings.CutPrefix(string(d.Provenance.Pointer), "/paths/~1")
		require.True(t, ok, "a finding outside every path item: %+v", d)
		name, _, _ := strings.Cut(at, "/")
		codes[name] = append(codes[name], strings.TrimPrefix(d.Code, "openapi/"))
	}
	out := map[string]string{}
	for _, op := range doc.Services[0].Groups[0].Operations {
		strategy, marker := "-", string(op.Provenance.Inferred)
		if op.Pagination != nil {
			strategy = string(op.Pagination.Strategy)
		}
		if marker == "" {
			marker = "-"
		}
		slices.Sort(codes[op.Name.Source])
		out[op.Name.Source] = fmt.Sprintf("%s %s %v", strategy, marker, codes[op.Name.Source])
	}
	return out
}

// TestInfer_ReadsTheSameInEitherOrder compiles each document with its
// parameters and members declared in both orders: the walk read and every
// finding are the same, as no choice turns on which was declared first. The
// order oracle compares no operation, so it cannot see this.
func TestInfer_ReadsTheSameInEitherOrder(t *testing.T) {
	t.Parallel()
	member := map[string]string{
		"result": `result: ` + stringList, "data": `data: ` + stringList, "tags": `tags: ` + stringList,
		"token": `pageToken: {type: string}`, "next": `nextPageToken: {type: string}`,
		"info": `info: {type: object, properties: {total_count: {type: integer}, total_pages: {type: integer}}}`,
	}
	param := map[string]string{
		"page": `{name: page, in: query, schema: {type: integer}}`, "token": `{name: pageToken, in: query, schema: {type: string}}`,
		"cursor": `{name: cursor, in: query, schema: {type: string}}`, "size": `{name: per_page, in: query, schema: {type: integer}}`,
		"limit": `{name: limit, in: query, schema: {type: integer}}`,
	}
	for name, tc := range map[string]struct{ params, members []string }{
		"a token beside its next value":  {[]string{"token", "size"}, []string{"token", "result", "next", "info"}},
		"two lists, one named as items":  {[]string{"page", "size"}, []string{"tags", "result", "info"}},
		"two lists, both named as items": {[]string{"page"}, []string{"data", "result"}},
		"two continuations":              {[]string{"page", "cursor"}, []string{"result"}},
		"two page sizes":                 {[]string{"page", "size", "limit"}, []string{"result"}},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			src := func(reversed bool) string {
				ps, ms := slices.Clone(tc.params), slices.Clone(tc.members)
				if reversed {
					slices.Reverse(ps)
					slices.Reverse(ms)
				}
				return listing("["+spell(ps, param)+"]", "{type: object, properties: {"+spell(ms, member)+"}}")
			}
			op, doc, diags := listOp(t, src(false), openapi.Options{})
			rop, rdoc, rdiags := listOp(t, src(true), openapi.Options{})
			assert.Equal(t, describe(doc, op), describe(rdoc, rop))
			if d := cmp.Diff(op.Pagination, rop.Pagination); d != "" {
				t.Errorf("the walk read turns on the declaration order (-as written +reversed):\n%s", d)
			}
			assert.Equal(t, undecided(diags), undecided(rdiags))
		})
	}
}

// TestInfer_ReportsTheSameWhateverTheResponseOrder compiles each document with
// its success responses, and each one's contents, declared in both orders: an
// operation whose bodies stop a walk for several reasons is reported with the
// same one, which turns on no declaration order.
func TestInfer_ReportsTheSameWhateverTheResponseOrder(t *testing.T) {
	t.Parallel()
	const (
		csv     = `text/csv: {schema: {type: string}}`
		noList  = `application/json: {schema: {type: object, properties: {count: {type: integer}}}}`
		results = `application/json: {schema: {type: object, properties: {result: ` + stringList + `}}}`
		data    = `application/json: {schema: {type: object, properties: {data: ` + stringList + `}}}`
		number  = `application/json: {schema: {type: integer}}`
	)
	for name, tc := range map[string]struct {
		responses [][2]string // a status code and its contents, one per line
		want      string
	}{
		"two contents, each holding no page": {[][2]string{{"200", csv + "\n" + noList}},
			`the application/json response body holds no list of items`},
		"two pages at different members beside a body holding none": {
			[][2]string{{"200", results}, {"201", data}, {"202", number}},
			`the application/json response body is neither an object nor a list`},
		"a body holding no page beside one with no body": {[][2]string{{"200", csv}, {"204", ""}},
			`the text/csv response body is neither an object nor a list`},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			src := func(reversed bool) string {
				var b strings.Builder
				b.WriteString(`openapi: 3.1.0
info: {title: t, version: "1"}
paths:
  /things:
    get:
      operationId: listThings
      parameters: [{name: page, in: query, schema: {type: integer}}]
      responses:
`)
				responses := slices.Clone(tc.responses)
				if reversed {
					slices.Reverse(responses)
				}
				for _, r := range responses {
					contents := strings.Split(r[1], "\n")
					if reversed {
						slices.Reverse(contents)
					}
					b.WriteString("        \"" + r[0] + "\":\n          description: d\n")
					if r[1] == "" {
						continue
					}
					b.WriteString("          content:\n")
					for _, c := range contents {
						b.WriteString("            " + c + "\n")
					}
				}
				return b.String()
			}
			_, _, diags := listOp(t, src(false), openapi.Options{})
			_, _, rdiags := listOp(t, src(true), openapi.Options{})
			want := []string{"no pagination is inferred: " + tc.want}
			assert.Equal(t, want, undecided(diags), "as written")
			assert.Equal(t, want, undecided(rdiags), "reversed")
		})
	}
}

// TestInfer_ReadsAMountedDeclarationForEachMount pins a path item mounted at
// two paths: each operation it mounts gets a Pagination of its own, which no
// write to the other's reaches, and one it cannot settle is reported once, at
// the declaration both mounts share.
func TestInfer_ReadsAMountedDeclarationForEachMount(t *testing.T) {
	t.Parallel()
	mounted := func(param string) string { // param is a flow mapping's name and schema entries
		return `openapi: 3.1.0
info: {title: t, version: "1"}
paths:
  /a: {$ref: '#/components/pathItems/Listing'}
  /b: {$ref: '#/components/pathItems/Listing'}
components:
  pathItems:
    Listing:
      get:
        parameters: [{` + param + `, in: query}]
        responses:
          "200": {description: ok, content: {application/json: {schema: ` + envelope + `}}}
`
	}
	doc, _ := compile(t, mounted(`name: page, schema: {type: integer}`), openapi.Options{})
	ops := doc.Services[0].Groups[0].Operations
	require.Len(t, ops, 2)
	for _, op := range ops {
		assert.Equal(t, "page cursor=page limit=- items=/result next=- count=/result_info/total_count "+
			"pages=/result_info/total_pages stop=[]", describe(doc, op), op.ID)
	}
	for _, op := range ops {
		require.NotNil(t, op.Pagination)
		require.NotNil(t, op.Pagination.TotalCount)
	}
	items, count := ops[1].Pagination.Items.Segments[0], ops[1].Pagination.TotalCount.Segments[0]
	ops[0].Pagination.Items.Segments[0] = "p/elsewhere"
	ops[0].Pagination.TotalCount.Segments[0] = "p/elsewhere"
	assert.Equal(t, items, ops[1].Pagination.Items.Segments[0], "each operation holds its own items path")
	assert.Equal(t, count, ops[1].Pagination.TotalCount.Segments[0], "and its own total's")

	_, diags := compile(t, mounted(`name: cursor, schema: {type: string}`), openapi.Options{})
	assert.Equal(t, []string{`no pagination is inferred: the application/json response body holds no member ` +
		`"next_cursor" or "cursor" of the cursor's type`}, undecided(diags))
	got, ok := openapitest.FirstDiag(diags, "openapi/undecided-pagination")
	require.True(t, ok)
	assert.Equal(t, "/components/pathItems/Listing/get", string(got.Provenance.Pointer))
}

// spell joins the spellings keys name in spelled, as a YAML flow collection's
// entries.
func spell(keys []string, spelled map[string]string) string {
	out := make([]string, len(keys))
	for i, k := range keys {
		out[i] = spelled[k]
	}
	return strings.Join(out, ", ")
}
