// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package pathfold_test

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"io/fs"
	"math/rand/v2"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"testing"
	"unicode"
	"unicode/utf8"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"
	"golang.org/x/text/unicode/norm"

	"github.com/dexpace/morphic/ir/irtest"
	"github.com/dexpace/morphic/pathfold"
)

type sweepInput struct {
	name  string
	names []string
}

type apfsRecord struct {
	tables  string
	sweeps  map[string]sweepRecord
	apart   map[string][][][]string
	wrote   []string
	refused []string
}

type sweepRecord struct {
	names     int
	files     int
	partition string
}

// TestFold_JoinsWhatAPFSJoins holds pathfold.Fold to the partition of names
// that a case-insensitive APFS volume collapses into one file across the T-52
// sweeps, verifies every weaker fold parts at least one file, checks
// idempotence across all generated names, and holds pathfold.Portable to the
// code points APFS refused and the runs of class zero it wrote. The record at
// testdata/apfs.golden.txt is rewritten only on macOS with -update.
func TestFold_JoinsWhatAPFSJoins(t *testing.T) {
	t.Parallel()
	sweeps := []sweepInput{
		{name: "characters", names: generateCharacterSweep()},
		{name: "sequences", names: generateSequenceSweep()},
		{name: "mark-runs", names: generateMarkRunSweep()},
	}
	recordPath := filepath.Join("testdata", "apfs.golden.txt")

	if irtest.Update() {
		if runtime.GOOS == "darwin" {
			raw := recordAPFSSweeps(t, sweeps)
			irtest.CompareBytes(t, recordPath, raw)
		} else {
			t.Logf("leaving %s unchanged: APFS sweeps require macOS", recordPath)
		}
	}

	raw, err := os.ReadFile(recordPath)
	if err != nil {
		t.Fatalf("read APFS record %s (rewrite on macOS with -update): %v", recordPath, err)
	}
	record := parseAPFSRecord(t, recordPath, raw)

	t.Run("tables", func(t *testing.T) {
		wantTables := currentTablesLine()
		if record.tables != wantTables {
			t.Errorf("Unicode tables moved: record has %q, build has %q; rerun the APFS sweeps on macOS with -update",
				record.tables, wantTables)
		}
	})

	partitions := make(map[string][][]int, len(sweeps))
	allSweepsPassed := true
	for _, sweep := range sweeps {
		passed := t.Run(sweep.name, func(t *testing.T) {
			recordedSweep, found := record.sweeps[sweep.name]
			require.True(t, found, "APFS record %s lacks sweep %q", recordPath, sweep.name)
			partitions[sweep.name] = verifySweepAgainstRecord(t, sweep, recordedSweep, record.apart[sweep.name])
		})
		if !passed {
			allSweepsPassed = false
		}
	}

	t.Run("weaker folds", func(t *testing.T) {
		if !allSweepsPassed || len(partitions) != len(sweeps) {
			t.Skip("needs every sweep's partition, which a failed or filtered-out sweep does not give")
		}
		checkWeakerFoldsSplitFiles(t, sweeps, partitions)
	})

	t.Run("runs of class zero", func(t *testing.T) {
		require.Empty(t, cmp.Diff(classZeroRuns(), record.wrote), "names APFS wrote (-want +got)")
		for _, name := range record.wrote {
			assert.NoError(t, pathfold.Portable(name), "APFS wrote %+q", name)
		}
	})

	t.Run("refused code points", func(t *testing.T) {
		if record.tables != currentTablesLine() {
			t.Skip("the record was made under other Unicode tables")
		}
		checkRefusedCodePoints(t, record.refused)
	})

	t.Run("idempotent", func(t *testing.T) {
		for _, sweep := range sweeps {
			for _, name := range sweep.names {
				once := pathfold.Fold(name)
				if again := pathfold.Fold(once); again != once {
					t.Errorf("sweep %s: Fold(Fold(%q)) = %q, want %q", sweep.name, name, again, once)
				}
			}
		}
	})
}

func currentTablesLine() string {
	return fmt.Sprintf("unicode=%s norm=%s cases=%s", unicode.Version, norm.Version, cases.UnicodeVersion)
}

func generateCharacterSweep() []string {
	upperCaser := cases.Upper(language.Und)
	lowerCaser := cases.Lower(language.Und)
	titleCaser := cases.Title(language.Und)
	foldCaser := cases.Fold()

	out := make([]string, 0, 180_000)
	for r := rune(1); r <= unicode.MaxRune; r++ {
		if !utf8.ValidRune(r) || r == '/' || unicode.Is(unicode.Cn, r) || unicode.Is(unicode.Co, r) {
			continue
		}
		character := string(r)
		forms := [9]string{
			character,
			norm.NFC.String(character),
			norm.NFD.String(character),
			norm.NFKC.String(character),
			norm.NFKD.String(character),
			upperCaser.String(character),
			lowerCaser.String(character),
			titleCaser.String(character),
			foldCaser.String(character),
		}
		for _, form := range forms {
			if strings.ContainsAny(form, "/\x00") {
				continue
			}
			out = append(out, "a"+form)
		}
	}
	slices.Sort(out)
	return slices.Compact(out)
}

func generateSequenceSweep() []string {
	upperCaser := cases.Upper(language.Und)
	lowerCaser := cases.Lower(language.Und)
	foldCaser := cases.Fold()

	alphabet := make([]rune, 0, 16_000)
	for r := rune(1); r <= unicode.MaxRune; r++ {
		if !utf8.ValidRune(r) || r == '/' || unicode.Is(unicode.Cn, r) || unicode.Is(unicode.Co, r) {
			continue
		}
		character := string(r)
		if foldCaser.String(character) != character ||
			norm.NFD.String(character) != character ||
			norm.NFC.String(character) != character ||
			norm.NFD.PropertiesString(character).CCC() != 0 {
			alphabet = append(alphabet, r)
		}
	}

	const (
		draws    = 40_000
		maxBytes = 255
	)
	source := rand.NewPCG(2223, 52)
	alphabetLength := uint64(len(alphabet))
	out := make([]string, 0, 100_000)

	var baseBuilder strings.Builder
	for range draws {
		length := 1 + source.Uint64()%4
		baseBuilder.Reset()
		for range length {
			baseBuilder.WriteRune(alphabet[source.Uint64()%alphabetLength])
		}
		base := baseBuilder.String()
		decomposedBase := norm.NFD.String(base)
		forms := [9]string{
			base,
			norm.NFC.String(base),
			decomposedBase,
			upperCaser.String(base),
			lowerCaser.String(base),
			foldCaser.String(base),
			reverseMarks(decomposedBase),
			reverseMarks(base),
			decomposeEachRuneAlone(base),
		}
		for _, form := range forms {
			if form == "" || form == "." || form == ".." || len(form) > maxBytes || strings.ContainsAny(form, "/\x00") {
				continue
			}
			out = append(out, form)
		}
	}
	slices.Sort(out)
	return slices.Compact(out)
}

// maxMarksInARow is the longest run of marks a sweep may write: APFS writes 31
// and refuses 32, and creating a refused name of 34 marks in a directory
// holding files spun at full CPU without returning.
const maxMarksInARow = 31

// generateMarkRunSweep draws runs of 28 to 31 marks that decompose to
// themselves, behind a starter, each beside every spelling with two
// neighbouring marks swapped: the lengths at which norm.NFD's stream-safe
// form cuts a run, and the swaps canonical order makes one or keeps apart.
func generateMarkRunSweep() []string {
	marks := make([]rune, 0, 1_000)
	for r := rune(1); r <= unicode.MaxRune; r++ {
		if !utf8.ValidRune(r) || unicode.Is(unicode.Cn, r) {
			continue
		}
		character := string(r)
		if norm.NFD.String(character) == character && norm.NFD.PropertiesString(character).CCC() != 0 {
			marks = append(marks, r)
		}
	}

	const (
		draws    = 1_000
		minMarks = 28
	)
	starters := []string{"a", "Q", "\u0915"}
	source := rand.NewPCG(2223, 31)
	out := make([]string, 0, draws*maxMarksInARow)
	for range draws {
		length := minMarks + source.Uint64()%(maxMarksInARow-minMarks+1)
		run := make([]rune, length)
		for index := range run {
			run[index] = marks[source.Uint64()%uint64(len(marks))]
		}
		starter := starters[source.Uint64()%uint64(len(starters))]
		out = append(out, starter+string(run))
		for index := range len(run) - 1 {
			swapped := slices.Clone(run)
			swapped[index], swapped[index+1] = swapped[index+1], swapped[index]
			out = append(out, starter+string(swapped))
		}
	}
	slices.Sort(out)
	return slices.Compact(out)
}

// classZeroRuns are names APFS wrote holding long runs norm.NFD counts as
// non-starters, characters of class zero among them.
func classZeroRuns() []string {
	return []string{
		strings.Repeat("\u1161", 85),
		strings.Repeat("\u11a8", 85),
		strings.Repeat("\u09be", 85),
		strings.Repeat("\uff9e", 40),
		"a" + strings.Repeat("\u0316", 30) + "\u1161" + strings.Repeat("\u0316", 30),
	}
}

// writtenOnAPFS creates each of names in an empty directory, requiring APFS to
// write it, and returns them as record lines.
func writtenOnAPFS(t *testing.T, names []string) []string {
	t.Helper()
	dir := t.TempDir()
	lines := make([]string, 0, len(names))
	for _, name := range names {
		require.LessOrEqual(t, marksInARowBeforeWriting(name), maxMarksInARow, "%+q holds more marks in a row than APFS writes", name)
		target := filepath.Join(dir, name)
		file, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		require.NoError(t, err, "create %+q", name)
		require.NoError(t, file.Close(), "close %+q", name)
		lines = append(lines, "wrote "+strconv.QuoteToASCII(name))
	}
	return lines
}

// marksInARowBeforeWriting counts the longest run of marks in name's
// decomposition, each character decomposed alone, which orders no mark and so
// moves none across a starter. It is counted apart from Portable's count, so a
// defect there cannot let a sweep write a name the volume hangs on.
func marksInARowBeforeWriting(name string) int {
	longest, current := 0, 0
	for _, character := range decomposeEachRuneAlone(name) {
		if norm.NFD.PropertiesString(string(character)).CCC() == 0 {
			current = 0
			continue
		}
		current++
		longest = max(longest, current)
	}
	return longest
}

func reverseMarks(input string) string {
	runes := []rune(input)
	for start := 0; start < len(runes); {
		if norm.NFD.PropertiesString(string(runes[start])).CCC() == 0 {
			start++
			continue
		}
		end := start + 1
		for end < len(runes) && norm.NFD.PropertiesString(string(runes[end])).CCC() != 0 {
			end++
		}
		slices.Reverse(runes[start:end])
		start = end
	}
	return string(runes)
}

func decomposeEachRuneAlone(input string) string {
	var out strings.Builder
	out.Grow(len(input))
	for _, r := range input {
		out.WriteString(norm.NFD.String(string(r)))
	}
	return out.String()
}

func recordAPFSSweeps(t *testing.T, sweeps []sweepInput) []byte {
	t.Helper()
	for _, sweep := range sweeps {
		for _, name := range sweep.names {
			require.LessOrEqual(t, marksInARowBeforeWriting(name), maxMarksInARow,
				"sweep %s: %+q holds more marks in a row than APFS writes", sweep.name, name)
		}
	}
	assertCaseAndNormInsensitiveVolume(t)

	var out bytes.Buffer
	out.WriteString("# which names APFS made one file, which it wrote and which code points it refused, " +
		"for TestFold_JoinsWhatAPFSJoins; rewritten on macOS by -update\n")
	fmt.Fprintf(&out, "tables %s\n", currentTablesLine())

	var apartBuffer bytes.Buffer
	for _, sweep := range sweeps {
		partition := sweepOnAPFS(t, sweep.names)
		checkFoldNeverSplitsAPFSFiles(t, sweep.name, sweep.names, partition)
		digest := partitionDigest(partition)
		fmt.Fprintf(&out, "sweep %s names=%d files=%d partition=%s\n",
			sweep.name, len(sweep.names), len(partition), digest)
		for _, line := range buildApartLines(sweep.name, sweep.names, partition) {
			apartBuffer.WriteString(line)
			apartBuffer.WriteByte('\n')
		}
	}
	out.Write(apartBuffer.Bytes())
	for _, line := range writtenOnAPFS(t, classZeroRuns()) {
		out.WriteString(line)
		out.WriteByte('\n')
	}
	for _, line := range codePointRanges(refusedOnAPFS(t)) {
		fmt.Fprintf(&out, "refused %s\n", line)
	}
	return out.Bytes()
}

// refusedOnAPFS creates, in an empty directory, a file named a and each code
// point from U+0080 that UTF-8 encodes, removing each it writes, and reports
// whether APFS refused each.
func refusedOnAPFS(t *testing.T) func(rune) bool {
	t.Helper()
	dir := t.TempDir()
	refused := make([]bool, unicode.MaxRune+1)
	for r := rune(0x80); r <= unicode.MaxRune; r++ {
		if !utf8.ValidRune(r) {
			continue
		}
		target := filepath.Join(dir, "a"+string(r))
		file, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err != nil {
			require.NotErrorIs(t, err, fs.ErrExist, "create %+q", target)
			refused[r] = true
			continue
		}
		require.NoError(t, file.Close(), "close %+q", target)
		require.NoError(t, os.Remove(target), "remove %+q", target)
	}
	return func(r rune) bool { return refused[r] }
}

// codePointRanges lists the code points from U+0080 that UTF-8 encodes and
// holds says hold, as START..END lines of uppercase hex, or one code point
// alone where a range holds one.
func codePointRanges(holds func(rune) bool) []string {
	var lines []string
	start, end, inRange := rune(0), rune(0), false
	flush := func() {
		if !inRange {
			return
		}
		inRange = false
		if start == end {
			lines = append(lines, fmt.Sprintf("%04X", start))
			return
		}
		lines = append(lines, fmt.Sprintf("%04X..%04X", start, end))
	}
	for r := rune(0x80); r <= unicode.MaxRune; r++ {
		if !utf8.ValidRune(r) || !holds(r) {
			flush()
			continue
		}
		if !inRange {
			start, inRange = r, true
		}
		end = r
	}
	flush()
	return lines
}

// checkRefusedCodePoints holds the code points APFS refused to those the
// tables leave unassigned, and pathfold.Portable to refusing exactly those
// behind an a.
func checkRefusedCodePoints(t *testing.T, recorded []string) {
	t.Helper()
	unassigned := func(r rune) bool { return unicode.Is(unicode.Cn, r) }
	require.Empty(t, cmp.Diff(codePointRanges(unassigned), recorded),
		"code points APFS refused, unassigned ones (-want +got)")

	const maxReported = 10
	disagreements := 0
	for r := rune(0x80); r <= unicode.MaxRune; r++ {
		if !utf8.ValidRune(r) {
			continue
		}
		if refused := pathfold.Portable("a"+string(r)) != nil; refused != unassigned(r) {
			disagreements++
			if disagreements <= maxReported {
				t.Errorf("Portable(%+q) refused=%v, APFS refused=%v", "a"+string(r), refused, unassigned(r))
			}
		}
	}
	assert.Zero(t, disagreements, "code points Portable and APFS disagree on")
}

func assertCaseAndNormInsensitiveVolume(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	probes := [][2]string{
		{"Probe", "PROBE"},
		{"\u00e9", "e\u0301"},
	}
	for _, pair := range probes {
		firstPath := filepath.Join(dir, pair[0])
		file, err := os.OpenFile(firstPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		require.NoError(t, err, "create probe %q", pair[0])
		require.NoError(t, file.Close())

		secondPath := filepath.Join(dir, pair[1])
		other, err := os.OpenFile(secondPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err == nil {
			require.NoError(t, other.Close())
			t.Fatalf("APFS sweeps require a case- and normalization-insensitive volume: %q and %q created two files",
				pair[0], pair[1])
		}
		require.ErrorIs(t, err, fs.ErrExist, "probe %q against %q", pair[1], pair[0])
	}
}

func sweepOnAPFS(t *testing.T, names []string) [][]int {
	t.Helper()
	dir := t.TempDir()
	groups := make(map[int][]int, len(names))
	order := make([]int, 0, len(names))

	for index, name := range names {
		target := filepath.Join(dir, name)
		file, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err == nil {
			_, writeErr := file.WriteString(strconv.Itoa(index))
			closeErr := file.Close()
			require.NoError(t, writeErr, "write index for %q", name)
			require.NoError(t, closeErr, "close %q", name)
			groups[index] = []int{index}
			order = append(order, index)
			continue
		}
		require.ErrorIs(t, err, fs.ErrExist, "create %q", name)
		content, readErr := os.ReadFile(target)
		require.NoError(t, readErr, "read existing owner of %q", name)
		owner, parseErr := strconv.Atoi(string(content))
		require.NoError(t, parseErr, "parse owner index %q of %q", string(content), name)
		groups[owner] = append(groups[owner], index)
	}

	partition := make([][]int, 0, len(order))
	for _, owner := range order {
		partition = append(partition, groups[owner])
	}
	return partition
}

func checkFoldNeverSplitsAPFSFiles(t *testing.T, sweepName string, names []string, partition [][]int) {
	t.Helper()
	const maxReported = 10
	splits := 0
	for _, group := range partition {
		firstFold := pathfold.Fold(names[group[0]])
		for _, index := range group[1:] {
			if gotFold := pathfold.Fold(names[index]); gotFold != firstFold {
				splits++
				if splits <= maxReported {
					t.Errorf("sweep %s: Fold splits APFS file %q (%q) from %q (%q)",
						sweepName, names[group[0]], firstFold, names[index], gotFold)
				}
			}
		}
	}
	if splits > maxReported {
		t.Errorf("sweep %s: Fold split %d names across APFS files", sweepName, splits)
	}
}

func buildApartLines(sweepName string, names []string, partition [][]int) []string {
	byFold := make(map[string][][]string, len(partition))
	for _, group := range partition {
		fileNames := make([]string, len(group))
		for i, index := range group {
			fileNames[i] = names[index]
		}
		folded := pathfold.Fold(fileNames[0])
		byFold[folded] = append(byFold[folded], fileNames)
	}

	type apartClass struct {
		firstName string
		line      string
	}
	var classes []apartClass
	for _, files := range byFold {
		if len(files) < 2 {
			continue
		}
		slices.SortFunc(files, func(left, right []string) int {
			return strings.Compare(left[0], right[0])
		})
		var line strings.Builder
		fmt.Fprintf(&line, "apart %s", sweepName)
		for fileIndex, fileNames := range files {
			if fileIndex > 0 {
				line.WriteString(" |")
			}
			for _, name := range fileNames {
				line.WriteByte(' ')
				line.WriteString(strconv.QuoteToASCII(name))
			}
		}
		classes = append(classes, apartClass{firstName: files[0][0], line: line.String()})
	}
	slices.SortFunc(classes, func(left, right apartClass) int {
		return strings.Compare(left.firstName, right.firstName)
	})
	lines := make([]string, len(classes))
	for i, item := range classes {
		lines[i] = item.line
	}
	return lines
}

func partitionDigest(partition [][]int) string {
	hasher := sha256.New()
	var lineBuffer []byte
	for _, group := range partition {
		lineBuffer = lineBuffer[:0]
		for i, index := range group {
			if i > 0 {
				lineBuffer = append(lineBuffer, ' ')
			}
			lineBuffer = strconv.AppendInt(lineBuffer, int64(index), 10)
		}
		lineBuffer = append(lineBuffer, '\n')
		hasher.Write(lineBuffer)
	}
	return fmt.Sprintf("sha256:%x", hasher.Sum(nil))
}

func parseAPFSRecord(t *testing.T, recordPath string, raw []byte) apfsRecord {
	t.Helper()
	record := apfsRecord{
		sweeps: make(map[string]sweepRecord),
		apart:  make(map[string][][][]string),
	}
	lines := strings.Split(strings.TrimSuffix(string(raw), "\n"), "\n")
	for lineNumber, line := range lines {
		if line == "" || strings.HasPrefix(line, "# ") {
			continue
		}
		kind, rest, ok := strings.Cut(line, " ")
		require.True(t, ok, "%s:%d: malformed line %q", recordPath, lineNumber+1, line)
		switch kind {
		case "tables":
			record.tables = rest
		case "sweep":
			name, entry := parseSweepLine(t, recordPath, lineNumber+1, rest)
			record.sweeps[name] = entry
		case "apart":
			name, groups := parseApartLine(t, recordPath, lineNumber+1, rest)
			record.apart[name] = append(record.apart[name], groups)
		case "wrote":
			name, err := strconv.Unquote(rest)
			require.NoError(t, err, "%s:%d: malformed written name %q", recordPath, lineNumber+1, rest)
			record.wrote = append(record.wrote, name)
		case "refused":
			record.refused = append(record.refused, rest)
		default:
			t.Fatalf("%s:%d: unknown record kind %q", recordPath, lineNumber+1, kind)
		}
	}
	return record
}

func parseSweepLine(t *testing.T, recordPath string, lineNumber int, rest string) (string, sweepRecord) {
	t.Helper()
	fields := strings.Fields(rest)
	require.Len(t, fields, 4, "%s:%d: malformed sweep line %q", recordPath, lineNumber, rest)
	sweepName := fields[0]
	namesText, hasNames := strings.CutPrefix(fields[1], "names=")
	filesText, hasFiles := strings.CutPrefix(fields[2], "files=")
	partitionText, hasPartition := strings.CutPrefix(fields[3], "partition=")
	require.True(t, hasNames && hasFiles && hasPartition, "%s:%d: malformed sweep fields %q", recordPath, lineNumber, rest)

	nameCount, err := strconv.Atoi(namesText)
	require.NoError(t, err, "%s:%d: invalid names count %q", recordPath, lineNumber, namesText)
	fileCount, err := strconv.Atoi(filesText)
	require.NoError(t, err, "%s:%d: invalid files count %q", recordPath, lineNumber, filesText)
	return sweepName, sweepRecord{names: nameCount, files: fileCount, partition: partitionText}
}

func parseApartLine(t *testing.T, recordPath string, lineNumber int, rest string) (string, [][]string) {
	t.Helper()
	sweepName, body, ok := strings.Cut(rest, " ")
	require.True(t, ok, "%s:%d: malformed apart line %q", recordPath, lineNumber, rest)

	var groups [][]string
	var current []string
	body = strings.TrimLeft(body, " ")
	for body != "" {
		if body[0] == '|' {
			require.NotEmpty(t, current, "%s:%d: empty file group in apart line", recordPath, lineNumber)
			groups = append(groups, current)
			current = nil
			body = strings.TrimLeft(body[1:], " ")
			continue
		}
		quoted, err := strconv.QuotedPrefix(body)
		require.NoError(t, err, "%s:%d: malformed quoted name in %q", recordPath, lineNumber, body)
		unquoted, err := strconv.Unquote(quoted)
		require.NoError(t, err, "%s:%d: unquote %s", recordPath, lineNumber, quoted)
		current = append(current, unquoted)
		body = body[len(quoted):]
		require.True(t, body == "" || body[0] == ' ', "%s:%d: expected space after %s", recordPath, lineNumber, quoted)
		body = strings.TrimLeft(body, " ")
	}
	require.NotEmpty(t, current, "%s:%d: trailing separator in apart line", recordPath, lineNumber)
	groups = append(groups, current)
	require.GreaterOrEqual(t, len(groups), 2, "%s:%d: apart line names fewer than two files", recordPath, lineNumber)
	return sweepName, groups
}

func verifySweepAgainstRecord(t *testing.T, sweep sweepInput, record sweepRecord, apartClasses [][][]string) [][]int {
	t.Helper()
	require.Len(t, sweep.names, record.names,
		"sweep %s generated %d names, record expects %d", sweep.name, len(sweep.names), record.names)

	indexByName := make(map[string]int, len(sweep.names))
	byFold := make(map[string][]int, record.files)
	for index, name := range sweep.names {
		indexByName[name] = index
		folded := pathfold.Fold(name)
		byFold[folded] = append(byFold[folded], index)
	}

	splitByFold := make(map[string][][]int, len(apartClasses))
	for _, groups := range apartClasses {
		classFold := ""
		listedCount := 0
		seen := make(map[int]bool)
		var splitGroups [][]int
		for _, fileNames := range groups {
			fileIndexes := make([]int, 0, len(fileNames))
			for _, name := range fileNames {
				index, exists := indexByName[name]
				require.True(t, exists, "sweep %s: apart name %q is not in generated sweep", sweep.name, name)
				require.False(t, seen[index], "sweep %s: apart name %q listed twice", sweep.name, name)
				seen[index] = true
				folded := pathfold.Fold(name)
				if classFold == "" {
					classFold = folded
				} else {
					require.Equal(t, classFold, folded,
						"sweep %s: apart line mixes fold classes (%q vs %q for %q)", sweep.name, classFold, folded, name)
				}
				fileIndexes = append(fileIndexes, index)
				listedCount++
			}
			slices.Sort(fileIndexes)
			splitGroups = append(splitGroups, fileIndexes)
		}
		_, duplicateClass := splitByFold[classFold]
		require.False(t, duplicateClass, "sweep %s: two apart lines name fold class %q", sweep.name, classFold)
		classMembers := byFold[classFold]
		require.Len(t, classMembers, listedCount,
			"sweep %s: apart line lists %d names, fold class %q holds %d", sweep.name, listedCount, classFold, len(classMembers))
		splitByFold[classFold] = splitGroups
	}

	partition := make([][]int, 0, record.files)
	for folded, members := range byFold {
		if splitGroups, isSplit := splitByFold[folded]; isSplit {
			partition = append(partition, splitGroups...)
			continue
		}
		partition = append(partition, members)
	}
	slices.SortFunc(partition, func(left, right []int) int {
		return left[0] - right[0]
	})

	gotDigest := partitionDigest(partition)
	if len(partition) != record.files || gotDigest != record.partition {
		switch {
		case len(partition) > record.files:
			t.Errorf("sweep %s: Fold produced %d classes, APFS has %d (Fold parts names APFS joins); digest %s vs %s",
				sweep.name, len(partition), record.files, gotDigest, record.partition)
		case len(partition) < record.files:
			t.Errorf("sweep %s: Fold produced %d classes, APFS has %d (Fold joins files APFS keeps apart); digest %s vs %s",
				sweep.name, len(partition), record.files, gotDigest, record.partition)
		default:
			t.Errorf("sweep %s: Fold produced %d classes matching APFS count, but grouped names otherwise; digest %s vs %s",
				sweep.name, len(partition), gotDigest, record.partition)
		}
	}
	return partition
}

func checkWeakerFoldsSplitFiles(t *testing.T, sweeps []sweepInput, partitions map[string][][]int) {
	t.Helper()
	weaker := []struct {
		name string
		fold func(string) string
	}{
		{name: "each character decomposed alone", fold: foldEachCharacterAlone},
		{name: "cases.Fold without upper-casing", fold: foldWithoutUpperCasing},
		{name: "ASCII letters alone", fold: foldASCIILettersAlone},
		{name: "norm.NFD alone", fold: foldWithNFDAlone},
	}
	for _, variant := range weaker {
		totalSplits := 0
		for _, sweep := range sweeps {
			splits := countSplitNames(sweep.names, partitions[sweep.name], variant.fold)
			t.Logf("weaker fold %q on sweep %s: %d split names", variant.name, sweep.name, splits)
			totalSplits += splits
		}
		assert.Positive(t, totalSplits, "weaker fold %q split no APFS file across either sweep", variant.name)
	}
}

func countSplitNames(names []string, partition [][]int, fold func(string) string) int {
	splits := 0
	for _, group := range partition {
		firstFold := fold(names[group[0]])
		for _, index := range group[1:] {
			if fold(names[index]) != firstFold {
				splits++
			}
		}
	}
	return splits
}

func foldEachCharacterAlone(name string) string {
	return cases.Fold().String(strings.Map(unicode.ToUpper, decomposeEachRuneAlone(name)))
}

func foldWithNFDAlone(name string) string {
	return cases.Fold().String(strings.Map(unicode.ToUpper, norm.NFD.String(name)))
}

func foldWithoutUpperCasing(name string) string {
	return cases.Fold().String(norm.NFD.String(name))
}

func foldASCIILettersAlone(name string) string {
	var out strings.Builder
	out.Grow(len(name))
	for i := range len(name) {
		character := name[i]
		if character >= 'A' && character <= 'Z' {
			character += 'a' - 'A'
		}
		out.WriteByte(character)
	}
	return out.String()
}
