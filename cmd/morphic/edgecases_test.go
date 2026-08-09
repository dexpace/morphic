package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
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

// closeFailWriteCloser accepts writes but fails Close, to drive writeCompiled's
// f.Close error branch.
type closeFailWriteCloser struct {
	buf      bytes.Buffer
	closeErr error
}

func (c *closeFailWriteCloser) Write(p []byte) (int, error) { return c.buf.Write(p) }
func (c *closeFailWriteCloser) Sync() error                 { return nil }
func (c *closeFailWriteCloser) Close() error                { return c.closeErr }

// writeFailWriteCloser opens successfully but fails every Write, to drive
// writeCompiled's write-then-close-then-return error branch (e.g. the disk
// filling up mid-write, after outPath was already truncated by the open).
type writeFailWriteCloser struct {
	writeErr error
	closed   bool
}

func (w *writeFailWriteCloser) Write([]byte) (int, error) { return 0, w.writeErr }
func (w *writeFailWriteCloser) Sync() error               { return nil }

func (w *writeFailWriteCloser) Close() error {
	w.closed = true
	return nil
}

// syncFailWriteCloser accepts writes but fails Sync, modelling a filesystem that
// takes the bytes into cache and then cannot flush them (a full or failing
// device), to drive fillTemp's sync branch.
type syncFailWriteCloser struct {
	buf     bytes.Buffer
	syncErr error
	closed  bool
}

func (s *syncFailWriteCloser) Write(p []byte) (int, error) { return s.buf.Write(p) }
func (s *syncFailWriteCloser) Sync() error                 { return s.syncErr }

func (s *syncFailWriteCloser) Close() error {
	s.closed = true
	return nil
}

// writeFailFile is a real file that has already been created (and so truncated)
// but fails every Write, modelling the disk filling up after the destination was
// opened — the exact sequence that used to destroy the previous output.
type writeFailFile struct {
	*os.File
	writeErr error
}

func (w *writeFailFile) Write([]byte) (int, error) { return 0, w.writeErr }

// nilDocCompiler claims openapi 3.1 and lowers to a nil Document with no error,
// modelling a compiler that refuses to lower (e.g. an unsupported construct)
// so runCompile's Document==nil branch is exercised.
type nilDocCompiler struct{}

func (nilDocCompiler) Formats() []compilers.SourceFormat {
	return []compilers.SourceFormat{{Name: "openapi", Version: "3.1"}}
}

func (nilDocCompiler) Detect(compilers.Source) (compilers.SourceFormat, bool) {
	return compilers.SourceFormat{Name: "openapi", Version: "3.1"}, true
}

func (nilDocCompiler) DecodeOptions(compilers.OptionSet) (any, error) { return nil, nil }

func (nilDocCompiler) Compile(context.Context, []compilers.Source, compilers.Options) (*ir.Document, []ir.Diagnostic, error) {
	return nil, []ir.Diagnostic{{
		Severity: ir.SeverityError,
		Code:     "openapi/unsupported-version",
		Message:  "refused",
	}}, nil
}

// badDoc returns an ir.Document that cannot be marshalled to JSON: its
// Unmodeled holds an invalid json.RawMessage, whose malformed bytes
// json.Marshal's encoder rejects while compacting them.
func badDoc() *ir.Document {
	return &ir.Document{
		Name:      "Bad",
		Unmodeled: ir.Unmodeled{"openapi:x": {Value: ir.RawValue("{invalid")}},
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

func TestRunParse_EngineConstructFails(t *testing.T) {
	orig := newEngine
	t.Cleanup(func() { newEngine = orig })
	newEngine = func() (*engine.Engine, error) { return nil, errors.New("boom") }

	spec := writeFile(t, "spec.yaml", testspec.Tiny)
	var stdout, stderr bytes.Buffer

	code := run([]string{"compile", spec}, &stdout, &stderr)

	assert.Equal(t, 2, code)
	assert.Contains(t, stderr.String(), "morphic: boom")
}

func TestRunParse_NilDocumentReturnsOne(t *testing.T) {
	orig := newEngine
	t.Cleanup(func() { newEngine = orig })
	newEngine = func() (*engine.Engine, error) {
		return engine.NewWith(nilDocCompiler{})
	}

	spec := writeFile(t, "spec.yaml", testspec.Tiny)
	var stdout, stderr bytes.Buffer

	code := run([]string{"compile", spec}, &stdout, &stderr)

	assert.Equal(t, 1, code)
	assert.Empty(t, stdout.String(), "no IR JSON should be written for a nil document")
	assert.Contains(t, stderr.String(), "openapi/unsupported-version")
}

func TestRunParse_SkipValidateToStdout(t *testing.T) {
	t.Parallel()
	spec := writeFile(t, "spec.yaml", testspec.Tiny)
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
	spec := writeFile(t, "spec.yaml", testspec.Tiny)
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

// swapCreateOutput installs fn as the temp-file creator for one test.
func swapCreateOutput(t *testing.T, fn func(string, os.FileMode) (outputFile, error)) {
	t.Helper()
	orig := createOutput
	t.Cleanup(func() { createOutput = orig })
	createOutput = fn
}

func TestWriteParsed_WriteErrorClosesFile(t *testing.T) {
	wc := &writeFailWriteCloser{writeErr: errors.New("disk gone")}
	swapCreateOutput(t, func(string, os.FileMode) (outputFile, error) { return wc, nil })

	// doc marshals fine; the write to the temp file is what fails, exercising
	// the close-and-return path.
	err := writeCompiled(filepath.Join(t.TempDir(), "out.json"), io.Discard, &ir.Document{Name: "ok"})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "write ir document")
	assert.True(t, wc.closed, "file must still be closed after a write error")
}

// TestWriteParsed_WriteErrorPreservesDestination is the regression guard for a
// failed run destroying the previous output: the bytes must go to a temp file, so
// a mid-write failure leaves the existing destination byte-for-byte intact.
func TestWriteParsed_WriteErrorPreservesDestination(t *testing.T) {
	out := filepath.Join(t.TempDir(), "ir.json")
	const existing = `{"name":"Existing"}` + "\n"
	require.NoError(t, os.WriteFile(out, []byte(existing), 0o644))

	// The creator truncates whatever path it is handed, exactly as the os.Create
	// this fix replaced did, and then fails the write. So if the code under test
	// ever opens the destination directly, the old content is really gone and the
	// assertion below really fires — the test cannot pass by using a fake that
	// never touches the disk.
	var opened string
	swapCreateOutput(t, func(path string, perm os.FileMode) (outputFile, error) {
		opened = path
		f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, perm)
		if err != nil {
			return nil, err
		}
		return &writeFailFile{File: f, writeErr: errors.New("disk full")}, nil
	})

	err := writeCompiled(out, io.Discard, &ir.Document{Name: "ok"})

	require.Error(t, err)
	assert.NotEqual(t, out, opened, "the write must go through a temp file, never the destination")
	got, readErr := os.ReadFile(out)
	require.NoError(t, readErr)
	assert.Equal(t, existing, string(got), "a failed write must not disturb the previous output")
}

func TestWriteParsed_MarshalErrorLeavesFileUntouched(t *testing.T) {
	t.Parallel()
	out := filepath.Join(t.TempDir(), "ir.json")
	const existing = `{"name":"Existing"}` + "\n"
	require.NoError(t, os.WriteFile(out, []byte(existing), 0o644))

	err := writeCompiled(out, io.Discard, badDoc())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "marshal ir document")
	got, readErr := os.ReadFile(out)
	require.NoError(t, readErr)
	assert.Equal(t, existing, string(got),
		"a failed marshal must not truncate a file that already exists at outPath")
}

func TestWriteParsed_CloseError(t *testing.T) {
	wc := &closeFailWriteCloser{closeErr: errors.New("close failed")}
	swapCreateOutput(t, func(string, os.FileMode) (outputFile, error) { return wc, nil })

	err := writeCompiled(filepath.Join(t.TempDir(), "out.json"), io.Discard, &ir.Document{Name: "ok"})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "close output")
	assert.NotEmpty(t, wc.buf.Bytes(), "document should have been written before close")
}

// TestWriteParsed_ReplacesAtomically pins the success path end to end: the
// destination gains the new bytes, keeps the mode it already had, and no temp
// file is left beside it.
func TestWriteParsed_ReplacesAtomically(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	out := filepath.Join(dir, "ir.json")
	require.NoError(t, os.WriteFile(out, []byte("stale\n"), 0o640))

	require.NoError(t, writeCompiled(out, io.Discard, &ir.Document{Name: "Fresh"}))

	got, err := os.ReadFile(out)
	require.NoError(t, err)
	assert.Contains(t, string(got), `"Fresh"`)

	info, err := os.Stat(out)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o640), info.Mode().Perm(),
		"replacing a file must preserve the mode it already carried")

	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	assert.Len(t, entries, 1, "no temp file may survive a successful write")
}

// The three tests below pin the behaviours publishing by rename gives up, all
// documented on replaceFile. None is a bug: each is a way to reach the
// destination's bytes other than through its own name, and honouring any of them
// means writing in place, which is the truncation replaceFile exists to remove.
// They are pinned so that changing one is a change to a stated contract rather
// than silent drift.

// TestWriteParsed_ReadOnlyDirFails pins that writing needs permission on the
// destination's directory, not merely on the destination. Truncating an existing
// writable file in a read-only directory used to succeed.
func TestWriteParsed_ReadOnlyDirFails(t *testing.T) {
	t.Parallel()
	if os.Geteuid() == 0 {
		t.Skip("root bypasses directory permission checks, so the write would succeed")
	}
	dir := t.TempDir()
	out := filepath.Join(dir, "ir.json")
	require.NoError(t, os.WriteFile(out, []byte("PREVIOUS\n"), 0o644))
	require.NoError(t, os.Chmod(dir, 0o555))
	// Restore write permission so t.TempDir's own cleanup can remove the tree.
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })

	err := writeCompiled(out, io.Discard, &ir.Document{Name: "Fresh"})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "create output")
	got, readErr := os.ReadFile(out)
	require.NoError(t, readErr)
	assert.Equal(t, "PREVIOUS\n", string(got),
		"a destination that cannot be replaced must keep its previous content")
}

// TestWriteParsed_SymlinkIsReplaced pins that a symlink destination is replaced
// by a regular file rather than followed and written through, so its target
// keeps its old content.
func TestWriteParsed_SymlinkIsReplaced(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	target := filepath.Join(dir, "target.json")
	link := filepath.Join(dir, "link.json")
	require.NoError(t, os.WriteFile(target, []byte("PREVIOUS\n"), 0o644))
	require.NoError(t, os.Symlink(target, link))

	require.NoError(t, writeCompiled(link, io.Discard, &ir.Document{Name: "Fresh"}))

	info, err := os.Lstat(link)
	require.NoError(t, err)
	assert.Zero(t, info.Mode()&os.ModeSymlink, "the symlink must be replaced, not followed")

	got, err := os.ReadFile(link)
	require.NoError(t, err)
	assert.Contains(t, string(got), `"Fresh"`)

	behind, err := os.ReadFile(target)
	require.NoError(t, err)
	assert.Equal(t, "PREVIOUS\n", string(behind),
		"the symlink's former target must be left untouched")
}

// TestWriteParsed_HardLinkIsBroken pins that other hard links to the destination
// keep pointing at the old inode, and so keep the old content, instead of
// observing the new bytes.
func TestWriteParsed_HardLinkIsBroken(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	out := filepath.Join(dir, "a.json")
	other := filepath.Join(dir, "b.json")
	require.NoError(t, os.WriteFile(out, []byte("PREVIOUS\n"), 0o644))
	require.NoError(t, os.Link(out, other))

	require.NoError(t, writeCompiled(out, io.Discard, &ir.Document{Name: "Fresh"}))

	got, err := os.ReadFile(out)
	require.NoError(t, err)
	assert.Contains(t, string(got), `"Fresh"`)

	kept, err := os.ReadFile(other)
	require.NoError(t, err)
	assert.Equal(t, "PREVIOUS\n", string(kept),
		"the other hard link must keep the old inode's content")
}

// TestWriteParsed_NearLimitNameFails pins the one limitation that is a new
// failure rather than a lost capability: the temp name is 21 characters longer
// than the destination's own, so a basename within 21 of the filesystem's limit
// cannot be written at all. A truncating write to the same path succeeded, which
// the first half of this test establishes before the second half shows the
// destination one character too long failing.
func TestWriteParsed_NearLimitNameFails(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	// nameMax is the POSIX floor every filesystem Morphic targets meets or
	// exceeds; probing the real limit would make the test depend on the
	// filesystem under /tmp.
	const nameMax = 255
	const tempOverhead = 21

	writable := filepath.Join(dir, strings.Repeat("a", nameMax-tempOverhead)+".json")
	require.NoError(t, os.WriteFile(writable, []byte("x"), 0o644),
		"precondition: the filesystem must accept a name this long at all")
	require.NoError(t, os.Remove(writable))

	tooLong := filepath.Join(dir, strings.Repeat("a", nameMax-tempOverhead+1)+".json")
	require.NoError(t, os.WriteFile(tooLong, []byte("PREVIOUS\n"), 0o644),
		"precondition: a truncating write to this name succeeds")

	err := writeCompiled(tooLong, io.Discard, &ir.Document{Name: "Fresh"})

	require.Error(t, err, "the temp name must not fit where the destination does")
	assert.Contains(t, err.Error(), "create output")
	got, readErr := os.ReadFile(tooLong)
	require.NoError(t, readErr)
	assert.Equal(t, "PREVIOUS\n", string(got),
		"the destination must keep its previous content when the temp name will not fit")
}

func TestWriteParsed_StatError(t *testing.T) {
	t.Parallel()
	// A regular file used as a parent directory makes Stat fail with ENOTDIR,
	// which is an error but not "does not exist".
	notDir := filepath.Join(t.TempDir(), "file")
	require.NoError(t, os.WriteFile(notDir, []byte("x"), 0o644))

	err := writeCompiled(filepath.Join(notDir, "ir.json"), io.Discard, &ir.Document{Name: "ok"})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "stat output")
}

func TestWriteParsed_TempNameCollisionRetries(t *testing.T) {
	var names []string
	swapCreateOutput(t, func(path string, perm os.FileMode) (outputFile, error) {
		names = append(names, path)
		if len(names) == 1 {
			return nil, os.ErrExist // first name taken; the loop must draw another
		}
		return os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, perm)
	})

	out := filepath.Join(t.TempDir(), "ir.json")
	require.NoError(t, writeCompiled(out, io.Discard, &ir.Document{Name: "ok"}))

	require.Len(t, names, 2, "a taken temp name must be retried, not reused")
	assert.NotEqual(t, names[0], names[1], "each attempt must draw a fresh name")
}

func TestWriteParsed_TempNamesExhausted(t *testing.T) {
	attempts := 0
	swapCreateOutput(t, func(string, os.FileMode) (outputFile, error) {
		attempts++
		return nil, os.ErrExist
	})

	err := writeCompiled(filepath.Join(t.TempDir(), "ir.json"), io.Discard, &ir.Document{Name: "ok"})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "no unused temp name")
	assert.Equal(t, maxTempAttempts, attempts, "the name search must be bounded")
}

// TestWriteParsed_SyncErrorPreservesDestination drives the branch that makes the
// rename safe to trust: a filesystem that accepts the bytes but cannot flush
// them must abort before publishing, leaving the old destination whole. A sync
// failure that fell through to the rename would publish a file whose contents a
// crash could still lose, which is the loss this whole path exists to prevent.
func TestWriteParsed_SyncErrorPreservesDestination(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "ir.json")
	require.NoError(t, os.WriteFile(out, []byte("old\n"), 0o644))

	f := &syncFailWriteCloser{syncErr: errors.New("sync refused")}
	swapCreateOutput(t, func(string, os.FileMode) (outputFile, error) { return f, nil })

	err := writeCompiled(out, io.Discard, &ir.Document{Name: "ok"})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "sync output")
	assert.True(t, f.closed, "a failed sync must still close the temp file")
	got, readErr := os.ReadFile(out)
	require.NoError(t, readErr)
	assert.Equal(t, "old\n", string(got), "a failed sync must not disturb the destination")
	entries, readErr := os.ReadDir(dir)
	require.NoError(t, readErr)
	assert.Len(t, entries, 1, "a failed sync must leave no temp file beside the destination")
}

func TestWriteParsed_ChmodError(t *testing.T) {
	out := filepath.Join(t.TempDir(), "ir.json")
	require.NoError(t, os.WriteFile(out, []byte("old\n"), 0o644))

	origChmod := chmodOutput
	t.Cleanup(func() { chmodOutput = origChmod })
	chmodOutput = func(string, os.FileMode) error { return errors.New("chmod refused") }

	err := writeCompiled(out, io.Discard, &ir.Document{Name: "ok"})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "chmod output")
	got, readErr := os.ReadFile(out)
	require.NoError(t, readErr)
	assert.Equal(t, "old\n", string(got), "a failed chmod must not disturb the destination")
}

func TestWriteParsed_RenameError(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "ir.json")

	origRename := renameOutput
	t.Cleanup(func() { renameOutput = origRename })
	renameOutput = func(string, string) error { return errors.New("rename refused") }

	err := writeCompiled(out, io.Discard, &ir.Document{Name: "ok"})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "replace output")
	entries, readErr := os.ReadDir(dir)
	require.NoError(t, readErr)
	assert.Empty(t, entries, "a failed rename must remove the temp file it left behind")
}

func TestWriteParsed_ToStdoutSuccess(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	require.NoError(t, writeCompiled("", &buf, &ir.Document{Name: "ok"}))
	assert.True(t, bytes.HasSuffix(buf.Bytes(), []byte("\n")))
}
