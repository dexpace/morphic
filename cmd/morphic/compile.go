package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"math/rand/v2"
	"os"
	"path/filepath"

	"github.com/dexpace/morphic/engine"
	"github.com/dexpace/morphic/ir"
)

// newEngine constructs the pipeline engine. It is a package var so tests can
// inject a construction failure — the real engine.New registers exactly one
// compiler into a fresh registry, so neither of Register's failure modes (no
// formats reported, a format collision) can actually fire — and a stub engine
// that returns a nil Document.
var newEngine = engine.New

// createOutput creates the temp file that -o writes through. It is a package var
// so tests can inject an io.WriteCloser whose Write or Close fails; a real
// *os.File's Close does not fail after a successful write on the platforms
// Morphic targets.
var createOutput = func(path string, perm os.FileMode) (io.WriteCloser, error) {
	return os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, perm)
}

// chmodOutput restores a replaced destination's mode onto the temp file. It is a
// package var so tests can inject a chmod failure, which is otherwise reachable
// only by racing the filesystem.
var chmodOutput = os.Chmod

// renameOutput publishes a fully written temp file over the destination. It is a
// package var so tests can inject a rename failure, which is otherwise reachable
// only by racing the filesystem.
var renameOutput = os.Rename

// maxTempAttempts bounds the search for an unused temp-file name. Each attempt
// draws a fresh random suffix, so exhausting it means the destination directory
// is unusable rather than that the names happened to collide.
const maxTempAttempts = 10

// newFilePerm is the mode a temp file is created with: the same 0666 os.Create
// requests, so the umask narrows it identically. A destination that already
// exists overrides this via chmodOutput — see destMode.
const newFilePerm os.FileMode = 0o666

// runCompile implements the `compile` subcommand: lower one spec file to IR JSON,
// render its diagnostics to stderr, and return the process exit code.
func runCompile(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("compile", flag.ContinueOnError)
	fs.SetOutput(stderr)
	outPath := fs.String("o", "", "write IR JSON to this file instead of stdout")
	failOn := fs.String("fail-on", "error",
		"fail (exit 1) on diagnostics at or above this severity: error|warning")
	skipValidate := fs.Bool("skip-validate", false, "skip the referential-integrity validate pass")
	explain := fs.String("explain", "",
		"report what compiling produced at this source pointer instead of writing IR JSON")

	positional, err := parseArgs(fs, args)
	if err != nil {
		printUsage(stderr)
		return 2
	}
	if *failOn != "error" && *failOn != "warning" {
		emitf(stderr, "morphic: invalid --fail-on %q (want error or warning)\n", *failOn)
		printUsage(stderr)
		return 2
	}
	if len(positional) != 1 {
		emitf(stderr, "morphic: compile requires exactly one spec file\n")
		printUsage(stderr)
		return 2
	}

	eng, err := newEngine()
	if err != nil {
		emitf(stderr, "morphic: %v\n", err)
		return 2
	}
	res, err := eng.Run(context.Background(), positional[0], engine.RunOptions{SkipValidate: *skipValidate})
	if err != nil {
		emitf(stderr, "morphic: %v\n", err)
		return 2
	}
	renderDiagnostics(stderr, res)
	if res.Document == nil {
		return 1
	}
	if *explain != "" {
		explainDocument(stdout, res.Document, res.Diagnostics, *explain)
		return exitCodeFor(res.Diagnostics, *failOn)
	}
	if err := writeCompiled(*outPath, stdout, res.Document); err != nil {
		emitf(stderr, "morphic: %v\n", err)
		return 2
	}
	return exitCodeFor(res.Diagnostics, *failOn)
}

// parseArgs binds fs and collects positional arguments, tolerating flags that
// appear either before or after the spec path (stdlib flag stops at the first
// non-flag argument, so it is invoked once per positional).
func parseArgs(fs *flag.FlagSet, args []string) ([]string, error) {
	var positional []string
	rest := args
	for {
		if err := fs.Parse(rest); err != nil {
			return nil, fmt.Errorf("parse flags: %w", err)
		}
		rest = fs.Args()
		if len(rest) == 0 {
			return positional, nil
		}
		positional = append(positional, rest[0])
		rest = rest[1:]
	}
}

// renderDiagnostics writes each diagnostic to w, one per line, as
// "<severity> <code> <path>#<pointer>: <message>". This is the sole place in
// the pipeline where diagnostics are rendered for a human.
func renderDiagnostics(w io.Writer, res *engine.Result) {
	for _, d := range res.Diagnostics {
		if path := sourcePath(res.Document, d.Provenance.Source); path != "" {
			emitf(w, "%s %s %s#%s: %s\n",
				d.Severity, d.Code, path, d.Provenance.Pointer, d.Message)
			continue
		}
		// No source file (e.g. a pass diagnostic whose pointer is an IR id): show
		// the bare pointer rather than fabricating a location in the spec file.
		emitf(w, "%s %s %s: %s\n", d.Severity, d.Code, d.Provenance.Pointer, d.Message)
	}
}

// sourcePath resolves a diagnostic's source index to its file path, returning
// "" when the document or index is unavailable.
func sourcePath(doc *ir.Document, source int) string {
	if doc == nil || source < 0 || source >= len(doc.Sources) {
		return ""
	}
	return doc.Sources[source].Path
}

// exitCodeFor returns 1 when any diagnostic is at or above the failOn severity,
// otherwise 0. failOn is one of "error" or "warning".
func exitCodeFor(diags []ir.Diagnostic, failOn string) int {
	threshold := severityRank(ir.Severity(failOn))
	for _, d := range diags {
		if severityRank(d.Severity) >= threshold {
			return 1
		}
	}
	return 0
}

// severityRank orders severities so a threshold comparison is a plain integer
// compare: error > warning > info > unknown.
func severityRank(s ir.Severity) int {
	switch s {
	case ir.SeverityError:
		return 3
	case ir.SeverityWarning:
		return 2
	case ir.SeverityInfo:
		return 1
	default:
		return 0
	}
}

// writeCompiled emits doc's pretty IR JSON to outPath, or to stdout when outPath
// is empty. For a file destination, doc is marshalled in full before any file is
// touched, so a failed marshal never disturbs a file already there — see
// marshalDocument — and the bytes are then published atomically by replaceFile.
func writeCompiled(outPath string, stdout io.Writer, doc *ir.Document) error {
	if outPath == "" {
		return writeDocument(stdout, doc)
	}
	raw, err := marshalDocument(doc)
	if err != nil {
		return err
	}
	return replaceFile(outPath, raw)
}

// replaceFile writes raw to outPath atomically: the bytes land in a temp file in
// the destination's own directory — so the publishing rename never crosses a
// filesystem boundary — and replace outPath only once all of them are on disk.
// A failed or partial write therefore leaves outPath's previous content intact
// instead of truncating it.
//
// Publishing by rename replaces the directory entry rather than the bytes behind
// it, which is what makes the swap atomic and which costs three things a
// truncating write gave for free. All three are accepted deliberately:
//
//   - Writing needs permission on the destination's directory, not just on the
//     destination. Rewriting an existing writable file inside a read-only
//     directory used to succeed and now fails at temp-file creation.
//   - A symlink at outPath is replaced by a regular file instead of being
//     followed and written through, so its target keeps its old content.
//   - Other hard links to outPath keep pointing at the old inode, and so keep
//     the old content, instead of observing the new bytes.
//
// Losing them is the price of the guarantee: each is a way for the destination's
// bytes to be reached other than through its own name, and honouring any of them
// means writing in place, which is exactly the truncation this replaces.
func replaceFile(outPath string, raw []byte) error {
	perm, replacing, err := destMode(outPath)
	if err != nil {
		return err
	}

	tmp, err := writeTemp(outPath, raw)
	if err != nil {
		return err
	}

	// A brand-new file keeps the umask-narrowed mode it was created with; one
	// that replaces an existing destination inherits that destination's mode,
	// which is what truncating it in place would have preserved.
	if replacing {
		if err := chmodOutput(tmp, perm); err != nil {
			_ = os.Remove(tmp)
			return fmt.Errorf("chmod output %q: %w", outPath, err)
		}
	}

	if err := renameOutput(tmp, outPath); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("replace output %q: %w", outPath, err)
	}
	return nil
}

// destMode reports the mode an existing destination carries, and whether there
// is one at all. A missing destination is the ordinary case, not an error.
func destMode(outPath string) (os.FileMode, bool, error) {
	info, err := os.Stat(outPath)
	if errors.Is(err, os.ErrNotExist) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, fmt.Errorf("stat output %q: %w", outPath, err)
	}
	return info.Mode().Perm(), true, nil
}

// writeTemp writes raw to a newly created file beside outPath and returns that
// file's path. Creation is O_EXCL under a random name, so it neither clobbers an
// existing file nor follows a symlink planted at the name it drew; that makes the
// unpredictability of the suffix a convenience, not a security boundary.
func writeTemp(outPath string, raw []byte) (string, error) {
	dir := filepath.Dir(outPath)
	base := filepath.Base(outPath)

	for range maxTempAttempts {
		tmp := filepath.Join(dir, fmt.Sprintf(".%s.tmp%016x", base, rand.Uint64()))
		f, err := createOutput(tmp, newFilePerm)
		if errors.Is(err, os.ErrExist) {
			continue
		}
		if err != nil {
			return "", fmt.Errorf("create output %q: %w", outPath, err)
		}
		if err := fillTemp(f, tmp, raw); err != nil {
			return "", err
		}
		return tmp, nil
	}
	return "", fmt.Errorf("create output %q: no unused temp name in %d attempts",
		outPath, maxTempAttempts)
}

// fillTemp writes raw to f and closes it, removing tmp if either step fails so a
// failed run leaves no debris beside the destination.
func fillTemp(f io.WriteCloser, tmp string, raw []byte) error {
	if err := writeRaw(f, raw); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("close output %q: %w", tmp, err)
	}
	return nil
}

// marshalDocument renders doc to indented JSON with a trailing newline (the
// same bytes as irtest.WriteGolden). It is factored out of writeDocument so
// writeCompiled can marshal a file destination fully in memory before it ever
// opens — and so truncates — outPath.
func marshalDocument(doc *ir.Document) ([]byte, error) {
	raw, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal ir document: %w", err)
	}
	return append(raw, '\n'), nil
}

// writeDocument marshals doc to indented JSON with a trailing newline (the same
// bytes as irtest.WriteGolden) and writes it to w.
func writeDocument(w io.Writer, doc *ir.Document) error {
	raw, err := marshalDocument(doc)
	if err != nil {
		return err
	}
	return writeRaw(w, raw)
}

// writeRaw writes raw to w, wrapping any error with context.
func writeRaw(w io.Writer, raw []byte) error {
	if _, err := w.Write(raw); err != nil {
		return fmt.Errorf("write ir document: %w", err)
	}
	return nil
}
