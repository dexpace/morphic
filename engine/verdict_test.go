// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package engine_test

import (
	"context"
	"encoding/json/jsontext"
	"fmt"
	"path/filepath"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/engine"
	"github.com/dexpace/morphic/engine/triage"
	"github.com/dexpace/morphic/internal/testspec"
	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/pass"
)

// thresholds lists the FailOn constants and the empty default.
var thresholds = []engine.FailOn{
	"", engine.FailOnError, engine.FailOnWarning, engine.FailOnInfo, engine.FailOnNone,
}

// TestParseFailOn_ReadsEachThreshold pins each spelling as literal text: the
// text is what a flag or a config file hands over, and a round trip through
// the constants would pass however they were spelt.
func TestParseFailOn_ReadsEachThreshold(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		text string
		want engine.FailOn
	}{
		{"error", engine.FailOnError},
		{"warning", engine.FailOnWarning},
		{"info", engine.FailOnInfo},
		{"none", engine.FailOnNone},
	} {
		got, err := engine.ParseFailOn(tc.text)
		require.NoError(t, err, "%q", tc.text)
		assert.Equal(t, tc.want, got)
	}
}

// TestParseFailOn_RefusesWhatNamesNoThreshold pins the empty string among the
// refusals: RunOptions reads it as the default, but a caller parsing text was
// handed a value, and an empty one is a mistake to report. A threshold spelled
// in another case or padded, and a severity the IR does not define, are
// refused alike.
func TestParseFailOn_RefusesWhatNamesNoThreshold(t *testing.T) {
	t.Parallel()
	for _, s := range []string{"", "Error", " none", "warning ", "hint", "fatal"} {
		_, err := engine.ParseFailOn(s)
		require.ErrorIs(t, err, engine.ErrUnknownFailOn, "%q", s)
		assert.Contains(t, err.Error(), fmt.Sprintf("fail-on %q (known: error, warning, info, none)", s))
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
	res, err := eng.Run(t.Context(), missing, engine.RunOptions{FailOn: "hint"})

	require.ErrorIs(t, err, engine.ErrUnknownFailOn)
	assert.Nil(t, res)
	assert.NotContains(t, err.Error(), "read spec")
}

// TestEngine_RunVerdict holds Result.Failed to each threshold over a
// document's diagnostics. The compiler stores them and validate is off, so a
// row's list is the whole list the verdict reads. The mixed rows put the
// failing diagnostic first and last, so the verdict must read every one and
// keep what an earlier one decided.
func TestEngine_RunVerdict(t *testing.T) {
	t.Parallel()
	failing := map[engine.FailOn][]ir.Severity{
		"":                   {ir.SeverityError},
		engine.FailOnError:   {ir.SeverityError},
		engine.FailOnWarning: {ir.SeverityError, ir.SeverityWarning},
		engine.FailOnInfo:    {ir.SeverityError, ir.SeverityWarning, ir.SeverityInfo},
		engine.FailOnNone:    nil,
	}
	lists := [][]ir.Severity{
		nil,
		{ir.SeverityInfo},
		{ir.SeverityWarning},
		{ir.SeverityError},
		{ir.SeverityError, ir.SeverityInfo},
		{ir.SeverityInfo, ir.SeverityWarning, ir.SeverityError},
	}
	for _, failOn := range thresholds {
		for _, sevs := range lists {
			want := slices.ContainsFunc(sevs, func(s ir.Severity) bool {
				return slices.Contains(failing[failOn], s)
			})
			t.Run(fmt.Sprintf("fail-on=%q/%v", failOn, sevs), func(t *testing.T) {
				t.Parallel()
				res := runStored(t, failOn, diagnosticsAt(sevs...))

				require.NotNil(t, res.Document)
				assert.Equal(t, want, res.Failed)
			})
		}
	}
}

// TestEngine_RunVerdictReadsThePassesFindings pins that the verdict is taken
// after the passes, over their findings too: validate reporting the dangling
// reference a transform left fails a run whose compiler reported nothing, and
// turning validate off leaves only the transform's info.
func TestEngine_RunVerdictReadsThePassesFindings(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name     string
		disabled []string
		want     bool
	}{
		{"validate on", nil, true},
		{"validate off", []string{engine.ValidatePass}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			res, codes := runWith(t, splitDiagCompiler{}, []pass.Pass{danglePass{}, pass.Validator{}}, tc.disabled...)

			assert.Equal(t, tc.want, slices.Contains(codes, "ir/dangling-type-ref"),
				"the row must turn on whether validate reported, or it proves nothing")
			assert.Equal(t, tc.want, res.Failed)
		})
	}
}

// TestEngine_RunWithoutADocumentFails pins that no threshold passes a run that
// produced no document, FailOnNone included, nor a rule rating its findings
// down: there is nothing for the next stage to read. Both refusal paths are
// covered, a source no compiler claims and a compile that lowered nothing.
func TestEngine_RunWithoutADocumentFails(t *testing.T) {
	t.Parallel()
	unclaimed, err := engine.New()
	require.NoError(t, err)
	declined, err := engine.New(engine.WithCompilers(nilDocCompiler{reported: diagnosticsAt(ir.SeverityError)}))
	require.NoError(t, err)

	for _, failOn := range thresholds {
		for _, tc := range []struct {
			name string
			eng  *engine.Engine
			spec string
		}{
			{"no compiler claims it", unclaimed, "hello: world\n"},
			{"the compiler lowers nothing", declined, testspec.Tiny},
		} {
			t.Run(fmt.Sprintf("fail-on=%q/%s", failOn, tc.name), func(t *testing.T) {
				t.Parallel()
				res, err := tc.eng.Run(t.Context(), writeSpec(t, tc.spec), engine.RunOptions{FailOn: failOn, Triage: []triage.Rule{
					{Code: engine.CodeUnrecognizedFormat, Severity: ir.SeverityInfo, Reason: "r"},
					{Code: "x/finding", Severity: ir.SeverityInfo, Reason: "r"},
				}})

				require.NoError(t, err)
				assert.Nil(t, res.Document)
				assert.Len(t, res.UnusedRules, 1, "one rule rates the run's one finding down")
				assert.True(t, res.Failed)
			})
		}
	}
}

// TestEngine_EveryEntryPointJudgesItsRun pins that RunSpec, RunSources and
// RunDocument judge their Result as Run does: a run whose findings hold an
// error fails at the default threshold, passes at FailOnNone, and passes once a
// rule rates the error a warning. RunSpec and RunSources compile a spec with a
// reference to a document nothing holds; RunDocument runs validate over a
// dangling reference.
func TestEngine_EveryEntryPointJudgesItsRun(t *testing.T) {
	t.Parallel()
	eng, err := engine.New()
	require.NoError(t, err)
	lowered := []triage.Rule{
		{Code: "openapi/unresolved-ref", Severity: ir.SeverityWarning, Reason: "r"},
		{Code: "ir/dangling-type-ref", Severity: ir.SeverityWarning, Reason: "r"},
	}
	for _, tc := range []struct {
		failOn engine.FailOn
		triage []triage.Rule
		want   bool
	}{{"", nil, true}, {engine.FailOnNone, nil, false}, {"", lowered, false}} {
		opts := engine.RunOptions{FailOn: tc.failOn, Triage: tc.triage}
		t.Run(fmt.Sprintf("fail-on=%q/rules %d/RunSpec", tc.failOn, len(tc.triage)), func(t *testing.T) {
			t.Parallel()
			spec := compilers.Source{Path: filepath.Join(t.TempDir(), "root.yaml"), Data: []byte(splitRoot)}
			res, err := eng.RunSpec(t.Context(), spec, opts)

			require.NoError(t, err)
			require.NotNil(t, res.Document)
			require.NotEmpty(t, unresolved(res.Diagnostics), "the row must hold an error, or it proves nothing")
			assert.Equal(t, tc.want, res.Failed)
		})
		t.Run(fmt.Sprintf("fail-on=%q/rules %d/RunSources", tc.failOn, len(tc.triage)), func(t *testing.T) {
			t.Parallel()
			res, err := eng.RunSources(t.Context(), []compilers.Source{{Path: "root.yaml", Data: []byte(splitRoot)}}, opts)

			require.NoError(t, err)
			require.NotNil(t, res.Document)
			require.NotEmpty(t, unresolved(res.Diagnostics), "the row must hold an error, or it proves nothing")
			assert.Equal(t, tc.want, res.Failed)
		})
		t.Run(fmt.Sprintf("fail-on=%q/rules %d/RunDocument", tc.failOn, len(tc.triage)), func(t *testing.T) {
			t.Parallel()
			res, err := eng.RunDocument(t.Context(), danglingDocument(), opts)

			require.NoError(t, err)
			require.True(t, hasDiagCode(res.Diagnostics, "ir/dangling-type-ref"), "the row must hold an error")
			assert.Equal(t, tc.want, res.Failed)
		})
	}
}

// reportPass is an analysis that reports what reported holds.
type reportPass struct{ reported []ir.Diagnostic }

func (reportPass) Name() string { return "report" }

func (p reportPass) Run(_ context.Context, doc *ir.Document) (*ir.Document, []ir.Diagnostic) {
	return doc, slices.Clone(p.reported)
}

// TestEngine_RunRefusesAnUnknownSeverity pins that a diagnostic of a severity
// the IR does not define is a Go error at every threshold, whichever stage
// reported it and whether or not there is a document. Ranked below every
// severity, a misspelt error would pass every gate.
// The second list puts it after an error, which has already failed the run,
// so the verdict must still read on.
func TestEngine_RunRefusesAnUnknownSeverity(t *testing.T) {
	t.Parallel()
	stages := []struct {
		name  string
		build func([]ir.Diagnostic) (*engine.Engine, error)
	}{
		{"the compiler, with a document", func(diags []ir.Diagnostic) (*engine.Engine, error) {
			return engine.New(engine.WithCompilers(splitDiagCompiler{stored: diags}))
		}},
		// A refusal states why, so its list leads with an error: one holding only
		// the unknown severity breaks the contract before the verdict reads it.
		{"the compiler, with none", func(diags []ir.Diagnostic) (*engine.Engine, error) {
			why := ir.Diagnostic{Severity: ir.SeverityError, Code: "x/refused"}
			return engine.New(engine.WithCompilers(nilDocCompiler{reported: append([]ir.Diagnostic{why}, diags...)}))
		}},
		{"a pass", func(diags []ir.Diagnostic) (*engine.Engine, error) {
			return engine.New(engine.WithCompilers(splitDiagCompiler{}), engine.WithPasses(reportPass{diags}))
		}},
	}
	for _, failOn := range thresholds {
		for _, sev := range []ir.Severity{"", "Error", "fatal"} {
			for _, sevs := range [][]ir.Severity{{sev}, {ir.SeverityError, sev}} {
				for _, stage := range stages {
					t.Run(fmt.Sprintf("fail-on=%q/%q/%s", failOn, sevs, stage.name), func(t *testing.T) {
						t.Parallel()
						eng, err := stage.build(diagnosticsAt(sevs...))
						require.NoError(t, err)

						res, err := eng.Run(t.Context(), writeSpec(t, testspec.Tiny), engine.RunOptions{FailOn: failOn})

						require.ErrorIs(t, err, engine.ErrUnknownSeverity)
						assert.Nil(t, res)
						assert.Contains(t, err.Error(), fmt.Sprintf("diagnostic %q has severity %q", "x/finding", sev))
					})
				}
			}
		}
	}
}

// runStored runs a compiler storing diags on its document, validate off, and
// judges the run at failOn.
func runStored(t *testing.T, failOn engine.FailOn, diags []ir.Diagnostic) *engine.Result {
	t.Helper()
	eng, err := engine.New(engine.WithCompilers(splitDiagCompiler{stored: diags}))
	require.NoError(t, err)

	res, err := eng.Run(t.Context(), writeSpec(t, testspec.Tiny), engine.RunOptions{
		FailOn:        failOn,
		DisablePasses: []string{engine.ValidatePass},
	})
	require.NoError(t, err)
	return res
}

// diagnosticsAt returns one diagnostic per severity in sevs, in order, each
// coded x/finding and told apart by its message.
func diagnosticsAt(sevs ...ir.Severity) []ir.Diagnostic {
	diags := make([]ir.Diagnostic, 0, len(sevs))
	for i, sev := range sevs {
		diags = append(diags, ir.Diagnostic{Severity: sev, Code: "x/finding", Message: fmt.Sprint(i)})
	}
	return diags
}

// TestEngine_RunDocumentIsJudgedAsRunIs pins that RunDocument judges a run by
// Run's rule: over a document carrying the findings a compile stored, the two
// reach one verdict at every threshold, and both refuse a severity the IR does
// not define. Validate is off, so the findings are the document's alone.
func TestEngine_RunDocumentIsJudgedAsRunIs(t *testing.T) {
	t.Parallel()
	eng, err := engine.New()
	require.NoError(t, err)
	for _, failOn := range thresholds {
		for _, sev := range []ir.Severity{ir.SeverityInfo, ir.SeverityWarning, ir.SeverityError, "fatal"} {
			t.Run(fmt.Sprintf("fail-on=%q/%s", failOn, sev), func(t *testing.T) {
				t.Parallel()
				doc := danglingDocument()
				doc.Diagnostics = diagnosticsAt(sev)
				opts := engine.RunOptions{FailOn: failOn, DisablePasses: []string{engine.ValidatePass}}

				res, err := eng.RunDocument(t.Context(), doc, opts)

				if sev == "fatal" {
					require.ErrorIs(t, err, engine.ErrUnknownSeverity)
					assert.Nil(t, res)
					return
				}
				require.NoError(t, err)
				assert.Equal(t, runStored(t, failOn, diagnosticsAt(sev)).Failed, res.Failed)
			})
		}
	}
}

// placedAt returns a diagnostic of severity sev coded x/finding at pointer in
// the spec, where a rule can place itself.
func placedAt(sev ir.Severity, pointer string) ir.Diagnostic {
	return ir.Diagnostic{Severity: sev, Code: "x/finding", Message: "m", Provenance: ir.Provenance{Pointer: jsontext.Pointer(pointer)}}
}

// runTriaged runs a compiler storing diags on its document, validate off,
// rating them by rules and judging the run at failOn.
func runTriaged(t *testing.T, failOn engine.FailOn, rules []triage.Rule, diags ...ir.Diagnostic) *engine.Result {
	t.Helper()
	eng, err := engine.New(engine.WithCompilers(splitDiagCompiler{stored: diags}))
	require.NoError(t, err)
	res, err := eng.Run(t.Context(), writeSpec(t, testspec.Tiny), engine.RunOptions{
		FailOn: failOn, Triage: rules, DisablePasses: []string{engine.ValidatePass},
	})
	require.NoError(t, err)
	return res
}

// TestEngine_RunVerdictReadsTheRatedSeverity holds the verdict to what a rule
// rates a finding at, not what its stage reported: an error rated at each
// severity fails exactly the thresholds that severity reaches, and one a rule
// accepts fails none, FailOnInfo's included.
func TestEngine_RunVerdictReadsTheRatedSeverity(t *testing.T) {
	t.Parallel()
	reaches := map[engine.FailOn][]ir.Severity{
		"":                   {ir.SeverityError},
		engine.FailOnError:   {ir.SeverityError},
		engine.FailOnWarning: {ir.SeverityError, ir.SeverityWarning},
		engine.FailOnInfo:    {ir.SeverityError, ir.SeverityWarning, ir.SeverityInfo},
		engine.FailOnNone:    nil,
	}
	for _, failOn := range thresholds {
		for _, rated := range []ir.Severity{ir.SeverityInfo, ir.SeverityWarning, ir.SeverityError} {
			t.Run(fmt.Sprintf("fail-on=%q/rated %s", failOn, rated), func(t *testing.T) {
				t.Parallel()
				res := runTriaged(t, failOn, []triage.Rule{{Code: "x/finding", Severity: rated, Reason: "r"}},
					placedAt(ir.SeverityError, "/a"))
				require.Len(t, res.Findings, 1)
				assert.Equal(t, rated, res.Findings[0].Severity)
				assert.Equal(t, slices.Contains(reaches[failOn], rated), res.Failed)
			})
		}
		t.Run(fmt.Sprintf("fail-on=%q/accepted", failOn), func(t *testing.T) {
			t.Parallel()
			res := runTriaged(t, failOn, []triage.Rule{{Code: "x/finding", Pointer: "/a", Accept: true, Reason: "r"}},
				placedAt(ir.SeverityError, "/a"), placedAt(ir.SeverityInfo, "/a"))
			require.Len(t, res.Findings, 2)
			assert.True(t, res.Findings[0].Accepted && res.Findings[1].Accepted)
			assert.False(t, res.Failed, "an accepted finding fails no threshold")
		})
	}
}

// TestEngine_RunVerdictReadsEveryRatedFinding puts the one finding that fails
// the run first and last among findings a rule rates or accepts away, so the
// verdict must read each and keep what an earlier one decided.
func TestEngine_RunVerdictReadsEveryRatedFinding(t *testing.T) {
	t.Parallel()
	rules := []triage.Rule{
		{Code: "x/finding", Pointer: "/accepted", Accept: true, Reason: "r"},
		{Code: "x/finding", Pointer: "/lowered", Severity: ir.SeverityInfo, Reason: "r"},
	}
	failing := placedAt(ir.SeverityError, "/kept")
	quiet := []ir.Diagnostic{placedAt(ir.SeverityError, "/accepted"), placedAt(ir.SeverityError, "/lowered")}

	assert.True(t, runTriaged(t, "", rules, append([]ir.Diagnostic{failing}, quiet...)...).Failed)
	assert.True(t, runTriaged(t, "", rules, append(slices.Clone(quiet), failing)...).Failed)
	assert.False(t, runTriaged(t, "", rules, quiet...).Failed)
}

// TestEngine_RunListsEachFindingAsRated holds Result.Findings to Diagnostics,
// index for index, each with the rule it met, and UnusedRules to the rules no
// finding met, in the order given.
func TestEngine_RunListsEachFindingAsRated(t *testing.T) {
	t.Parallel()
	rules := []triage.Rule{
		{Code: "x/unmet", Severity: ir.SeverityInfo, Reason: "first"},
		{Code: "x/finding", Pointer: "/a", Accept: true, Reason: "accepted"},
		{Code: "x/finding", Severity: ir.SeverityWarning, Reason: "lowered"},
		{Code: "x/finding", Pointer: "/unmet", Accept: true, Reason: "second"},
	}
	res := runTriaged(t, "", rules, placedAt(ir.SeverityError, "/b"), placedAt(ir.SeverityError, "/a"))

	require.Len(t, res.Findings, len(res.Diagnostics))
	for i, f := range res.Findings {
		assert.Equal(t, res.Diagnostics[i], f.Diagnostic, "finding %d", i)
	}
	assert.Equal(t, ir.SeverityWarning, res.Findings[0].Severity)
	assert.Equal(t, "lowered", res.Findings[0].Rule.Reason)
	assert.True(t, res.Findings[1].Accepted)
	assert.Equal(t, ir.SeverityError, res.Findings[1].Severity, "an accepted finding keeps the severity reported")
	assert.Equal(t, ir.SeverityError, res.Diagnostics[0].Severity, "Diagnostics keeps what the stage reported")
	assert.Equal(t, []triage.Rule{rules[0], rules[3]}, res.UnusedRules)
	assert.False(t, res.Failed)
}

// TestEngine_RunRefusesARuleBeforeReading pins the order: a rule no run can
// apply is the caller's mistake whatever the path says, so it is refused before
// a missing file could be reported instead, naming the rule's position.
func TestEngine_RunRefusesARuleBeforeReading(t *testing.T) {
	t.Parallel()
	eng, err := engine.New()
	require.NoError(t, err)

	missing := filepath.Join(t.TempDir(), "absent.yaml")
	res, err := eng.Run(t.Context(), missing, engine.RunOptions{Triage: []triage.Rule{
		{Code: "x/finding", Severity: ir.SeverityInfo, Reason: "r"},
		{Code: "x/finding", Accept: true, Reason: "r"},
	}})

	require.ErrorIs(t, err, triage.ErrInvalidRule)
	assert.Nil(t, res)
	assert.Contains(t, err.Error(), "rule 1")
	assert.NotContains(t, err.Error(), "read spec")
}

// TestEngine_RunRefusesAnUnknownSeverityWhateverItIsRated pins that a rule
// cannot hide a stage's broken contract: a finding reported at a severity the
// IR does not define is refused though a rule rates or accepts it.
func TestEngine_RunRefusesAnUnknownSeverityWhateverItIsRated(t *testing.T) {
	t.Parallel()
	for name, rule := range map[string]triage.Rule{
		"rated":    {Code: "x/finding", Severity: ir.SeverityInfo, Reason: "r"},
		"accepted": {Code: "x/finding", Pointer: "/a", Accept: true, Reason: "r"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			eng, err := engine.New(engine.WithCompilers(splitDiagCompiler{stored: []ir.Diagnostic{placedAt("fatal", "/a")}}))
			require.NoError(t, err)
			res, err := eng.Run(t.Context(), writeSpec(t, testspec.Tiny), engine.RunOptions{
				Triage: []triage.Rule{rule}, DisablePasses: []string{engine.ValidatePass},
			})
			require.ErrorIs(t, err, engine.ErrUnknownSeverity)
			assert.Nil(t, res)
		})
	}
}

// TestVerdict_RefusesARatingNoThresholdRanks holds the verdict to refusing a
// finding rated at a severity it cannot rank, which no checked rule sets: ranked
// below every threshold, it would pass every gate.
func TestVerdict_RefusesARatingNoThresholdRanks(t *testing.T) {
	t.Parallel()
	d := placedAt(ir.SeverityError, "/a")
	_, err := engine.Verdict("", &ir.Document{}, []triage.Finding{{Diagnostic: d, Severity: "fatal"}})
	require.ErrorIs(t, err, engine.ErrUnknownSeverity)
	assert.Contains(t, err.Error(), `diagnostic "x/finding" is rated at severity "fatal"`)

	failed, err := engine.Verdict("", &ir.Document{}, []triage.Finding{{Diagnostic: d, Severity: ir.SeverityError}})
	require.NoError(t, err)
	assert.True(t, failed)
}
