// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package harness

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	// differenceLines is the most lines of a side that lineDifference prints.
	differenceLines = 40
	// differenceCells is the most cells of the table that lineDifference builds.
	differenceCells = 1 << 20
)

// lineDifference prints what a and b do not share, and is empty where they
// are equal: `-n: line` for a line only a holds and `+n: line` for one only b
// holds, at its own side's number, a deletion before an insertion where
// either would do. It prints one text for one pair, which cmp.Diff does not.
//
// Past the lines shared at either end, a longest-common-subsequence table
// matches the rest where it fits differenceCells; otherwise each side prints
// the lines the other holds fewer of (unmatchedLines). A side prints
// differenceLines lines at most, then the count left out.
func lineDifference(a, b []string) string {
	start := commonPrefix(a, b)
	a, b = a[start:], b[start:]
	end := commonSuffix(a, b)
	a, b = a[:len(a)-end], b[:len(b)-end]
	if len(a) == 0 && len(b) == 0 {
		return ""
	}

	differenceWriterValue := &differenceWriter{offset: start}
	if (len(a)+1)*(len(b)+1) <= differenceCells {
		matchLines(differenceWriterValue, a, b)
	} else {
		unmatchedLines(differenceWriterValue, '-', a, b)
		unmatchedLines(differenceWriterValue, '+', b, a)
	}
	return differenceWriterValue.finish()
}

// commonPrefix is how many leading lines a and b share.
func commonPrefix(a, b []string) int {
	n := 0
	for n < len(a) && n < len(b) && a[n] == b[n] {
		n++
	}
	return n
}

// commonSuffix is how many trailing lines a and b share.
func commonSuffix(a, b []string) int {
	n := 0
	for n < len(a) && n < len(b) && a[len(a)-1-n] == b[len(b)-1-n] {
		n++
	}
	return n
}

// matchLines walks a and b from their starts along a longest common
// subsequence, printing each line it leaves out of the match: a deletion
// before an insertion where both lengthen the match alike.
func matchLines(differenceWriterValue *differenceWriter, a, b []string) {
	width := len(b) + 1
	common := commonLengths(a, b)
	i, j := 0, 0
	for i < len(a) || j < len(b) {
		switch {
		case i < len(a) && j < len(b) && a[i] == b[j]:
			i++
			j++
		case i < len(a) && (j == len(b) || common[(i+1)*width+j] >= common[i*width+j+1]):
			differenceWriterValue.print('-', i, a[i])
			i++
		default:
			differenceWriterValue.print('+', j, b[j])
			j++
		}
	}
}

// unmatchedLines prints, as sign's side, each line of side that other holds
// fewer of, as multisets: a line the two share is printed on neither side,
// though moved, where no table matches the two.
func unmatchedLines(differenceWriterValue *differenceWriter, sign byte, side, other []string) {
	held := make(map[string]int, len(other))
	for _, line := range other {
		held[line]++
	}
	for i, line := range side {
		if held[line] > 0 {
			held[line]--
			continue
		}
		differenceWriterValue.print(sign, i, line)
	}
}

// commonLengths is the table whose cell at i*(len(b)+1)+j holds the length of
// the longest common subsequence of a[i:] and b[j:].
func commonLengths(a, b []string) []int {
	width := len(b) + 1
	table := make([]int, (len(a)+1)*width)
	for i := range slices.Backward(a) {
		for j := range slices.Backward(b) {
			if a[i] == b[j] {
				table[i*width+j] = table[(i+1)*width+j+1] + 1
			} else {
				table[i*width+j] = max(table[(i+1)*width+j], table[i*width+j+1])
			}
		}
	}
	return table
}

// differenceWriter collects the lines lineDifference prints, up to the cap a side.
type differenceWriter struct {
	out strings.Builder
	// offset is how many lines the two sides share at their starts.
	offset int
	// shown and omitted count each side's lines, deleted then inserted.
	shown, omitted [2]int
}

// print writes the line at index i of the remainder of the side sign names,
// or counts it where that side is at its cap.
func (w *differenceWriter) print(sign byte, i int, line string) {
	side := 0
	if sign == '+' {
		side = 1
	}
	if w.shown[side] == differenceLines {
		w.omitted[side]++
		return
	}
	w.shown[side]++
	fmt.Fprintf(&w.out, "%c%d: %s\n", sign, w.offset+i+1, printable(line))
}

// finish returns the text written, and how many lines each side left out.
func (w *differenceWriter) finish() string {
	for side, sign := range []byte{'-', '+'} {
		if w.omitted[side] > 0 {
			fmt.Fprintf(&w.out, "%c… %d more\n", sign, w.omitted[side])
		}
	}
	return w.out.String()
}

// printable is line as written, or quoted where it holds a control character
// other than a tab, or bytes that are not UTF-8, which a reader could not see.
func printable(line string) string {
	unseen := func(r rune) bool { return r == utf8.RuneError || (unicode.IsControl(r) && r != '\t') }
	if strings.IndexFunc(line, unseen) >= 0 {
		return strconv.Quote(line)
	}
	return line
}
