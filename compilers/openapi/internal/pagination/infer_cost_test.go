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
	"github.com/dexpace/morphic/compilers/openapi/internal/whole"
)

// searchSpec is a document of n POSTs, each sending a request body of its own
// that names its page and page size, and answering with its items at the
// root: a walk the inference reads off the body.
func searchSpec(n int) string {
	var paths strings.Builder
	for i := range n {
		fmt.Fprintf(&paths, `  /o%[1]d:
    post:
      operationId: o%[1]d
      requestBody:
        content:
          application/json:
            schema: {type: object, properties: {q%[1]d: {type: string}, page: {type: integer}, per_page: {type: integer}}}
      responses:
        "200":
          description: ok
          content: {application/json: {schema: {type: object, properties: {result: {type: array, items: {type: string}}}}}}
`, i)
	}
	return openapitest.PathsSpec(paths.String())
}

// inferBytes returns the fewest bytes InferWalks allocates in three runs over
// the service src declares, compiled with inference off, the paginations each
// run fills taken off again before the next.
func inferBytes(t *testing.T, src string, operations int) float64 {
	t.Helper()
	defer runtime.GOMAXPROCS(runtime.GOMAXPROCS(1))
	doc, _, err := openapi.New().Compile(t.Context(), []compilers.Source{openapitest.SourceOf(src)},
		compilers.Options{FormatOptions: openapi.Options{PaginationInference: openapi.PaginationInference{Disabled: true}}})
	require.NoError(t, err)
	require.NotNil(t, doc)
	c := lowering.New(0, &load.Document{Doc: &soa.OpenAPI{}}, lowering.Policy{})
	fewest := math.Inf(1)
	for range 3 {
		var before, after runtime.MemStats
		runtime.ReadMemStats(&before)
		diags := pagination.InferWalks(c, whole.New(doc), nil)
		runtime.ReadMemStats(&after)
		require.Empty(t, diags, "every size fills its paginations alike")
		filled := 0
		for _, op := range whole.New(doc).Ops {
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

// TestInferWalks_AllocatesLinearInTheBodiesItReads holds what inferring walks
// from request bodies allocates to linear growth in the operations, each
// sending a body of its own: from 2n operations to 4n it grows at most 2.5
// times what it grows from n to 2n. A body read for each member it holds, or
// every body read for each operation, grows four times. Each size is averaged
// over four spread across its doubling. It does not run in parallel, since the
// count is process-wide.
func TestInferWalks_AllocatesLinearInTheBodiesItReads(t *testing.T) {
	var bytes [3]float64
	for k, n := range []int{50, 100, 200} {
		for j := range 4 {
			size := int(math.Round(float64(n) * math.Exp2(float64(j)/4)))
			bytes[k] += inferBytes(t, searchSpec(size), size) / 4
		}
	}
	assert.LessOrEqual(t, bytes[2]-bytes[1], 2.5*(bytes[1]-bytes[0]),
		"bytes around 50, 100 and 200 operations: %v", bytes)
}
