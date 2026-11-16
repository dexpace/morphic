// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package harness_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/internal/harness"
)

// record is a record of three lines under a comment, a blank line among them.
const record = "# found by the spelling oracle\n" +
	"a/x\tkept record\tkept-key\n" +
	"\n" +
	"b/x\tkind\tdecorated-object\n" +
	"c/x\tbase\tunclassified\n"

// reasons are the reasons record's lines may give.
func reasons() map[string]string {
	return map[string]string{"kept-key": "#1989", "decorated-object": "#1874"}
}

func readRecord(t *testing.T) harness.Recorded {
	t.Helper()
	r, err := harness.ReadRecorded([]byte(record))
	require.NoError(t, err)
	return r
}

// TestRecorded_EachDisagreementLandsInItsOwnReturn holds Judge's four
// returns apart: a finding the record does not list, one it lists with
// another What, a line no finding names, and a line whose reason is no key.
func TestRecorded_EachDisagreementLandsInItsOwnReturn(t *testing.T) {
	found := []harness.Finding{{"d/x", "new"}, {"b/x", "kind,base"}, {"c/x", "base"}}

	unlisted, changed, stale, unreasoned := readRecord(t).Judge(found, reasons())

	assert.Equal(t, []string{"d/x\tnew"}, unlisted)
	assert.Equal(t, []string{`b/x: listed "kind", found "kind,base"`}, changed)
	assert.Equal(t, []string{"a/x\tkept record"}, stale)
	assert.Equal(t, []string{"c/x\tunclassified"}, unreasoned)
}

// TestRecorded_AStaleLineFails pins that a fix leaves no line behind it: a
// line whose finding is gone is reported, and the rest agree.
func TestRecorded_AStaleLineFails(t *testing.T) {
	found := []harness.Finding{{"b/x", "kind"}, {"c/x", "base"}}

	unlisted, changed, stale, _ := readRecord(t).Judge(found, reasons())

	assert.Empty(t, unlisted)
	assert.Empty(t, changed)
	assert.Equal(t, []string{"a/x\tkept record"}, stale)
}

// TestRecorded_WriteKeepsEachReason pins Write against ReadRecorded: what it
// writes reads back, each line found again keeps its own reason, a new line
// is unclassified, and a line not found is gone.
func TestRecorded_WriteKeepsEachReason(t *testing.T) {
	found := []harness.Finding{{"c/x", "base"}, {"b/x", "kind"}, {"a/x", "kept record"}, {"e/x", "new"}}

	out := readRecord(t).Write("found by the spelling oracle\n\nrewritten by -update", found)

	assert.Equal(t, "# found by the spelling oracle\n#\n# rewritten by -update\n"+
		"a/x\tkept record\tkept-key\n"+
		"b/x\tkind\tdecorated-object\n"+
		"c/x\tbase\tunclassified\n"+
		"e/x\tnew\tunclassified\n", string(out))
	back, err := harness.ReadRecorded(out)
	require.NoError(t, err)
	unlisted, changed, stale, unreasoned := back.Judge(found, reasons())
	assert.Empty(t, unlisted)
	assert.Empty(t, changed)
	assert.Empty(t, stale)
	assert.Equal(t, []string{"c/x\tunclassified", "e/x\tunclassified"}, unreasoned)
}

func TestReadRecorded_RefusesWhatNoRecordHolds(t *testing.T) {
	for _, tc := range []struct{ name, data, want string }{
		{"two fields", "a/x\tkind\n", "record line 1: want name, what and reason"},
		{"four fields", "# c\na/x\tkind\tr\tmore\n", "record line 2: want name, what and reason"},
		{"an empty field", "a/x\t\tr\n", "record line 1: want name, what and reason"},
		{"a name listed twice", "a/x\tkind\tr\nb/x\tk\tr\na/x\tbase\tr\n", `record line 3: "a/x" is listed twice`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := harness.ReadRecorded([]byte(tc.data))
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.want)
		})
	}
}

func TestRecorded_ExplainsAFindingAtItsCoreName(t *testing.T) {
	r := readRecord(t)
	assert.True(t, r.Explains(harness.Finding{Name: "b/y", What: "kind"}, "b/x"))
	assert.False(t, r.Explains(harness.Finding{Name: "b/y", What: "base"}, "b/x"), "another What")
	assert.False(t, r.Explains(harness.Finding{Name: "b/y", What: "kind"}, "b/y"), "no line at that name")
}

// caseResults are a run's results: one case each oracle passes, one the
// spelling and presence oracles find something in, one a panic ended, and one
// a panic ended after the spelling oracle found what the record lists.
func caseResults() []harness.Result {
	return []harness.Result{
		{Spec: "a/x", Like: "a/x", Outcome: harness.OutcomeOK, Verdicts: []harness.Verdict{
			{Oracle: harness.OracleSpelling, Outcome: harness.OutcomeOK},
		}},
		{Spec: "b/y", Like: "b/x", Outcome: harness.OutcomeSpellingDependent, Detail: "kind\nthe diff", Verdicts: []harness.Verdict{
			{Oracle: harness.OracleSpelling, Outcome: harness.OutcomeSpellingDependent, Detail: "kind\nthe diff"},
			{Oracle: harness.OraclePresence, Outcome: harness.OutcomeNotCarried, Detail: "changes nothing"},
			{Oracle: harness.OracleInstance, Declined: harness.DeclineNoInstance},
		}},
		{Spec: "c/x", Like: "c/x", Outcome: harness.OutcomePanic, Detail: "boom", Verdicts: []harness.Verdict{
			{Oracle: harness.OracleSpelling, Declined: harness.DeclineNotReached},
		}},
		{Spec: "a/y", Like: "a/x", Outcome: harness.OutcomePanic, Detail: "presence exploded", Verdicts: []harness.Verdict{
			{Oracle: harness.OracleSpelling, Outcome: harness.OutcomeSpellingDependent, Detail: "kept record"},
			{Oracle: harness.OraclePresence, Declined: harness.DeclineNotReached},
		}},
	}
}

func TestFindings_AreTheFirstLineOfWhatAnOracleFound(t *testing.T) {
	results := caseResults()
	assert.Equal(t, []harness.Finding{{Name: "b/y", What: "kind"}, {Name: "a/y", What: "kept record"}},
		harness.Findings(results, harness.OracleSpelling))
	assert.Equal(t, []harness.Finding{{Name: "b/y", What: "changes nothing"}},
		harness.Findings(results, harness.OraclePresence))
	assert.Empty(t, harness.Findings(results, harness.OracleInstance))
}

// TestUnexplained_KeepsWhatNoRecordExplains pins what a run reports: a fault,
// whole even where the record explains what an oracle found before it, and a
// finding no record lists at its case's core name, with the verdicts the
// record explains taken out.
func TestUnexplained_KeepsWhatNoRecordExplains(t *testing.T) {
	results := caseResults()
	records := map[harness.Oracle]harness.Recorded{harness.OracleSpelling: readRecord(t)}

	got := harness.Unexplained(results, records)

	require.Len(t, got, 3)
	assert.Equal(t, harness.Result{
		Spec: "b/y", Like: "b/x", Outcome: harness.OutcomeNotCarried, Detail: "changes nothing",
		Verdicts: []harness.Verdict{{Oracle: harness.OraclePresence, Outcome: harness.OutcomeNotCarried, Detail: "changes nothing"}},
	}, got[0])
	assert.Equal(t, results[2], got[1])
	assert.Equal(t, results[3], got[2])
	assert.Contains(t, harness.Report(got), "presence exploded", "the report names the fault beside the finding")

	bare := harness.Unexplained(results, nil)
	require.Len(t, bare, 3, "with no record, every finding stands")
	assert.Equal(t, harness.OutcomeSpellingDependent, bare[0].Outcome)
	assert.Equal(t, results[1].Verdicts[:2], bare[0].Verdicts)
}

// TestResult_FaultedTellsAFaultFromAFinding holds Faulted to each outcome a
// result takes: a fault is an outcome no verdict of the result found.
func TestResult_FaultedTellsAFaultFromAFinding(t *testing.T) {
	results := caseResults()
	for i, want := range []bool{false, false, true, true} {
		assert.Equal(t, want, results[i].Faulted(), results[i].Spec)
	}
	panicked := harness.Result{Outcome: harness.OutcomePanic, Verdicts: []harness.Verdict{
		{Oracle: harness.OracleNoPanic, Outcome: harness.OutcomePanic, Detail: "boom"},
	}}
	assert.False(t, panicked.Faulted(), "a spec's panic is the no-panic oracle's finding")
	assert.True(t, harness.Result{Outcome: harness.OutcomeError}.Faulted())
}
