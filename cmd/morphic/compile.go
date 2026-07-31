package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/dexpace/morphic/engine"
	"github.com/dexpace/morphic/ir"
)

// newEngine constructs the pipeline engine. It is a package var so tests can
// inject a construction failure — the real engine.New only errors on a
// duplicate compiler registration, which the default registry never produces —
// and a stub engine that returns a nil Document.
var newEngine = engine.New

// openOutput creates the destination file for -o. It is a package var so tests
// can inject an io.WriteCloser whose Close fails; a real *os.File's Close does
// not fail after a successful write on the platforms Morphic targets.
var openOutput = func(path string) (io.WriteCloser, error) { return os.Create(path) }

// newCompileCommand builds compile's command-table entry. It is a function,
// not a package-level var, because its run field refers to runCompile, and
// runCompile itself needs this same metadata to render help and usage text;
// a var holding that cycle back to itself would be a Go initialization
// cycle, while two package-level functions may freely refer to each other.
func newCompileCommand() command {
	return command{
		name:    "compile",
		summary: "lower an API spec (OpenAPI 3.x) into Morphic IR JSON",
		usage:   "morphic compile <spec-file> [flags]",
		description: "Lower an API spec (OpenAPI 3.x) into Morphic IR JSON on stdout, and write\n" +
			"diagnostics to stderr.",
		flagSet: func() *flag.FlagSet {
			fs, _ := newCompileFlags()
			return fs
		},
		run: runCompile,
	}
}

// compileOptions holds the values compile's flags parse into.
type compileOptions struct {
	outPath      string
	failOn       string
	skipValidate bool
}

// newCompileFlags returns compile's FlagSet and the options its flags write
// into. The FlagSet prints nothing on its own: parse failures and help requests
// come back as errors from Parse, so the CLI renders exactly one text for them.
func newCompileFlags() (*flag.FlagSet, *compileOptions) {
	fs := flag.NewFlagSet("compile", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.Usage = func() {}

	var opts compileOptions
	fs.StringVar(&opts.outPath, "o", "", "write IR JSON to this file instead of stdout")
	fs.StringVar(&opts.failOn, "fail-on", "error",
		"fail (exit 1) on diagnostics at or above this severity: error|warning")
	fs.BoolVar(&opts.skipValidate, "skip-validate", false,
		"skip the referential-integrity validate pass")

	return fs, &opts
}

// runCompile implements the `compile` subcommand: lower one spec file to IR
// JSON, render its diagnostics to stderr, and return the process exit code.
func runCompile(args []string, stdout, stderr io.Writer) int {
	fs, opts := newCompileFlags()

	positional, err := parseArgs(fs, args)
	if errors.Is(err, flag.ErrHelp) {
		writeCommandHelp(stdout, newCompileCommand())
		return 0
	}
	if err != nil {
		return compileUsageError(stderr, "%v", err)
	}
	if opts.failOn != "error" && opts.failOn != "warning" {
		return compileUsageError(stderr, "invalid --fail-on %q (want error or warning)", opts.failOn)
	}
	if len(positional) != 1 {
		return compileUsageError(stderr, "compile requires exactly one spec file")
	}

	return compileSpec(positional[0], *opts, stdout, stderr)
}

// compileUsageError reports a misuse of compile: one reason line, one short
// usage pointer, exit 2. It never touches stdout.
func compileUsageError(stderr io.Writer, format string, args ...any) int {
	emitf(stderr, "morphic: "+format+"\n", args...)
	writeCommandUsage(stderr, newCompileCommand())
	return 2
}

// compileSpec runs the pipeline over specPath, writes the IR document and its
// diagnostics, and returns the process exit code.
func compileSpec(specPath string, opts compileOptions, stdout, stderr io.Writer) int {
	eng, err := newEngine()
	if err != nil {
		emitf(stderr, "morphic: %v\n", err)
		return 2
	}

	res, err := eng.Run(context.Background(), specPath, engine.RunOptions{SkipValidate: opts.skipValidate})
	if err != nil {
		emitf(stderr, "morphic: %v\n", err)
		return 2
	}

	renderDiagnostics(stderr, res)
	if res.Document == nil {
		return 1
	}
	if err := writeCompiled(opts.outPath, stdout, res.Document); err != nil {
		emitf(stderr, "morphic: %v\n", err)
		return 2
	}
	return exitCodeFor(res.Diagnostics, opts.failOn)
}

// parseArgs binds fs and collects positional arguments, tolerating flags that
// appear either before or after the spec path (stdlib flag stops at the first
// non-flag argument, so it is invoked once per positional).
func parseArgs(fs *flag.FlagSet, args []string) ([]string, error) {
	var positional []string
	rest := args
	for {
		if err := fs.Parse(rest); err != nil {
			// Returned verbatim, not wrapped: this error is rendered straight to
			// the user, and the flag package's messages already name the flag.
			return nil, err
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
// is empty.
func writeCompiled(outPath string, stdout io.Writer, doc *ir.Document) error {
	if outPath == "" {
		return writeDocument(stdout, doc)
	}
	f, err := openOutput(outPath)
	if err != nil {
		return fmt.Errorf("create output %q: %w", outPath, err)
	}
	if err := writeDocument(f, doc); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("close output %q: %w", outPath, err)
	}
	return nil
}

// writeDocument marshals doc to indented JSON with a trailing newline (the same
// bytes as irtest.WriteGolden) and writes it to w.
func writeDocument(w io.Writer, doc *ir.Document) error {
	raw, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal ir document: %w", err)
	}
	raw = append(raw, '\n')
	if _, err := w.Write(raw); err != nil {
		return fmt.Errorf("write ir document: %w", err)
	}
	return nil
}
