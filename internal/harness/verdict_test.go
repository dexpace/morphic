// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package harness_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/internal/harness"
	"github.com/dexpace/morphic/internal/testspec"
)

// TestCheck_GivesEveryOracleAVerdict pins that a result accounts for every
// oracle, in the order Oracles lists them, whatever the spec: one that passes,
// one drawing an error, and one no compiler takes.
func TestCheck_GivesEveryOracleAVerdict(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, data string }{
		{"a spec that passes", testspec.Minimal},
		{"a spec drawing an error", testspec.BadHeader},
		{"a source no compiler takes", "just some words\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			r := harness.Check(context.Background(), "spec.yaml", []byte(tc.data))

			oracles := make([]harness.Oracle, len(r.Verdicts))
			for i, v := range r.Verdicts {
				oracles[i] = v.Oracle
				assert.NotEqual(t, v.Asked(), v.Outcome == "", "%s: an outcome exactly when it asked", v.Oracle)
			}
			assert.Equal(t, harness.Oracles(), oracles)
		})
	}
}

// TestSummary_CountsEachOraclesReach pins the summary's arithmetic and layout:
// per oracle, what it asked of, found in and declined, each decline by reason,
// most frequent first and alphabetical among equals, and a result lacking an
// oracle's verdict counted as not reached by it.
func TestSummary_CountsEachOraclesReach(t *testing.T) {
	t.Parallel()
	ok := func(o harness.Oracle) harness.Verdict { return harness.Verdict{Oracle: o, Outcome: harness.OutcomeOK} }
	no := func(o harness.Oracle, why harness.Decline) harness.Verdict {
		return harness.Verdict{Oracle: o, Declined: why}
	}
	results := []harness.Result{
		{Spec: "a", Verdicts: []harness.Verdict{
			ok(harness.OracleNoPanic), ok(harness.OracleInvariants),
			{Oracle: harness.OracleNoErrorDiagnostic, Outcome: harness.OutcomeErrorDiag, Detail: "x: y"},
			ok(harness.OracleRoundTrip), ok(harness.OracleDeterminism), ok(harness.OracleIdempotence),
			no(harness.OracleOrder, harness.DeclineDuplicateKey),
		}},
		{Spec: "b", Verdicts: []harness.Verdict{
			ok(harness.OracleNoPanic), no(harness.OracleInvariants, harness.DeclineNoDocument),
			ok(harness.OracleNoErrorDiagnostic), no(harness.OracleRoundTrip, harness.DeclineNoDocument),
			ok(harness.OracleDeterminism), no(harness.OracleIdempotence, harness.DeclineNoDocument),
			no(harness.OracleOrder, harness.DeclineNothingToPermute),
		}},
		{Spec: "c", Verdicts: []harness.Verdict{ok(harness.OracleNoPanic)}},
	}

	got := harness.Summary(results)

	assert.Equal(t, ""+
		"oracle               asked  found declined\n"+
		"no-panic                 3      0        0\n"+
		"invariants               1      0        2  no document 1, not reached 1\n"+
		"no-error-diagnostic      2      1        1  not reached 1\n"+
		"round-trip               1      0        2  no document 1, not reached 1\n"+
		"determinism              2      0        1  not reached 1\n"+
		"idempotence              1      0        2  no document 1, not reached 1\n"+
		"order-invariance         0      0        3  duplicate key 1, not reached 1, nothing to permute 1\n",
		got)
}

// TestSummary_OrdersReasonsByHowOftenTheyDecline pins the order a row lists its
// reasons in, apart from the alphabet: the most frequent first.
func TestSummary_OrdersReasonsByHowOftenTheyDecline(t *testing.T) {
	t.Parallel()
	declining := func(why harness.Decline) harness.Result {
		return harness.Result{Verdicts: []harness.Verdict{{Oracle: harness.OracleOrder, Declined: why}}}
	}
	got := harness.Summary([]harness.Result{
		declining(harness.DeclineDuplicateKey),
		declining(harness.DeclineTooDeep), declining(harness.DeclineTooDeep),
	})
	assert.Contains(t, got, "too deep 2, duplicate key 1\n")
}

// TestReport_ListsEveryFindingOfAResult pins that a result's findings are each
// a line, in Check's order, and that a result finding nothing is one line of
// its outcome.
func TestReport_ListsEveryFindingOfAResult(t *testing.T) {
	t.Parallel()
	lines := reportLines(t, harness.Report([]harness.Result{
		{Spec: "a.yaml", Outcome: harness.OutcomeErrorDiag, Detail: "x/y: broken", Verdicts: []harness.Verdict{
			{Oracle: harness.OracleNoPanic, Outcome: harness.OutcomeOK},
			{Oracle: harness.OracleNoErrorDiagnostic, Outcome: harness.OutcomeErrorDiag, Detail: "x/y: broken"},
			{Oracle: harness.OracleRoundTrip, Declined: harness.DeclineNoDocument},
			{Oracle: harness.OracleOrder, Outcome: harness.OutcomeOrderDependent, Detail: "differs"},
		}},
		{Spec: "b.yaml", Outcome: harness.OutcomeOK, Verdicts: []harness.Verdict{
			{Oracle: harness.OracleNoPanic, Outcome: harness.OutcomeOK},
		}},
	}))
	require.Len(t, lines, 3)
	assert.Equal(t, []string{
		"a.yaml error-diagnostic x/y: broken",
		"a.yaml order-dependent  differs",
		"b.yaml ok",
	}, lines)
}
