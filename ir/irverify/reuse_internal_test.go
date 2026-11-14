// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package irverify

import (
	"go/types"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestReusedMaps_AreEmptiedThroughReusedMapAlone holds every map the package
// empties for another collection to reuse.Map, whose limit bounds what
// emptying costs: a bare clear of a map kept across collections makes one huge
// collection tax every later one, and a delete from one leaves it as large as
// it grew, which the map's limit cannot see.
func TestReusedMaps_AreEmptiedThroughReusedMapAlone(t *testing.T) {
	t.Parallel()
	p := verifyDecls(t)
	assert.Empty(t, p.FuncsUsing(types.Universe.Lookup("clear")), "a map is cleared through reuse.Map alone")
	assert.Empty(t, p.FuncsUsing(types.Universe.Lookup("delete")), "no check deletes from a map it reads again")
}
