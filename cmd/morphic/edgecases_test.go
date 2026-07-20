package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/engine"
	"github.com/dexpace/morphic/ir"
)

// failWriter is an io.Writer whose Write always fails, to drive write-error
// branches without relying on a full disk.
type failWriter struct{ err error }

func (f failWriter) Write([]byte) (int, error) { return 0, f.err }

// closeFailWriteCloser accepts writes but fails Close, to drive writeCompiled's
// f.Close error branch.
type closeFailWriteCloser struct {
	buf      bytes.Buffer
	closeErr error
}

func (c *closeFailWriteCloser) Write(p []byte) (int, error) { return c.buf.Write(p) }
func (c *closeFailWriteCloser) Close() error                { return c.closeErr }

// nilDocCompiler claims openapi 3.1 and lowers to a nil Document with no error,
// modelling a compiler that refuses to lower (e.g. an unsupported construct)
// so runCompile's Document==nil branch is exercised.
type nilDocCompiler struct{}

func (nilDocCompiler) Formats() []compilers.SourceFormat {
	return []compilers.SourceFormat{{Name: "openapi", Version: "3.1"}}
}

func (nilDocCompiler) Compile(context.Context, []compilers.Source, compilers.Options) (*ir.Document, []ir.Diagnostic, error) {
	return nil, []ir.Diagnostic{{
		Severity: ir.SeverityError,
		Code:     "openapi/unsupported-version",
		Message:  "refused",
	}}, nil
}

// badDoc returns an ir.Document that cannot be marshalled to JSON: its
// Extensions hold an invalid json.RawMessage, whose MarshalJSON rejects the
// malformed bytes.
func badDoc() *ir.Document {
	return &ir.Document{
		Name:       "Bad",
		Extensions: ir.Extensions{"openapi:x": ir.RawValue("{invalid")},
	}
}

func TestMain_ExitCode(t *testing.T) {
	origArgs, origExit := os.Args, osExit
	t.Cleanup(func() { os.Args, osExit = origArgs, origExit })

	var got int
	osExit = func(code int) { got = code }
	os.Args = []string{"morphic"} // no subcommand → usage → exit 2

	main()

	assert.Equal(t, 2, got)
}

func TestRunParse_EngineConstructFails(t *testing.T) {
	orig := newEngine
	t.Cleanup(func() { newEngine = orig })
	newEngine = func() (*engine.Engine, error) { return nil, errors.New("boom") }

	spec := writeFile(t, "spec.yaml", tinySpec)
	var stdout, stderr bytes.Buffer

	code := run([]string{"compile", spec}, &stdout, &stderr)

	assert.Equal(t, 2, code)
	assert.Contains(t, stderr.String(), "morphic: boom")
}

func TestRunParse_NilDocumentReturnsOne(t *testing.T) {
	orig := newEngine
	t.Cleanup(func() { newEngine = orig })
	newEngine = func() (*engine.Engine, error) {
		reg := compilers.NewRegistry()
		if err := reg.Register(nilDocCompiler{}); err != nil {
			return nil, err
		}
		return engine.NewWithRegistry(reg), nil
	}

	spec := writeFile(t, "spec.yaml", tinySpec)
	var stdout, stderr bytes.Buffer

	code := run([]string{"compile", spec}, &stdout, &stderr)

	assert.Equal(t, 1, code)
	assert.Empty(t, stdout.String(), "no IR JSON should be written for a nil document")
	assert.Contains(t, stderr.String(), "openapi/unsupported-version")
}

func TestRunParse_UnknownFlagIsUsageError(t *testing.T) {
	t.Parallel()
	spec := writeFile(t, "spec.yaml", tinySpec)
	var stdout, stderr bytes.Buffer

	code := run([]string{"compile", spec, "--bogus"}, &stdout, &stderr)

	assert.Equal(t, 2, code)
	assert.Contains(t, stderr.String(), "usage")
}

func TestRunParse_WrongPositionalCount(t *testing.T) {
	t.Parallel()
	spec := writeFile(t, "spec.yaml", tinySpec)
	tests := []struct {
		name string
		args []string
	}{
		{"no spec file", []string{"compile"}},
		{"two spec files", []string{"compile", spec, spec}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var stdout, stderr bytes.Buffer
			code := run(tt.args, &stdout, &stderr)
			assert.Equal(t, 2, code)
			assert.Contains(t, stderr.String(), "requires exactly one spec file")
		})
	}
}

func TestRunParse_SkipValidateToStdout(t *testing.T) {
	t.Parallel()
	spec := writeFile(t, "spec.yaml", tinySpec)
	var stdout, stderr bytes.Buffer

	code := run([]string{"compile", spec, "--skip-validate"}, &stdout, &stderr)

	require.Equal(t, 0, code, "stderr: %s", stderr.String())
	var doc ir.Document
	require.NoError(t, json.Unmarshal(stdout.Bytes(), &doc))
	assert.Equal(t, "Tiny", doc.Name)
}

func TestRunParse_MissingSpecFile(t *testing.T) {
	t.Parallel()
	var stdout, stderr bytes.Buffer

	code := run([]string{"compile", filepath.Join(t.TempDir(), "nope.yaml")}, &stdout, &stderr)

	assert.Equal(t, 2, code)
	assert.Contains(t, stderr.String(), "morphic:")
}

func TestRunParse_OutputCreateError(t *testing.T) {
	t.Parallel()
	spec := writeFile(t, "spec.yaml", tinySpec)
	// A path whose parent directory does not exist makes os.Create fail.
	badOut := filepath.Join(t.TempDir(), "missing-dir", "ir.json")
	var stdout, stderr bytes.Buffer

	code := run([]string{"compile", spec, "-o", badOut}, &stdout, &stderr)

	assert.Equal(t, 2, code)
	assert.Contains(t, stderr.String(), "create output")
}

func TestExitCodeFor_SeverityMatrix(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		diags  []ir.Diagnostic
		failOn string
		want   int
	}{
		{"info below error", []ir.Diagnostic{{Severity: ir.SeverityInfo}}, "error", 0},
		{"info below warning", []ir.Diagnostic{{Severity: ir.SeverityInfo}}, "warning", 0},
		{"warning meets warning", []ir.Diagnostic{{Severity: ir.SeverityWarning}}, "warning", 1},
		{"warning below error", []ir.Diagnostic{{Severity: ir.SeverityWarning}}, "error", 0},
		{"error meets error", []ir.Diagnostic{{Severity: ir.SeverityError}}, "error", 1},
		{"unknown severity ranks lowest", []ir.Diagnostic{{Severity: ir.Severity("weird")}}, "error", 0},
		{"no diagnostics", nil, "error", 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, exitCodeFor(tt.diags, tt.failOn))
		})
	}
}

func TestSeverityRank_AllLevels(t *testing.T) {
	t.Parallel()
	assert.Equal(t, 3, severityRank(ir.SeverityError))
	assert.Equal(t, 2, severityRank(ir.SeverityWarning))
	assert.Equal(t, 1, severityRank(ir.SeverityInfo))
	assert.Equal(t, 0, severityRank(ir.Severity("nonsense")))
}

func TestSourcePath_Cases(t *testing.T) {
	t.Parallel()
	doc := &ir.Document{Sources: []ir.SourceInfo{{Path: "spec.yaml"}}}
	tests := []struct {
		name   string
		doc    *ir.Document
		source int
		want   string
	}{
		{"nil document", nil, 0, ""},
		{"negative index", doc, -1, ""},
		{"index past end", doc, 1, ""},
		{"valid index", doc, 0, "spec.yaml"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, sourcePath(tt.doc, tt.source))
		})
	}
}

func TestRenderDiagnostics_WithAndWithoutSourcePath(t *testing.T) {
	t.Parallel()
	res := &engine.Result{
		Document: &ir.Document{Sources: []ir.SourceInfo{{Path: "spec.yaml"}}},
		Diagnostics: []ir.Diagnostic{
			{
				Severity:   ir.SeverityError,
				Code:       "openapi/bad",
				Message:    "resolved location",
				Provenance: ir.Provenance{Source: 0, Pointer: "/paths/~1x"},
			},
			{
				Severity:   ir.SeverityWarning,
				Code:       "ir/dangling",
				Message:    "no source file",
				Provenance: ir.Provenance{Source: 99, Pointer: "type:abc"},
			},
		},
	}
	var buf bytes.Buffer

	renderDiagnostics(&buf, res)

	out := buf.String()
	assert.Contains(t, out, "error openapi/bad spec.yaml#/paths/~1x: resolved location")
	assert.Contains(t, out, "warning ir/dangling type:abc: no source file")
}

func TestWriteDocument_MarshalError(t *testing.T) {
	t.Parallel()
	err := writeDocument(io.Discard, badDoc())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "marshal ir document")
}

func TestWriteDocument_WriteError(t *testing.T) {
	t.Parallel()
	err := writeDocument(failWriter{err: errors.New("disk gone")}, &ir.Document{Name: "ok"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "write ir document")
}

func TestWriteParsed_CreateError(t *testing.T) {
	t.Parallel()
	badOut := filepath.Join(t.TempDir(), "no-such-dir", "ir.json")
	err := writeCompiled(badOut, io.Discard, &ir.Document{Name: "ok"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "create output")
}

func TestWriteParsed_WriteErrorClosesFile(t *testing.T) {
	t.Parallel()
	out := filepath.Join(t.TempDir(), "ir.json")
	// A marshal failure surfaces through writeCompiled's writeDocument branch after
	// the file is created, exercising the close-and-return path.
	err := writeCompiled(out, io.Discard, badDoc())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "marshal ir document")
}

func TestWriteParsed_CloseError(t *testing.T) {
	orig := openOutput
	t.Cleanup(func() { openOutput = orig })
	wc := &closeFailWriteCloser{closeErr: errors.New("close failed")}
	openOutput = func(string) (io.WriteCloser, error) { return wc, nil }

	err := writeCompiled("out.json", io.Discard, &ir.Document{Name: "ok"})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "close output")
	assert.NotEmpty(t, wc.buf.Bytes(), "document should have been written before close")
}

func TestWriteParsed_ToStdoutSuccess(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	require.NoError(t, writeCompiled("", &buf, &ir.Document{Name: "ok"}))
	assert.True(t, bytes.HasSuffix(buf.Bytes(), []byte("\n")))
}
