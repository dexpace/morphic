// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package pinnedspecs_test

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/internal/harness"
	"github.com/dexpace/morphic/internal/harness/pinnedspecs"
	"github.com/dexpace/morphic/ir/irtest"
)

// pinned asks TestSweep_EachPinnedSpecHoldsItsRecord to read the listed specs
// from the directory it names.
var pinned = flag.String("pinned", "",
	"the directory scripts/fetch-pinned-specs.sh fetched the listed specs into, as make sweep runs it")

// The committed list of the specs the nightly sweep reads, and their records.
const (
	listPath    = "testdata/specs.txt"
	recordsPath = "testdata/records"
)

// TestSweep_EachPinnedSpecHoldsItsRecord holds what harness.Check finds in
// each listed spec, fetched into the directory -pinned names, to its record,
// as make sweep runs it: a finding the record does not list fails, and so does
// a line naming no finding. -update rewrites the records, keeping each line's
// reason.
func TestSweep_EachPinnedSpecHoldsItsRecord(t *testing.T) {
	if *pinned == "" {
		t.Skip("the listed specs are read from the directory -pinned names, as make sweep does")
	}
	for _, problem := range sweep(t, listPath, *pinned, recordsPath, reasons, irtest.Update()) {
		t.Error(problem)
	}
}

// sweep holds each spec the list at list names, fetched into dir, to its
// record, the file <name>.txt in records, whose reasons must be keys of
// reasons, and returns a line per difference, each opening with the spec's
// name. With update it rewrites each record instead, keeping each line's
// reason, giving a new finding unclassified, and returns what it cannot read.
func sweep(t *testing.T, list, dir, records string, reasons map[string]string, update bool) []string {
	t.Helper()
	data, err := os.ReadFile(list)
	require.NoError(t, err)
	specs, err := pinnedspecs.ReadList(data)
	require.NoError(t, err)

	var problems []string
	for _, spec := range specs {
		found, err := check(t, spec, dir)
		if err != nil {
			problems = append(problems, fmt.Sprintf("%s\t%v", spec.Name, err))
			continue
		}
		path := filepath.Join(records, spec.Name+".txt")
		record, err := readRecord(path, update)
		if err != nil {
			problems = append(problems, fmt.Sprintf("%s\t%v: write it (-update)", spec.Name, err))
			continue
		}
		if update {
			require.NoError(t, os.WriteFile(path, record.Write(recordHeader(spec.Name), found), 0o644))
			continue
		}
		problems = append(problems, judge(spec.Name, path, record, found, reasons)...)
	}
	return problems
}

// check reads spec's copy fetched into dir, held to its hash, and returns what
// harness.Check finds in it.
func check(t *testing.T, spec pinnedspecs.Spec, dir string) ([]harness.Finding, error) {
	t.Helper()
	data, err := spec.Read(dir)
	if err != nil {
		return nil, err
	}
	return pinnedspecs.Findings(harness.Check(t.Context(), spec.File(dir), data))
}

// readRecord reads the record at path. A record missing where update is set
// reads as one listing nothing, which the update then writes.
func readRecord(path string, update bool) (harness.Recorded, error) {
	data, err := os.ReadFile(path)
	if update && errors.Is(err, fs.ErrNotExist) {
		return harness.Recorded{}, nil
	}
	if err != nil {
		return nil, err
	}
	return harness.ReadRecorded(data)
}

// judge holds found, what the spec name names drew, to record, read from
// path, and returns a line per difference: the spec, the record's line or the
// finding, and the record and why the line fails.
func judge(name, path string, record harness.Recorded, found []harness.Finding, reasons map[string]string) []string {
	unlisted, changed, stale, unreasoned := record.Judge(found, reasons)
	var problems []string
	for _, group := range []struct {
		lines []string
		why   string
	}{
		{unlisted, "a finding the record does not list: fix the compiler, or list it with its reason (-update)"},
		{changed, "a line whose finding changed"},
		{stale, "a line whose finding is gone: delete it (-update)"},
		{unreasoned, "a line whose reason the reasons do not name"},
	} {
		for _, line := range group.lines {
			problems = append(problems, fmt.Sprintf("%s\t%s\t%s: %s", name, line, path, group.why))
		}
	}
	return problems
}

// recordHeader is the comment opening the record of the spec the list names
// name.
func recordHeader(name string) string {
	return "What harness.Check finds in the spec the list names " + name + ", and the reason\n" +
		"each finding stands: a diagnostic by its code and place, or an oracle, then\n" +
		"what was found, then the reason (reasons_test.go).\n"
}

// The fixtures the sweep's tests read as a list, its fetched specs and their
// records.
const (
	fixtureList    = "testdata/fixtures/specs.txt"
	fixtureFetched = "testdata/fixtures/fetched"
	fixtureRecords = "testdata/fixtures/records"
)

// fixtureReasons names each reason a fixture's record gives, by key.
var fixtureReasons = map[string]string{
	"accept-header": "expected: the fixture declares a header parameter named Accept",
	"no-sequence":   "expected: the fixture writes no allOf and no required list",
}

// TestSweep_FixturesHoldTheirRecords pins the fixtures the other tests alter:
// each holds its record as committed, so a difference those tests report is
// the one each plants.
func TestSweep_FixturesHoldTheirRecords(t *testing.T) {
	t.Parallel()
	assert.Empty(t, sweep(t, fixtureList, fixtureFetched, fixtureRecords, fixtureReasons, false))
}

// The lines of the accept fixture's record, and the name of its warning's.
const (
	acceptWarnedAt = "openapi/reserved-header-name /paths/~1a/get/parameters/0/name"
	acceptWarning  = acceptWarnedAt + "\twarning\taccept-header\n"
	acceptDecline  = "oracle sequence-order-invariance\tdeclined: nothing to permute\tno-sequence\n"
)

// notListed is the failure of a finding the record at path does not list.
func notListed(path string) string {
	return path + ": a finding the record does not list: fix the compiler, or list it with its reason (-update)"
}

// TestSweep_ReportsEachDifferenceFromARecord plants each difference a record
// can have from what its spec draws, in a copy of the fixtures' records, and
// holds the sweep to reporting that one, opening with the spec's name.
func TestSweep_ReportsEachDifferenceFromARecord(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, record, from, to string
		want                   func(path string) string
	}{
		{"a finding the record does not list", "accept", acceptWarning, "", func(path string) string {
			return "accept\t" + acceptWarnedAt + "\twarning\t" + notListed(path)
		}},
		{"a listed finding that is gone", "quiet", "\n", "\n" + acceptWarning, func(path string) string {
			return "quiet\t" + acceptWarnedAt + "\twarning\t" + path + ": a line whose finding is gone: delete it (-update)"
		}},
		{"a finding that changed", "accept", "\twarning\t", "\terror\t", func(path string) string {
			return "accept\t" + acceptWarnedAt + `: listed "error", found "warning"` + "\t" + path + ": a line whose finding changed"
		}},
		{"an unclassified line", "accept", "\taccept-header\n", "\tunclassified\n", func(path string) string {
			return "accept\t" + acceptWarnedAt + "\tunclassified\t" + path + ": a line whose reason the reasons do not name"
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			records := copyTree(t, fixtureRecords)
			path := filepath.Join(records, tc.record+".txt")
			replaceIn(t, path, tc.from, tc.to)

			got := sweep(t, fixtureList, fixtureFetched, records, fixtureReasons, false)

			assert.Equal(t, []string{tc.want(path)}, got)
		})
	}
}

// TestSweep_ReportsAListedSpecWithNoRecord pins that a spec whose record is
// missing is reported, naming the record to write, and the rest still judged.
func TestSweep_ReportsAListedSpecWithNoRecord(t *testing.T) {
	t.Parallel()
	records := copyTree(t, fixtureRecords)
	path := filepath.Join(records, "quiet.txt")
	require.NoError(t, os.Remove(path))
	replaceIn(t, filepath.Join(records, "accept.txt"), acceptDecline, "")

	got := sweep(t, fixtureList, fixtureFetched, records, fixtureReasons, false)

	assert.Equal(t, []string{
		"quiet\topen " + path + ": no such file or directory: write it (-update)",
		"accept\toracle sequence-order-invariance\tdeclined: nothing to permute\t" +
			notListed(filepath.Join(records, "accept.txt")),
	}, got)
}

// TestSweep_ReadsOnlyTheBytesItsListPins plants a copy the list does not pin,
// by changing the hash the list pins or the bytes fetched, and holds the sweep
// to reporting it rather than compiling it; and a copy whose new bytes the
// list pins, drawing a finding its record does not list, to reporting that.
func TestSweep_ReadsOnlyTheBytesItsListPins(t *testing.T) {
	t.Parallel()
	const contentType = "        - {name: Content-Type, in: header, schema: {type: string}}\n"
	quietSum := sha256Hex(t, filepath.Join(fixtureFetched, "quiet", "quiet.yaml"))
	acceptSum := sha256Hex(t, filepath.Join(fixtureFetched, "accept", "accept.yaml"))
	for _, tc := range []struct {
		name                string
		alterList, alterNew bool
		want                func(fetched string) string
	}{
		{"the list pins another hash", true, false, func(fetched string) string {
			return "quiet\tpinnedspecs: quiet: " + filepath.Join(fetched, "quiet", "quiet.yaml") +
				" has SHA-256 " + quietSum + ", where the list pins " + acceptSum
		}},
		{"the bytes fetched changed", false, true, func(fetched string) string {
			return "accept\tpinnedspecs: accept: " + filepath.Join(fetched, "accept", "accept.yaml") +
				" has SHA-256 " + sha256Hex(t, filepath.Join(fetched, "accept", "accept.yaml")) +
				", where the list pins " + acceptSum
		}},
		{"the list pins the changed bytes", true, true, func(string) string {
			return "accept\topenapi/reserved-header-name /paths/~1a/get/parameters/1/name\twarning\t" +
				notListed(filepath.Join(fixtureRecords, "accept.txt"))
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			fetched := copyTree(t, fixtureFetched)
			list := filepath.Join(t.TempDir(), "specs.txt")
			require.NoError(t, os.WriteFile(list, []byte(readFile(t, fixtureList)), 0o644))
			accept := filepath.Join(fetched, "accept", "accept.yaml")
			if tc.alterNew {
				replaceIn(t, accept, "\n      responses:", "\n"+contentType+"      responses:")
			}
			if tc.alterList && tc.alterNew {
				replaceIn(t, list, acceptSum, sha256Hex(t, accept))
			} else if tc.alterList {
				replaceIn(t, list, quietSum, acceptSum)
			}

			got := sweep(t, list, fetched, fixtureRecords, fixtureReasons, false)

			assert.Equal(t, []string{tc.want(fetched)}, got)
		})
	}
}

// TestSweep_UpdateRewritesEachRecord pins what -update writes: each record
// whole, its header first, a line per finding in name order, each keeping
// the reason its line gave and a new one unclassified, a line naming no
// finding gone, and a missing record written. What it writes reads back, and
// fails only for the unclassified line.
func TestSweep_UpdateRewritesEachRecord(t *testing.T) {
	t.Parallel()
	const stale = "oracle order-invariance\tdeclined: duplicate key\tno-sequence\n"
	records := copyTree(t, fixtureRecords)
	accept, quiet := filepath.Join(records, "accept.txt"), filepath.Join(records, "quiet.txt")
	replaceIn(t, accept, acceptWarning, stale)
	require.NoError(t, os.Remove(quiet))

	assert.Empty(t, sweep(t, fixtureList, fixtureFetched, records, fixtureReasons, true))

	assert.Equal(t, comment(recordHeader("accept"))+acceptWarnedAt+"\twarning\tunclassified\n"+acceptDecline,
		readFile(t, accept))
	assert.Equal(t, comment(recordHeader("quiet")), readFile(t, quiet))
	assert.Equal(t, []string{"accept\t" + acceptWarnedAt + "\tunclassified\t" + accept +
		": a line whose reason the reasons do not name"},
		sweep(t, fixtureList, fixtureFetched, records, fixtureReasons, false))
}

// comment is header as the comment lines opening a record.
func comment(header string) string {
	var b strings.Builder
	for line := range strings.Lines(header) {
		b.WriteString("# " + line)
	}
	return b.String()
}

// copyTree copies the directory tree at from into a directory of the test's
// own and returns its path.
func copyTree(t *testing.T, from string) string {
	t.Helper()
	to := filepath.Join(t.TempDir(), filepath.Base(from))
	require.NoError(t, os.CopyFS(to, os.DirFS(from)))
	return to
}

// replaceIn replaces the one place the file at path holds from with to.
func replaceIn(t *testing.T, path, from, to string) {
	t.Helper()
	text := readFile(t, path)
	require.Equal(t, 1, strings.Count(text, from), "%s holds %q once", path, from)
	require.NoError(t, os.WriteFile(path, []byte(strings.Replace(text, from, to, 1)), 0o644))
}

// sha256Hex is the SHA-256 of the file at path, in hex.
func sha256Hex(t *testing.T, path string) string {
	t.Helper()
	sum := sha256.Sum256([]byte(readFile(t, path)))
	return hex.EncodeToString(sum[:])
}
