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
	tables string
	sweeps map[string]sweepRecord
	apart  map[string][][][]string
}

type sweepRecord struct {
	names     int
	files     int
	partition string
}

// TestFold_JoinsWhatAPFSJoins holds pathfold.Fold to the partition of names
// that a case-insensitive APFS volume collapses into one file across both T-52
// sweeps, verifies every weaker fold parts at least one file, and checks
// idempotence across all generated names. The record at
// testdata/apfs.golden.txt is rewritten only on macOS with -update.
func TestFold_JoinsWhatAPFSJoins(t *testing.T) {
	sweeps := []sweepInput{
		{name: "characters", names: generateCharacterSweep()},
		{name: "sequences", names: generateSequenceSweep()},
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
	allSweepsOK := true
	for _, sweep := range sweeps {
		passed := t.Run(sweep.name, func(t *testing.T) {
			rec, found := record.sweeps[sweep.name]
			require.True(t, found, "APFS record %s lacks sweep %q", recordPath, sweep.name)
			partitions[sweep.name] = verifySweepAgainstRecord(t, sweep, rec, record.apart[sweep.name])
		})
		if !passed {
			allSweepsOK = false
		}
	}

	if allSweepsOK {
		t.Run("weaker folds", func(t *testing.T) {
			checkWeakerFoldsSplitFiles(t, sweeps, partitions)
		})
	}

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
		ch := string(r)
		forms := [9]string{
			ch,
			norm.NFC.String(ch),
			norm.NFD.String(ch),
			norm.NFKC.String(ch),
			norm.NFKD.String(ch),
			upperCaser.String(ch),
			lowerCaser.String(ch),
			titleCaser.String(ch),
			foldCaser.String(ch),
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
		ch := string(r)
		if foldCaser.String(ch) != ch ||
			norm.NFD.String(ch) != ch ||
			norm.NFC.String(ch) != ch ||
			norm.NFD.PropertiesString(ch).CCC() != 0 {
			alphabet = append(alphabet, r)
		}
	}

	const (
		draws    = 40_000
		maxBytes = 255
	)
	source := rand.NewPCG(2223, 52)
	alphaLen := uint64(len(alphabet))
	out := make([]string, 0, 100_000)

	var baseBuf strings.Builder
	for range draws {
		length := 1 + source.Uint64()%4
		baseBuf.Reset()
		for range length {
			baseBuf.WriteRune(alphabet[source.Uint64()%alphaLen])
		}
		base := baseBuf.String()
		nfdBase := norm.NFD.String(base)
		forms := [9]string{
			base,
			norm.NFC.String(base),
			nfdBase,
			upperCaser.String(base),
			lowerCaser.String(base),
			foldCaser.String(base),
			reverseMarks(nfdBase),
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
	assertCaseAndNormInsensitiveVolume(t)

	var out bytes.Buffer
	out.WriteString("# which names APFS made one file, for TestFold_JoinsWhatAPFSJoins; rewritten on macOS by -update\n")
	fmt.Fprintf(&out, "tables %s\n", currentTablesLine())

	var apartLines []string
	for _, sweep := range sweeps {
		partition := sweepOnAPFS(t, sweep.names)
		checkFoldNeverSplitsAPFSFiles(t, sweep.name, sweep.names, partition)
		digest := partitionDigest(partition)
		fmt.Fprintf(&out, "sweep %s names=%d files=%d partition=%s\n",
			sweep.name, len(sweep.names), len(partition), digest)
		apartLines = append(apartLines, buildApartLines(sweep.name, sweep.names, partition)...)
	}
	for _, line := range apartLines {
		out.WriteString(line)
		out.WriteByte('\n')
	}
	return out.Bytes()
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
			_ = other.Close()
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

	for idx, name := range names {
		target := filepath.Join(dir, name)
		file, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err == nil {
			_, writeErr := file.WriteString(strconv.Itoa(idx))
			closeErr := file.Close()
			require.NoError(t, writeErr, "write index for %q", name)
			require.NoError(t, closeErr, "close %q", name)
			groups[idx] = []int{idx}
			order = append(order, idx)
			continue
		}
		require.ErrorIs(t, err, fs.ErrExist, "create %q", name)
		content, readErr := os.ReadFile(target)
		require.NoError(t, readErr, "read existing owner of %q", name)
		owner, parseErr := strconv.Atoi(string(content))
		require.NoError(t, parseErr, "parse owner index %q of %q", string(content), name)
		groups[owner] = append(groups[owner], idx)
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
		for _, idx := range group[1:] {
			if gotFold := pathfold.Fold(names[idx]); gotFold != firstFold {
				splits++
				if splits <= maxReported {
					t.Errorf("sweep %s: Fold splits APFS file %q (%q) from %q (%q)",
						sweepName, names[group[0]], firstFold, names[idx], gotFold)
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
		for i, idx := range group {
			fileNames[i] = names[idx]
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
		for fileIdx, fileNames := range files {
			if fileIdx > 0 {
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
	var lineBuf []byte
	for _, group := range partition {
		lineBuf = lineBuf[:0]
		for i, idx := range group {
			if i > 0 {
				lineBuf = append(lineBuf, ' ')
			}
			lineBuf = strconv.AppendInt(lineBuf, int64(idx), 10)
		}
		lineBuf = append(lineBuf, '\n')
		_, _ = hasher.Write(lineBuf)
	}
	return fmt.Sprintf("sha256:%x", hasher.Sum(nil))
}

func parseAPFSRecord(t *testing.T, recordPath string, raw []byte) apfsRecord {
	t.Helper()
	rec := apfsRecord{
		sweeps: map[string]sweepRecord{},
		apart:  map[string][][][]string{},
	}
	lines := strings.Split(strings.TrimSuffix(string(raw), "\n"), "\n")
	for lineNo, line := range lines {
		if line == "" || strings.HasPrefix(line, "# ") {
			continue
		}
		kind, rest, ok := strings.Cut(line, " ")
		require.True(t, ok, "%s:%d: malformed line %q", recordPath, lineNo+1, line)
		switch kind {
		case "tables":
			rec.tables = rest
		case "sweep":
			name, entry := parseSweepLine(t, recordPath, lineNo+1, rest)
			rec.sweeps[name] = entry
		case "apart":
			name, groups := parseApartLine(t, recordPath, lineNo+1, rest)
			rec.apart[name] = append(rec.apart[name], groups)
		default:
			t.Fatalf("%s:%d: unknown record kind %q", recordPath, lineNo+1, kind)
		}
	}
	return rec
}

func parseSweepLine(t *testing.T, recordPath string, lineNo int, rest string) (string, sweepRecord) {
	t.Helper()
	fields := strings.Fields(rest)
	require.Len(t, fields, 4, "%s:%d: malformed sweep line %q", recordPath, lineNo, rest)
	sweepName := fields[0]
	namesText, hasNames := strings.CutPrefix(fields[1], "names=")
	filesText, hasFiles := strings.CutPrefix(fields[2], "files=")
	partText, hasPart := strings.CutPrefix(fields[3], "partition=")
	require.True(t, hasNames && hasFiles && hasPart, "%s:%d: malformed sweep fields %q", recordPath, lineNo, rest)

	nameCount, err := strconv.Atoi(namesText)
	require.NoError(t, err, "%s:%d: invalid names count %q", recordPath, lineNo, namesText)
	fileCount, err := strconv.Atoi(filesText)
	require.NoError(t, err, "%s:%d: invalid files count %q", recordPath, lineNo, filesText)
	return sweepName, sweepRecord{names: nameCount, files: fileCount, partition: partText}
}

func parseApartLine(t *testing.T, recordPath string, lineNo int, rest string) (string, [][]string) {
	t.Helper()
	sweepName, body, ok := strings.Cut(rest, " ")
	require.True(t, ok, "%s:%d: malformed apart line %q", recordPath, lineNo, rest)

	var groups [][]string
	var current []string
	body = strings.TrimLeft(body, " ")
	for body != "" {
		if body[0] == '|' {
			require.NotEmpty(t, current, "%s:%d: empty file group in apart line", recordPath, lineNo)
			groups = append(groups, current)
			current = nil
			body = strings.TrimLeft(body[1:], " ")
			continue
		}
		quoted, err := strconv.QuotedPrefix(body)
		require.NoError(t, err, "%s:%d: malformed quoted name in %q", recordPath, lineNo, body)
		unquoted, err := strconv.Unquote(quoted)
		require.NoError(t, err, "%s:%d: unquote %s", recordPath, lineNo, quoted)
		current = append(current, unquoted)
		body = body[len(quoted):]
		require.True(t, body == "" || body[0] == ' ', "%s:%d: expected space after %s", recordPath, lineNo, quoted)
		body = strings.TrimLeft(body, " ")
	}
	require.NotEmpty(t, current, "%s:%d: trailing separator in apart line", recordPath, lineNo)
	groups = append(groups, current)
	require.GreaterOrEqual(t, len(groups), 2, "%s:%d: apart line names fewer than two files", recordPath, lineNo)
	return sweepName, groups
}

func verifySweepAgainstRecord(t *testing.T, sweep sweepInput, rec sweepRecord, apartClasses [][][]string) [][]int {
	t.Helper()
	require.Equal(t, rec.names, len(sweep.names),
		"sweep %s generated %d names, record expects %d", sweep.name, len(sweep.names), rec.names)

	indexByName := make(map[string]int, len(sweep.names))
	byFold := make(map[string][]int, rec.files)
	for idx, name := range sweep.names {
		indexByName[name] = idx
		folded := pathfold.Fold(name)
		byFold[folded] = append(byFold[folded], idx)
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
				idx, exists := indexByName[name]
				require.True(t, exists, "sweep %s: apart name %q is not in generated sweep", sweep.name, name)
				require.False(t, seen[idx], "sweep %s: apart name %q listed twice", sweep.name, name)
				seen[idx] = true
				folded := pathfold.Fold(name)
				if classFold == "" {
					classFold = folded
				} else {
					require.Equal(t, classFold, folded,
						"sweep %s: apart line mixes fold classes (%q vs %q for %q)", sweep.name, classFold, folded, name)
				}
				fileIndexes = append(fileIndexes, idx)
				listedCount++
			}
			slices.Sort(fileIndexes)
			splitGroups = append(splitGroups, fileIndexes)
		}
		_, duplicateClass := splitByFold[classFold]
		require.False(t, duplicateClass, "sweep %s: two apart lines name fold class %q", sweep.name, classFold)
		classMembers := byFold[classFold]
		require.Equal(t, len(classMembers), listedCount,
			"sweep %s: apart line lists %d names, fold class %q holds %d", sweep.name, listedCount, classFold, len(classMembers))
		splitByFold[classFold] = splitGroups
	}

	partition := make([][]int, 0, rec.files)
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
	if len(partition) != rec.files || gotDigest != rec.partition {
		switch {
		case len(partition) > rec.files:
			t.Errorf("sweep %s: Fold produced %d classes, APFS has %d (Fold parts names APFS joins); digest %s vs %s",
				sweep.name, len(partition), rec.files, gotDigest, rec.partition)
		case len(partition) < rec.files:
			t.Errorf("sweep %s: Fold produced %d classes, APFS has %d (Fold joins files APFS keeps apart); digest %s vs %s",
				sweep.name, len(partition), rec.files, gotDigest, rec.partition)
		default:
			t.Errorf("sweep %s: Fold produced %d classes matching APFS count, but grouped names otherwise; digest %s vs %s",
				sweep.name, len(partition), gotDigest, rec.partition)
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

func countSplitNames(names []string, partition [][]int, foldFn func(string) string) int {
	splits := 0
	for _, group := range partition {
		firstFold := foldFn(names[group[0]])
		for _, idx := range group[1:] {
			if foldFn(names[idx]) != firstFold {
				splits++
			}
		}
	}
	return splits
}

func foldEachCharacterAlone(name string) string {
	return cases.Fold().String(strings.Map(unicode.ToUpper, decomposeEachRuneAlone(name)))
}

func foldWithoutUpperCasing(name string) string {
	return cases.Fold().String(norm.NFD.String(name))
}

func foldASCIILettersAlone(name string) string {
	var out strings.Builder
	out.Grow(len(name))
	for i := range len(name) {
		ch := name[i]
		if ch >= 'A' && ch <= 'Z' {
			ch += 'a' - 'A'
		}
		out.WriteByte(ch)
	}
	return out.String()
}
