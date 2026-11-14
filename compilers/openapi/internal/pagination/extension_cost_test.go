// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package pagination_test

import (
	"fmt"
	"math"
	"runtime"
	"strings"
	"testing"

	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi"
	"github.com/dexpace/morphic/compilers/openapi/internal/load"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/compilers/openapi/internal/pagination"
)

// pageSpec is a document of n operations, each reading its cursor, the next
// cursor and the items of its page by expressions through one model of n
// members, as the walk of each states: operation is one operation's text, its
// %[1]d the operation's number.
func pageSpec(n int, operation string) string {
	var paths, props strings.Builder
	for i := range n {
		fmt.Fprintf(&props, "        m%d: {type: string}\n", i)
		fmt.Fprintf(&paths, operation, i)
	}
	return openapitest.PathsSpec(paths.String()) + `components:
  schemas:
    M:
      type: object
      properties:
        items: {type: array, items: {type: string}}
` + props.String()
}

// pageSpecs are the shapes whose operations read the one model: the cursor from
// the request's body, and the cursor from a member of a parameter holding it.
var pageSpecs = map[string]func(n int) string{
	"body": func(n int) string {
		return pageSpec(n, `  /o%[1]d:
    post:
      operationId: o%[1]d
      x-page: {cursor: '$request.body#/m%[1]d', next: '$response.body#/m%[1]d', items: '$response.body#/items'}
      requestBody:
        content: {application/json: {schema: {$ref: '#/components/schemas/M'}}}
      responses:
        "200":
          description: ok
          content: {application/json: {schema: {$ref: '#/components/schemas/M'}}}
`)
	},
	"parameter": func(n int) string {
		return pageSpec(n, `  /o%[1]d:
    get:
      operationId: o%[1]d
      x-page: {cursor: '$request.c.m%[1]d', next: '$response.body#/m%[1]d', items: '$response.body#/items'}
      parameters: [{name: c, in: query, schema: {$ref: '#/components/schemas/M'}}]
      responses:
        "200":
          description: ok
          content: {application/json: {schema: {$ref: '#/components/schemas/M'}}}
`)
	},
}

// promoteBytes returns the fewest bytes Promote allocates in three runs over the
// service src declares, compiled with promotion off, the paginations each run
// fills taken off again before the next.
func promoteBytes(t *testing.T, src string, operations int) float64 {
	t.Helper()
	defer runtime.GOMAXPROCS(runtime.GOMAXPROCS(1))
	doc, _, err := openapi.New().Compile(t.Context(), []compilers.Source{openapitest.SourceOf(src)},
		compilers.Options{FormatOptions: openapi.Options{Promotions: openapi.ExtensionPromotions{Disabled: true}}})
	require.NoError(t, err)
	require.NotNil(t, doc)
	c := lowering.New(0, &load.Document{Doc: &soa.OpenAPI{}}, lowering.Policy{Promotions: pagePolicy()})
	fewest := math.Inf(1)
	for range 3 {
		var before, after runtime.MemStats
		runtime.ReadMemStats(&before)
		_, diags := pagination.Promote(c, &doc.Services[0], doc.Types)
		runtime.ReadMemStats(&after)
		require.Empty(t, diags, "every size fills its paginations alike")
		filled := 0
		for _, op := range lowering.Operations(&doc.Services[0]) {
			if op.Pagination != nil {
				filled++
			}
			op.Pagination = nil
		}
		require.Equal(t, operations, filled)
		fewest = min(fewest, float64(after.TotalAlloc-before.TotalAlloc))
	}
	return fewest
}

// TestPromote_AllocatesLinearInWhatItReads holds what promoting allocates to
// linear growth in the operations reading one model, through a request body or
// through a parameter: from 2n operations to 4n it grows at most 2.5 times what
// it grows from n to 2n. An index of the model's members built again for each
// expression that reads it grows four times. Each size is averaged over four
// spread across its doubling, so a map growing in steps reads as the growth it
// averages to. It does not run in parallel, since the count is process-wide.
func TestPromote_AllocatesLinearInWhatItReads(t *testing.T) {
	for name, spec := range pageSpecs {
		var bytes [3]float64
		for k, n := range []int{50, 100, 200} {
			for j := range 4 {
				size := int(math.Round(float64(n) * math.Exp2(float64(j)/4)))
				bytes[k] += promoteBytes(t, spec(size), size) / 4
			}
		}
		assert.LessOrEqual(t, bytes[2]-bytes[1], 2.5*(bytes[1]-bytes[0]),
			"%s: bytes around 50, 100 and 200 operations: %v", name, bytes)
	}
}
