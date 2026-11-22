// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package pinnedspecs_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/internal/harness"
	"github.com/dexpace/morphic/internal/harness/pinnedspecs"
	"github.com/dexpace/morphic/ir"
)

// TestFindings_NamesEachDiagnosticByCodeAndPlace pins how a record names a
// diagnostic: its code, then its pointer, the IR node it names where it has
// no pointer, its line and column where it has neither, or the document. A
// pointer, a line and column or the document comes after a source that is not
// the spec's own; a node is named alone, whatever its source. A place holding
// a tab or a line break is quoted, and diagnostics of one code at one place
// are one finding listing their severities.
func TestFindings_NamesEachDiagnosticByCodeAndPlace(t *testing.T) {
	t.Parallel()
	at := func(severity ir.Severity, code string, provenance ir.Provenance) ir.Diagnostic {
		return ir.Diagnostic{Severity: severity, Code: code, Message: "m", Provenance: provenance}
	}
	result := harness.Result{Spec: "spec.yaml", Outcome: harness.OutcomeOK, Diagnostics: []ir.Diagnostic{
		at(ir.SeverityWarning, "c/pointer", ir.Provenance{Pointer: "/paths/~1a"}),
		at(ir.SeverityWarning, "c/twice", ir.Provenance{Pointer: "/x"}),
		at(ir.SeverityError, "c/twice", ir.Provenance{Pointer: "/x"}),
		at(ir.SeverityInfo, "c/node", ir.Provenance{Source: ir.NoSource, Node: "type/x"}),
		at(ir.SeverityInfo, "c/node-and-pointer", ir.Provenance{Pointer: "/p", Node: "type/x"}),
		at(ir.SeverityInfo, "c/position", ir.Provenance{Position: ir.Position{Line: 3, Column: 5}}),
		at(ir.SeverityInfo, "c/document", ir.Provenance{}),
		at(ir.SeverityInfo, "c/other-source", ir.Provenance{Source: 1, Pointer: "/y"}),
		at(ir.SeverityInfo, "c/other-document", ir.Provenance{Source: 2}),
		at(ir.SeverityInfo, "c/tab", ir.Provenance{Pointer: "/a\tb"}),
		at(ir.SeverityInfo, "c/tab-node", ir.Provenance{Node: "n\nm"}),
	}}

	got, err := pinnedspecs.Findings(result)
	require.NoError(t, err)

	assert.Equal(t, []harness.Finding{
		{Name: "c/document document", What: "info"},
		{Name: "c/node node type/x", What: "info"},
		{Name: "c/node-and-pointer /p", What: "info"},
		{Name: "c/other-document source 2 document", What: "info"},
		{Name: "c/other-source source 1 /y", What: "info"},
		{Name: "c/pointer /paths/~1a", What: "warning"},
		{Name: "c/position line 3 column 5", What: "info"},
		{Name: `c/tab "/a\tb"`, What: "info"},
		{Name: `c/tab-node node "n\nm"`, What: "info"},
		{Name: "c/twice /x", What: "error, warning"},
	}, got)
}

// TestFindings_NamesEachOracleThatDidNotPass pins how a record names an
// oracle's verdict: one that passed is none, one that declined says why, and
// one that found something gives its detail's first line, quoted where it
// holds a tab.
func TestFindings_NamesEachOracleThatDidNotPass(t *testing.T) {
	t.Parallel()
	result := harness.Result{Spec: "spec.yaml", Outcome: harness.OutcomeOrderDependent, Verdicts: []harness.Verdict{
		{Oracle: harness.OracleNoPanic, Outcome: harness.OutcomeOK},
		{Oracle: harness.OracleOrder, Outcome: harness.OutcomeOrderDependent, Detail: "first line\nsecond line"},
		{Oracle: harness.OracleDeterminism, Outcome: harness.OutcomeNondeterministic, Detail: "a\tb"},
		{Oracle: harness.OracleSequenceOrder, Declined: harness.DeclineNothingToPermute},
	}}

	got, err := pinnedspecs.Findings(result)
	require.NoError(t, err)

	assert.Equal(t, []harness.Finding{
		{Name: "oracle order-invariance", What: "first line"},
		{Name: "oracle determinism", What: `"a\tb"`},
		{Name: "oracle sequence-order-invariance", What: "declined: nothing to permute"},
	}, got)
}

// TestFindings_RefusesAResultThatIsNoAccountOfASpec pins that a result naming
// no spec, and one a fault ended, whose oracles asked nothing, are refused.
func TestFindings_RefusesAResultThatIsNoAccountOfASpec(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		result harness.Result
		want   string
	}{
		{"no spec", harness.Result{Outcome: harness.OutcomeOK}, "a result naming no spec"},
		{"a fault", harness.Result{Spec: "s.yaml", Outcome: harness.OutcomeError, Detail: "read failed"},
			"s.yaml: the run ended in a fault: error: read failed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := pinnedspecs.Findings(tc.result)
			require.Error(t, err)
			assert.Nil(t, got)
			assert.Contains(t, err.Error(), tc.want)
		})
	}
}
