// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package cache_test

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dexpace/morphic/engine/cache"
)

func TestKeyOf_SpellsAHashOfItsFields(t *testing.T) {
	t.Parallel()
	key := cache.KeyOf("a", "b")
	assert.Len(t, key.String(), hex.EncodedLen(sha256.Size))
	assert.Equal(t, strings.ToLower(key.String()), key.String())
	assert.Equal(t, key, cache.KeyOf("a", "b"), "a key depends on nothing but its fields")
}

// TestKeyOf_ADifferentSequenceIsADifferentKey moves bytes across each boundary,
// which is what a key built by joining fields would miss.
func TestKeyOf_ADifferentSequenceIsADifferentKey(t *testing.T) {
	t.Parallel()
	sequences := [][]string{
		{},
		{""},
		{"", ""},
		{"ab"},
		{"a", "b"},
		{"b", "a"},
		{"ab", ""},
		{"", "ab"},
		{"a", "", "b"},
		{"\x01a"},
		{"\x01", "a"},
	}
	seen := map[cache.Key][]string{}
	for _, fields := range sequences {
		key := cache.KeyOf(fields...)
		other, taken := seen[key]
		assert.False(t, taken, "%q and %q share a key", fields, other)
		seen[key] = fields
	}
}
