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

// osExit is os.Exit, a package var so tests can drive main without terminating
// the test process.
var osExit = os.Exit

func main() {
	osExit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// run dispatches subcommands and returns the process exit code. It exists so
// tests can drive the CLI without a subprocess; only main calls os.Exit.
func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		writeRootHelp(stdout)
		return 0
	}
	// A leading help flag is the help command spelled differently, so it takes
	// the same path rather than a shortcut to root help. That is what makes
	// "morphic -h compile" print compile's help instead of silently dropping
	// the name, and "morphic -h bogus" report it instead of masking it.
	if args[0] == "help" || isHelpFlag(args[0]) {
		return runHelp(args[1:], stdout, stderr)
	}

	c, ok := lookup(args[0])
	if !ok {
		return rootUsageError(stderr, fmt.Sprintf("unknown command %q", args[0]))
	}
	return dispatch(c, args[1:], stdout, stderr)
}

// emitf writes a formatted line to w. Write errors on a human-facing stream are
// unactionable, so they are deliberately discarded.
func emitf(w io.Writer, format string, args ...any) {
	_, _ = fmt.Fprintf(w, format, args...)
}
