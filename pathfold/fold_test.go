// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package pathfold_test

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"unicode"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/text/cases"
	"golang.org/x/text/unicode/norm"

	"github.com/dexpace/morphic/ir/irtest"
	"github.com/dexpace/morphic/pathfold"
)

func TestFold_JoinsWhatAFileSystemMakesOnePath(t *testing.T) {
	t.Parallel()
	rows := []struct {
		name   string
		first  string
		second string
	}{
		{name: "ASCII letter case", first: "Setup", second: "SetUp"},
		{name: "long s", first: "\u017fetup", second: "setup"},
		{name: "Kelvin sign", first: "\u212a", second: "K"},
		{name: "final sigma", first: "\u03c3\u03c2", second: "\u03c3\u03c3"},
		{name: "sharp s", first: "Stra\u00dfe", second: "Strasse"},
		{name: "composed and decomposed e acute", first: "\u00e9", second: "e\u0301"},
		{name: "canonical mark order", first: "q\u0323\u0307", second: "q\u0307\u0323"},
		{name: "Cherokee case pair", first: "\u13a0", second: "\uab70"},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, pathfold.Fold(row.first), pathfold.Fold(row.second), "%q vs %q", row.first, row.second)
		})
	}
}

// TestFold_JoinsAPairAPFSKeepsApart pins the one pair from the APFS sweeps that
// Fold joins though APFS keeps them apart, which costs a name a suffix rather
// than losing a file (docs/emitters/emit/rules.md).
func TestFold_JoinsAPairAPFSKeepsApart(t *testing.T) {
	t.Parallel()
	assert.Equal(t, pathfold.Fold("a\u0131"), pathfold.Fold("ai"))
}

func TestFold_KeepsApartWhatItMust(t *testing.T) {
	t.Parallel()
	rows := []struct {
		name   string
		first  string
		second string
	}{
		{name: "compatibility letter", first: "\u210cx", second: "hx"},
		{name: "non-spacing mark", first: "caf\u00e9", second: "cafe"},
		{name: "spacing combining mark", first: "\u0928\u093e\u092e", second: "\u0928\u092e"},
		{name: "format character", first: "x\u200dy", second: "xy"},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			t.Parallel()
			assert.NotEqual(t, pathfold.Fold(row.first), pathfold.Fold(row.second), "%q vs %q", row.first, row.second)
		})
	}
}

func TestFold_ReadsAByteThatIsNotUTF8AsAReplacementCharacter(t *testing.T) {
	t.Parallel()
	rows := []struct {
		name  string
		input string
		want  string
	}{
		{name: "lone invalid byte", input: "\xff", want: "\ufffd"},
		{name: "invalid byte inside ASCII", input: "a\xfeb", want: "a\ufffdb"},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, pathfold.Fold(row.want), pathfold.Fold(row.input), "%q", row.input)
		})
	}
}

// TestFold_IsIdempotent holds Fold(Fold(r)) == Fold(r) for every valid Unicode
// scalar value.
func TestFold_IsIdempotent(t *testing.T) {
	t.Parallel()
	for r := range rune(unicode.MaxRune + 1) {
		if !utf8.ValidRune(r) {
			continue
		}
		once := pathfold.Fold(string(r))
		if again := pathfold.Fold(once); again != once {
			t.Errorf("Fold(Fold(%04X)) = %q, want %q", r, again, once)
		}
	}
}

// TestFold_EveryCharacterEqualsItsGolden pins the fold of every assigned code
// point against testdata/fold.golden.txt. Every committed entry must continue
// to hold under -update, so a toolchain or x/text release may only extend the
// golden with newly assigned code points.
func TestFold_EveryCharacterEqualsItsGolden(t *testing.T) {
	t.Parallel()
	goldenPath := filepath.Join("testdata", "fold.golden.txt")
	checkCommittedFoldGolden(t, goldenPath)
	irtest.CompareBytes(t, goldenPath, renderFoldGolden())
}

// checkCommittedFoldGolden verifies that every line already committed in
// goldenPath still holds under pathfold.Fold before CompareBytes compares or
// rewrites the file.
func checkCommittedFoldGolden(t *testing.T, goldenPath string) {
	t.Helper()
	raw, err := os.ReadFile(goldenPath)
	if errors.Is(err, os.ErrNotExist) {
		return
	}
	require.NoError(t, err)

	const maxReported = 10
	mismatches := 0
	report := func(r rune, want, got string) {
		mismatches++
		if mismatches <= maxReported {
			t.Errorf("committed fold for %04X changed: golden %q, got %q", r, want, got)
		}
	}

	lines := strings.Split(strings.TrimSuffix(string(raw), "\n"), "\n")
	for lineNo, line := range lines {
		if lineNo == 0 && strings.HasPrefix(line, "# ") {
			continue
		}
		if line == "" {
			continue
		}
		if startText, endText, isRange := strings.Cut(line, ".."); isRange {
			startRune := parseHexRune(t, goldenPath, lineNo+1, startText)
			endRune := parseHexRune(t, goldenPath, lineNo+1, endText)
			require.LessOrEqual(t, startRune, endRune, "%s:%d: inverted range %q", goldenPath, lineNo+1, line)
			for r := startRune; r <= endRune; r++ {
				want := string(r)
				if got := pathfold.Fold(want); got != want {
					report(r, want, got)
				}
			}
			continue
		}
		fields := strings.Fields(line)
		require.GreaterOrEqual(t, len(fields), 2, "%s:%d: malformed fold line %q", goldenPath, lineNo+1, line)
		inputRune := parseHexRune(t, goldenPath, lineNo+1, fields[0])
		var wantBuf strings.Builder
		for _, field := range fields[1:] {
			wantBuf.WriteRune(parseHexRune(t, goldenPath, lineNo+1, field))
		}
		want := wantBuf.String()
		if got := pathfold.Fold(string(inputRune)); got != want {
			report(inputRune, want, got)
		}
	}
	if mismatches > 0 {
		t.Errorf("%d committed fold entries no longer hold in %s", mismatches, goldenPath)
	}
}

// parseHexRune decodes an uppercase hexadecimal code point from a golden line.
func parseHexRune(t *testing.T, goldenPath string, lineNo int, raw string) rune {
	t.Helper()
	value, err := strconv.ParseUint(raw, 16, 32)
	require.NoError(t, err, "%s:%d: invalid hex code point %q", goldenPath, lineNo, raw)
	r := rune(value)
	require.True(t, utf8.ValidRune(r), "%s:%d: invalid rune %04X", goldenPath, lineNo, r)
	return r
}

// renderFoldGolden produces the golden table covering every assigned code
// point: maximal contiguous runs of code points folding to themselves as
// START..END lines, and every other assigned code point with its folded runes.
func renderFoldGolden() []byte {
	var out bytes.Buffer
	fmt.Fprintf(&out, "# fold of every assigned code point; unicode=%s norm=%s cases=%s\n",
		unicode.Version, norm.Version, cases.UnicodeVersion)

	inRun := false
	var runStart, runEnd rune
	flushRun := func() {
		if !inRun {
			return
		}
		fmt.Fprintf(&out, "%04X..%04X\n", runStart, runEnd)
		inRun = false
	}

	for r := range rune(unicode.MaxRune + 1) {
		if !utf8.ValidRune(r) || unicode.Is(unicode.Cn, r) {
			flushRun()
			continue
		}
		text := string(r)
		folded := pathfold.Fold(text)
		if folded == text {
			if !inRun {
				runStart, runEnd, inRun = r, r, true
			} else {
				runEnd = r
			}
			continue
		}
		flushRun()
		fmt.Fprintf(&out, "%04X", r)
		for _, foldedRune := range folded {
			fmt.Fprintf(&out, " %04X", foldedRune)
		}
		out.WriteByte('\n')
	}
	flushRun()
	return out.Bytes()
}
