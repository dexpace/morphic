// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package schema

import (
	"math/bits"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestKeySet_HoldsTheCensusDomainAlone pins that a key set holds each key of
// censusDomain apart and no other: a key a reader outside the census reads,
// such as a description, has no bit, so no build can say it read it.
func TestKeySet_HoldsTheCensusDomainAlone(t *testing.T) {
	t.Parallel()
	all := keySetOf(censusDomain[:]...)
	assert.Equal(t, len(censusDomain), bits.OnesCount32(uint32(all)), "each key has a bit of its own")
	for _, k := range censusDomain {
		assert.True(t, keySetOf(k).has(k), "%s has a bit", k)
	}
	assert.Zero(t, keyBit("description"), "a key outside the domain has no bit")
	assert.False(t, all.has("description"))
	assert.False(t, keySetOf("properties").has("items"))
}
