// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package schema

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/compilers/openapi/internal/resolve"
)

// TestHeldAt_AContextPlacingNoPositionHoldsNothing pins what a tuple's entries
// read where the context reads another document's content at the source's
// positions, which the bindings place nowhere: no schema, rather than the
// source's at the same pointer.
func TestHeldAt_AContextPlacingNoPositionHoldsNothing(t *testing.T) {
	t.Parallel()
	c := lowering.Ctx{}.InScope(resolve.Scope{Foreign: true, Holder: "o.yaml"})
	_, placed := c.Position("/items/0")
	assert.False(t, placed, "sanity: the context places no position")
	assert.Nil(t, heldAt(c, "/items/0"))
}
