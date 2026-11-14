// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package pagination_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/ir"
)

// rootWalk is a pagination key at a document's root stating a cursor walk.
const rootWalk = `x-fern-pagination:
  cursor: $request.page_token
  next_cursor: $response.next_page_token
  results: $response.data
`

// rootDoc is a document of the paths /things and, where more is set, /other,
// each one GET whose operation entry is op, written at the indent of its
// operationId, under the document-root entries root and the path item
// entries item of /things. Each GET takes page_token and answers with data
// and next_page_token.
func rootDoc(root, item, op string, more bool) string {
	get := func(name string) string {
		return `    get:
      operationId: ` + name + `
` + op + `      parameters: [{name: page_token, in: query, schema: {type: string}}]
      responses:
        "200":
          description: ok
          content:
            application/json:
              schema:
                type: object
                properties:
                  data: {type: array, items: {type: string}}
                  next_page_token: {type: string}
`
	}
	src := `openapi: 3.1.0
info: {title: t, version: "1"}
` + root + `paths:
  /things:
` + item + get("listThings")
	if more {
		src += "  /other:\n" + get("listOther")
	}
	return src
}

// TestPromote_ReadsTheRootForTrue holds a pagination key written true on an
// operation to the walk the document's root states under the same key: the
// operation is filled as if it had written the root's object itself, marked
// as promoted, draws no finding, and both the operation's key and the root's
// entry stay kept.
func TestPromote_ReadsTheRootForTrue(t *testing.T) {
	t.Parallel()
	src := rootDoc(rootWalk, "", "      x-fern-pagination: true\n", false)
	for _, written := range inEitherOrder(t, src) {
		doc, diags := compile(t, written, openapi.Options{})
		assert.Empty(t, diags)

		op := openapitest.FindOp(t, doc, "listThings")
		assert.Equal(t, &ir.Pagination{
			Strategy:    ir.PageStrategyCursor,
			Inferred:    true,
			InputCursor: inputPath(queryParam("things", "page_token")),
			Items:       propPath(bodyProps("things", "get") + "/data"),
			NextCursor:  propPath(bodyProps("things", "get") + "/next_page_token"),
		}, op.Pagination)
		assert.Equal(t, ir.Inferred("extension-promotion"), op.Provenance.Inferred)
		assert.Contains(t, doc.Unmodeled, "openapi:x-fern-pagination", "the root's entry stays kept")
		assert.Contains(t, op.Unmodeled, "openapi:x-fern-pagination", "the operation's key stays kept")
	}
}

// TestPromote_TrueWithNoRootStatement holds a pagination key written true to
// filling nothing where the root states no walk for it: one finding, at the
// operation's key when the root writes none, at the root's own when it writes
// no object, shared by every operation writing true; nothing at all for
// false; none for a path item that states a walk of its own, which no
// promotion reads, the root's not taken in its place; and none inferred for
// any of them, though a parameter names a walk. An operation writing nothing
// is still inferred, whatever the root states.
func TestPromote_TrueWithNoRootStatement(t *testing.T) {
	t.Parallel()
	const (
		opKey       = "/paths/~1things/get/x-fern-pagination"
		onTrue      = "      x-fern-pagination: true\n"
		pathItemKey = "    x-fern-pagination: {offset: $request.start, results: $response.data}\n"
	)
	type finding struct{ pointer, message string }
	trueNoRoot := finding{opKey, `extension "x-fern-pagination" is true, which reads the walk the document's root ` +
		`states under the same key, and the root writes none, so no pagination is inferred for the operation`}
	for _, tc := range []struct {
		name, root, item, op string
		more                 bool
		want                 []finding
	}{
		{"no root key", "", "", onTrue, false, []finding{trueNoRoot}},
		{"a root that is a string, two operations writing true", "x-fern-pagination: cursor\n", "", onTrue, true,
			[]finding{{"/x-fern-pagination", `extension "x-fern-pagination" is a string, not an object, so it states no ` +
				`walk for the operations that write true`}}},
		{"false beside a root that states a walk", rootWalk, "", "      x-fern-pagination: false\n", false, nil},
		{"false and no root key", "", "", "      x-fern-pagination: false\n", false, nil},
		{"true beside a path item that states a walk", rootWalk, pathItemKey, onTrue, false,
			[]finding{{opKey, `extension "x-fern-pagination" is true beside a statement its path item writes, which no ` +
				`promotion reads, so no pagination is filled for the operation`}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			for _, src := range inEitherOrder(t, rootDoc(tc.root, tc.item, tc.op, tc.more)) {
				doc, diags := compile(t, src, openapi.Options{})
				got := make([]finding, 0, len(diags))
				for _, d := range diags {
					assert.Equal(t, "openapi/unpromoted-extension", d.Code)
					assert.Equal(t, ir.SeverityInfo, d.Severity)
					got = append(got, finding{string(d.Provenance.Pointer), d.Message})
				}
				assert.ElementsMatch(t, tc.want, got)
				for _, name := range []string{"listThings", "listOther"} {
					if name == "listOther" && !tc.more {
						continue
					}
					op := openapitest.FindOp(t, doc, name)
					assert.Nil(t, op.Pagination, "%s: its own statement speaks of a walk, and no inference fills it", name)
					assert.Empty(t, op.Provenance.Inferred, name)
				}
			}
		})
	}

	t.Run("an operation writing nothing beside a root that states a walk", func(t *testing.T) {
		t.Parallel()
		for _, src := range inEitherOrder(t, rootDoc(rootWalk, "", "", false)) {
			doc, diags := compile(t, src, openapi.Options{})
			assert.Empty(t, diags, "no operation opted in: the root's entry stays kept, and nothing is reported")

			op := openapitest.FindOp(t, doc, "listThings")
			require.NotNil(t, op.Pagination)
			assert.Equal(t, &ir.Pagination{
				Strategy:    ir.PageStrategyToken,
				Inferred:    true,
				InputCursor: inputPath(queryParam("things", "page_token")),
				Items:       propPath(bodyProps("things", "get") + "/data"),
				NextCursor:  propPath(bodyProps("things", "get") + "/next_page_token"),
				Stop:        []ir.PageStop{ir.PageStopNoNext},
			}, op.Pagination)
			assert.Equal(t, lowering.PaginationInferenceHeuristic, op.Provenance.Inferred)
			assert.Contains(t, doc.Unmodeled, "openapi:x-fern-pagination")
		}
	})
}
