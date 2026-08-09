package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/engine"
	"github.com/dexpace/morphic/internal/testspec"
)

// warnSpec lowers cleanly but stamps one info diagnostic, so it separates
// "reached --fail-on" from "produced diagnostics".
const warnSpec = `openapi: 3.1.0
info: {title: T, version: "1"}
paths: {}
components:
  schemas:
    S: {type: object, not: {required: [x]}}
`

// errSpec has an unresolvable $ref, so the compiler reports an error
// diagnostic while still producing a document.
const errSpec = `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /x:
    get:
      operationId: getX
      responses:
        "200":
          description: ok
          content:
            application/json: {schema: {$ref: '#/components/schemas/Missing'}}
`

func TestRun_ValidateExitCodes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		spec     string
		args     []string
		wantCode int
		wantDiag string
	}{
		{"clean spec", testspec.Tiny, nil, 0, ""},
		{"info diagnostics below the threshold", warnSpec, nil, 0, "openapi/validation-only-keyword"},
		{"info diagnostics below a lowered threshold", warnSpec,
			[]string{"--fail-on", "warning"}, 0, "openapi/validation-only-keyword"},
		{"error diagnostics", errSpec, nil, 1, "openapi/unresolved-ref"},
		{"error diagnostics with --skip-validate", errSpec,
			[]string{"--skip-validate"}, 1, "openapi/unresolved-ref"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			spec := writeFile(t, "spec.yaml", tt.spec)
			var stdout, stderr bytes.Buffer

			code := run(append([]string{"validate", spec}, tt.args...), &stdout, &stderr)

			assert.Equal(t, tt.wantCode, code, "stderr: %s", stderr.String())
			assert.Empty(t, stdout.String(), "validate must write nothing to stdout")
			if tt.wantDiag == "" {
				assert.Empty(t, stderr.String(), "a clean spec has nothing to report")
				return
			}
			assert.Contains(t, stderr.String(), tt.wantDiag)
		})
	}
}

// TestRun_ValidateAgreesWithCompile is the acceptance criterion the subcommand
// exists to meet: for the same spec it reports the same diagnostics and returns
// the same exit code as compile, so a gate can swap one for the other and read
// the result the same way. compile writes to a file rather than stdout, since
// the artifact is exactly what validate declines to produce.
func TestRun_ValidateAgreesWithCompile(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		spec string
		args []string
	}{
		{"clean spec", testspec.Tiny, nil},
		{"info diagnostics", warnSpec, nil},
		{"error diagnostics", errSpec, nil},
		{"--fail-on warning", warnSpec, []string{"--fail-on", "warning"}},
		{"--skip-validate", warnSpec, []string{"--skip-validate"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			spec := writeFile(t, "spec.yaml", tt.spec)
			out := filepath.Join(t.TempDir(), "ir.json")

			var compileOut, compileErr bytes.Buffer
			compileCode := run(append([]string{"compile", spec, "-o", out}, tt.args...),
				&compileOut, &compileErr)

			var validateOut, validateErr bytes.Buffer
			validateCode := run(append([]string{"validate", spec}, tt.args...),
				&validateOut, &validateErr)

			assert.Equal(t, compileCode, validateCode, "exit codes must agree")
			assert.Empty(t, cmp.Diff(compileErr.String(), validateErr.String()),
				"diagnostics must agree")
			assert.Empty(t, validateOut.String(), "validate must write nothing to stdout")

			_, err := os.Stat(out)
			require.NoError(t, err, "precondition: compile wrote the artifact validate skips")
		})
	}
}

// TestRun_ValidateWritesNoFile pins the property the subcommand is for: there is
// no argument that makes it produce an artifact, so nothing appears in the
// directory it runs against.
func TestRun_ValidateWritesNoFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	spec := filepath.Join(dir, "spec.yaml")
	require.NoError(t, os.WriteFile(spec, []byte(testspec.Tiny), 0o644))
	var stdout, stderr bytes.Buffer

	require.Equal(t, 0, run([]string{"validate", spec}, &stdout, &stderr),
		"stderr: %s", stderr.String())

	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	require.Len(t, entries, 1)
	assert.Equal(t, "spec.yaml", entries[0].Name(), "validate must leave nothing behind")
}

func TestRun_ValidateUsageErrors(t *testing.T) {
	t.Parallel()

	spec := writeFile(t, "spec.yaml", testspec.Tiny)
	tests := []struct {
		name   string
		args   []string
		reason string
	}{
		{"no spec file", []string{"validate"}, "validate requires exactly one spec file"},
		{"two spec files", []string{"validate", spec, spec},
			"validate requires exactly one spec file"},
		{"bad fail-on", []string{"validate", spec, "--fail-on", "hint"},
			`invalid --fail-on "hint"`},
		{"unknown flag", []string{"validate", spec, "--bogus"},
			"flag provided but not defined: -bogus"},
		{"compile's -o is not validate's", []string{"validate", spec, "-o", "ir.json"},
			"flag provided but not defined: -o"},
		{"compile's --explain is not validate's", []string{"validate", spec, "--explain", "/"},
			"flag provided but not defined: -explain"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var stdout, stderr bytes.Buffer

			assert.Equal(t, 2, run(tt.args, &stdout, &stderr))
			assert.Empty(t, stdout.String(), "usage errors must never write to stdout")
			assert.Contains(t, stderr.String(), tt.reason)
			assert.Contains(t, stderr.String(), "morphic validate <spec-file> [flags]",
				"a misuse of validate must point at validate's own usage")
		})
	}
}

func TestRun_ValidateEngineFailures(t *testing.T) {
	t.Parallel()

	// The two rows are different classes and exit differently. A format nothing
	// recognizes is a problem with the spec, so it is a diagnostic and exits 1; a
	// file that cannot be read is a problem with the invocation, so it stays a Go
	// error and exits 2. Nothing about a spec's own contents reaches 2.
	tests := []struct {
		name     string
		spec     string
		wantCode int
		wantErr  string
	}{
		{"unrecognized spec format", "hello: world\n", 1, "engine/unrecognized-format"},
		{"unreadable spec file", "", 2, "morphic:"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			path := filepath.Join(t.TempDir(), "spec.yaml")
			if tt.spec != "" {
				require.NoError(t, os.WriteFile(path, []byte(tt.spec), 0o644))
			}
			var stdout, stderr bytes.Buffer

			assert.Equal(t, tt.wantCode, run([]string{"validate", path}, &stdout, &stderr))
			assert.Empty(t, stdout.String(), "validate must write nothing to stdout")
			assert.Contains(t, stderr.String(), tt.wantErr)
		})
	}
}

// TestRun_ValidateNilDocumentReturnsOne covers the branch a spec cannot reach:
// a compiler that reports an error and lowers nothing at all. validate returns
// 1 rather than compile's 2, because there is no document to fail to write.
func TestRun_ValidateNilDocumentReturnsOne(t *testing.T) {
	orig := newEngine
	t.Cleanup(func() { newEngine = orig })
	newEngine = func() (*engine.Engine, error) { return engine.NewWith(nilDocCompiler{}) }

	spec := writeFile(t, "spec.yaml", testspec.Tiny)
	var stdout, stderr bytes.Buffer

	assert.Equal(t, 1, run([]string{"validate", spec}, &stdout, &stderr))
	assert.Empty(t, stdout.String(), "validate must write nothing to stdout")
	assert.Contains(t, stderr.String(), "openapi/unsupported-version")
}
