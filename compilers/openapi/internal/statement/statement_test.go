// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package statement_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/statement"
)

// wide returns a component W writing n extension keys, x-0 to x-(n-1).
func wide(n int) string {
	keys := make([]string, n)
	for i := range n {
		keys[i] = fmt.Sprintf("x-%d: %d", i, i)
	}
	return document("3.1.0", "W: {"+strings.Join(keys, ", ")+"}")
}

// TestSchema_KeyOnAWideObjectScansNothing pins that an object writing many
// keys answers Key from its index: a lookup allocates nothing, and the keys it
// compares do not grow from 2,000 keys to 20,000. It runs serially, as
// AllocsPerRun counts the whole process.
func TestSchema_KeyOnAWideObjectScansNothing(t *testing.T) {
	compared := map[int]int{}
	for _, n := range []int{2000, 20000} {
		table, c := tableOver(t, wide(n), rulesAt(t, "3.1.0", ""))
		st := table.At(component(c, "W"))
		require.Len(t, st.Keys, n)
		last := fmt.Sprintf("x-%d", n-1)
		k, read := st.Key(last)
		require.True(t, read)
		assert.Equal(t, last, k.Name)
		_, read = st.Key("absent")
		assert.False(t, read)
		assert.Zero(t, testing.AllocsPerRun(100, func() { st.Key(last) }), "a lookup allocates nothing")
		compared[n] = statement.Compared(st.Schema, last)
	}
	assert.Equal(t, compared[2000], compared[20000], "the keys compared do not grow with the object")
}
