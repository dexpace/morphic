// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package pagination_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/ir"
)

// The parameters and members the flag cases combine.
const (
	cursorParam = `{name: cursor, in: query, schema: {type: string}}`
	resultList  = `result: {type: array, items: {type: string}}`
)

// changesDoc is a document of one GET, listChanges, taking the query
// parameters params and answering 200 with a JSON object holding the members
// props, each written in YAML flow form.
func changesDoc(params, props string) string {
	return `openapi: 3.1.0
info: {title: t, version: "1"}
paths:
  /changes:
    get:
      operationId: listChanges
      parameters: [` + params + `]
      responses:
        "200":
          description: ok
          content:
            application/json:
              schema:
                type: object
                properties: {` + props + `}
`
}

// resultInfo is the member result_info, an object holding members.
func resultInfo(members ...string) string {
	return `result_info: {type: object, properties: {` + strings.Join(members, ", ") + `}}`
}

// TestInferWalks_EndsACursorWalkAtItsFlag holds a cursor walk whose page
// states a flag beside its next cursor to ending at that flag, and listing no
// other stop, in whichever order the members are declared: the page's own
// statement of where the walk ends is what a pager reads, not a cursor it
// keeps handing back.
func TestInferWalks_EndsACursorWalkAtItsFlag(t *testing.T) {
	t.Parallel()
	src := changesDoc(cursorParam, resultList+", "+resultInfo(
		`next_cursor: {type: string, description: Where the next page or a later poll resumes.}`,
		`has_more: {type: boolean, description: True when another page follows.}`))
	for i, written := range inEitherOrder(t, src) {
		t.Run([]string{"as written", "every mapping reversed"}[i], func(t *testing.T) {
			t.Parallel()
			doc, diags := compile(t, written, openapi.Options{})
			assert.Empty(t, diags)

			op := openapitest.FindOp(t, doc, "listChanges")
			r, cursor := bodyProps("changes", "get"), queryParam("changes", "cursor")
			assert.Equal(t, &ir.Pagination{
				Strategy:    ir.PageStrategyCursor,
				Inferred:    true,
				InputCursor: inputPath(cursor),
				Items:       propPath(r + "/result"),
				NextCursor:  propPath(r+"/result_info", r+"/result_info/properties/next_cursor"),
				HasMore:     propPath(r+"/result_info", r+"/result_info/properties/has_more"),
				Stop:        []ir.PageStop{ir.PageStopNoMore},
			}, op.Pagination)
			assert.Equal(t, lowering.PaginationInferenceHeuristic, op.Provenance.Inferred)
		})
	}
}

// flagCase is one page whose members the flag cases read: the page's members
// besides the items, the continuation the operation takes, and what is read of
// it. next and flag are paths of member IDs below the response body's members.
// A walk reading a flag ends by it alone; one reading none ends at its next
// cursor, or, where finding is set, is left with no pagination and one finding
// ending in it.
type flagCase struct {
	params  string
	root    string
	finding string
	next    []string
	flag    []string
}

// TestInferWalks_FlagNames pins which boolean a cursor walk reads as its flag:
// the one member of the page's root, or of a model one of its members holds,
// that is named for one, whatever its spelling; two so named, wherever they
// sit, are undecided, named by their paths and in sorted order, in both
// declaration orders; and a boolean named otherwise beside the next cursor is
// undecided, one further down, or of another type, is no flag. A page walk
// reads none, as it ends at an empty page whatever a flag says.
func TestInferWalks_FlagNames(t *testing.T) {
	t.Parallel()
	const (
		undecidedTail = `, each able to end the walk`
		infoNext      = "result_info/properties/next_cursor"
	)
	bool1 := func(name string) string { return name + `: {type: boolean}` }
	cursorInInfo := []string{"result_info", infoNext}
	for name, tc := range map[string]flagCase{
		"has_more spelled in camel case": {
			cursorParam, resultList + ", " + resultInfo(`next_cursor: {type: string}`, bool1("hasMore")), "", cursorInInfo,
			[]string{"result_info", "result_info/properties/hasMore"}},
		"is_truncated": {
			cursorParam, resultList + ", " + resultInfo(`next_cursor: {type: string}`, bool1("is_truncated")), "", cursorInInfo,
			[]string{"result_info", "result_info/properties/is_truncated"}},
		"has_next": {
			cursorParam, resultList + ", " + resultInfo(`next_cursor: {type: string}`, bool1("has_next")), "", cursorInInfo,
			[]string{"result_info", "result_info/properties/has_next"}},
		"has_next_page": {
			cursorParam, resultList + ", " + resultInfo(`next_cursor: {type: string}`, bool1("has_next_page")), "", cursorInInfo,
			[]string{"result_info", "result_info/properties/has_next_page"}},
		"a flag beside a boolean named otherwise": {
			cursorParam, resultList + ", " + resultInfo(`next_cursor: {type: string}`, bool1("has_more"),
				bool1("total_count_is_exact")), "", cursorInInfo,
			[]string{"result_info", "result_info/properties/has_more"}},
		"a flag beside an unnamed boolean, of an object holding the cursor": {
			cursorParam, resultList + ", " + resultInfo(bool1("more"), `cursor: {type: string}`, bool1("is_truncated")), "",
			[]string{"result_info", "result_info/properties/cursor"},
			[]string{"result_info", "result_info/properties/is_truncated"}},
		"a flag at the root, the cursor in an object": {
			cursorParam, resultList + ", " + bool1("has_more") + ", " + resultInfo(`next_cursor: {type: string}`), "",
			cursorInInfo, []string{"has_more"}},
		"the one flag, in an object that holds no cursor": {
			cursorParam, resultList + ", " + resultInfo(`next_cursor: {type: string}`) + ", " +
				`search: {type: object, properties: {query: {type: string}, has_more: {type: boolean}}}`, "",
			cursorInInfo, []string{"search", "search/properties/has_more"}},
		"a boolean named for none": {
			cursorParam, resultList + ", " + resultInfo(`next_cursor: {type: string}`, bool1("truncated")),
			`holds "truncated" beside its next cursor, a boolean that may end the walk under a name the policy does ` +
				`not read`, nil, nil},
		"two flags of one object": {
			cursorParam, resultList + ", " + resultInfo(`next_cursor: {type: string}`, bool1("has_more"), bool1("is_truncated")),
			`holds flags "result_info.has_more" and "result_info.is_truncated"` + undecidedTail, nil, nil},
		"one name at two places": {
			cursorParam, resultList + ", " + resultInfo(`next_cursor: {type: string}`, bool1("has_more")) + ", " +
				`search: {type: object, properties: {query: {type: string}, has_more: {type: boolean}}}`,
			`holds flags "result_info.has_more" and "search.has_more"` + undecidedTail, nil, nil},
		"a flag named for none besides the named one, beside the cursor": {
			cursorParam, resultList + ", " + resultInfo(`next_cursor: {type: string}`, bool1("truncated"), bool1("has_more")), "",
			cursorInInfo, []string{"result_info", "result_info/properties/has_more"}},
		"a flag two objects down, which is read as none": {
			cursorParam, resultList + ", " + resultInfo(`next_cursor: {type: string}`,
				`deeper: {type: object, properties: {has_more: {type: boolean}}}`), "", cursorInInfo, nil},
		"a member named for a flag that is no boolean": {
			cursorParam, resultList + ", " + resultInfo(`next_cursor: {type: string}`, `has_more: {type: string}`), "",
			cursorInInfo, nil},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			findings := make([]string, 0, 2)
			for _, src := range inEitherOrder(t, changesDoc(tc.params, tc.root)) {
				doc, diags := compile(t, src, openapi.Options{})
				op := openapitest.FindOp(t, doc, "listChanges")
				got := undecidedAt(diags, "/paths/~1changes/get")
				if tc.finding != "" {
					assert.Nil(t, op.Pagination)
					require.Len(t, got, 1, "one finding, wherever each member is declared")
					assert.True(t, strings.HasPrefix(got[0], "no pagination is inferred: "), got[0])
					assert.True(t, strings.HasSuffix(got[0], tc.finding), got[0])
					findings = append(findings, got[0])
					continue
				}
				assert.Empty(t, diags)
				assertFlagWalk(t, op, tc)
			}
			if tc.finding != "" {
				require.Len(t, findings, 2)
				assert.Equal(t, findings[0], findings[1], "the same finding in both declaration orders")
			}
		})
	}
}

// assertFlagWalk holds op's pagination to the cursor walk tc describes.
func assertFlagWalk(t *testing.T, op ir.Operation, tc flagCase) {
	t.Helper()
	r, cursor := bodyProps("changes", "get"), queryParam("changes", "cursor")
	under := func(rel []string) []string {
		out := make([]string, len(rel))
		for i, id := range rel {
			out[i] = r + "/" + id
		}
		return out
	}
	want := &ir.Pagination{
		Strategy:    ir.PageStrategyCursor,
		Inferred:    true,
		InputCursor: inputPath(cursor),
		Items:       propPath(r + "/result"),
		NextCursor:  propPath(under(tc.next)...),
		Stop:        []ir.PageStop{ir.PageStopNoNext},
	}
	if tc.flag != nil {
		want.HasMore = propPath(under(tc.flag)...)
		want.Stop = []ir.PageStop{ir.PageStopNoMore}
	}
	assert.Equal(t, want, op.Pagination)
	assert.Equal(t, lowering.PaginationInferenceHeuristic, op.Provenance.Inferred)
}

// TestInferWalks_ReadsNoFlagForAPageWalk holds a page or an offset walk, which
// ends at a page holding no items whatever a flag says, to reading no flag
// and listing no stop, though the page states one a cursor walk would end at.
func TestInferWalks_ReadsNoFlagForAPageWalk(t *testing.T) {
	t.Parallel()
	for name, param := range map[string]string{
		"a page number": `{name: page, in: query, schema: {type: integer}}`,
		"an offset":     `{name: offset, in: query, schema: {type: integer}}`,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			src := changesDoc(param, resultList+", "+resultInfo(`has_more: {type: boolean}`))
			for _, written := range inEitherOrder(t, src) {
				doc, diags := compile(t, written, openapi.Options{})
				assert.Empty(t, diags)

				op := openapitest.FindOp(t, doc, "listChanges")
				require.NotNil(t, op.Pagination)
				assert.Nil(t, op.Pagination.HasMore, "an empty page ends the walk, whatever the flag says")
				assert.Empty(t, op.Pagination.Stop)
				assert.Contains(t, []ir.PageStrategy{ir.PageStrategyPage, ir.PageStrategyOffset}, op.Pagination.Strategy)
			}
		})
	}
}

// TestPromote_FillsTheFlag holds a pagination an extension states, naming the
// flag among its values, to filling HasMore from it and ending the walk by it
// alone: the source states the flag, which is the stop.
func TestPromote_FillsTheFlag(t *testing.T) {
	t.Parallel()
	src := changesDoc(cursorParam, resultList+", "+resultInfo(`next_cursor: {type: string}`, `has_more: {type: boolean}`))
	src = strings.Replace(src, "      operationId: listChanges\n", `      operationId: listChanges
      x-page:
        cursor: $request.cursor
        next: $response.result_info.next_cursor
        items: $response.result
        more: $response.result_info.has_more
`, 1)
	policy := openapi.ExtensionPromotions{Targets: map[openapi.ExtensionTarget][]openapi.ExtensionSource{
		openapi.TargetPaginationCursor:     {{Key: "x-page", Member: "cursor"}},
		openapi.TargetPaginationNextCursor: {{Key: "x-page", Member: "next"}},
		openapi.TargetPaginationItems:      {{Key: "x-page", Member: "items"}},
		openapi.TargetPaginationHasMore:    {{Key: "x-page", Member: "more"}},
	}}
	for _, written := range inEitherOrder(t, src) {
		doc, diags := compile(t, written, openapi.Options{Promotions: policy})
		assert.Empty(t, diags)

		op := openapitest.FindOp(t, doc, "listChanges")
		r, cursor := bodyProps("changes", "get"), queryParam("changes", "cursor")
		assert.Equal(t, &ir.Pagination{
			Strategy:    ir.PageStrategyCursor,
			Inferred:    true,
			InputCursor: inputPath(cursor),
			Items:       propPath(r + "/result"),
			NextCursor:  propPath(r+"/result_info", r+"/result_info/properties/next_cursor"),
			HasMore:     propPath(r+"/result_info", r+"/result_info/properties/has_more"),
			Stop:        []ir.PageStop{ir.PageStopNoMore},
		}, op.Pagination)
		assert.Equal(t, ir.Inferred("extension-promotion"), op.Provenance.Inferred)
	}
}
