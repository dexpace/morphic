// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package schema

import (
	"math/bits"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

// TestReadCheck_RecordsEachDisagreement pins what the census sites record
// where a test asks: a key kept that a build read, and one homed that none
// read, each at its site, kind and pointer, and nothing for a key handled or
// agreed on.
func TestReadCheck_RecordsEachDisagreement(t *testing.T) {
	t.Parallel()
	l, s, at := lowererDeclaringS(t)
	require.True(t, hasKey(s, "type"))
	var r readCheck
	r.compare("own", "model", s, 0, nil, nil, string(at))
	r.compare("own", "model", s, keyBit("type"), nil, []string{"type"}, string(at))
	r.compare("own", "model", s, keyBit("type"), nil, nil, string(at))
	r.compare("own", "model", s, 0, []string{"type"}, nil, string(at))
	assert.Equal(t, []string{
		"own\tmodel\ttype\thomed, unread\t" + string(at),
		"own\tmodel\ttype\tkept, read\t" + string(at),
	}, r.disagreements)
	assert.Nil(t, readCheckIn(l.Ctx), "a run no test seeded records nothing")
}
