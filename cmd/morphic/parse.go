package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/dexpace/morphic/engine"
	"github.com/dexpace/morphic/ir"
)

// runParse implements the `parse` subcommand: lower one spec file to IR JSON,
// render its diagnostics to stderr, and return the process exit code.
func runParse(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("parse", flag.ContinueOnError)
	fs.SetOutput(stderr)
	outPath := fs.String("o", "", "write IR JSON to this file instead of stdout")
	failOn := fs.String("fail-on", "error",
		"fail (exit 1) on diagnostics at or above this severity: error|warning")
	skipValidate := fs.Bool("skip-validate", false, "skip the referential-integrity validate pass")

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
		emitf(stderr, "morphic: parse requires exactly one spec file\n")
		printUsage(stderr)
		return 2
	}

	eng, err := engine.New()
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
	if err := writeParsed(*outPath, stdout, res.Document); err != nil {
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

// writeParsed emits doc's pretty IR JSON to outPath, or to stdout when outPath
// is empty.
func writeParsed(outPath string, stdout io.Writer, doc *ir.Document) error {
	if outPath == "" {
		return writeDocument(stdout, doc)
	}
	f, err := os.Create(outPath)
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
