package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
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

// TestRun_SpecProblemsExitOne covers the classes of bad spec the pipeline
// rejects before a compiler ever runs. Every one of them is a problem with the
// file the user named, so every one is exit 1 with a coded diagnostic — the same
// answer a bad `$ref` inside a spec the compiler does accept already gets.
//
// Exit 2 is reserved for a misuse of the CLI and for I/O that failed, so a CI
// wrapper can tell "your spec is broken" from "you invoked morphic wrong". A
// spec the tool read and understood well enough to name the problem in is
// neither of those, and reporting it as one made the compiler's own
// openapi/unsupported-version diagnostic unreachable from the shipped binary.
func TestRun_SpecProblemsExitOne(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, contents, code string
	}{
		{"unrecognized format", "hello: world\n", "engine/unrecognized-format"},
		{"swagger 2.0", "swagger: \"2.0\"\n", "engine/no-compiler-for-format"},
		{"undecodable source", "openapi: [unterminated\n", "openapi/undecodable-source"},
		{"no compiler for version", "openapi: 4.0.0\ninfo: {title: T, version: \"1\"}\npaths: {}\n",
			"engine/no-compiler-for-format"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			spec := writeFile(t, "spec.yaml", tt.contents)
			var stdout, stderr bytes.Buffer

			code := run([]string{"compile", spec}, &stdout, &stderr)

			assert.Equal(t, 1, code, "stderr: %s", stderr.String())
			assert.Contains(t, stderr.String(), tt.code)
			assert.Empty(t, stdout.String(), "no IR JSON for a spec that produced no document")
			assert.NotContains(t, stderr.String(), "usage:",
				"a bad spec is not a misuse of the CLI, so no usage block")
		})
	}
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
		// A fourth class: the pair is well-formed and the name is known, and the
		// value is still unusable. Read as "no overlay" it would exit 0 having
		// applied none, which is the one outcome --opt exists to rule out.
		{"empty value", "overlay=", `"overlay": want a file path`},
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

// unsortedSpec declares its schemas out of alphabetical order and its paths in
// an order of their own, so a document compiled from it exercises both halves
// of the determinism invariant: maps emitted in sorted-key order and slices in
// source order.
const unsortedSpec = `openapi: 3.1.0
info: {title: Ordered, version: "1"}
paths:
  /zebra:
    get: {operationId: getZebra, responses: {"200": {description: ok}}}
  /apple:
    get: {operationId: getApple, responses: {"200": {description: ok}}}
components:
  schemas:
    Zeta: {type: object, properties: {b: {type: string}, a: {type: integer}}}
    Alpha: {type: string}
    Mid: {type: array, items: {type: string}}
`

// compileToFile runs compile over spec with the given extra flags and returns
// the bytes -o wrote.
func compileToFile(t *testing.T, spec string, flags ...string) []byte {
	t.Helper()
	out := filepath.Join(t.TempDir(), "ir.json")
	args := append([]string{"compile", spec, "-o", out}, flags...)
	var stdout, stderr bytes.Buffer

	require.Equal(t, 0, run(args, &stdout, &stderr), "stderr: %s", stderr.String())

	raw, err := os.ReadFile(out)
	require.NoError(t, err)
	return raw
}

// TestRun_FileOutputIsCompact pins the artifact format: -o writes compact JSON,
// which is one line and a trailing newline, since a raw newline cannot appear
// inside a JSON string.
func TestRun_FileOutputIsCompact(t *testing.T) {
	t.Parallel()
	spec := writeFile(t, "spec.yaml", testspec.Tiny)

	raw := compileToFile(t, spec)

	assert.Equal(t, 1, bytes.Count(raw, []byte("\n")),
		"compact JSON is one line plus its trailing newline, got:\n%s", raw)
	var doc ir.Document
	require.NoError(t, json.Unmarshal(raw, &doc))
	assert.Equal(t, "Tiny", doc.Name)
}

// TestRun_PrettyRestoresIndentedFile pins the escape hatch, and pins it against
// the bytes that were there before rather than against "looks indented": -o
// -pretty must write exactly what stdout writes, which is the format -o itself
// used to write.
func TestRun_PrettyRestoresIndentedFile(t *testing.T) {
	t.Parallel()
	spec := writeFile(t, "spec.yaml", testspec.Tiny)

	raw := compileToFile(t, spec, "-pretty")

	var stdout, stderr bytes.Buffer
	require.Equal(t, 0, run([]string{"compile", spec}, &stdout, &stderr), "stderr: %s", stderr.String())
	assert.Empty(t, cmp.Diff(stdout.String(), string(raw)),
		"-pretty must write the bytes stdout gets")
}

// TestRun_CompactOutputKeepsDocumentOrder is the determinism check the format
// change has to survive: compacting the indented artifact must reproduce the
// compact one byte for byte. Whitespace removal cannot reorder anything, so the
// two agreeing means the encoder emitted the same map keys in the same sorted
// order and the same slices in the same source order as the marshaller it
// replaced. Compiling twice pins that a second run agrees with the first.
func TestRun_CompactOutputKeepsDocumentOrder(t *testing.T) {
	t.Parallel()
	spec := writeFile(t, "spec.yaml", unsortedSpec)

	compact := compileToFile(t, spec)
	pretty := compileToFile(t, spec, "-pretty")

	var flattened bytes.Buffer
	require.NoError(t, json.Compact(&flattened, pretty))
	assert.Empty(t, cmp.Diff(flattened.String()+"\n", string(compact)),
		"compacting the indented artifact must reproduce the compact one exactly")

	assert.Empty(t, cmp.Diff(string(compact), string(compileToFile(t, spec))),
		"two runs over one spec must write the same bytes")

	// The assertion above is only worth something if the spec really does force
	// a reordering, so confirm the emitted key order is not the declared one.
	assert.Less(t, bytes.Index(compact, []byte("Alpha")), bytes.Index(compact, []byte("Zeta")),
		"schemas must be emitted in sorted-key order, not declaration order")
}
