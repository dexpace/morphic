// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package pagination_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/ir"
)

// The bodies and responses the cases below send and answer with.
const (
	searchPage    = `query: {type: string}, page: {type: integer}, per_page: {type: integer}`
	okResult      = `"200": {description: ok, content: {application/json: {schema: {type: object, properties: {result: ` + stringList + `}}}}}`
	postUndecided = "/paths/~1search/post"
	searchRequest = "p/openapi/paths/~1search/post/requestBody/content/application~1json/schema/properties"
)

// searchDoc is a document of one operation, search, of the method taking the
// query parameters params, a JSON request body holding the members body and
// the response entries responses, each written in YAML flow form.
func searchDoc(method, params, body, responses string) string {
	return fmt.Sprintf(`openapi: 3.1.0
info: {title: t, version: "1"}
paths:
  /search:
    %s:
      operationId: search
      parameters: [%s]
      requestBody:
        content:
          application/json:
            schema: {type: object, properties: {%s}}
      responses: {%s}
`, method, params, body, responses)
}

// TestInferWalks_ReadsAPostThatSearches holds a POST whose request body
// carries the page and the page size, and whose response states the items at
// its root, to being a walk whose inputs address the body, in whichever order
// the members are declared: a page walk, or a cursor walk whose next cursor is
// named for the cursor and not as the cursor itself.
func TestInferWalks_ReadsAPostThatSearches(t *testing.T) {
	t.Parallel()
	q, r := searchRequest, bodyProps("search", "post")
	for name, tc := range map[string]struct {
		src  string
		want *ir.Pagination
	}{
		"a page number and a page size": {
			searchDoc("post", "", searchPage, okResult),
			&ir.Pagination{Strategy: ir.PageStrategyPage, Inferred: true, InputCursor: inputPath("", q+"/page"),
				InputLimit: inputPath("", q+"/per_page"), Items: propPath(r + "/result")}},
		"a cursor answered by a next cursor": {
			searchDoc("post", "", `query: {type: string}, cursor: {type: string}`,
				`"200": {description: ok, content: {application/json: {schema: {type: object, properties: {result: `+
					stringList+`, next_cursor: {type: string}}}}}}`),
			&ir.Pagination{Strategy: ir.PageStrategyCursor, Inferred: true, InputCursor: inputPath("", q+"/cursor"),
				Items: propPath(r + "/result"), NextCursor: propPath(r + "/next_cursor"),
				Stop: []ir.PageStop{ir.PageStopNoNext}}},
		"a page number and a page size, the page a bare list": {
			searchDoc("post", "", searchPage, `"200": {description: ok, content: {application/json: {schema: `+stringList+`}}}`),
			&ir.Pagination{Strategy: ir.PageStrategyPage, Inferred: true, InputCursor: inputPath("", q+"/page"),
				InputLimit: inputPath("", q+"/per_page"), Items: propPath()}},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			for _, src := range inEitherOrder(t, tc.src) {
				doc, diags := compile(t, src, openapi.Options{})
				assert.Empty(t, diags)

				op := openapitest.FindOp(t, doc, "search")
				assert.Equal(t, tc.want, op.Pagination)
				assert.Equal(t, lowering.PaginationInferenceHeuristic, op.Provenance.Inferred)
			}
		})
	}
}

// TestInferWalks_LeavesAPostItCannotTellFromACreate holds a POST failing one of
// the conditions that tell a search from a create to no pagination and one
// finding at the operation, each condition its own reason, as a create
// answering with what it was sent would otherwise be read as a walk: the page
// in the query, no page size in the body, a next cursor named as the cursor
// itself, items below the root, and a response that is no 200. A body offset
// is no trigger, and draws nothing.
func TestInferWalks_LeavesAPostItCannotTellFromACreate(t *testing.T) {
	t.Parallel()
	created := `"201": {description: created, content: {application/json: {schema: {type: object, ` +
		`properties: {result: ` + stringList + `}}}}}`
	for _, tc := range []struct {
		name, src string
		condition int
	}{
		{"the page moved to a query parameter",
			searchDoc("post", `{name: page, in: query, schema: {type: integer}}`,
				`query: {type: string}, per_page: {type: integer}`, okResult), 1},
		{"no page size in the body", searchDoc("post", "", `query: {type: string}, page: {type: integer}`, okResult), 3},
		{"the cursor named as the next cursor itself",
			searchDoc("post", "", `query: {type: string}, cursor: {type: string}`,
				`"200": {description: ok, content: {application/json: {schema: {type: object, properties: {result: `+
					stringList+`, cursor: {type: string}}}}}}`), 4},
		{"the items below the root", searchDoc("post", "", searchPage,
			`"200": {description: ok, content: {application/json: {schema: {type: object, properties: `+
				`{result: {type: object, properties: {domains: `+stringList+`}}}}}}}`), 5},
		{"the response a 201", searchDoc("post", "", searchPage, created), 6},
		{"a 200 beside a 201", searchDoc("post", "", searchPage, okResult+", "+created), 6},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			for _, src := range inEitherOrder(t, tc.src) {
				doc, diags := compile(t, src, openapi.Options{})
				assert.Nil(t, openapitest.FindOp(t, doc, "search").Pagination)
				got := undecidedAt(diags, postUndecided)
				require.Len(t, got, 1, "one finding at the operation: %v", diags)
				assert.True(t, strings.HasPrefix(got[0], "no pagination is inferred: "), got[0])
				assert.NotContains(t, got[0], "only a GET or a QUERY is safe to send again", "a POST is read, and has its own reason")
				assert.Len(t, diags, 1)
			}
		})
	}

	t.Run("each condition gives its own reason", func(t *testing.T) {
		t.Parallel()
		byCondition := map[int]string{}
		for _, tc := range []struct {
			condition int
			src       string
		}{
			{1, searchDoc("post", `{name: page, in: query, schema: {type: integer}}`, `query: {type: string}, per_page: {type: integer}`, okResult)},
			{3, searchDoc("post", "", `query: {type: string}, page: {type: integer}`, okResult)},
			{4, searchDoc("post", "", `cursor: {type: string}`,
				`"200": {description: ok, content: {application/json: {schema: {type: object, properties: {result: `+
					stringList+`, cursor: {type: string}}}}}}`)},
			{5, searchDoc("post", "", searchPage, `"200": {description: ok, content: {application/json: {schema: {type: object, `+
				`properties: {result: {type: object, properties: {domains: `+stringList+`}}}}}}}`)},
			{6, searchDoc("post", "", searchPage, created)},
		} {
			_, diags := compile(t, tc.src, openapi.Options{})
			got := undecidedAt(diags, postUndecided)
			require.Len(t, got, 1)
			byCondition[tc.condition] = got[0]
		}
		seen := map[string]int{}
		for condition, message := range byCondition {
			if other, ok := seen[message]; ok {
				assert.Failf(t, "one reason for two conditions", "conditions %d and %d both give %q", other, condition, message)
			}
			seen[message] = condition
		}
	})

	t.Run("a body offset beside a limit is no trigger and draws nothing", func(t *testing.T) {
		t.Parallel()
		src := searchDoc("post", "", `query: {type: string}, offset: {type: integer}, limit: {type: integer}`, okResult)
		for _, written := range inEitherOrder(t, src) {
			doc, diags := compile(t, written, openapi.Options{})
			assert.Nil(t, openapitest.FindOp(t, doc, "search").Pagination)
			assert.Empty(t, diags)
		}
	})
}

// TestInferWalks_ReadsNoBodyOfAMethodThatIsNoSearch holds a PUT, a PATCH and a
// DELETE, whose bodies state what to store, change or remove, to never being
// read as a walk, whatever members the body names.
func TestInferWalks_ReadsNoBodyOfAMethodThatIsNoSearch(t *testing.T) {
	t.Parallel()
	for _, method := range []string{"put", "patch", "delete"} {
		t.Run(method, func(t *testing.T) {
			t.Parallel()
			doc, _ := compile(t, searchDoc(method, "", searchPage, okResult), openapi.Options{})
			assert.Nil(t, openapitest.FindOp(t, doc, "search").Pagination)
		})
	}
}

// objectParam is a deepObject query parameter name holding the members.
func objectParam(name, members string) string {
	return `{name: ` + name + `, in: query, style: deepObject, schema: {type: object, properties: {` + members + `}}}`
}

// TestInferWalks_ReadsAnObjectParameter holds a walk an object query
// parameter carries to being read through its members, the page and the page
// size each a member of the parameter, whichever way the parameter is bound
// and in whichever order its members are declared; a parameter naming no
// continuation among its members draws nothing.
func TestInferWalks_ReadsAnObjectParameter(t *testing.T) {
	t.Parallel()
	const members = `page: {type: integer}, per_page: {type: integer}`
	list := `{type: object, properties: {items: ` + stringList + `}}`
	querystring := `openapi: 3.2.0
info: {title: t, version: "1"}
paths:
  /things:
    get:
      operationId: listThings
      parameters:
        - name: paging
          in: querystring
          content:
            application/x-www-form-urlencoded:
              schema: {type: object, properties: {` + members + `}}
      responses:
        "200": {description: ok, content: {application/json: {schema: ` + list + `}}}
`
	for name, tc := range map[string]struct{ src, param, schema string }{
		"a deepObject parameter": {listing(`[`+objectParam("paging", members)+`]`, list),
			"param/openapi/paths/~1things/get/parameters/paging/query",
			"p/openapi/paths/~1things/get/parameters/0/schema/properties/"},
		"a querystring parameter": {querystring,
			"param/openapi/paths/~1things/get/parameters/paging/querystring",
			"p/openapi/paths/~1things/get/parameters/0/content/application~1x-www-form-urlencoded/schema/properties/"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			for _, src := range inEitherOrder(t, tc.src) {
				op, _, diags := listOp(t, src, openapi.Options{})
				assert.Empty(t, diags)
				assert.Equal(t, &ir.Pagination{
					Strategy:    ir.PageStrategyPage,
					Inferred:    true,
					InputCursor: inputPath(tc.param, tc.schema+"page"),
					InputLimit:  inputPath(tc.param, tc.schema+"per_page"),
					Items:       propPath(bodyProps("things", "get") + "/items"),
				}, op.Pagination)
			}
		})
	}

	t.Run("a parameter whose members and name name no continuation", func(t *testing.T) {
		t.Parallel()
		op, _, diags := listOp(t, listing(`[`+objectParam("filter", `colour: {type: string}, size: {type: string}`)+`]`, list),
			openapi.Options{})
		assert.Nil(t, op.Pagination)
		assert.Empty(t, diags)
	})
}

// TestInferWalks_ReadsAnObjectParameterNamedForARole holds an object
// parameter that is itself named for a role, as a JSON:API page object is, to
// being read through its members and not by its own name: its offset and its
// limit are one walk, and the parameter is no page beside them. A page object
// whose members name no continuation is the type no walk sends, as before.
func TestInferWalks_ReadsAnObjectParameterNamedForARole(t *testing.T) {
	t.Parallel()
	article := func(members string) string {
		return `openapi: 3.1.0
info: {title: t, version: "1"}
paths:
  /articles:
    get:
      operationId: listArticles
      parameters:
        - {name: page, in: query, style: deepObject, schema: {type: object, properties: {` + members + `}}}
      responses:
        "200":
          description: ok
          content:
            application/vnd.api+json:
              schema: {type: object, properties: {data: {type: array, items: {type: string}}}}
`
	}
	for _, src := range inEitherOrder(t, article(`offset: {type: integer}, limit: {type: integer}`)) {
		doc, diags := compile(t, src, openapi.Options{})
		assert.Empty(t, diags)

		param := "param/openapi/paths/~1articles/get/parameters/page/query"
		schema := "p/openapi/paths/~1articles/get/parameters/0/schema/properties/"
		assert.Equal(t, &ir.Pagination{
			Strategy:    ir.PageStrategyOffset,
			Inferred:    true,
			InputCursor: inputPath(param, schema+"offset"),
			InputLimit:  inputPath(param, schema+"limit"),
			Items: propPath("p/openapi/paths/~1articles/get/responses/200/content/application~1vnd.api+json/" +
				"schema/properties/data"),
		}, openapitest.FindOp(t, doc, "listArticles").Pagination)
	}

	doc, diags := compile(t, article(`filter: {type: string}, sort: {type: string}`), openapi.Options{})
	assert.Nil(t, openapitest.FindOp(t, doc, "listArticles").Pagination)
	assert.Equal(t, []string{`no pagination is inferred: query parameter "page" names where a walk continues, and ` +
		`its type is no plain number or string`}, undecided(diags))
}

// TestInferWalks_TwoPlacesAreUndecided holds two places naming where a walk
// continues, or the page size, to leaving the operation with no pagination
// and one finding naming each, however the two are declared and wherever each
// sits: a query parameter beside a member of an object parameter, and one
// beside a member of the request body of a POST.
func TestInferWalks_TwoPlacesAreUndecided(t *testing.T) {
	t.Parallel()
	const members = `page: {type: integer}, per_page: {type: integer}`
	list := `{type: object, properties: {items: ` + stringList + `}}`
	cursor := `{name: cursor, in: query, schema: {type: string}}`
	object := objectParam("paging", members)
	for name, tc := range map[string]struct{ params, want string }{
		"a parameter declared before the object": {cursor + `, ` + object,
			`query parameter "cursor" and member "page" of query parameter "paging" each name where a walk continues`},
		"a parameter declared after the object": {object + `, ` + cursor,
			`query parameter "cursor" and member "page" of query parameter "paging" each name where a walk continues`},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			for _, src := range inEitherOrder(t, listing(`[`+tc.params+`]`, list)) {
				op, _, diags := listOp(t, src, openapi.Options{})
				assert.Nil(t, op.Pagination)
				assert.Equal(t, []string{"no pagination is inferred: " + tc.want}, undecided(diags))
			}
		})
	}

	t.Run("a page size named twice", func(t *testing.T) {
		t.Parallel()
		src := listing(`[`+object+`, {name: limit, in: query, schema: {type: integer}}]`, list)
		op, _, diags := listOp(t, src, openapi.Options{})
		assert.Nil(t, op.Pagination)
		got := undecided(diags)
		require.Len(t, got, 1)
		assert.Contains(t, got[0], `member "per_page" of query parameter "paging"`)
		assert.Contains(t, got[0], `query parameter "limit"`)
	})

	t.Run("a query parameter and a member of a POST's body", func(t *testing.T) {
		t.Parallel()
		src := searchDoc("post", `{name: cursor, in: query, schema: {type: string}}`, searchPage, okResult)
		for _, written := range inEitherOrder(t, src) {
			doc, diags := compile(t, written, openapi.Options{})
			assert.Nil(t, openapitest.FindOp(t, doc, "search").Pagination)
			got := undecidedAt(diags, postUndecided)
			require.Len(t, got, 1)
			assert.Contains(t, got[0], `query parameter "cursor"`)
			assert.Contains(t, got[0], `request body member "page"`)
		}
	})
}
