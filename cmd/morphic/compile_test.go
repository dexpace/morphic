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

	"github.com/dexpace/morphic/ir"
)

const tinySpec = `openapi: 3.1.0
info: {title: Tiny, version: "1"}
paths:
  /ping:
    get:
      operationId: ping
      responses: {"200": {description: ok}}
`

func writeFile(t *testing.T, name, contents string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	require.NoError(t, os.WriteFile(path, []byte(contents), 0o644))
	return path
}

func TestRun_ParseWritesIRToFile(t *testing.T) {
	t.Parallel()
	spec := writeFile(t, "spec.yaml", tinySpec)
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
	var stdout, stderr bytes.Buffer
	assert.Equal(t, 2, run(nil, &stdout, &stderr))
	assert.Equal(t, 2, run([]string{"bogus"}, &stdout, &stderr))
	assert.Equal(t, 2, run([]string{"compile", "x.yaml", "--fail-on", "hint"}, &stdout, &stderr))
	assert.True(t, strings.Contains(stderr.String(), "usage"))
}
