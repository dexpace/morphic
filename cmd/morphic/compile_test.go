package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/internal/testspec"
	"github.com/dexpace/morphic/ir"
)

func writeFile(t *testing.T, name, contents string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	require.NoError(t, os.WriteFile(path, []byte(contents), 0o644))
	return path
}

func TestRun_ParseWritesIRToFile(t *testing.T) {
	t.Parallel()
	spec := writeFile(t, "spec.yaml", testspec.Tiny)
	out := filepath.Join(t.TempDir(), "ir.json")
	var stdout, stderr bytes.Buffer

	code := run([]string{"compile", spec, "-o", out}, &stdout, &stderr)

	require.Equal(t, 0, code, "stderr: %s", stderr.String())
	raw, err := os.ReadFile(out)
	require.NoError(t, err)
	var doc ir.Document
	require.NoError(t, json.Unmarshal(raw, &doc))
	assert.Equal(t, "Tiny", doc.Name)
	assert.True(t, bytes.HasSuffix(raw, []byte("\n")))
}

func TestRun_ParseUnknownSpecFails(t *testing.T) {
	t.Parallel()
	spec := writeFile(t, "junk.yaml", "hello: world\n")
	var stdout, stderr bytes.Buffer
	code := run([]string{"compile", spec}, &stdout, &stderr)
	assert.Equal(t, 2, code)
	assert.Contains(t, stderr.String(), "unrecognized spec format")
}

func TestRun_DiagnosticsGateExitCode(t *testing.T) {
	t.Parallel()
	// A parseable spec that produces at least one warning-or-info diagnostic
	// but no errors: validation-only keyword.
	spec := writeFile(t, "warn.yaml", `openapi: 3.1.0
info: {title: T, version: "1"}
paths: {}
components:
  schemas:
    S: {type: object, not: {required: [x]}}
`)
	var stdout, stderr bytes.Buffer
	require.Equal(t, 0, run([]string{"compile", spec}, &stdout, &stderr),
		"info diagnostics must not fail the default gate")
	assert.Contains(t, stderr.String(), "openapi/validation-only-keyword")

	stdout.Reset()
	stderr.Reset()
	// Not every diagnostic severity is easy to synthesize; the gate logic is
	// also unit-tested directly:
	assert.Equal(t, 1, exitCodeFor([]ir.Diagnostic{{Severity: ir.SeverityWarning}}, "warning"))
	assert.Equal(t, 0, exitCodeFor([]ir.Diagnostic{{Severity: ir.SeverityWarning}}, "error"))
	assert.Equal(t, 1, exitCodeFor([]ir.Diagnostic{{Severity: ir.SeverityError}}, "error"))
}

func TestRun_UsageErrors(t *testing.T) {
	t.Parallel()

	spec := writeFile(t, "spec.yaml", testspec.Tiny)
	tests := []struct {
		name   string
		args   []string
		reason string
	}{
		{"unknown command", []string{"bogus"}, `unknown command "bogus"`},
		{"help of unknown command", []string{"help", "bogus"}, `unknown command "bogus"`},
		{"help of unknown command with help flag", []string{"help", "bogus", "--help"}, `unknown command "bogus"`},
		{"help flag of unknown command", []string{"-h", "bogus"}, `unknown command "bogus"`},
		{"help with extra args", []string{"help", "compile", "extra"}, "help accepts at most one command"},
		{"help flag with extra args", []string{"--help", "compile", "extra"}, "help accepts at most one command"},
		{"help with extra args and help flag", []string{"help", "a", "b", "--help"}, "help accepts at most one command"},
		{"unknown flag", []string{"compile", spec, "--bogus"}, "flag provided but not defined: -bogus"},
		{"no spec file", []string{"compile"}, "compile requires exactly one spec file"},
		{"two spec files", []string{"compile", spec, spec}, "compile requires exactly one spec file"},
		{"bad fail-on", []string{"compile", spec, "--fail-on", "hint"}, `invalid --fail-on "hint"`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var stdout, stderr bytes.Buffer

			assert.Equal(t, 2, run(tt.args, &stdout, &stderr))
			assert.Empty(t, stdout.String(), "usage errors must never write to stdout")
			assert.Contains(t, stderr.String(), tt.reason)
			assert.Equal(t, 1, strings.Count(stderr.String(), "usage:"),
				"exactly one usage block per misuse, got:\n%s", stderr.String())
		})
	}
}
