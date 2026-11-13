// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package openapitest_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
)

// TestPathKeyFragments_EachFragmentBeginsAtAHash holds the table to what its
// rows claim, apart from either split it is held against: a fragment begins at
// a "#" of its key, and the table holds keys carrying one and keys carrying
// none.
func TestPathKeyFragments_EachFragmentBeginsAtAHash(t *testing.T) {
	t.Parallel()
	carrying := 0
	for key, at := range openapitest.PathKeyFragments() {
		if at < 0 {
			continue
		}
		carrying++
		assert.Equal(t, byte('#'), key[at], "%q's fragment begins at its # (%d)", key, at)
	}
	assert.NotZero(t, carrying)
	assert.Less(t, carrying, len(openapitest.PathKeyFragments()))
}
