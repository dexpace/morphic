package engine_test

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/engine"
	"github.com/dexpace/morphic/internal/testspec"
	"github.com/dexpace/morphic/ir"
)

func TestParseFailOn_ReadsEachThreshold(t *testing.T) {
	t.Parallel()
	for _, want := range []engine.FailOn{engine.FailOnError, engine.FailOnWarning, engine.FailOnNone} {
		got, err := engine.ParseFailOn(string(want))
		require.NoError(t, err)
		assert.Equal(t, want, got)
	}
}

// TestParseFailOn_RefusesWhatNamesNoThreshold pins the empty string among the
// refusals: RunOptions reads it as the default, but a caller parsing text was
// handed a value, and an empty one is a mistake to report. A severity that is
// no threshold, and one spelled in another case, are refused alike.
func TestParseFailOn_RefusesWhatNamesNoThreshold(t *testing.T) {
	t.Parallel()
	for _, s := range []string{"", "info", "Error", "hint"} {
		_, err := engine.ParseFailOn(s)
		require.ErrorIs(t, err, engine.ErrUnknownFailOn, "%q", s)
		assert.Contains(t, err.Error(), "want error, warning or none")
	}
}

// TestEngine_RunRefusesAnUnknownFailOnBeforeReading pins the order: a
// misconfigured run is the caller's mistake whatever the path says, so the
// threshold is refused before a missing file could be reported instead.
func TestEngine_RunRefusesAnUnknownFailOnBeforeReading(t *testing.T) {
	t.Parallel()
	eng, err := engine.New()
	require.NoError(t, err)

	missing := filepath.Join(t.TempDir(), "absent.yaml")
	_, err = eng.Run(t.Context(), missing, engine.RunOptions{FailOn: "hint"})

	require.ErrorIs(t, err, engine.ErrUnknownFailOn)
	assert.NotContains(t, err.Error(), "read spec")
}

// TestEngine_RunVerdict holds Result.Failed to the threshold over a document's
// diagnostics, one row per boundary. The compiler stores the diagnostics and
// validate is off, so the row's list is the whole list the verdict reads.
func TestEngine_RunVerdict(t *testing.T) {
	t.Parallel()
	diag := func(sev ir.Severity) []ir.Diagnostic {
		return []ir.Diagnostic{{Severity: sev, Code: "x/finding"}}
	}
	tests := []struct {
		name   string
		diags  []ir.Diagnostic
		failOn engine.FailOn
		want   bool
	}{
		{"no diagnostics", nil, "", false},
		{"info under the default", diag(ir.SeverityInfo), "", false},
		{"warning under the default", diag(ir.SeverityWarning), "", false},
		{"error under the default", diag(ir.SeverityError), "", true},
		{"error at error", diag(ir.SeverityError), engine.FailOnError, true},
		{"warning at error", diag(ir.SeverityWarning), engine.FailOnError, false},
		{"warning at warning", diag(ir.SeverityWarning), engine.FailOnWarning, true},
		{"error at warning", diag(ir.SeverityError), engine.FailOnWarning, true},
		{"info at warning", diag(ir.SeverityInfo), engine.FailOnWarning, false},
		{"undefined severity at warning", diag("nonsense"), engine.FailOnWarning, false},
		{"error at none", diag(ir.SeverityError), engine.FailOnNone, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			eng, err := engine.NewWith(splitDiagCompiler{stored: tc.diags})
			require.NoError(t, err)

			res, err := eng.Run(t.Context(), writeSpec(t, testspec.Tiny), engine.RunOptions{
				FailOn:        tc.failOn,
				DisablePasses: []string{engine.ValidatePass},
			})

			require.NoError(t, err)
			require.NotNil(t, res.Document)
			assert.Equal(t, tc.want, res.Failed)
		})
	}
}

// TestEngine_RunWithoutADocumentFails pins that no threshold passes a run that
// produced no document, FailOnNone included: there is nothing for the next
// stage to read. Both refusal paths are covered, a source no compiler claims and
// a compile that lowered nothing.
func TestEngine_RunWithoutADocumentFails(t *testing.T) {
	t.Parallel()
	unclaimed, err := engine.New()
	require.NoError(t, err)
	declined, err := engine.NewWith(nilDocCompiler{})
	require.NoError(t, err)

	for _, tc := range []struct {
		name string
		eng  *engine.Engine
		spec string
	}{
		{"no compiler claims it", unclaimed, "hello: world\n"},
		{"the compiler lowers nothing", declined, testspec.Tiny},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			res, err := tc.eng.Run(t.Context(), writeSpec(t, tc.spec), engine.RunOptions{FailOn: engine.FailOnNone})
			require.NoError(t, err)
			assert.Nil(t, res.Document)
			assert.True(t, res.Failed)
		})
	}
}
