// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package triage_test

import (
	"slices"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/engine/triage"
	"github.com/dexpace/morphic/internal/decls"
	"github.com/dexpace/morphic/ir"
)

// rate is a rule rating every finding of code at sev.
func rate(code string, sev ir.Severity) triage.Rule {
	return triage.Rule{Code: code, Severity: sev, Reason: "r"}
}

// TestNewRuleset_RefusesWhatNoRunCanApply pins each refusal to the clause that
// names it, so a check that stopped refusing one, or refused it for another
// reason, reddens its row.
func TestNewRuleset_RefusesWhatNoRunCanApply(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name  string
		rules []triage.Rule
		want  string
	}{
		{"no code", []triage.Rule{{Severity: ir.SeverityWarning, Reason: "r"}}, "names no code"},
		{"a wildcard", []triage.Rule{rate("openapi/*", ir.SeverityInfo)}, "writes a wildcard"},
		{"a pointer that is no pointer",
			[]triage.Rule{{Code: "c", Pointer: "components", Severity: ir.SeverityInfo, Reason: "r"}},
			`pointer "components" is not an RFC 6901 JSON Pointer`},
		{"an escape escaping nothing",
			[]triage.Rule{{Code: "c", Pointer: "/a~2", Severity: ir.SeverityInfo, Reason: "r"}},
			"is not an RFC 6901 JSON Pointer"},
		{"two places",
			[]triage.Rule{{Code: "c", Pointer: "/a", Node: "s/x", Severity: ir.SeverityInfo, Reason: "r"}},
			"names a pointer and a node"},
		{"rates and accepts",
			[]triage.Rule{{Code: "c", Pointer: "/a", Severity: ir.SeverityInfo, Accept: true, Reason: "r"}},
			"both rates and accepts"},
		{"accepts everywhere", []triage.Rule{{Code: "c", Accept: true, Reason: "r"}},
			"accepts without naming a place"},
		{"does nothing", []triage.Rule{{Code: "c", Pointer: "/a", Reason: "r"}}, "neither rates nor accepts"},
		{"an unknown severity", []triage.Rule{rate("c", "fatal")},
			`severity "fatal" is not one of error, warning, info`},
		{"no reason", []triage.Rule{{Code: "c", Severity: ir.SeverityInfo}}, "gives no reason"},
		{"a blank reason", []triage.Rule{{Code: "c", Severity: ir.SeverityInfo, Reason: " \t"}}, "gives no reason"},
		{"one place twice", []triage.Rule{
			{Code: "c", Pointer: "/a", Severity: ir.SeverityInfo, Reason: "r"},
			{Code: "c", Pointer: "/a", Accept: true, Reason: "r"},
		}, "rule 1 (code \"c\"): rules the findings rule 0 rules"},
		{"one code twice", []triage.Rule{rate("c", ir.SeverityInfo), rate("d", ir.SeverityInfo), rate("c", ir.SeverityError)},
			"rule 2 (code \"c\"): rules the findings rule 0 rules"},
		{"one node twice", []triage.Rule{
			{Code: "c", Node: "s/x", Severity: ir.SeverityInfo, Reason: "r"},
			{Code: "c", Node: "s/x", Severity: ir.SeverityError, Reason: "r"},
		}, "rules the findings rule 0 rules"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := triage.NewRuleset(tc.rules)
			require.ErrorIs(t, err, triage.ErrInvalidRule)
			assert.Contains(t, err.Error(), tc.want)
		})
	}
}

// TestNewRuleset_TakesEveryRuleItCanApply is the other side of each refusal: a
// rule one step from it is taken, so a check that refused too much reddens.
func TestNewRuleset_TakesEveryRuleItCanApply(t *testing.T) {
	t.Parallel()
	rules := []triage.Rule{
		rate("c", ir.SeverityInfo),
		{Code: "c", Pointer: "/a~1b~0", Severity: ir.SeverityWarning, Reason: "r"},
		{Code: "c", Pointer: "/a", Accept: true, Reason: "r"},
		{Code: "c", Node: "s/x", Accept: true, Reason: "r"},
		{Code: "c", Node: "s/y", Severity: ir.SeverityError, Reason: "r"},
		rate("d", ir.SeverityError),
	}
	_, err := triage.NewRuleset(rules)
	require.NoError(t, err)
	_, err = triage.NewRuleset(nil)
	require.NoError(t, err)
}

// TestNewRuleset_CopiesTheRules holds a ruleset to what it was built from: a
// caller writing into its list afterwards changes no rating.
func TestNewRuleset_CopiesTheRules(t *testing.T) {
	t.Parallel()
	rules := []triage.Rule{rate("c", ir.SeverityInfo)}
	s, err := triage.NewRuleset(rules)
	require.NoError(t, err)
	rules[0].Severity = ir.SeverityError

	findings, _ := s.Rate([]ir.Diagnostic{{Severity: ir.SeverityError, Code: "c"}})
	require.Len(t, findings, 1)
	assert.Equal(t, ir.SeverityInfo, findings[0].Severity)
}

// TestSeverities_AreTheOnesIRDeclares holds the ranked list to the severities
// package ir declares. One added to ir and missing here would be refused as
// unknown on every run reporting it, and a compiler's own tests, which run
// without the engine, would not notice.
func TestSeverities_AreTheOnesIRDeclares(t *testing.T) {
	t.Parallel()
	pkg, err := decls.Load("github.com/dexpace/morphic/ir")
	require.NoError(t, err)
	consts, err := pkg.StringConsts("Severity")
	require.NoError(t, err)
	require.NotEmpty(t, consts, "a pin over no declarations holds nothing")

	declared := make([]ir.Severity, 0, len(consts))
	for _, c := range consts {
		declared = append(declared, ir.Severity(c.Value))
	}
	assert.Empty(t, cmp.Diff(slices.Sorted(slices.Values(declared)),
		slices.Sorted(slices.Values(triage.Severities()))))
}

// TestRank_OrdersTheSeveritiesLeastFirst pins the order a threshold compares
// by, and an undeclared severity ranking nowhere.
func TestRank_OrdersTheSeveritiesLeastFirst(t *testing.T) {
	t.Parallel()
	for want, sev := range []ir.Severity{ir.SeverityInfo, ir.SeverityWarning, ir.SeverityError} {
		got, ok := triage.Rank(sev)
		require.True(t, ok, "%s", sev)
		assert.Equal(t, want, got, "%s", sev)
	}
	_, ok := triage.Rank("fatal")
	assert.False(t, ok)
}
