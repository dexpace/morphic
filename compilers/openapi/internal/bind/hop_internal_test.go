// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package bind

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestIsAnchorName pins the names a $anchor can declare, by JSON Schema
// 2020-12 section 8.2.2: none empty, none opening with a digit, '-' or '.'.
func TestIsAnchorName(t *testing.T) {
	t.Parallel()
	for name, want := range map[string]bool{
		"a": true, "_": true, "Z9-_.": true, "": false, "1a": false, "-a": false, ".a": false, "a b": false,
		"a/b": false, "é": false,
	} {
		assert.Equal(t, want, isAnchorName(name), "%q", name)
	}
}
