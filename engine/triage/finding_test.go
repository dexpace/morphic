// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package triage_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/engine/triage"
	"github.com/dexpace/morphic/ir"
)

// finding is a diagnostic of code at sev, located by prov.
func finding(sev ir.Severity, code string, prov ir.Provenance) ir.Diagnostic {
	return ir.Diagnostic{Severity: sev, Code: code, Message: "m", Provenance: prov}
}

// ruled is what Rate made of one finding: its rating and the reason of the rule
// it met, "" for none.
type ruled struct {
	Severity ir.Severity
	Accepted bool
	Reason   string
}

// rated returns what s made of each of diags, failing the test unless each
// finding carries its diagnostic as it was reported.
func rated(t *testing.T, s triage.Ruleset, diags []ir.Diagnostic) ([]ruled, []triage.Rule) {
	t.Helper()
	findings, unused := s.Rate(diags)
	require.Len(t, findings, len(diags), "a finding per diagnostic")
	got := make([]ruled, len(findings))
	for i, f := range findings {
		require.Equal(t, diags[i], f.Diagnostic, "finding %d carries its diagnostic as reported", i)
		got[i] = ruled{Severity: f.Severity, Accepted: f.Accepted}
		if f.Rule != nil {
			got[i].Reason = f.Rule.Reason
		}
	}
	return got, unused
}

// TestRuleset_RateMatchesTheMostPlacedRule pins which rule a finding meets: one
// at its pointer in the spec, then one at its node, then one naming its code
// alone, and none of another code.
func TestRuleset_RateMatchesTheMostPlacedRule(t *testing.T) {
	t.Parallel()
	s, err := triage.NewRuleset([]triage.Rule{
		{Code: "c", Severity: ir.SeverityInfo, Reason: "wide"},
		{Code: "c", Pointer: "/a", Severity: ir.SeverityWarning, Reason: "at /a"},
		{Code: "c", Node: "s/x", Accept: true, Reason: "on s/x"},
	})
	require.NoError(t, err)

	got, unused := rated(t, s, []ir.Diagnostic{
		finding(ir.SeverityError, "c", ir.Provenance{Pointer: "/a"}),
		finding(ir.SeverityError, "c", ir.Provenance{Pointer: "/b"}),
		finding(ir.SeverityError, "c", ir.Provenance{Source: ir.NoSource, Node: "s/x"}),
		finding(ir.SeverityError, "c", ir.Provenance{Pointer: "/a", Node: "s/x"}),
		finding(ir.SeverityError, "c", ir.Provenance{Pointer: "/b", Node: "s/x"}),
		finding(ir.SeverityError, "d", ir.Provenance{Pointer: "/a"}),
	})
	assert.Equal(t, []ruled{
		{ir.SeverityWarning, false, "at /a"},
		{ir.SeverityInfo, false, "wide"},
		{ir.SeverityError, true, "on s/x"},
		{ir.SeverityWarning, false, "at /a"},
		{ir.SeverityError, true, "on s/x"},
		{ir.SeverityError, false, ""},
	}, got)
	assert.Empty(t, unused)
}

// TestRuleset_RatePlacesAPointerInTheSpecAlone holds a pointer to the spec: the
// same pointer in another document, or on a finding naming no source, is
// another place, which only a rule naming the code alone rules.
func TestRuleset_RatePlacesAPointerInTheSpecAlone(t *testing.T) {
	t.Parallel()
	s, err := triage.NewRuleset([]triage.Rule{
		{Code: "c", Pointer: "/a", Accept: true, Reason: "in the spec"},
	})
	require.NoError(t, err)

	got, unused := rated(t, s, []ir.Diagnostic{
		finding(ir.SeverityError, "c", ir.Provenance{Source: 1, Pointer: "/a"}),
		finding(ir.SeverityError, "c", ir.Provenance{Source: ir.NoSource, Pointer: "/a"}),
	})
	assert.Equal(t, []ruled{{ir.SeverityError, false, ""}, {ir.SeverityError, false, ""}}, got)
	assert.Len(t, unused, 1, "a rule meeting no finding in the spec is unused")

	got, unused = rated(t, s, []ir.Diagnostic{
		finding(ir.SeverityError, "c", ir.Provenance{Pointer: "/a", WrittenBy: 1}),
	})
	assert.Equal(t, []ruled{{ir.SeverityError, true, "in the spec"}}, got,
		"a position an overlay wrote is still the spec's")
	assert.Empty(t, unused)
}

// TestRuleset_RateMatchesTheCodeExactly holds a code to its whole spelling: a
// rule names no code beneath it, nor one it is a prefix of.
func TestRuleset_RateMatchesTheCodeExactly(t *testing.T) {
	t.Parallel()
	s, err := triage.NewRuleset([]triage.Rule{rate("openapi/validation", ir.SeverityInfo)})
	require.NoError(t, err)

	got, unused := rated(t, s, []ir.Diagnostic{
		finding(ir.SeverityError, "openapi/validation/validation-required-field", ir.Provenance{}),
		finding(ir.SeverityError, "openapi/validatio", ir.Provenance{}),
		finding(ir.SeverityError, "OPENAPI/VALIDATION", ir.Provenance{}),
	})
	for i, g := range got {
		assert.Equal(t, ruled{ir.SeverityError, false, ""}, g, "finding %d", i)
	}
	assert.Len(t, unused, 1)
}

// TestRuleset_RateListsTheRulesNoFindingMet pins the unused list: each rule no
// finding met, in the order given, and none that met one, however many.
func TestRuleset_RateListsTheRulesNoFindingMet(t *testing.T) {
	t.Parallel()
	rules := []triage.Rule{
		rate("unmet-first", ir.SeverityInfo),
		rate("met", ir.SeverityInfo),
		{Code: "met", Pointer: "/x", Accept: true, Reason: "unmet second"},
		rate("unmet-third", ir.SeverityWarning),
	}
	s, err := triage.NewRuleset(rules)
	require.NoError(t, err)

	_, unused := rated(t, s, []ir.Diagnostic{
		finding(ir.SeverityError, "met", ir.Provenance{Pointer: "/y"}),
		finding(ir.SeverityError, "met", ir.Provenance{Pointer: "/z"}),
	})
	assert.Equal(t, []triage.Rule{rules[0], rules[2], rules[3]}, unused)
}

// TestRuleset_RateLeavesAnUnruledFindingAsReported pins the zero ruleset, and a
// finding no rule meets, to its reported severity, an undeclared one included:
// judging it is the caller's.
func TestRuleset_RateLeavesAnUnruledFindingAsReported(t *testing.T) {
	t.Parallel()
	diags := []ir.Diagnostic{
		finding(ir.SeverityWarning, "c", ir.Provenance{}),
		finding("fatal", "c", ir.Provenance{}),
	}
	got, unused := rated(t, triage.Ruleset{}, diags)
	assert.Equal(t, []ruled{{ir.SeverityWarning, false, ""}, {"fatal", false, ""}}, got)
	assert.Empty(t, unused)

	findings, unused := triage.Ruleset{}.Rate(nil)
	assert.Nil(t, findings)
	assert.Nil(t, unused)
}

// TestRuleset_RateSharesNoRuleBetweenCalls holds each call's findings to rules
// of their own: writing through one call's Finding.Rule changes no other call.
func TestRuleset_RateSharesNoRuleBetweenCalls(t *testing.T) {
	t.Parallel()
	s, err := triage.NewRuleset([]triage.Rule{rate("c", ir.SeverityInfo)})
	require.NoError(t, err)
	diags := []ir.Diagnostic{finding(ir.SeverityError, "c", ir.Provenance{})}

	first, _ := s.Rate(diags)
	first[0].Rule.Severity = ir.SeverityError
	second, _ := s.Rate(diags)
	assert.Equal(t, ir.SeverityInfo, second[0].Severity)
	assert.Equal(t, ir.SeverityInfo, second[0].Rule.Severity)
}
