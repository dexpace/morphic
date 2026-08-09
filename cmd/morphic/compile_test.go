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
	assert.Contains(t, stderr.String(), "no compiler recognizes")
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

// groupingSpec tags its one operation "zoo" while its path starts with "a", so
// the two grouping strategies name the group differently and the assertion below
// cannot pass by accident.
const groupingSpec = `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /a/b:
    get:
      operationId: ab
      tags: [zoo]
      responses: {"200": {description: ok}}
`

// TestRun_CompilerOptionReachesTheCompiler asserts from the CLI what the Go API
// could already do: a compiler option the user set changes the document. The
// control run pins that the difference is the flag and not the spec.
func TestRun_CompilerOptionReachesTheCompiler(t *testing.T) {
	t.Parallel()
	spec := writeFile(t, "spec.yaml", groupingSpec)

	assert.Equal(t, "zoo", groupNameOf(t, spec), "the default groups by tag")
	assert.Equal(t, "a", groupNameOf(t, spec, "-opt", "grouping=path-prefix"),
		"the compiler option must reach the compiler")
}

// groupNameOf compiles spec with args and returns its first operation group's
// source name.
func groupNameOf(t *testing.T, spec string, args ...string) string {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := run(append([]string{"compile", spec}, args...), &stdout, &stderr)
	require.Equal(t, 0, code, "stderr: %s", stderr.String())

	var doc ir.Document
	require.NoError(t, json.Unmarshal(stdout.Bytes(), &doc))
	require.Len(t, doc.Services, 1)
	require.NotEmpty(t, doc.Services[0].Groups)
	return doc.Services[0].Groups[0].Name.Source
}

// TestRun_OverlayOptionReachesTheCompiler covers the option whose value is a
// file. A compiler reads no files of its own, so the whole chain has to work for
// this to compile anything: the CLI collects the path, the engine loads it, and
// the compiler decodes the bytes into its own overlay type.
func TestRun_OverlayOptionReachesTheCompiler(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	spec := filepath.Join(dir, "spec.yaml")
	require.NoError(t, os.WriteFile(spec, []byte(testspec.Tiny), 0o644))
	overlay := filepath.Join(dir, "patch.yaml")
	require.NoError(t, os.WriteFile(overlay, []byte(`overlay: 1.0.0
info: {title: Patch, version: "1"}
actions:
  - target: $.info
    update: {title: Patched}
`), 0o644))

	var stdout, stderr bytes.Buffer
	code := run([]string{"compile", spec, "--opt", "overlay=" + overlay}, &stdout, &stderr)
	require.Equal(t, 0, code, "stderr: %s", stderr.String())

	var doc ir.Document
	require.NoError(t, json.Unmarshal(stdout.Bytes(), &doc))
	assert.Equal(t, "Patched", doc.Name, "the overlay must have been applied")
	assert.Len(t, doc.Sources, 2, "an applied overlay is a second source")
}

func TestRun_BadCompilerOptionIsRefused(t *testing.T) {
	t.Parallel()
	spec := writeFile(t, "spec.yaml", testspec.Tiny)
	tests := []struct {
		name, arg, wantErr string
	}{
		{"malformed pair", "grouping", "want key=value"},
		{"unknown name", "gruoping=tags", `unknown option "gruoping"`},
		{"unusable value", "grouping=alphabetical", `want "tags" or "path-prefix"`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var stdout, stderr bytes.Buffer
			code := run([]string{"compile", spec, "--opt", tc.arg}, &stdout, &stderr)
			assert.Equal(t, 2, code)
			assert.Contains(t, stderr.String(), tc.wantErr)
			assert.Empty(t, stdout.String(), "a refused option must write no document")
		})
	}
}
