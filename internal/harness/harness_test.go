package harness_test

import (
	"context"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/internal/harness"
	"github.com/dexpace/morphic/internal/testspec"
)

func TestCheck_ValidSpecIsOK(t *testing.T) {
	t.Parallel()
	r := harness.Check(context.Background(), "minimal", []byte(testspec.Minimal))
	assert.Equal(t, harness.OutcomeOK, r.Outcome, r.Detail)
}

func TestCheck_GarbageBytesDoNotPanic(t *testing.T) {
	t.Parallel()
	r := harness.Check(context.Background(), "garbage", []byte("\x00not a spec:::"))
	// A parse failure is an Error/ErrorDiag outcome, never a panic escaping Check.
	assert.NotEqual(t, harness.OutcomePanic, r.Outcome, r.Detail)
}

func TestCheck_NilContextIsErrorNotPanic(t *testing.T) {
	t.Parallel()
	//nolint:staticcheck // deliberately passing a nil ctx to exercise the boundary guard.
	r := harness.Check(nil, "minimal", []byte(testspec.Minimal))
	// A caller mistake is a harness error, never a spec-attributed compiler panic.
	assert.Equal(t, harness.OutcomeError, r.Outcome, r.Detail)
	assert.Contains(t, r.Detail, "nil context")
}

func TestCheck_EmptySpecIsErrorNotPanic(t *testing.T) {
	t.Parallel()
	r := harness.Check(context.Background(), "", []byte(testspec.Minimal))
	assert.Equal(t, harness.OutcomeError, r.Outcome, r.Detail)
	assert.Contains(t, r.Detail, "empty spec")
}

func TestReport_IsStableAndSorted(t *testing.T) {
	t.Parallel()
	results := []harness.Result{
		{Spec: "b", Outcome: harness.OutcomeOK},
		{Spec: "a", Outcome: harness.OutcomeError, Detail: "boom"},
	}
	got := harness.Report(results)
	assert.Contains(t, got, "a")
	assert.Contains(t, got, "b")
	assert.Less(t, strings.Index(got, "a"), strings.Index(got, "b"),
		"results sorted by spec name")
	assert.Equal(t, "b", results[0].Spec,
		"Report sorts a copy: the caller's slice keeps the order it was passed in")
}

// reportLines splits a Report into its lines, dropping the trailing newline the
// last line ends with so an empty final element is never mistaken for a row.
func reportLines(t *testing.T, report string) []string {
	t.Helper()
	require.NotEmpty(t, report, "a non-empty result set renders at least one line")
	return strings.Split(strings.TrimSuffix(report, "\n"), "\n")
}

func TestReport_ColumnsAreSizedToTheResults(t *testing.T) {
	t.Parallel()
	// Longer than the 40-column spec field the report used to pad to, which is
	// true of nearly every path in testdata.
	const longSpec = "testdata/conformance/openapi/allof-boolean-branch.yaml"
	lines := reportLines(t, harness.Report([]harness.Result{
		{Spec: longSpec, Outcome: harness.OutcomeRoundtrip, Detail: "IR JSON differs"},
		{Spec: "a.yaml", Outcome: harness.OutcomeError, Detail: "boom"},
	}))
	require.Len(t, lines, 2, "one line per result")

	short, long := lines[0], lines[1] // sorted by spec: a.yaml, then testdata/...
	shortOutcome := columnStart(t, short, string(harness.OutcomeError))
	longOutcome := columnStart(t, long, string(harness.OutcomeRoundtrip))
	assert.Equal(t, shortOutcome, longOutcome,
		"the outcome column starts at the same offset on both lines")
	assert.Equal(t, columnStart(t, short, "boom"), columnStart(t, long, "IR JSON differs"),
		"the detail column starts at the same offset on both lines")
	assert.Equal(t, len(longSpec)+1, longOutcome,
		"the spec column is exactly as wide as the longest spec, plus one separator")
}

// columnStart returns the offset at which column begins in line, failing when
// the line does not carry it — an absent column would otherwise compare equal to
// another absent one and assert nothing.
func columnStart(t *testing.T, line, column string) int {
	t.Helper()
	i := strings.Index(line, column)
	require.GreaterOrEqual(t, i, 0, "line %q carries column %q", line, column)
	return i
}

// TestReport_OutcomeColumnIsSizedOnlyByLinesThatUseIt pins what the second
// column is measured against. A result with no Detail has nothing to the right
// of its outcome, so sizing the column to it pads every line that does carry a
// Detail out to a column standing empty on the line that set its width — the
// padding this report exists to stop emitting. The longest outcome the harness
// has goes on the line showing no detail, so widening the column is the only way
// the assertion below can fail.
func TestReport_OutcomeColumnIsSizedOnlyByLinesThatUseIt(t *testing.T) {
	t.Parallel()
	lines := reportLines(t, harness.Report([]harness.Result{
		{Spec: "a.yaml", Outcome: harness.OutcomeNondeterministic},
		{Spec: "b.yaml", Outcome: harness.OutcomeError, Detail: "boom"},
	}))
	require.Len(t, lines, 2, "one line per result")
	assert.Equal(t, "b.yaml "+string(harness.OutcomeError)+" boom", lines[1],
		"a detail follows its own outcome, not a column sized by a line that has none")
}

func TestReport_LinesAreNotPaddedPastTheirLastColumn(t *testing.T) {
	t.Parallel()
	lines := reportLines(t, harness.Report([]harness.Result{
		{Spec: "a.yaml", Outcome: harness.OutcomeOK},
		{Spec: "b.yaml", Outcome: harness.OutcomeError, Detail: "boom"},
	}))
	require.Len(t, lines, 2, "one line per result")
	for _, line := range lines {
		assert.Equal(t, strings.TrimRight(line, " "), line,
			"no line carries padding after its last column")
	}
}

func TestReport_WidthsAreCountedInRunes(t *testing.T) {
	t.Parallel()
	// Eight runes, eleven bytes: a byte-counted width would pad the spec column
	// three spaces past where the spec ends, since fmt's %-*s pads in runes.
	const spec = "ééé.yaml"
	lines := reportLines(t, harness.Report([]harness.Result{
		{Spec: spec, Outcome: harness.OutcomeError, Detail: "boom"},
	}))
	require.Len(t, lines, 1, "one line per result")

	upToOutcome, _, found := strings.Cut(lines[0], string(harness.OutcomeError))
	require.True(t, found, "the line names its outcome")
	assert.Equal(t, utf8.RuneCountInString(spec)+1, utf8.RuneCountInString(upToOutcome),
		"the spec column is padded to the spec's rune count, not its byte count")
}

func TestReport_NoResultsRenderNothing(t *testing.T) {
	t.Parallel()
	assert.Empty(t, harness.Report(nil), "an empty sweep has no lines to render")
}
