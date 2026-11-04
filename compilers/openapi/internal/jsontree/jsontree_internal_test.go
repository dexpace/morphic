// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package jsontree

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestCursorAt_AnEarlierOffsetIsAnsweredFromTheStart holds the position of an
// offset asked for after a later one to the position of the same offset asked
// for first, on a text with a line end and a character wider than a byte.
func TestCursorAt_AnEarlierOffsetIsAnsweredFromTheStart(t *testing.T) {
	t.Parallel()
	data := []byte("[\n\"é\", 1]")
	offsets := []int{0, 2, 3, len(data) - 1}

	for _, off := range offsets {
		fresh := cursor{data: data, line: 1, column: 1}
		wantLine, wantColumn := fresh.at(off)

		used := cursor{data: data, line: 1, column: 1}
		used.at(len(data))
		gotLine, gotColumn := used.at(off)

		assert.Equal(t, [2]int{wantLine, wantColumn}, [2]int{gotLine, gotColumn}, "offset %d", off)
	}
}
