// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package pathfold_test

import (
	"bytes"
	"errors"
	"fmt"
	"math/rand/v2"
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
		{name: "mark order at 30 marks", first: markOrder(29, true), second: markOrder(29, false)},
		{name: "mark order at 31 marks", first: markOrder(30, true), second: markOrder(30, false)},
		{name: "mark order at 32 marks", first: markOrder(31, true), second: markOrder(31, false)},
		{name: "mark order at 101 marks", first: markOrder(100, true), second: markOrder(100, false)},
		{
			name:   "mark order past a U+034F the name already holds",
			first:  "\u034fb" + strings.Repeat("\u0301", 31) + "\u0323",
			second: "\u034f\u1e05" + strings.Repeat("\u0301", 31),
		},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, pathfold.Fold(row.first), pathfold.Fold(row.second), "%q vs %q", row.first, row.second)
		})
	}
}

// markOrder spells b, then acutes acute accents and one dot below, the dot
// last or first: marks of two classes in two orders, which canonical order
// makes one.
func markOrder(acutes int, dotLast bool) string {
	if dotLast {
		return "b" + strings.Repeat("\u0301", acutes) + "\u0323"
	}
	return "b\u0323" + strings.Repeat("\u0301", acutes)
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
		{
			name:   "conjoining vowels with a U+034F among them",
			first:  strings.Repeat("\u1161", 84),
			second: strings.Repeat("\u1161", 30) + "\u034f" + strings.Repeat("\u1161", 54),
		},
		{
			name:   "marks of one class swapped at 31 marks",
			first:  "b" + strings.Repeat("\u0301", 29) + "\u0300\u0301",
			second: "b" + strings.Repeat("\u0301", 29) + "\u0301\u0300",
		},
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
		{name: "two adjacent invalid bytes", input: "\xff\xfe", want: "\ufffd\ufffd"},
		{name: "truncated four-byte lead F0 before a composed letter", input: "\xf0\u00e9", want: "\ufffd\u00e9"},
		{name: "truncated four-byte lead F1 before a composed letter", input: "\xf1\u00e9", want: "\ufffd\u00e9"},
		{name: "truncated four-byte lead F3 before a composed letter", input: "\xf3\u00e9", want: "\ufffd\u00e9"},
		{name: "truncated four-byte lead F4 before a composed letter", input: "\xf4\u00e9", want: "\ufffd\u00e9"},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, pathfold.Fold(row.want), pathfold.Fold(row.input), "%q", row.input)
		})
	}
}

// TestFold_ReadsEveryInvalidSpellingAsRangingReadsIt holds the fold of seeded
// strings mixing bytes that are not UTF-8, truncated sequences and letters
// that decompose to the fold of the same strings read a rune at a time, each
// byte that is not UTF-8 as one U+FFFD.
func TestFold_ReadsEveryInvalidSpellingAsRangingReadsIt(t *testing.T) {
	t.Parallel()
	pieces := []string{
		"\xf0", "\xf1", "\xf3", "\xf4", "\xff", "\xc3", "\xe2\x82", "\xf0\x9f\x98", "\x80",
		"\u00e9", "\u1e05", "\u00c5", "\uac01", "\u0301", "\u0323", "a", "Q",
	}
	const draws = 20_000
	source := rand.NewPCG(2223, 8)
	var builder strings.Builder
	for range draws {
		builder.Reset()
		for range 1 + source.Uint64()%8 {
			if source.Uint64()%4 == 0 {
				builder.WriteByte(byte(0x80 + source.Uint64()%0x80))
				continue
			}
			builder.WriteString(pieces[source.Uint64()%uint64(len(pieces))])
		}
		drawn := builder.String()
		if readAsRunes := string([]rune(drawn)); pathfold.Fold(drawn) != pathfold.Fold(readAsRunes) {
			t.Fatalf("Fold(%+q) = %+q, but Fold(%+q) = %+q",
				drawn, pathfold.Fold(drawn), readAsRunes, pathfold.Fold(readAsRunes))
		}
	}
}

// TestDecomposeUncut_EqualsNFDWhereNFDCutsNothing holds the decomposition
// Fold takes past norm.NFD's cut to norm.NFD itself wherever that cuts
// nothing: each assigned character before two marks, and seeded strings of
// marks and starters.
func TestDecomposeUncut_EqualsNFDWhereNFDCutsNothing(t *testing.T) {
	t.Parallel()
	inputs := make([]string, 0, 320_000)
	for r := range rune(unicode.MaxRune + 1) {
		if utf8.ValidRune(r) && !unicode.Is(unicode.Cn, r) {
			inputs = append(inputs, string(r)+"\u0323\u0301")
		}
	}
	inputs = append(inputs, generateMarkHeavyStrings()...)

	const maxReported = 10
	mismatches := 0
	for _, input := range inputs {
		if norm.NFD.String(input) == pathfold.DecomposeUncut(input) {
			continue
		}
		mismatches++
		if mismatches <= maxReported {
			t.Errorf("DecomposeUncut(%+q) = %+q, norm.NFD gives %+q", input, pathfold.DecomposeUncut(input), norm.NFD.String(input))
		}
	}
	assert.Zero(t, mismatches, "of %d inputs", len(inputs))
}

// generateMarkHeavyStrings draws strings of up to 12 characters, three in four
// a mark, from a fixed seed, keeping those norm.NFD cuts nothing in.
func generateMarkHeavyStrings() []string {
	starters := []rune{'a', '\u00e9', '\u1100', '\u1161', '\uac01', '\u0915', '\u1e9b', '\u034f', '\u212b'}
	marks := make([]rune, 0, 1_000)
	for r := range rune(unicode.MaxRune + 1) {
		if utf8.ValidRune(r) && !unicode.Is(unicode.Cn, r) && norm.NFD.PropertiesString(string(r)).CCC() != 0 {
			marks = append(marks, r)
		}
	}
	const draws = 50_000
	source := rand.NewPCG(2223, 12)
	out := make([]string, 0, draws)
	var builder strings.Builder
	for range draws {
		builder.Reset()
		for range 1 + source.Uint64()%12 {
			if source.Uint64()%4 == 0 {
				builder.WriteRune(starters[source.Uint64()%uint64(len(starters))])
				continue
			}
			builder.WriteRune(marks[source.Uint64()%uint64(len(marks))])
		}
		drawn := builder.String()
		if strings.Count(norm.NFD.String(drawn), "\u034f") == strings.Count(drawn, "\u034f") {
			out = append(out, drawn)
		}
	}
	return out
}

// TestDecomposeUncut_KeepsMarksOfOneClassInTheirOrder holds the order of marks
// of one class past norm.NFD's cut: twenty above, each another rune, between
// twelve below, which canonical order moves before them all.
func TestDecomposeUncut_KeepsMarksOfOneClassInTheirOrder(t *testing.T) {
	t.Parallel()
	const above, below = 230, 220
	aboveMarks := []rune("\u0300\u0301\u0302\u0303\u0304\u0305\u0306\u0307\u0308\u0309" +
		"\u030a\u030b\u030c\u030d\u030e\u030f\u0310\u0311\u0312\u0313")
	belowMarks := []rune("\u0316\u0317\u0318\u0319\u031c\u031d\u031e\u031f\u0320\u0323\u0324\u0325")
	written := []rune{'b'}
	for index, mark := range aboveMarks {
		require.EqualValues(t, above, norm.NFD.PropertiesString(string(mark)).CCC(), "%U", mark)
		written = append(written, mark)
		if index < len(belowMarks) {
			require.EqualValues(t, below, norm.NFD.PropertiesString(string(belowMarks[index])).CCC(), "%U", belowMarks[index])
			written = append(written, belowMarks[index])
		}
	}
	want := "b" + string(belowMarks) + string(aboveMarks)
	assert.Equal(t, want, pathfold.DecomposeUncut(string(written)))
}

// TestFold_HoldsNoFewerCharactersThanItsName holds the fold of every code
// point, and of seeded strings with bytes that are not UTF-8 among them, to at
// least as many characters as it folds, which bounds the bytes a spelling of a
// name can take.
func TestFold_HoldsNoFewerCharactersThanItsName(t *testing.T) {
	t.Parallel()
	changedCodePoints := make([]rune, 0, 4_000)
	for r := range rune(unicode.MaxRune + 1) {
		if !utf8.ValidRune(r) {
			continue
		}
		folded := pathfold.Fold(string(r))
		if folded == "" {
			t.Fatalf("Fold(%+q) holds no character", string(r))
		}
		if folded != string(r) {
			changedCodePoints = append(changedCodePoints, r)
		}
	}

	const draws = 20_000
	source := rand.NewPCG(2223, 17)
	var builder strings.Builder
	for range draws {
		builder.Reset()
		for range 1 + source.Uint64()%40 {
			if source.Uint64()%20 == 0 {
				builder.WriteByte(byte(0x80 + source.Uint64()%0x80))
				continue
			}
			builder.WriteRune(changedCodePoints[source.Uint64()%uint64(len(changedCodePoints))])
		}
		drawn := builder.String()
		folded := pathfold.Fold(drawn)
		require.GreaterOrEqual(t, utf8.RuneCountInString(folded), utf8.RuneCountInString(drawn),
			"Fold(%+q) = %+q", drawn, folded)
	}
}

// TestFold_AllocatesAtMostThreeTimesForAnASCIIName pins Fold's path for a name
// norm.NFD cuts nothing in to the three allocations it makes there, so a
// decomposition a character at a time, which allocates per character, fails
// it. It is not run in parallel: AllocsPerRun measures the process.
func TestFold_AllocatesAtMostThreeTimesForAnASCIIName(t *testing.T) {
	const allocations = 3
	assert.LessOrEqual(t, testing.AllocsPerRun(10, func() { _ = pathfold.Fold("CreateUserRequestBody.kt") }),
		float64(allocations))
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
// to hold and to be rendered under -update, which otherwise leaves the file as
// committed, so a toolchain or x/text release may only extend the golden with
// newly assigned code points.
func TestFold_EveryCharacterEqualsItsGolden(t *testing.T) {
	t.Parallel()
	goldenPath := filepath.Join("testdata", "fold.golden.txt")
	rendered := renderFoldGolden()
	checkCommittedFoldGolden(t, goldenPath, rendered)
	if t.Failed() {
		t.Fatalf("leaving %s as committed", goldenPath)
	}
	irtest.CompareBytes(t, goldenPath, rendered)
}

// checkCommittedFoldGolden verifies that every code point already committed in
// goldenPath is in rendered and still folds as committed, before CompareBytes
// compares or rewrites the file.
func checkCommittedFoldGolden(t *testing.T, goldenPath string, rendered []byte) {
	t.Helper()
	raw, err := os.ReadFile(goldenPath)
	if errors.Is(err, os.ErrNotExist) {
		return
	}
	require.NoError(t, err)

	renderedCodePoints := make([]bool, unicode.MaxRune+1)
	readFoldGolden(t, "the rendered golden", rendered, func(r rune, _ string) { renderedCodePoints[r] = true })

	const maxReported = 10
	mismatches := 0
	report := func(format string, arguments ...any) {
		mismatches++
		if mismatches <= maxReported {
			t.Errorf(format, arguments...)
		}
	}
	readFoldGolden(t, goldenPath, raw, func(r rune, want string) {
		if !renderedCodePoints[r] {
			report("committed fold for %04X is no longer rendered", r)
		}
		if got := pathfold.Fold(string(r)); got != want {
			report("committed fold for %04X changed: golden %q, got %q", r, want, got)
		}
	})
	if mismatches > 0 {
		t.Errorf("%d committed fold entries no longer hold in %s", mismatches, goldenPath)
	}
}

// readFoldGolden calls visit with each code point the fold golden raw covers
// and the fold it records for it; goldenPath names raw in messages.
func readFoldGolden(t *testing.T, goldenPath string, raw []byte, visit func(r rune, want string)) {
	t.Helper()
	lines := strings.Split(strings.TrimSuffix(string(raw), "\n"), "\n")
	for lineNumber, line := range lines {
		if lineNumber == 0 && strings.HasPrefix(line, "# ") {
			continue
		}
		if line == "" {
			continue
		}
		if startText, endText, isRange := strings.Cut(line, ".."); isRange {
			startRune := parseHexRune(t, goldenPath, lineNumber+1, startText)
			endRune := parseHexRune(t, goldenPath, lineNumber+1, endText)
			require.LessOrEqual(t, startRune, endRune, "%s:%d: inverted range %q", goldenPath, lineNumber+1, line)
			for r := startRune; r <= endRune; r++ {
				visit(r, string(r))
			}
			continue
		}
		fields := strings.Fields(line)
		require.GreaterOrEqual(t, len(fields), 2, "%s:%d: malformed fold line %q", goldenPath, lineNumber+1, line)
		inputRune := parseHexRune(t, goldenPath, lineNumber+1, fields[0])
		var wantBuilder strings.Builder
		for _, field := range fields[1:] {
			wantBuilder.WriteRune(parseHexRune(t, goldenPath, lineNumber+1, field))
		}
		visit(inputRune, wantBuilder.String())
	}
}

// parseHexRune decodes an uppercase hexadecimal code point from a golden line.
func parseHexRune(t *testing.T, goldenPath string, lineNumber int, raw string) rune {
	t.Helper()
	value, err := strconv.ParseUint(raw, 16, 32)
	require.NoError(t, err, "%s:%d: invalid hex code point %q", goldenPath, lineNumber, raw)
	r := rune(value)
	require.True(t, utf8.ValidRune(r), "%s:%d: invalid rune %04X", goldenPath, lineNumber, r)
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
