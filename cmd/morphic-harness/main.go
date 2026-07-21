package main

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/dexpace/morphic/internal/harness"
)

// osExit is os.Exit, a package var so tests can drive main without terminating
// the test process.
var osExit = os.Exit

func main() {
	osExit(run(context.Background(), os.Args[1:], os.Stdout, os.Stderr))
}

// run sweeps every path argument through the harness oracles, writes the
// combined report to stdout, and returns the process exit code: 0 when every
// checked spec is OutcomeOK, 1 when any spec fails an oracle, 2 on a usage or
// filesystem error. It exists so tests can drive the CLI without a subprocess;
// only main calls os.Exit.
func run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		emitf(stderr, "%s\n", usage)
		return 2
	}

	var all []harness.Result
	for _, path := range args {
		results, err := harness.CheckPath(ctx, path)
		if err != nil {
			emitf(stderr, "morphic-harness: %v\n", err)
			return 2
		}
		all = append(all, results...)
	}

	emitf(stdout, "%s", harness.Report(all))
	return exitCode(all)
}

// exitCode returns 1 when any result is not OutcomeOK, otherwise 0.
func exitCode(results []harness.Result) int {
	for _, r := range results {
		if r.Outcome != harness.OutcomeOK {
			return 1
		}
	}
	return 0
}

// emitf writes a formatted message to w; write errors on a human-facing stream
// are unactionable and deliberately discarded.
func emitf(w io.Writer, format string, args ...any) {
	_, _ = fmt.Fprintf(w, format, args...)
}

const usage = `usage:
  morphic-harness <path>...

Each path is a spec file or a directory of specs (*.yaml, *.yml, *.json,
excluding *.golden.json). Every spec is swept through the Morphic oracles
(no panic/error, IR invariants, JSON round-trip, determinism). Exit status is
0 when every spec passes, 1 when any spec fails an oracle, 2 on a usage or
filesystem error.`
