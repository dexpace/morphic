package engine_test

import (
	"fmt"
	"path/filepath"
	"slices"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/engine"
	"github.com/dexpace/morphic/internal/decls"
	"github.com/dexpace/morphic/internal/testspec"
	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/pass"
)

// thresholds lists the FailOn constants and the empty default.
var thresholds = []engine.FailOn{
	"", engine.FailOnError, engine.FailOnWarning, engine.FailOnInfo, engine.FailOnNone,
}

// TestSeverities_AreTheOnesIRDeclares holds the engine's ranked list to the
// severities package ir declares. One added to ir and missing here would be
// refused as unknown on every run reporting it, and a compiler's own tests,
// which run without the engine, would not notice.
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
		slices.Sorted(slices.Values(engine.Severities()))))
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
// produced no document, FailOnNone included: there is nothing for the next
// stage to read. Both refusal paths are covered, a source no compiler claims
// and a compile that lowered nothing.
func TestEngine_RunWithoutADocumentFails(t *testing.T) {
	t.Parallel()
	unclaimed, err := engine.New()
	require.NoError(t, err)
	declined, err := engine.NewWith(nilDocCompiler{reported: diagnosticsAt(ir.SeverityError)})
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
				res, err := tc.eng.Run(t.Context(), writeSpec(t, tc.spec), engine.RunOptions{FailOn: failOn})

				require.NoError(t, err)
				assert.Nil(t, res.Document)
				assert.True(t, res.Failed)
			})
		}
	}
}

// TestEngine_EveryEntryPointJudgesItsRun pins that RunSources and RunDocument
// judge their Result as Run does: a run whose findings hold an error fails at
// the default threshold and passes at FailOnNone. RunSources compiles a spec
// with a reference to a document no source holds; RunDocument runs validate
// over a dangling reference.
func TestEngine_EveryEntryPointJudgesItsRun(t *testing.T) {
	t.Parallel()
	eng, err := engine.New()
	require.NoError(t, err)
	for _, tc := range []struct {
		failOn engine.FailOn
		want   bool
	}{{"", true}, {engine.FailOnNone, false}} {
		opts := engine.RunOptions{FailOn: tc.failOn}
		t.Run(fmt.Sprintf("fail-on=%q/RunSources", tc.failOn), func(t *testing.T) {
			t.Parallel()
			res, err := eng.RunSources(t.Context(), []compilers.Source{{Path: "root.yaml", Data: []byte(splitRoot)}}, opts)

			require.NoError(t, err)
			require.NotNil(t, res.Document)
			require.NotEmpty(t, unresolved(res.Diagnostics), "the row must hold an error, or it proves nothing")
			assert.Equal(t, tc.want, res.Failed)
		})
		t.Run(fmt.Sprintf("fail-on=%q/RunDocument", tc.failOn), func(t *testing.T) {
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

func (p reportPass) Run(doc *ir.Document) (*ir.Document, []ir.Diagnostic) {
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
			return engine.NewWith(splitDiagCompiler{stored: diags})
		}},
		// A refusal states why, so its list leads with an error: one holding only
		// the unknown severity breaks the contract before the verdict reads it.
		{"the compiler, with none", func(diags []ir.Diagnostic) (*engine.Engine, error) {
			why := ir.Diagnostic{Severity: ir.SeverityError, Code: "x/refused"}
			return engine.NewWith(nilDocCompiler{reported: append([]ir.Diagnostic{why}, diags...)})
		}},
		{"a pass", func(diags []ir.Diagnostic) (*engine.Engine, error) {
			return engine.NewWithPasses([]pass.Pass{reportPass{diags}}, splitDiagCompiler{})
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
	eng, err := engine.NewWith(splitDiagCompiler{stored: diags})
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
