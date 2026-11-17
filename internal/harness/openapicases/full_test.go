// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package openapicases_test

import (
	"flag"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/internal/harness"
	"github.com/dexpace/morphic/internal/harness/openapicases"
	"github.com/dexpace/morphic/ir/irtest"
)

// full asks TestFull_EveryCaseHoldsTheRecords to check every case.
var full = flag.Bool("full", false, "check every schema case against the records, as make cases does")

// outside is, by case oracle, the file listing what it finds outside the core
// that the core cases do not explain, and the name its header gives it.
var outside = map[harness.Oracle][2]string{
	harness.OracleSpelling: {"testdata/full/spelling.txt", "spelling"},
	harness.OraclePresence: {"testdata/full/presence.txt", "presence"},
	harness.OracleInstance: {"testdata/full/instances.txt", "instance"},
}

// outsideHeader is the comment opening the file listing what the case oracle
// named finds outside the core.
func outsideHeader(name string) string {
	return "What the " + name + " oracle finds at the cases outside the core that what the core\n" +
		"cases find does not explain, and the reason each finding stands: case name,\n" +
		"what was found, reason (Reasons).\n"
}

// TestFull_EveryCaseHoldsTheRecords holds what the case oracles find at every
// case, with -full, to the records: a finding outside the core that neither
// the core's records nor its parts explain (harness.Unexplained) is a line of
// testdata/full, with its reason, and a line there naming no such finding
// fails. -update rewrites those files, keeping each line's reason. No case
// faults.
func TestFull_EveryCaseHoldsTheRecords(t *testing.T) {
	if !*full {
		t.Skip("every case is checked with -full, as make cases does")
	}
	all := func([]int) bool { return true }
	results := slices.Concat(schemaFull.of(t, openapicases.New(), all), groupFull.of(t, openapicases.Groups(), all))
	core := map[harness.Oracle]harness.Recorded{}
	for _, r := range results {
		require.False(t, r.Faulted(), "%s: %s: %s", r.Spec, r.Outcome, r.Detail)
	}
	for _, o := range harness.CaseOracles() {
		var err error
		core[o], err = openapicases.Record(o)
		require.NoError(t, err)
	}
	left := slices.DeleteFunc(harness.Unexplained(results, core), func(r harness.Result) bool { return r.Spec == r.Like })

	for _, o := range harness.CaseOracles() {
		path, name := outside[o][0], outside[o][1]
		data, err := os.ReadFile(path)
		require.NoError(t, err)
		record, err := harness.ReadRecorded(data)
		require.NoError(t, err)
		found := harness.Findings(left, o)
		if irtest.Update() {
			require.NoError(t, os.WriteFile(path, record.Write(outsideHeader(name), found), 0o644))
			continue
		}
		unlisted, changed, stale, unreasoned := record.Judge(found, openapicases.Reasons())
		report(t, path, "a finding no record explains: fix the compiler, or list it with its reason (-full -update)", unlisted)
		report(t, path, "a line whose finding changed", changed)
		report(t, path, "a line whose finding is gone: delete it (-full -update)", stale)
		report(t, path, "a line whose reason Reasons does not name", unreasoned)
	}
}

// report fails t once per line, each on a line of its own starting with the
// case it names, as the nightly run's report reads them.
func report(t *testing.T, path, why string, lines []string) {
	t.Helper()
	for _, line := range lines {
		t.Errorf("%s\t%s: %s", line, path, why)
	}
}

// TestFull_EachLineNamesACaseOutsideTheCore holds each line of the records
// outside the core, which the gate does not check against a run, to what a
// read can tell: it names a case of a product outside its core, and gives a
// reason Reasons names.
func TestFull_EachLineNamesACaseOutsideTheCore(t *testing.T) {
	t.Parallel()
	products := []harness.Cases{openapicases.New(), openapicases.Groups()}
	for _, o := range harness.CaseOracles() {
		core, err := openapicases.Record(o)
		require.NoError(t, err)
		every, err := openapicases.FullRecord(o)
		require.NoError(t, err)
		for name := range every {
			if _, ok := core[name]; ok {
				continue
			}
			assert.True(t, slices.ContainsFunc(products, func(c harness.Cases) bool { return outsideCore(c, name) }),
				"%s names no case outside a product's core", name)
		}
		unlisted, changed, _, unreasoned := every.Judge(nil, openapicases.Reasons())
		assert.Empty(t, slices.Concat(unlisted, changed))
		assert.Empty(t, unreasoned, "a line whose reason Reasons does not name: give it the reason of its issue")
	}
}

// outsideCore reports whether name names a case of c, each of its values one
// of its axis's, one of which at least is not core.
func outsideCore(c harness.Cases, name string) bool {
	axes := c.Axes()
	values := strings.Split(name, "/")
	if len(values) != len(axes) {
		return false
	}
	core := true
	for k, a := range axes {
		i := slices.IndexFunc(a.Values, func(v harness.Value) bool { return v.Name == values[k] })
		if i < 0 {
			return false
		}
		core = core && a.Values[i].Like == ""
	}
	return !core
}
