// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package pagination_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/ir"
)

// TestInferWalks_ReadsTheBodyMembersEveryContentHolds holds a POST whose page
// and page size only one of its request contents holds to no walk and no
// finding: a member some request does not send is no place, so nothing
// triggers the policy. A page sent as a string whose member defaults to a
// whole number is read as a page.
func TestInferWalks_ReadsTheBodyMembersEveryContentHolds(t *testing.T) {
	t.Parallel()
	src := strings.Replace(searchDoc("post", "", searchPage, okResult), "      responses:", `          application/x-www-form-urlencoded:
            schema: {type: object, properties: {query: {type: string}}}
      responses:`, 1)
	for _, written := range inEitherOrder(t, src) {
		doc, diags := compile(t, written, openapi.Options{})
		assert.Nil(t, openapitest.FindOp(t, doc, "search").Pagination)
		assert.Empty(t, diags)
	}

	q := searchRequest
	doc, diags := compile(t, searchDoc("post", "", `page: {type: string, default: "1"}, per_page: {type: integer}`,
		okResult), openapi.Options{})
	assert.Empty(t, diags)
	assert.Equal(t, &ir.Pagination{Strategy: ir.PageStrategyPage, Inferred: true, InputCursor: inputPath("", q+"/page"),
		InputLimit: inputPath("", q+"/per_page"), Items: propPath(bodyProps("search", "post") + "/result")},
		openapitest.FindOp(t, doc, "search").Pagination)
}

// TestInferWalks_SaysWhyAPostIsNoWalk holds the reasons a POST is left with
// none to the places and statuses it names, whatever their order: every query
// parameter naming where its walk continues, and the range of statuses a
// success response with a body answers.
func TestInferWalks_SaysWhyAPostIsNoWalk(t *testing.T) {
	t.Parallel()
	ranged := `"2XX": {description: ok, content: {application/json: {schema: {type: object, properties: {result: ` +
		stringList + `}}}}}`
	for name, tc := range map[string]struct{ src, want string }{
		"two query parameters": {searchDoc("post", `{name: page, in: query, schema: {type: integer}}, `+
			`{name: cursor, in: query, schema: {type: string}}`, `query: {type: string}`, okResult),
			`the operation is a POST, and query parameters "cursor" and "page" each name where its walk continues`},
		"a range of statuses": {searchDoc("post", "", searchPage, ranged), `the operation is a POST answering 200-299`},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			for _, src := range inEitherOrder(t, tc.src) {
				doc, diags := compile(t, src, openapi.Options{})
				assert.Nil(t, openapitest.FindOp(t, doc, "search").Pagination)
				got := undecidedAt(diags, postUndecided)
				require.Len(t, got, 1)
				assert.Contains(t, got[0], tc.want)
			}
		})
	}
}

// TestInferWalks_ReadsALinkWalkOnlyOffAPage holds a link walk's trigger to the
// page's shape and the Link header's: contents holding the page at different
// members are undecided; a root holding no list of items is no page; a Link
// header is required of each success response declaring a body, a bodyless
// one aside, and only of a plain string type.
func TestInferWalks_ReadsALinkWalkOnlyOffAPage(t *testing.T) {
	t.Parallel()
	const link = `Link: {schema: {type: string}}`
	page := `{type: object, properties: {` + itemsList + `, next: ` + uriString + `}}`
	twoContents := strings.Replace(linkWalkDoc("get", "things", "", "", page),
		"content: {application/json: {schema: "+page+"}}",
		"content: {application/json: {schema: "+page+"}, application/xml: {schema: "+page+"}}", 1)
	bodyless := strings.Replace(linkWalkDoc("get", "things", "", link, stringList), "      responses:\n",
		"      responses:\n        \"204\": {description: none}\n", 1)
	for _, tc := range []struct {
		name, src string
		want      *ir.Pagination
		finding   string
	}{
		{"two contents holding the page apart", twoContents, nil,
			"the success responses' bodies hold their pages at different members"},
		{"a root holding no list of items", linkWalkDoc("get", "things", "", "",
			`{type: object, properties: {next: `+uriString+`, count: {type: integer}}}`), nil, ""},
		{"a Link header beside a bodyless response", bodyless, &ir.Pagination{Strategy: ir.PageStrategyLinkHeader,
			Inferred: true, Items: propPath(), Stop: []ir.PageStop{ir.PageStopNoNext}}, ""},
		{"a Link header of no plain string", linkWalkDoc("get", "things", "", `Link: {schema: {type: integer}}`,
			stringList), nil, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			for _, src := range inEitherOrder(t, tc.src) {
				op, _, diags := listOp(t, src, openapi.Options{})
				assert.Equal(t, tc.want, op.Pagination)
				if tc.finding == "" {
					assert.Empty(t, diags)
					continue
				}
				got := undecided(diags)
				require.Len(t, got, 1)
				assert.True(t, strings.HasSuffix(got[0], tc.finding), got[0])
			}
		})
	}
}
