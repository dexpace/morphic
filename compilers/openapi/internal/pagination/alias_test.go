// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package pagination_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/ir"
)

// TestPromote_ReadsAPageThroughAnAlias holds a pagination an extension states,
// naming members of a body whose schema is one reference or two to a model, to
// the members of that model, in whichever order the aliases and the model are
// declared: the pagination is filled, marked as promoted, and draws no finding.
func TestPromote_ReadsAPageThroughAnAlias(t *testing.T) {
	t.Parallel()
	const (
		page = "    Page:\n      type: object\n      properties:\n" +
			"        result: {type: array, items: {type: string}}\n        next_cursor: {type: string}\n"
		pets = "    Pets: {$ref: '#/components/schemas/Page'}\n"
		dogs = "    Dogs: {$ref: '#/components/schemas/Pets'}\n"
	)
	for name, tc := range map[string]struct{ body, schemas string }{
		"one hop, the alias first":    {"Pets", pets + page},
		"one hop, the model first":    {"Pets", page + pets},
		"two hops, aliases first":     {"Dogs", dogs + pets + page},
		"two hops, the model first":   {"Dogs", page + pets + dogs},
		"two hops, the model between": {"Dogs", dogs + page + pets},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			src := `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /dogs:
    get:
      operationId: listDogs
      x-fern-pagination: {cursor: $request.cursor, next_cursor: $response.next_cursor, results: $response.result}
      parameters: [{name: cursor, in: query, schema: {type: string}}]
      responses:
        "200": {description: ok, content: {application/json: {schema: {$ref: '#/components/schemas/` + tc.body + `'}}}}
components:
  schemas:
` + tc.schemas
			doc, diags := compile(t, src, openapi.Options{})
			assert.Empty(t, diags, "no finding at all")

			op := openapitest.FindOp(t, doc, "listDogs")
			require.NotNil(t, op.Pagination)
			cursor := ir.ParamID("param/openapi/paths/~1dogs/get/parameters/cursor/query")
			assert.Equal(t, ir.Pagination{
				Strategy:    ir.PageStrategyCursor,
				Inferred:    true,
				InputCursor: &ir.InputPath{Param: &cursor},
				Items:       &ir.PropPath{Segments: []ir.PropID{"p/openapi/components/schemas/Page/properties/result"}},
				NextCursor:  &ir.PropPath{Segments: []ir.PropID{"p/openapi/components/schemas/Page/properties/next_cursor"}},
			}, *op.Pagination)
			assert.Equal(t, ir.Inferred("extension-promotion"), op.Provenance.Inferred)
		})
	}
}
