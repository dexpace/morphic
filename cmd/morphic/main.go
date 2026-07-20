// Command morphic is the Morphic CLI: it lowers an API spec into Morphic IR.
//
// It is the only layer that renders diagnostics for a human (architecture §4);
// every stage below it emits typed ir.Diagnostic values and never writes to
// stderr.
package main

import (
	"fmt"
	"io"
	"os"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// run dispatches subcommands and returns the process exit code. It exists so
// tests can drive the CLI without a subprocess; only main calls os.Exit.
func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		printUsage(stderr)
		return 2
	}
	switch args[0] {
	case "parse":
		return runParse(args[1:], stdout, stderr)
	default:
		emitf(stderr, "morphic: unknown command %q\n", args[0])
		printUsage(stderr)
		return 2
	}
}

// emitf writes a formatted line to w. Write errors on a human-facing stream are
// unactionable, so they are deliberately discarded.
func emitf(w io.Writer, format string, args ...any) {
	_, _ = fmt.Fprintf(w, format, args...)
}

// printUsage writes the usage text to w.
func printUsage(w io.Writer) {
	emitf(w, "%s\n", usage)
}

const usage = `usage:
  morphic parse <spec-file> [-o out.json] [--fail-on error|warning] [--skip-validate]

parse lowers an API spec (OpenAPI 3.x) into Morphic IR JSON.`
