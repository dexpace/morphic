package operation

import (
	"testing"

	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/stretchr/testify/assert"

	"github.com/dexpace/morphic/ir"
)

// TestApplyOperationServers_WithoutRootNode is the operation half of
// TestApplyPathServers_WithoutRootNode: a declared list whose source node
// cannot be read keeps nothing, and announces nothing it did not keep.
func TestApplyOperationServers_WithoutRootNode(t *testing.T) {
	t.Parallel()
	l := newRawLowerer(&soa.OpenAPI{})
	op := &ir.Operation{}
	diags := applyOperationServers(l.ctx, op, &soa.Operation{Servers: []*soa.Server{{URL: "https://x"}}}, "/paths/~1a/get")
	assert.Nil(t, op.Unmodeled, "servers with no raw node are not preserved")
	assert.Empty(t, diags)
}
