// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package harness

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// numbered returns n lines, each the prefix and its place from zero.
func numbered(prefix string, n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = fmt.Sprintf("%s%d", prefix, i)
	}
	return out
}

// TestLineDifference_PrintsOnlyTheLinesThatDiffer pins the exact text for each
// shape of difference: it names each side's own line numbers, a deletion comes
// before an insertion where either would do, and a line the sides share is
// never printed.
func TestLineDifference_PrintsOnlyTheLinesThatDiffer(t *testing.T) {
	for _, tc := range []struct {
		name string
		a, b []string
		want string
	}{
		{"both empty", nil, nil, ""},
		{"equal", []string{"a", "b"}, []string{"a", "b"}, ""},
		{"one line changed", []string{"x", "y", "z"}, []string{"x", "Y", "z"}, "-2: y\n+2: Y\n"},
		{"two changes far apart print nothing between",
			[]string{"a", "b", "c", "d", "e", "f", "g"}, []string{"a", "B", "c", "d", "e", "F", "g"},
			"-2: b\n+2: B\n-6: f\n+6: F\n"},
		{"a line moved down", []string{"x", "y", "z"}, []string{"y", "z", "x"}, "-1: x\n+3: x\n"},
		{"a line swapped with its neighbour", []string{"p", "q"}, []string{"q", "p"}, "-1: p\n+2: p\n"},
		{"an insertion at the end", []string{"a", "b"}, []string{"a", "b", "c"}, "+3: c\n"},
		{"lines deleted at the start", []string{"a", "b", "c"}, []string{"c"}, "-1: a\n-2: b\n"},
		{"a line that is not text prints quoted", []string{"x\x00y"}, nil, "-1: \"x\\x00y\"\n"},
		{"a tab prints as written", []string{"\tx"}, nil, "-1: \tx\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, lineDifference(tc.a, tc.b))
		})
	}
}

// TestLineDifference_NeverPrintsMoreThanTheCapASide holds both forms to the
// lines-a-side cap, and the table to its cell cap: a pair whose table would
// pass it prints the head of each remainder, where a pair at the cap still
// matches the line the sides share.
func TestLineDifference_NeverPrintsMoreThanTheCapASide(t *testing.T) {
	t.Run("in the table's form", func(t *testing.T) {
		got := lineDifference(numbered("a", 50), numbered("b", 50))

		var want strings.Builder
		for i, l := range numbered("a", differenceLines) {
			fmt.Fprintf(&want, "-%d: %s\n", i+1, l)
		}
		for i, l := range numbered("b", differenceLines) {
			fmt.Fprintf(&want, "+%d: %s\n", i+1, l)
		}
		want.WriteString("-… 10 more\n+… 10 more\n")
		assert.Equal(t, want.String(), got)
	})

	// Each pair holds one line the sides share, second on each: a table
	// matches it, so prints the first lines of the two sides together, where
	// the fallback prints the first side's lines before the second's.
	shared := func(prefix string, n int) []string {
		l := numbered(prefix, n)
		l[1] = "shared"
		return l
	}
	t.Run("a table at the cell cap", func(t *testing.T) {
		got := lineDifference(shared("x", 1023), shared("y", 1023))

		assert.True(t, strings.HasPrefix(got, "-1: x0\n+1: y0\n-3: x2\n"), got)
	})
	t.Run("a table past the cell cap", func(t *testing.T) {
		a, b := shared("x", 1024), shared("y", 1024)

		got := lineDifference(a, b)

		var want strings.Builder
		for i, l := range a[:differenceLines] {
			fmt.Fprintf(&want, "-%d: %s\n", i+1, l)
		}
		for i, l := range b[:differenceLines] {
			fmt.Fprintf(&want, "+%d: %s\n", i+1, l)
		}
		want.WriteString("-… 984 more\n+… 984 more\n")
		assert.Equal(t, want.String(), got)
	})
}
