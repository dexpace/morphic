// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package irverify

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestDeclarations_ZeroValueDeclaresNothing holds the zero declarations to
// what its doc says, a document declaring none: it lists none and spells no
// path, whatever position a check asks for, rather than dereferencing a
// collector that is not there.
func TestDeclarations_ZeroValueDeclaresNothing(t *testing.T) {
	t.Parallel()
	var none declarations
	assert.Nil(t, none.list())
	assert.Nil(t, none.pathsAt(map[int]bool{0: true}))
	assert.False(t, none.truncated)
}
