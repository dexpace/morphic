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
// faults. It logs what each oracle asked and declined.
func TestFull_EveryCaseHoldsTheRecords(t *testing.T) {
	if !*full {
		t.Skip("every case is checked with -full, as make cases does")
	}
	all := func([]int) bool { return true }
	results := slices.Concat(schemaFull.of(t, openapicases.New(), all), groupFull.of(t, openapicases.Groups(), all))
	core := map[harness.Oracle]harness.Recorded{}
	for _, result := range results {
		require.False(t, result.Faulted(), "%s: %s: %s", result.Spec, result.Outcome, result.Detail)
	}
	for _, oracle := range harness.CaseOracles() {
		var err error
		core[oracle], err = openapicases.Record(oracle)
		require.NoError(t, err)
	}
	left := slices.DeleteFunc(harness.Unexplained(results, core), func(result harness.Result) bool { return result.Spec == result.Like })
	t.Logf("what each case oracle asked and declined:\n%s", harness.CaseSummary(results))

	for _, oracle := range harness.CaseOracles() {
		path, name := outside[oracle][0], outside[oracle][1]
		data, err := os.ReadFile(path)
		require.NoError(t, err)
		record, err := harness.ReadRecorded(data)
		require.NoError(t, err)
		found := harness.Findings(left, oracle)
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

// products are the products of schema cases in the order morphic-harness
// counts their cases for -case-index: the schema cases, then the suite groups.
func products() []harness.Cases { return []harness.Cases{openapicases.New(), openapicases.Groups()} }

// report fails t once per line, each on a line of its own starting with the
// case it names, as the nightly run's report reads them, and ending with the
// -case-index index that writes the case's documents and verdicts.
func report(t *testing.T, path, why string, lines []string) {
	t.Helper()
	ps := products()
	for _, line := range lines {
		name, _, _ := strings.Cut(line, "\t")
		name, _, _ = strings.Cut(name, ": listed ")
		at, ok := caseIndex(ps, name)
		require.True(t, ok, "%s names no case", name)
		t.Errorf("%s\t%s: %s; morphic-harness -cases full -case-index %d", line, path, why, at)
	}
}

// caseIndex returns the index -case-index takes for the case name names across
// products, counted one product after another, and false for a name no case
// has.
func caseIndex(products []harness.Cases, name string) (int, bool) {
	first := 0
	values := strings.Split(name, "/")
	for _, cases := range products {
		axes := cases.Axes()
		if i, ok := indexIn(axes, values); ok {
			return first + i, true
		}
		count, err := harness.CaseCount(cases)
		if err != nil {
			return 0, false
		}
		first += count
	}
	return 0, false
}

// indexIn returns the index among axes of the case whose value names are
// values, the last axis counting fastest, and false where none is.
func indexIn(axes []harness.Axis, values []string) (int, bool) {
	if len(values) != len(axes) {
		return 0, false
	}
	i := 0
	for k, a := range axes {
		v := slices.IndexFunc(a.Values, func(value harness.Value) bool { return value.Name == values[k] })
		if v < 0 {
			return 0, false
		}
		i = i*len(a.Values) + v
	}
	return i, true
}

// TestFull_ACaseIndexIsTheOneMorphicHarnessTakes holds caseIndex to the
// coordinates harness gives each index it is asked of, in both products and
// across the boundary between them, and to no index for a name no case has.
func TestFull_ACaseIndexIsTheOneMorphicHarnessTakes(t *testing.T) {
	t.Parallel()
	ps := products()
	schemas, err := harness.CaseCount(ps[0])
	require.NoError(t, err)
	for _, at := range []int{0, 7, schemas - 1, schemas, schemas + 11} {
		cases, i := ps[0], at
		if at >= schemas {
			cases, i = ps[1], at-schemas
		}
		coordinates, err := harness.Coordinates(cases, i)
		require.NoError(t, err)
		names := make([]string, len(coordinates))
		for k, a := range cases.Axes() {
			names[k] = a.Values[coordinates[k]].Name
		}
		got, ok := caseIndex(ps, strings.Join(names, "/"))
		require.True(t, ok, at)
		assert.Equal(t, at, got)
	}
	_, ok := caseIndex(ps, "3.1.0/no-such-target/none/none/component/inline")
	assert.False(t, ok)
}

// TestFull_EachLineNamesACaseOutsideTheCore holds each line of the records
// outside the core, which the gate does not check against a run, to what a
// read can tell: it names a case of a product outside its core, and gives a
// reason Reasons names.
func TestFull_EachLineNamesACaseOutsideTheCore(t *testing.T) {
	t.Parallel()
	ps := products()
	for _, oracle := range harness.CaseOracles() {
		core, err := openapicases.Record(oracle)
		require.NoError(t, err)
		every, err := openapicases.FullRecord(oracle)
		require.NoError(t, err)
		for name := range every {
			if _, ok := core[name]; ok {
				continue
			}
			assert.True(t, slices.ContainsFunc(ps, func(cases harness.Cases) bool { return outsideCore(cases, name) }),
				"%s names no case outside a product's core", name)
		}
		unlisted, changed, _, unreasoned := every.Judge(nil, openapicases.Reasons())
		assert.Empty(t, slices.Concat(unlisted, changed))
		assert.Empty(t, unreasoned, "a line whose reason Reasons does not name: give it the reason of its issue")
	}
}

// outsideCore reports whether name names a case of cases, each of its values one
// of its axis's, one of which at least is not core.
func outsideCore(cases harness.Cases, name string) bool {
	axes := cases.Axes()
	values := strings.Split(name, "/")
	if len(values) != len(axes) {
		return false
	}
	core := true
	for k, a := range axes {
		i := slices.IndexFunc(a.Values, func(value harness.Value) bool { return value.Name == values[k] })
		if i < 0 {
			return false
		}
		core = core && a.Values[i].Like == ""
	}
	return !core
}
