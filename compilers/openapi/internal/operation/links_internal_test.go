// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package operation

import (
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
// ir.MaxGroupDepth: an operation nested deeper than any document the compiler
// builds is not indexed, so the walk is bounded however deep a service is.
func TestIndexOperations_StopsAtTheGroupDepthBound(t *testing.T) {
	t.Parallel()
	deep := ir.OperationGroup{Operations: []ir.Operation{{ID: "op/x/deep"}}}
	for range ir.MaxGroupDepth + 1 {
		deep = ir.OperationGroup{Groups: []ir.OperationGroup{deep}}
	}
	svc := &ir.Service{Groups: []ir.OperationGroup{{Operations: []ir.Operation{{ID: "op/x/top"}}}, deep}}
	ix := indexOperations(newRawLowerer(&soa.OpenAPI{}).ctx, svc)
	assert.Contains(t, ix.byID, ir.OpID("op/x/top"))
	assert.NotContains(t, ix.byID, ir.OpID("op/x/deep"))
}
