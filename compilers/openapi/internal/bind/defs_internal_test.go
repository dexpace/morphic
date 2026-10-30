// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package bind

import (
	"encoding/json/jsontext"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestPrefix_SlicesItsPointer pins prefix: the pointer to the first n tokens,
// cut from the pointer itself, so it allocates nothing however long it is.
func TestPrefix_SlicesItsPointer(t *testing.T) {
	pointer := jsontext.Pointer("/a~1b/c/~0d/e")
	for n, want := range []string{"", "/a~1b", "/a~1b/c", "/a~1b/c/~0d", "/a~1b/c/~0d/e", "/a~1b/c/~0d/e"} {
		assert.Equal(t, want, prefix(pointer, n), n)
	}
	long := jsontext.Pointer(strings.Repeat("/p", 1000))
	assert.Zero(t, testing.AllocsPerRun(10, func() { _ = prefix(long, 500) }))
}
