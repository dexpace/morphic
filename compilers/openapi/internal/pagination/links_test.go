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
	"github.com/dexpace/morphic/ir"
)

// The parameters and members the link cases combine.
const (
	pageParam     = `{name: page, in: query, schema: {type: integer}}`
	pageOnlyParam = `[` + pageParam + `]`
	uriString     = `{type: string, format: uri}`
	itemsList     = `items: ` + stringList
)

// TestInferWalks_ReadsLinks holds the next and previous links a page states,
// typed as URLs, to being read as the walk's links beside the strategy the
// parameter names, in either declaration order; a plain string named next is
// no link, and the previous link is read without it.
func TestInferWalks_ReadsLinks(t *testing.T) {
	t.Parallel()
	page := func(next string) string {
		return `{type: object, properties: {result: ` + stringList + `, result_info: {type: object, properties: {` +
			`next: ` + next + `, previous: ` + uriString + `}}}}`
	}
	r := bodyProps("things", "get")
	for name, tc := range map[string]struct {
		body string
		next *ir.PropPath
	}{
		"both typed as URLs":  {page(uriString), propPath(r+"/result_info", r+"/result_info/properties/next")},
		"a plain string next": {page(`{type: string}`), nil},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			for _, src := range inEitherOrder(t, listing(pageOnlyParam, tc.body)) {
				op, _, diags := listOp(t, src, openapi.Options{})
				assert.Empty(t, diags)
				assert.Equal(t, &ir.Pagination{
					Strategy:    ir.PageStrategyPage,
					Inferred:    true,
					InputCursor: inputPath(queryParam("things", "page")),
					Items:       propPath(r + "/result"),
					NextLink:    tc.next,
					PrevLink:    propPath(r+"/result_info", r+"/result_info/properties/previous"),
				}, op.Pagination)
			}
		})
	}
}

// heldApart is a document of one GET, listThings, taking a page number and
// answering with a JSON page and, when xml is set, an XML one, each composing
// one model of the items and declaring its next link of its own.
func heldApart(xml bool) string {
	src := `openapi: 3.1.0
info: {title: t, version: "1"}
paths:
  /things:
    get:
      operationId: listThings
      parameters: [{name: page, in: query, schema: {type: integer}}]
      responses:
        "200":
          description: ok
          content:
            application/json: {schema: {$ref: '#/components/schemas/JSONPage'}}
            application/xml: {schema: {$ref: '#/components/schemas/XMLPage'}}
components:
  schemas:
    Base: {type: object, properties: {result: {type: array, items: {type: string}}}}
    JSONPage: {type: object, allOf: [{$ref: '#/components/schemas/Base'}], properties: {next: {type: string, format: uri}}}
    XMLPage: {type: object, allOf: [{$ref: '#/components/schemas/Base'}], properties: {next: {type: string, format: uri}}}
`
	if xml {
		return src
	}
	return strings.Replace(src, "            application/xml: {schema: {$ref: '#/components/schemas/XMLPage'}}\n", "", 1)
}

// TestInferWalks_ALinkHeldApartIsLeftUnset holds a page whose contents share
// their items and each declare their next link at a member of their own to
// the walk it has beside any link, and no link: the pager reads whichever
// content arrives, so a link only one holds is no link of the walk. With one
// content left, its link is read.
func TestInferWalks_ALinkHeldApartIsLeftUnset(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		xml  bool
		next *ir.PropPath
	}{
		"two contents": {true, nil},
		"one content":  {false, propPath("p/openapi/components/schemas/JSONPage/properties/next")},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			for _, src := range inEitherOrder(t, heldApart(tc.xml)) {
				op, _, diags := listOp(t, src, openapi.Options{})
				assert.Empty(t, diags)
				assert.Equal(t, &ir.Pagination{
					Strategy:    ir.PageStrategyPage,
					Inferred:    true,
					InputCursor: inputPath(queryParam("things", "page")),
					Items:       propPath("p/openapi/components/schemas/Base/properties/result"),
					NextLink:    tc.next,
				}, op.Pagination)
			}
		})
	}
}

// TestInferWalks_NextNames holds a member named next, beside a cursor whose
// type is a number, to being the next cursor, and one beside a string cursor
// to being undecided: a plain string may be the cursor or a link.
func TestInferWalks_NextNames(t *testing.T) {
	t.Parallel()
	r := bodyProps("things", "get")
	number := listing(`[{name: cursor, in: query, schema: {type: integer}}]`,
		`{type: object, properties: {`+itemsList+`, next: {type: integer}}}`)
	for _, src := range inEitherOrder(t, number) {
		op, _, diags := listOp(t, src, openapi.Options{})
		assert.Empty(t, diags)
		assert.Equal(t, &ir.Pagination{
			Strategy:    ir.PageStrategyCursor,
			Inferred:    true,
			InputCursor: inputPath(queryParam("things", "cursor")),
			Items:       propPath(r + "/items"),
			NextCursor:  propPath(r + "/next"),
			Stop:        []ir.PageStop{ir.PageStopNoNext},
		}, op.Pagination)
	}

	str := listing(`[{name: cursor, in: query, schema: {type: string}}]`,
		`{type: object, properties: {`+itemsList+`, next: {type: string}}}`)
	for _, src := range inEitherOrder(t, str) {
		op, _, diags := listOp(t, src, openapi.Options{})
		assert.Nil(t, op.Pagination)
		got := undecided(diags)
		require.Len(t, got, 1)
		assert.True(t, strings.HasSuffix(got[0], `holds "next", a plain string that may be its next cursor or a link`), got[0])
	}
}

// headerDoc is a document of one GET, listThings, taking a cursor, whose 200
// response declares the headers header entries, a JSON body body and, where
// others is not empty, the further response entries others.
func headerDoc(headers, body, others string) string {
	return fmt.Sprintf(`openapi: 3.1.0
info: {title: t, version: "1"}
paths:
  /things:
    get:
      operationId: listThings
      parameters: [{name: cursor, in: query, schema: {type: string}}]
      responses:
        "200": {description: ok, headers: {%s}, content: {application/json: {schema: %s}}}
        %s
`, headers, body, others)
}

// TestInferWalks_ReadsAHeaderCursor holds a cursor a response header hands
// back to being the walk's next cursor, found under the header's name with a
// leading X- cut, and one the body names as well to being undecided: a pager
// cannot tell which of the two to follow.
func TestInferWalks_ReadsAHeaderCursor(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"X-Next-Cursor", "X-Cursor", "Next-Cursor"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			src := headerDoc(name+`: {schema: {type: string}}`, stringList, "")
			op, _, diags := listOp(t, src, openapi.Options{})
			assert.Empty(t, diags)
			assert.Equal(t, &ir.Pagination{
				Strategy:    ir.PageStrategyCursor,
				Inferred:    true,
				InputCursor: inputPath(queryParam("things", "cursor")),
				Items:       propPath(),
				NextCursor: &ir.PropPath{In: "header", Segments: []ir.PropID{
					ir.PropID("p/openapi/paths/~1things/get/responses/200/headers/" + name)}},
				Stop: []ir.PageStop{ir.PageStopNoNext},
			}, op.Pagination)
			assert.Equal(t, lowering.PaginationInferenceHeuristic, op.Provenance.Inferred)
		})
	}

	t.Run("a cursor in the body and in a header", func(t *testing.T) {
		t.Parallel()
		src := headerDoc(`X-Next-Cursor: {schema: {type: string}}`,
			`{type: object, properties: {result: `+stringList+`, next_cursor: {type: string}}}`, "")
		for _, written := range inEitherOrder(t, src) {
			op, _, diags := listOp(t, written, openapi.Options{})
			assert.Nil(t, op.Pagination)
			got := undecided(diags)
			require.Len(t, got, 1)
			assert.True(t, strings.HasSuffix(got[0], `names its next cursor in the body and in response header "X-Next-Cursor"`), got[0])
		}
	})

	t.Run("a header no cursor is named for", func(t *testing.T) {
		t.Parallel()
		op, _, diags := listOp(t, headerDoc(`X-Request-Id: {schema: {type: string}}`, stringList, ""), openapi.Options{})
		assert.Nil(t, op.Pagination)
		assert.Len(t, undecided(diags), 1, "as before: a list holds no next cursor")
	})

	t.Run("a header one success response declares and another does not", func(t *testing.T) {
		t.Parallel()
		others := `"203": {description: ok, content: {application/json: {schema: ` + stringList + `}}}`
		op, _, _ := listOp(t, headerDoc(`X-Next-Cursor: {schema: {type: string}}`, stringList, others), openapi.Options{})
		assert.Nil(t, op.Pagination, "a pager reading whichever response arrives has no cursor from the other")
	})
}

// linkWalkDoc is a document of one operation, listThings, of the method at
// path, taking the parameters params, declaring the response headers headers
// and answering 200 with the JSON body body, each written in YAML flow form.
func linkWalkDoc(method, path, params, headers, body string) string {
	return fmt.Sprintf(`openapi: 3.1.0
info: {title: t, version: "1"}
paths:
  /%s:
    %s:
      operationId: listThings
      parameters: [%s]
      responses:
        "200": {description: ok, headers: {%s}, content: {application/json: {schema: %s}}}
`, path, method, params, headers, body)
}

// TestInferWalks_LinkWalks holds an operation naming no input to what its
// response states: a link walk only where the response looks like a page and
// nothing else, its root holding the items, the links and counts, and no
// other member, a resource that links to its neighbour being none, and a link
// walk on no method but a GET or a QUERY, beside no input that continues it.
func TestInferWalks_LinkWalks(t *testing.T) {
	t.Parallel()
	const link = `Link: {schema: {type: string}}`
	pageB := `{type: object, properties: {` + itemsList + `, next: ` + uriString + `}}`
	r := bodyProps("things", "get")
	paged := func(strategy ir.PageStrategy, items *ir.PropPath, next, prev *ir.PropPath) *ir.Pagination {
		return &ir.Pagination{Strategy: strategy, Inferred: true, Items: items, NextLink: next, PrevLink: prev,
			Stop: []ir.PageStop{ir.PageStopNoNext}}
	}
	for _, tc := range []struct {
		name, src string
		want      *ir.Pagination
		finding   bool
	}{
		{"a root of values, a count and a link", linkWalkDoc("get", "things", "", "",
			`{type: object, properties: {value: `+stringList+`, count: {type: integer}, nextLink: {type: string}}}`),
			paged(ir.PageStrategyNextLink, propPath(r+"/value"), propPath(r+"/nextLink"), nil), false},
		{"a root of items and a next link typed as a URL", linkWalkDoc("get", "things", "", "", pageB),
			paged(ir.PageStrategyNextLink, propPath(r+"/items"), propPath(r+"/next"), nil), false},
		{"a next and a previous link by their long names", linkWalkDoc("get", "things", "", "",
			`{type: object, properties: {`+itemsList+`, next_link: {type: string}, previous_link: {type: string}}}`),
			paged(ir.PageStrategyNextLink, propPath(r+"/items"), propPath(r+"/next_link"), propPath(r+"/previous_link")), false},
		{"a chapter linking to its neighbours", linkWalkDoc("get", "books/{book}/chapters/{n}",
			`{name: book, in: path, required: true, schema: {type: string}}, `+
				`{name: n, in: path, required: true, schema: {type: integer}}`, "",
			`{type: object, properties: {title: {type: string}, paragraphs: `+stringList+`, next: `+uriString+
				`, previous: `+uriString+`}}`), nil, false},
		{"a root holding a string id", linkWalkDoc("get", "things", "", "",
			`{type: object, properties: {id: {type: string}, `+itemsList+`, next: `+uriString+`}}`), nil, false},
		{"a root holding an integer id", linkWalkDoc("get", "things", "", "",
			`{type: object, properties: {id: {type: integer}, `+itemsList+`, next: `+uriString+`}}`), nil, false},
		{"a count that is a string", linkWalkDoc("get", "things", "", "",
			`{type: object, properties: {count: {type: string}, `+itemsList+`, next: `+uriString+`}}`), nil, false},
		{"a link one object down", linkWalkDoc("get", "things", "", "",
			`{type: object, properties: {`+itemsList+`, paging: {type: object, properties: {next: `+uriString+`}}}}`),
			nil, false},
		{"a page of a POST", linkWalkDoc("post", "things", "", "", pageB), nil, false},
		{"a Link header beside a list", linkWalkDoc("get", "things", "", link, stringList),
			paged(ir.PageStrategyLinkHeader, propPath(), nil, nil), false},
		{"a Link header beside a body that is no page", linkWalkDoc("get", "orgs/{org}/cache/usage",
			`{name: org, in: path, required: true, schema: {type: string}}`, link,
			`{type: object, properties: {total_count: {type: integer}, size_in_bytes: {type: integer}}}`), nil, false},
		{"a page with a next link and a Link header", linkWalkDoc("get", "things", "", link, pageB), nil, true},
		{"a Link header beside a page number", linkWalkDoc("get", "things", pageParam, link,
			stringList), &ir.Pagination{Strategy: ir.PageStrategyPage, Inferred: true,
			InputCursor: inputPath(queryParam("things", "page")), Items: propPath()}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			for _, src := range inEitherOrder(t, tc.src) {
				op, _, diags := listOp(t, src, openapi.Options{})
				assert.Equal(t, tc.want, op.Pagination)
				if tc.want != nil {
					assert.Equal(t, lowering.PaginationInferenceHeuristic, op.Provenance.Inferred)
				}
				if tc.finding {
					assert.Len(t, diags, 1)
					assert.Len(t, undecided(diags), 1, "one finding, as the response states two continuations")
					continue
				}
				assert.Empty(t, diags, "a response that is no page triggers nothing and draws no finding")
			}
		})
	}
}
