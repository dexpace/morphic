// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package operation

import (
	"fmt"
	"math"
	"runtime"
	"strings"
	"testing"

	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/servers"
	"github.com/dexpace/morphic/ir"
)

// TestLinksResolve_AnOperationTheServiceLacks drives the invariant no document
// can break: a link drafted on an operation the service does not hold. It is
// reported as the compiler's own fault, and nothing is written.
func TestLinksResolve_AnOperationTheServiceLacks(t *testing.T) {
	t.Parallel()
	l := newRawLowerer(&soa.OpenAPI{})
	var links Links
	links.add("op/x/gone", responseSlot{}, []linkDraft{{at: l.ctx, use: "/p/links/L", name: "L"}})
	diags := links.Resolve(l.ctx, &ir.Service{}, ir.TypeRegistry{}, servers.NewRegistry())
	require.Len(t, diags, 1)
	assert.Equal(t, diag.InternalInvariant, diags[0].Code)
}

// TestIndexOperations_StopsAtTheGroupDepthBound holds the index to
// ir.MaxGroupDepth, as ir.ForEachOperation is held: an operation in a group at
// that level is indexed, and one a level deeper, past any document the
// compiler builds, is not, so the walk is bounded however deep a service is.
func TestIndexOperations_StopsAtTheGroupDepthBound(t *testing.T) {
	t.Parallel()
	nested := func(id ir.OpID, level int) ir.OperationGroup {
		g := ir.OperationGroup{Operations: []ir.Operation{{ID: id}}}
		for range level {
			g = ir.OperationGroup{Groups: []ir.OperationGroup{g}}
		}
		return g
	}
	svc := &ir.Service{Groups: []ir.OperationGroup{
		nested("op/x/top", 0), nested("op/x/cap", ir.MaxGroupDepth), nested("op/x/past", ir.MaxGroupDepth+1),
	}}
	ix := indexOperations(newRawLowerer(&soa.OpenAPI{}).ctx, svc)
	assert.Contains(t, ix.byID, ir.OpID("op/x/top"))
	assert.Contains(t, ix.byID, ir.OpID("op/x/cap"), "a group at the bound is read")
	assert.NotContains(t, ix.byID, ir.OpID("op/x/past"), "a group past it is not")
}

// membersSpec is a document whose link reads, through n steps along member a,
// a member p0 of a model declaring n members besides a, from a body of
// contents media types, each holding that model.
func membersSpec(n, contents int) string {
	var props, pointer, media strings.Builder
	for i := range n {
		fmt.Fprintf(&props, "        p%d: {type: string}\n", i)
		pointer.WriteString("/a")
	}
	for i := range contents {
		fmt.Fprintf(&media, "            application/x%d+json: {schema: {$ref: '#/components/schemas/M'}}\n", i)
	}
	return `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /a:
    get:
      operationId: a
      parameters: [{name: q, in: query, schema: {type: string}}]
      responses:
        "200":
          description: ok
          content:
` + media.String() + `          links: {L: {operationId: a, parameters: {q: '$response.body#` + pointer.String() + `/p0'}}}
components:
  schemas:
    M:
      type: object
      properties:
        a: {$ref: '#/components/schemas/M'}
` + props.String()
}

// paramsSpec is a document whose operation declares n query parameters and n
// response headers, and whose link gives each parameter, at the operation
// itself, the value from names: the same request parameter, or the header of
// its index.
func paramsSpec(n int, from string) string {
	var params, headers, values strings.Builder
	for i := range n {
		fmt.Fprintf(&params, "        - {name: p%d, in: query, schema: {type: string}}\n", i)
		fmt.Fprintf(&headers, "            h%d: {schema: {type: string}}\n", i)
		fmt.Fprintf(&values, "                p%d: '%s%d'\n", i, from, i)
	}
	return `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /a:
    get:
      operationId: a
      parameters:
` + params.String() + `      responses:
        "200":
          description: ok
          headers:
` + headers.String() + `          links:
            L:
              operationId: a
              parameters:
` + values.String()
}

// linkSpecs are the shapes whose reads each name an entry of one list n long:
// a model's members, through one media type or n holding that model, an
// operation's parameters, and a response's headers.
var linkSpecs = map[string]func(n int) string{
	"members":  func(n int) string { return membersSpec(n, 1) },
	"contents": func(n int) string { return membersSpec(n, n) },
	"params":   func(n int) string { return paramsSpec(n, "$request.query.p") },
	"headers":  func(n int) string { return paramsSpec(n, "$response.header.h") },
}

// TestLinksResolve_IndexesEachListOnce pins that resolving reads every list it
// names through lookups, each indexed once however many values or media types
// name entries of it: one model, one operation's parameters, and one
// response's headers.
func TestLinksResolve_IndexesEachListOnce(t *testing.T) {
	t.Parallel()
	for name, spec := range linkSpecs {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			l, lowered, reg, _ := lowerBeforeLinks(t, spec(8))
			lk := newLookups(reg)
			require.Empty(t, lowered.Links.resolve(l.ctx, &lowered.Service, lk, servers.Reopen(lowered.Servers)))
			require.Len(t, lowered.Service.Groups[0].Operations[0].Responses[0].Links, 1, "the link lowers")
			want := map[string][3]int{
				"members": {1, 1, 0}, "contents": {1, 1, 0}, "params": {0, 1, 0}, "headers": {0, 1, 1},
			}[name]
			assert.Equal(t, want, [3]int{len(lk.members), len(lk.params), len(lk.headers)},
				"the models, operations and responses indexed")
		})
	}
}

// resolveBytes returns the fewest bytes resolving the links of src allocates
// in three runs over one service lowered before the measured window, the link
// each run lowers onto its one response taken off it again before the next.
func resolveBytes(t *testing.T, src string) float64 {
	t.Helper()
	defer runtime.GOMAXPROCS(runtime.GOMAXPROCS(1))
	l, lowered, reg, _ := lowerBeforeLinks(t, src)
	resp := &lowered.Service.Groups[0].Operations[0].Responses[0]
	fewest := math.Inf(1)
	for range 3 {
		resp.Links = nil
		srv := servers.Reopen(lowered.Servers)
		var before, after runtime.MemStats
		runtime.ReadMemStats(&before)
		diags := lowered.Links.Resolve(l.ctx, &lowered.Service, reg, srv)
		runtime.ReadMemStats(&after)
		require.Empty(t, diags, "every size lowers its link alike")
		require.Len(t, resp.Links, 1)
		fewest = min(fewest, float64(after.TotalAlloc-before.TotalAlloc))
	}
	return fewest
}

// TestLinksResolve_AllocatesLinearInWhatItReads holds what resolving allocates
// to linear growth in the lists its values read: from 2n entries to 4n it
// grows at most 2.5 times what it grows from n to 2n. An index built again for
// each value that reads it grows four times. Each size is averaged over four
// spread across its doubling, so a map growing in steps reads as the growth it
// averages to. It does not run in parallel, since the count is process-wide.
func TestLinksResolve_AllocatesLinearInWhatItReads(t *testing.T) {
	for name, spec := range linkSpecs {
		var bytes [3]float64
		for k, n := range []int{50, 100, 200} {
			for j := range 4 {
				bytes[k] += resolveBytes(t, spec(int(math.Round(float64(n)*math.Exp2(float64(j)/4))))) / 4
			}
		}
		assert.LessOrEqual(t, bytes[2]-bytes[1], 2.5*(bytes[1]-bytes[0]),
			"%s: bytes around 50, 100 and 200 entries: %v", name, bytes)
	}
}
