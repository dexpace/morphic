// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package schema

import (
	"testing"

	"github.com/speakeasy-api/openapi/jsonschema/oas3"
	soa "github.com/speakeasy-api/openapi/openapi"

	"github.com/dexpace/morphic/ir"
)

// TestKeepUnreadAcross_NeedsAUnion drives the state its callers cannot
// produce, each handing it the Union its build just made: a node of another
// kind, which has no variants to keep beside.
func TestKeepUnreadAcross_NeedsAUnion(t *testing.T) {
	t.Parallel()
	l := NewRawLowerer(&soa.OpenAPI{})
	keep := keepUnreadAcross(l.Ctx, l.Types, StandaloneSchema(&oas3.Schema{}), distributedShape, nil, "/p")
	assertInternalInvariant(t, keep(&ir.Scalar{ID: "t/anon/scalar"}, 0))
}
