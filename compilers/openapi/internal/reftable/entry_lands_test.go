// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package reftable_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dexpace/morphic/compilers/openapi/internal/reftable"
)

// TestEntry_LandsWhereItsHopResolvesOffALoop pins Lands: a hop that resolves
// lands, unless its position is a member of a loop of $refs, whose chain
// resolves nowhere, and a hop that does not resolve lands nowhere.
func TestEntry_LandsWhereItsHopResolvesOffALoop(t *testing.T) {
	t.Parallel()
	resolved := reftable.Hop{Status: reftable.Resolved}
	assert.True(t, reftable.ReferenceSite{Hop: resolved, Chain: reftable.Chain{Status: reftable.Resolved}}.Lands())
	assert.True(t, reftable.ReferenceSite{Hop: resolved, Chain: reftable.Chain{Status: reftable.Cyclic}}.Lands(), "leading into a loop")
	assert.False(t, reftable.ReferenceSite{Hop: resolved, Chain: reftable.Chain{Status: reftable.Cyclic, PositionLiesOnReferenceCycle: true}}.Lands())
	assert.False(t, reftable.ReferenceSite{Hop: reftable.Hop{Status: reftable.Unresolved}}.Lands())
}
