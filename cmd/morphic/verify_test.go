package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/engine"
	"github.com/dexpace/morphic/internal/testspec"
	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/ir/irverify"
)

// brokenIRCompiler lowers every spec to a document with count types whose
// registry keys disagree with their IDs: one invariant broken count times.
type brokenIRCompiler struct {
	nilDocCompiler
	count int
}

func (c brokenIRCompiler) Compile(_ context.Context, sources []compilers.Source, _ compilers.Options) (*ir.Document, []ir.Diagnostic, error) {
	types := make(ir.TypeRegistry, c.count)
	for i := range c.count {
		types[ir.TypeID(fmt.Sprintf("t/openapi/key%03d", i))] = &ir.Scalar{ID: "t/openapi/other"}
	}
	return &ir.Document{
		IRVersion: ir.IRVersion,
		Types:     types,
		Sources:   []ir.SourceInfo{{Format: "openapi@3.1", Path: sources[0].Path, Hash: ir.SourceHash(sources[0].Data)}},
	}, nil, nil
}

// useEngine makes the commands compile with an engine built from opts for the
// rest of the test.
func useEngine(t *testing.T, opts ...engine.Option) {
	t.Helper()
	orig := newEngine
	t.Cleanup(func() { newEngine = orig })
	newEngine = func() (*engine.Engine, error) { return engine.New(opts...) }
}

// TestRun_VerifyEndsARunWhoseStageBreaksTheIR runs both commands over a
// compiler that returns a document breaking the IR's invariants: they report
// the first few in the error, every one on a line beneath it, and exit 2, as
// for any stage that broke its contract. Without --verify the same document goes
// through, which is what the flag is for.
func TestRun_VerifyEndsARunWhoseStageBreaksTheIR(t *testing.T) {
	useEngine(t, engine.WithCompilers(brokenIRCompiler{count: 4}))
	spec := writeFile(t, "spec.yaml", testspec.Tiny)

	for _, command := range []string{"compile", "validate"} {
		t.Run(command, func(t *testing.T) {
			var stdout, stderr bytes.Buffer

			code := run([]string{command, spec, "--verify"}, &stdout, &stderr)

			assert.Equal(t, exitFailure, code)
			assert.Empty(t, stdout.String(), "no IR is written for a run that failed")
			lines := strings.Split(strings.TrimRight(stderr.String(), "\n"), "\n")
			assert.Contains(t, lines[0], "morphic: engine: verify compile")
			assert.Contains(t, lines[0], "the document breaks")
			assert.Equal(t, 4, strings.Count(stderr.String(), "ir/type-id-mismatch at"),
				"every violation is on a line of its own beneath the error: %q", stderr.String())
			assert.NotContains(t, stderr.String(), "Try ", "a broken stage is not a misuse")
		})
	}
	t.Run("not asked", func(t *testing.T) {
		var stdout, stderr bytes.Buffer

		// The validate pass is off, so nothing else looks at what the compiler built.
		code := run([]string{"validate", spec, "--skip-validate"}, &stdout, &stderr)

		assert.Equal(t, 0, code, "stderr: %s", stderr.String())
	})
}

func TestRun_VerifyListsAtMostFiftyViolations(t *testing.T) {
	useEngine(t, engine.WithCompilers(brokenIRCompiler{count: 120}))
	spec := writeFile(t, "spec.yaml", testspec.Tiny)
	var stdout, stderr bytes.Buffer

	code := run([]string{"validate", spec, "--verify"}, &stdout, &stderr)

	assert.Equal(t, exitFailure, code)
	counted := regexp.MustCompile(`the document breaks (\d+) invariants`).FindStringSubmatch(stderr.String())
	require.Len(t, counted, 2, "the error says how many invariants broke")
	total, err := strconv.Atoi(counted[1])
	require.NoError(t, err)
	require.Greater(t, total, maxViolationLines, "the document must break more than the bound for this to show it")
	assert.Equal(t, maxViolationLines, strings.Count(stderr.String(), "\n  ir/"), "the lines are bounded")
	assert.Contains(t, stderr.String(), fmt.Sprintf("\n  and %d more\n", total-maxViolationLines),
		"and the rest are counted")
}

// TestRun_VerifyAcceptsWhatTheShippedCompilerLowers pins that the flag changes
// nothing about a run that keeps every invariant: same exit, same document.
func TestRun_VerifyAcceptsWhatTheShippedCompilerLowers(t *testing.T) {
	t.Parallel()
	spec := writeFile(t, "spec.yaml", testspec.Tiny)
	var plain, verified, stderr bytes.Buffer

	require.Equal(t, 0, run([]string{"compile", spec}, &plain, &stderr), stderr.String())
	code := run([]string{"compile", spec, "--verify"}, &verified, &stderr)

	assert.Equal(t, 0, code, "stderr: %s", stderr.String())
	assert.Equal(t, plain.String(), verified.String())
}

// TestReportViolations_BoundsWhatItWrites pins the bounds on a line: the
// document's own text is quoted onto one line and cut.
func TestReportViolations_BoundsWhatItWrites(t *testing.T) {
	t.Parallel()
	err := &engine.VerifyError{Stage: "stage", Violations: []irverify.Violation{
		{Code: "ir/a", Path: "doc.Types[a\n\"b\"]", Message: "m"},
		{Code: "ir/b", Path: strings.Repeat("p", 5000), Message: strings.Repeat("m", 5000)},
	}}
	var out bytes.Buffer

	reportError(&out, failure(err)) // as runPipeline hands it over: a failure, not a misuse

	lines := strings.Split(strings.TrimRight(out.String(), "\n"), "\n")
	require.Len(t, lines, 3)
	assert.Equal(t, `  ir/a at "doc.Types[a\n\"b\"]": m`, lines[1])
	assert.Equal(t, fmt.Sprintf(`  ir/b at "%s": %s`, strings.Repeat("p", maxViolationPath), strings.Repeat("m", maxViolationText)), lines[2])
}

func TestReportViolations_SaysNothingForAnyOtherError(t *testing.T) {
	t.Parallel()
	var out bytes.Buffer

	reportViolations(&out, errors.New("an I/O error"))

	assert.Empty(t, out.String())
}
