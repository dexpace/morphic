// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package bind_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dexpace/morphic/compilers/openapi/internal/bind"
)

// TestEntry_LandsWhereItsHopResolvesOffALoop pins Lands: a hop that resolves
// lands, unless its position is a member of a loop of $refs, whose chain
// resolves nowhere, and a hop that does not resolve lands nowhere.
func TestEntry_LandsWhereItsHopResolvesOffALoop(t *testing.T) {
	t.Parallel()
	resolved := bind.Hop{Status: bind.Resolved}
	assert.True(t, bind.Entry{Hop: resolved, Chain: bind.Chain{Status: bind.Resolved}}.Lands())
	assert.True(t, bind.Entry{Hop: resolved, Chain: bind.Chain{Status: bind.Cyclic}}.Lands(), "leading into a loop")
	assert.False(t, bind.Entry{Hop: resolved, Chain: bind.Chain{Status: bind.Cyclic, Member: true}}.Lands())
	assert.False(t, bind.Entry{Hop: bind.Hop{Status: bind.Unresolved}}.Lands())
}
