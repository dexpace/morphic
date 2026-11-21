// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package pagination_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/compilers/openapi/internal/pagination"
	"github.com/dexpace/morphic/ir"
)

// rootWalk is a pagination key at a document's root stating a cursor walk.
const rootWalk = `x-fern-pagination:
  cursor: $request.page_token
  next_cursor: $response.next_page_token
  results: $response.data
`

// rootDoc is a document of the paths /things and, where more is set, /other,
// each one GET whose operation entry is operation, written at the indent of its
// operationId, under the document-root entries root and the path item
// entries item of /things. Each GET takes page_token and answers with data
// and next_page_token.
func rootDoc(root, item, operation string, more bool) string {
	get := func(name string) string {
		return `    get:
      operationId: ` + name + `
` + operation + `      parameters: [{name: page_token, in: query, schema: {type: string}}]
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

		operation := openapitest.FindOperation(t, doc, "listThings")
		assert.Equal(t, &ir.Pagination{
			Strategy:    ir.PageStrategyCursor,
			Inferred:    true,
			InputCursor: inputPath(queryParam("things", "page_token")),
			Items:       propertyPath(bodyProperties("things", "get") + "/data"),
			NextCursor:  propertyPath(bodyProperties("things", "get") + "/next_page_token"),
		}, operation.Pagination)
		assert.Equal(t, ir.Inferred("extension-promotion"), operation.Provenance.Inferred)
		assert.Contains(t, doc.Unmodeled, "openapi:x-fern-pagination", "the root's entry stays kept")
		assert.Contains(t, operation.Unmodeled, "openapi:x-fern-pagination", "the operation's key stays kept")
	}
}

// TestPromote_TrueWithNoRootStatement holds a pagination key written true to
// filling nothing where the root states no walk for it: one finding, at the
// operation's key when the root writes none, at the root's own when it writes
// no object, shared by every operation writing true; nothing at all for
// false; at the path item's key when it writes no object, whose statement is
// nearer than the root's, which is not taken in its place; and none inferred
// for any of them, though a parameter names a walk. An operation writing
// nothing is still inferred, whatever the root states.
func TestPromote_TrueWithNoRootStatement(t *testing.T) {
	t.Parallel()
	const (
		operationKey = "/paths/~1things/get/x-fern-pagination"
		onTrue       = "      x-fern-pagination: true\n"
		pathItemKey  = "    x-fern-pagination: cursor\n"
	)
	type finding struct{ pointer, message string }
	trueNoRoot := finding{operationKey, `extension "x-fern-pagination" is true, which reads the walk its path item, else ` +
		`the document's root, states under the same key, and neither writes one, so no pagination is inferred for ` +
		`the operation`}
	for _, tc := range []struct {
		name, root, item, operation string
		more                        bool
		want                        []finding
	}{
		{"no root key", "", "", onTrue, false, []finding{trueNoRoot}},
		{"a root that is a string, two operations writing true", "x-fern-pagination: cursor\n", "", onTrue, true,
			[]finding{{"/x-fern-pagination", `extension "x-fern-pagination" is a string, not an object, so it states no ` +
				`walk for the operations that write true`}}},
		{"false beside a root that states a walk", rootWalk, "", "      x-fern-pagination: false\n", false, nil},
		{"false and no root key", "", "", "      x-fern-pagination: false\n", false, nil},
		{"true beside a path item stating no walk, the root stating one", rootWalk, pathItemKey, onTrue, false,
			[]finding{{"/paths/~1things/x-fern-pagination", `extension "x-fern-pagination" is a string, not an ` +
				`object, so it states no walk for the operations that write true`}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			for _, src := range inEitherOrder(t, rootDoc(tc.root, tc.item, tc.operation, tc.more)) {
				doc, diags := compile(t, src, openapi.Options{})
				got := make([]finding, 0, len(diags))
				for _, diagnostic := range diags {
					assert.Equal(t, "openapi/unpromoted-extension", diagnostic.Code)
					assert.Equal(t, ir.SeverityInfo, diagnostic.Severity)
					got = append(got, finding{string(diagnostic.Provenance.Pointer), diagnostic.Message})
				}
				assert.ElementsMatch(t, tc.want, got)
				for _, name := range []string{"listThings", "listOther"} {
					if name == "listOther" && !tc.more {
						continue
					}
					operation := openapitest.FindOperation(t, doc, name)
					assert.Nil(t, operation.Pagination, "%s: its own statement speaks of a walk, and no inference fills it", name)
					assert.Empty(t, operation.Provenance.Inferred, name)
				}
			}
		})
	}

	t.Run("an operation writing nothing beside a root that states a walk", func(t *testing.T) {
		t.Parallel()
		for _, src := range inEitherOrder(t, rootDoc(rootWalk, "", "", false)) {
			doc, diags := compile(t, src, openapi.Options{})
			assert.Empty(t, diags, "no operation opted in: the root's entry stays kept, and nothing is reported")

			operation := openapitest.FindOperation(t, doc, "listThings")
			require.NotNil(t, operation.Pagination)
			assert.Equal(t, &ir.Pagination{
				Strategy:    ir.PageStrategyToken,
				Inferred:    true,
				InputCursor: inputPath(queryParam("things", "page_token")),
				Items:       propertyPath(bodyProperties("things", "get") + "/data"),
				NextCursor:  propertyPath(bodyProperties("things", "get") + "/next_page_token"),
				Stop:        []ir.PageStop{ir.PageStopNoNext},
			}, operation.Pagination)
			assert.Equal(t, pagination.InferenceHeuristic, operation.Provenance.Inferred)
			assert.Contains(t, doc.Unmodeled, "openapi:x-fern-pagination")
		}
	})
}
