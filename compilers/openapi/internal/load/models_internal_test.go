// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package load

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestOwnRoot_AModelWithNoReadableCoreHasNone pins ownRoot's two guards: a $ref
// whose own mapping cannot be read has none to answer with, rather than the
// target's.
func TestOwnRoot_AModelWithNoReadableCoreHasNone(t *testing.T) {
	t.Parallel()
	assert.Nil(t, ownRoot(bareReference{}))
	assert.Nil(t, ownRoot(coredReference{core: 42}))
}
