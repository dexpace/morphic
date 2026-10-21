package main

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/dexpace/morphic/engine"
	"github.com/dexpace/morphic/internal/census"
	"github.com/dexpace/morphic/internal/harness"
)

// osExit is os.Exit, a package var so tests can drive main without terminating
// the test process.
var osExit = os.Exit

// newEngine constructs the pipeline engine. It is a package var so tests can
// inject a construction failure, which the real engine.New cannot produce: it
// registers one compiler into a fresh registry.
var newEngine = engine.New

func main() {
	osExit(run(context.Background(), os.Args[1:], os.Stdout, os.Stderr))
}

// run compiles every spec the path arguments name, counts the facts each
// document sets, writes the report to stdout and returns the process exit code:
// 0 on a report, 2 on a usage error or a fault reading the IR's declarations or
// a spec. A spec that lowers to no document is counted as such and skipped. It
// exists so tests can drive the CLI without a subprocess; only main calls
// os.Exit.
func run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		emitf(stderr, "%s\n", usage)
		return 2
	}

	c, err := sweep(ctx, args)
	if err != nil {
		emitf(stderr, "morphic-census: %v\n", err)
		return 2
	}
	emitf(stdout, "%s", c.Report())
	return 0
}

// sweep counts every spec the paths name, in the order they are listed.
func sweep(ctx context.Context, paths []string) (*census.Census, error) {
	u, err := census.NewUniverse()
	if err != nil {
		return nil, err
	}
	e, err := newEngine()
	if err != nil {
		return nil, fmt.Errorf("engine: %w", err)
	}

	c := census.New(u)
	for _, path := range paths {
		specs, err := harness.SpecFiles(ctx, path)
		if err != nil {
			return nil, err
		}
		for _, spec := range specs {
			if err := count(ctx, e, c, spec); err != nil {
				return nil, err
			}
		}
	}
	return c, nil
}

// count compiles the spec at path and adds its document to c, or counts that
// there was none. A run that fails at the default threshold still counts its
// document, since the none threshold would hand that document to an emitter.
func count(ctx context.Context, e *engine.Engine, c *census.Census, path string) error {
	res, err := e.Run(ctx, path, engine.RunOptions{})
	if err != nil {
		return err
	}
	if res.Document == nil {
		c.NoDocument()
		return nil
	}
	return c.Add(res.Document)
}

// emitf writes a formatted message to w; write errors on a human-facing stream
// are unactionable and deliberately discarded.
func emitf(w io.Writer, format string, args ...any) {
	_, _ = fmt.Fprintf(w, format, args...)
}

const usage = `usage:
  morphic-census <path>...

Each path is a spec file or a directory of specs: every file in it that a
registered compiler takes, excluding *.golden.* snapshots and dot-directories.
Every spec is compiled with the default options, and the facts its document sets
are counted: the members of the IR's structs, the emptiness of each member whose
empty value differs from its absence, and the values of its enumerations. The
report counts the documents, the specs that lowered to none and the walks cut at
the depth bound, then lists every fact with the number of documents that set it
and the number of instances, zeros included. It reads the IR's declarations
through the go command, so run it with go run from inside the module. Exit
status is 0 on a report, 2 on a usage error, an unreadable spec, or declarations
it cannot read.`
