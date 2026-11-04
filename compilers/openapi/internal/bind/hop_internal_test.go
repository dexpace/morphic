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

// TestIsPlainName pins the plain names the JSON Schema drafts before 2020-12
// declare an anchor by: a letter, then letters, digits, '-', '_', ':' and '.',
// so a colon is one and a leading '_' is not, and anchorNamed reads by it
// where the dialect's rule says so.
func TestIsPlainName(t *testing.T) {
	t.Parallel()
	for name, want := range map[string]bool{
		"a": true, "a:b": true, "Z9-_.:": true, "": false, "_": false, "1a": false, "-a": false, "a b": false,
		"a/b": false, "é": false,
	} {
		assert.Equal(t, want, isPlainName(name), "%q", name)
		assert.Equal(t, want, anchorNamed(DialectRule{PlainNames: true}, name), "%q", name)
		assert.Equal(t, isAnchorName(name), anchorNamed(DialectRule{}, name), "%q", name)
	}
}
