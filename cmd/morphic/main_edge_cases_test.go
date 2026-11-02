// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package main

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/engine"
	"github.com/dexpace/morphic/internal/testspec"
	"github.com/dexpace/morphic/ir"
)

// failWriter is an io.Writer whose Write always fails, to drive write-error
// branches without relying on a full disk.
type failWriter struct{ err error }

func (f failWriter) Write([]byte) (int, error) { return 0, f.err }

// nilDocCompiler claims openapi 3.1 and lowers to a nil Document with no error,
// modelling a compiler that refuses to lower (e.g. an unsupported construct)
// so runPipeline's Document==nil branch is exercised.
type nilDocCompiler struct{}

func (nilDocCompiler) Formats() []compilers.SourceFormat {
	return []compilers.SourceFormat{{Name: "openapi", Version: "3.1"}}
}

func (nilDocCompiler) Detect(context.Context, compilers.Source, compilers.Options) (compilers.Recognition, []ir.Diagnostic, bool) {
	return compilers.Recognition{Format: compilers.SourceFormat{Name: "openapi", Version: "3.1"}}, nil, true
}

func (nilDocCompiler) DecodeOptions(compilers.OptionSet) (any, error) { return nil, nil }

func (nilDocCompiler) SourceLimit(compilers.Options) int { return 0 }

func (nilDocCompiler) SourceTable(sources []compilers.Source, _ compilers.Options) []ir.SourceInfo {
	table := make([]ir.SourceInfo, 0, len(sources))
	for _, src := range sources {
		table = append(table, ir.SourceInfo{Path: src.Path})
	}
	return table
}

func (nilDocCompiler) Compile(context.Context, []compilers.Source, compilers.Options) (*ir.Document, []ir.Diagnostic, error) {
	return nil, []ir.Diagnostic{{
		Severity: ir.SeverityError,
		Code:     "openapi/unsupported-version",
		Message:  "refused",
	}}, nil
}

// panickingCompiler panics lowering every spec.
type panickingCompiler struct{ nilDocCompiler }

func (panickingCompiler) Compile(context.Context, []compilers.Source, compilers.Options) (*ir.Document, []ir.Diagnostic, error) {
	panic("lowering exploded")
}

// panickingPass panics in Run.
type panickingPass struct{}

func (panickingPass) Name() string { return "exploder" }

func (panickingPass) Run(context.Context, *ir.Document) (*ir.Document, []ir.Diagnostic) {
	panic("pass exploded")
}

// TestRun_AStagePanicIsAFailureNotACrash pins what the user sees of a compiler
// or a pass that panics: exit 2, nothing on stdout, and one line saying which
// stage broke its contract and where, with neither a stack trace nor the hint a
// misuse of the command gets.
func TestRun_AStagePanicIsAFailureNotACrash(t *testing.T) {
	spec := writeFile(t, "spec.yaml", testspec.Tiny)
	tests := []struct {
		name string
		opts []engine.Option
		want string
	}{
		{"a compiler", []engine.Option{engine.WithCompilers(panickingCompiler{})}, "Compile: panic: lowering exploded"},
		{"a pass", []engine.Option{engine.WithCompilers(brokenIRCompiler{count: 1}), engine.WithPasses(panickingPass{})},
			`run pass "exploder": Run: panic: pass exploded`},
	}
	for _, tt := range tests {
		for _, command := range []string{"compile", "validate"} {
			t.Run(tt.name+"/"+command, func(t *testing.T) {
				useEngine(t, tt.opts...)
				var stdout, stderr bytes.Buffer

				code := run(t.Context(), []string{command, spec}, &stdout, &stderr)

				assert.Equal(t, exitFailure, code)
				assert.Empty(t, stdout.String())
				lines := strings.Split(strings.TrimRight(stderr.String(), "\n"), "\n")
				require.Len(t, lines, 1, "one line, no trace: %q", stderr.String())
				assert.Contains(t, lines[0], "morphic: engine:")
				assert.Contains(t, lines[0], tt.want)
				assert.Contains(t, lines[0], "broke its contract")
				assert.NotContains(t, stderr.String(), "Try ", "a broken stage is not a misuse")
			})
		}
	}
}

func TestMain_ExitCode(t *testing.T) {
	origArgs, origExit := os.Args, osExit
	t.Cleanup(func() { os.Args, osExit = origArgs, origExit })

	var got int
	osExit = func(code int) { got = code }
	os.Args = []string{"morphic", "bogus"} // unknown command → usage → exit 2

	main()

	assert.Equal(t, 2, got)
}

func TestRun_CompileEngineConstructFails(t *testing.T) {
	orig := newEngine
	t.Cleanup(func() { newEngine = orig })
	newEngine = func(...engine.Option) (*engine.Engine, error) { return nil, errors.New("boom") }

	spec := writeFile(t, "spec.yaml", testspec.Tiny)
	var stdout, stderr bytes.Buffer

	code := run(t.Context(), []string{"compile", spec}, &stdout, &stderr)

	assert.Equal(t, 2, code)
	assert.Contains(t, stderr.String(), "morphic: boom")
}

func TestRun_CompileNilDocumentReturnsOne(t *testing.T) {
	orig := newEngine
	t.Cleanup(func() { newEngine = orig })
	newEngine = func(...engine.Option) (*engine.Engine, error) {
		return engine.New(engine.WithCompilers(nilDocCompiler{}))
	}

	spec := writeFile(t, "spec.yaml", testspec.Tiny)
	var stdout, stderr bytes.Buffer

	code := run(t.Context(), []string{"compile", spec}, &stdout, &stderr)

	assert.Equal(t, 1, code)
	assert.Empty(t, stdout.String(), "no IR JSON should be written for a nil document")
	assert.Equal(t, "error openapi/unsupported-version "+spec+": refused\n", stderr.String(),
		"nilDocCompiler's SourceTable names the spec even without a Document")
}

func TestRun_CompileSkipValidateToStdout(t *testing.T) {
	t.Parallel()
	spec := writeFile(t, "spec.yaml", testspec.Tiny)
	var stdout, stderr bytes.Buffer

	code := run(t.Context(), []string{"compile", spec, "--skip-validate"}, &stdout, &stderr)

	require.Equal(t, 0, code, "stderr: %s", stderr.String())
	var doc ir.Document
	require.NoError(t, json.Unmarshal(stdout.Bytes(), &doc))
	assert.Equal(t, "Tiny", doc.Name)
}

func TestRun_CompileMissingSpecFile(t *testing.T) {
	t.Parallel()
	var stdout, stderr bytes.Buffer

	code := run(t.Context(), []string{"compile", filepath.Join(t.TempDir(), "nope.yaml")}, &stdout, &stderr)

	assert.Equal(t, 2, code)
	assert.Contains(t, stderr.String(), "morphic:")
}

func TestRun_CompileOutputCreateError(t *testing.T) {
	t.Parallel()
	spec := writeFile(t, "spec.yaml", testspec.Tiny)
	// A path whose parent directory does not exist makes os.Create fail.
	badOut := filepath.Join(t.TempDir(), "missing-dir", "ir.json")
	var stdout, stderr bytes.Buffer

	code := run(t.Context(), []string{"compile", spec, "-o", badOut}, &stdout, &stderr)

	assert.Equal(t, 2, code)
	assert.Contains(t, stderr.String(), "create output")
}

// unresolvedRefSpec is an OpenAPI 3.1 document whose only $ref names a schema
// that is not there. It matters that it still lowers to a document: a spec that
// lowers to nil returns 1 before the -o write is ever attempted, so it could not
// exercise the combination writeFailureExit decides.
const unresolvedRefSpec = `openapi: 3.1.0
info: {title: Bad, version: "1"}
paths:
  /x:
    get:
      responses:
        "200":
          description: ok
          content:
            application/json:
              schema: {$ref: "#/components/schemas/Missing"}
`

// missingParentDest returns a destination inside a directory that is not there,
// so creating the temp file beside it fails.
func missingParentDest(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "missing-dir", "ir.json")
}

// readOnlyDirDest returns a writable file inside a read-only directory — the
// shape atomicfile.TestReplace_ReadOnlyDirFails pins, where the destination
// itself could be rewritten but its directory will not take the temp file.
func readOnlyDirDest(t *testing.T) string {
	t.Helper()
	if os.Geteuid() == 0 {
		t.Skip("root bypasses directory permission checks, so the write would succeed")
	}
	dir := t.TempDir()
	out := filepath.Join(dir, "ir.json")
	require.NoError(t, os.WriteFile(out, []byte("PREVIOUS\n"), 0o644))
	require.NoError(t, os.Chmod(dir, 0o555))
	// Restore write permission so t.TempDir's own cleanup can remove the tree.
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })
	return out
}

// TestRun_CompileWriteFailureDoesNotMaskDiagnostics pins that a destination which
// refuses the write does not decide the exit code. A spec that reached the
// --fail-on threshold exits 1 wherever -o pointed; only a run with nothing to
// report at the threshold exits 2 for the write. Both destinations here are
// refused for the same reason — output.ReplaceDocument publishes by rename and
// their directories will not take a temp file — which is also why -o /dev/null fails,
// untestable here because a root /dev would take the temp file and the rename
// would then replace /dev/null itself.
func TestRun_CompileWriteFailureDoesNotMaskDiagnostics(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		spec     string
		dest     func(t *testing.T) string
		wantCode int
		// wantDiag is a diagnostic code stderr must carry, or "" when the spec is
		// clean and stderr must carry no diagnostic at all.
		wantDiag string
	}{
		{"threshold reached, missing parent dir", unresolvedRefSpec, missingParentDest, 1, "openapi/unresolved-ref"},
		{"threshold reached, read-only dir", unresolvedRefSpec, readOnlyDirDest, 1, "openapi/unresolved-ref"},
		{"nothing reported, missing parent dir", testspec.Tiny, missingParentDest, 2, ""},
		{"nothing reported, read-only dir", testspec.Tiny, readOnlyDirDest, 2, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			spec := writeFile(t, "spec.yaml", tt.spec)
			var stdout, stderr bytes.Buffer

			code := run(t.Context(), []string{"compile", spec, "-o", tt.dest(t)}, &stdout, &stderr)

			assert.Equal(t, tt.wantCode, code, "stderr: %s", stderr.String())
			assert.Contains(t, stderr.String(), "create output",
				"the destination must really refuse the write, or this case proves nothing")
			if tt.wantDiag == "" {
				assert.NotContains(t, stderr.String(), "openapi/",
					"the clean spec must reach the write with no diagnostic behind it")
				return
			}
			assert.Contains(t, stderr.String(), tt.wantDiag,
				"a failed write must not swallow the diagnostics it was reported alongside")
		})
	}
}

// danglingDocCompiler lowers to a Document with a dangling type ref and no
// compiler diagnostics, so the only thing that can report it is the validate
// pass.
type danglingDocCompiler struct{ nilDocCompiler }

func (danglingDocCompiler) Compile(_ context.Context, sources []compilers.Source, _ compilers.Options) (*ir.Document, []ir.Diagnostic, error) {
	return &ir.Document{
		IRVersion: ir.IRVersion,
		Name:      "Dangling",
		Types:     ir.TypeRegistry{},
		// The table the compiler contract holds a document to: its one source,
		// read as the format detection recognized and hashed from its bytes.
		Sources: []ir.SourceInfo{{Format: "openapi@3.1", Path: sources[0].Path, Hash: ir.SourceHash(sources[0].Data)}},
		Services: []ir.Service{{
			ID: "s/x",
			Groups: []ir.OperationGroup{{
				Operations: []ir.Operation{{
					ID: "op/x",
					Errors: []ir.ErrorCase{{
						Payload: &ir.Payload{Contents: []ir.Content{{Type: ir.TypeRef{Target: "t/missing"}}}},
					}},
				}},
			}},
		}},
	}, nil, nil
}

// TestRun_PassFlagsSilenceTheValidatePass pins that --skip-validate and
// --disable-pass validate each turn the pass off end to end, on both commands:
// the document's dangling ref fails the run by default and goes unreported
// under either flag.
func TestRun_PassFlagsSilenceTheValidatePass(t *testing.T) {
	orig := newEngine
	t.Cleanup(func() { newEngine = orig })
	newEngine = func(...engine.Option) (*engine.Engine, error) {
		return engine.New(engine.WithCompilers(danglingDocCompiler{}))
	}
	spec := writeFile(t, "spec.yaml", testspec.Tiny)

	commands := [][]string{
		{"compile", spec, "-o", filepath.Join(t.TempDir(), "ir.json")},
		{"validate", spec},
	}
	tests := []struct {
		flags []string
		want  int
	}{
		{nil, exitFindings},
		{[]string{"--skip-validate"}, 0},
		{[]string{"--disable-pass", "validate"}, 0},
	}
	for _, command := range commands {
		for _, tt := range tests {
			var stdout, stderr bytes.Buffer
			args := slices.Concat(command, tt.flags)

			code := run(t.Context(), args, &stdout, &stderr)

			assert.Equal(t, tt.want, code, "%v: stderr %q", args, stderr.String())
			assert.Equal(t, tt.want == exitFindings, strings.Contains(stderr.String(), "ir/dangling-type-ref"),
				"%v: stderr %q", args, stderr.String())
		}
	}
}

// firstWriteFails refuses its first write and keeps every write after it, so
// a test can fail the diagnostics and still read what the run reported about
// that.
type firstWriteFails struct {
	refused bool
	text    bytes.Buffer
}

func (w *firstWriteFails) Write(p []byte) (int, error) {
	if !w.refused {
		w.refused = true
		return 0, errors.New("pipe closed")
	}
	return w.text.Write(p)
}

// TestRun_DiagnosticsWriteFailureEndsTheRun pins that a stderr refusing the
// diagnostics fails the run as a refused document does, on both commands: exit
// 2 when the verdict earned 0, the verdict's own code otherwise, and the reason
// reported after it. compile then writes no document, so nothing is published
// beside findings nobody saw. A spec with no findings never asks the stream.
func TestRun_DiagnosticsWriteFailureEndsTheRun(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		spec string
		want int
	}{
		{"a finding below the threshold", warnSpec, exitFailure},
		{"a finding at the threshold", errSpec, exitFindings},
		{"no findings", testspec.Tiny, 0},
	}
	for _, command := range []string{"compile", "validate"} {
		for _, tt := range tests {
			t.Run(command+"/"+tt.name, func(t *testing.T) {
				t.Parallel()
				spec := writeFile(t, "spec.yaml", tt.spec)
				var stdout bytes.Buffer
				stderr := &firstWriteFails{}

				code := run(t.Context(), []string{command, spec}, &stdout, stderr)

				assert.Equal(t, tt.want, code, "stderr: %s", stderr.text.String())
				if tt.want == 0 {
					assert.False(t, stderr.refused, "a run with no findings writes no diagnostic")
					assert.Equal(t, command == "compile", stdout.Len() > 0, "compile writes its document as usual")
					return
				}
				assert.Equal(t, "morphic: write diagnostics: pipe closed\n", stderr.text.String())
				assert.Empty(t, stdout.String(), "no document follows findings that were not reported")
			})
		}
	}
}
