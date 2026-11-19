// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package schema

import (
	"math/bits"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestKeySet_HoldsTheReadDomainAlone pins that a key set holds each key of
// readDomain apart and no other: a key a reader outside the keeping of unread keywords reads,
// such as a description, has no bit, so no build can say it read it.
func TestKeySet_HoldsTheReadDomainAlone(t *testing.T) {
	t.Parallel()
	all := keySetOf(readDomain...)
	assert.Equal(t, len(readDomain), bits.OnesCount32(uint32(all)), "each key has a bit of its own")
	for _, k := range readDomain {
		assert.True(t, keySetOf(k).has(k), "%s has a bit", k)
	}
	assert.Zero(t, keyBit("description"), "a key outside the domain has no bit")
	assert.False(t, all.has("description"))
	assert.False(t, keySetOf("properties").has("items"))
}
