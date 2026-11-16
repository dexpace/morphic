// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"

	"github.com/dexpace/morphic/internal/harness"
	"github.com/dexpace/morphic/internal/harness/openapicases"
)

// osExit is os.Exit, a package var so tests can drive main without terminating
// the test process.
var osExit = os.Exit

func main() {
	osExit(run(context.Background(), formats(), os.Args[1:], os.Stdout, os.Stderr))
}

// run parses args and runs what they ask for, writing what it finds to stdout,
// and returns the process exit code: 0 when nothing is found, 1 when anything
// is, 2 on a usage or filesystem error. Paths are swept (sweep); -cases checks
// the schema cases of fmts instead, and -at one of them. It exists so tests
// can drive the CLI without a subprocess, over formats of their own; only main
// calls os.Exit.
func run(ctx context.Context, fmts []format, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("morphic-harness", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	mode := fs.String("cases", "", "")
	at := fs.Int("at", 0, "")
	if err := fs.Parse(args); err != nil {
		return usageError(stderr, err.Error())
	}
	atSet := false
	fs.Visit(func(f *flag.Flag) { atSet = atSet || f.Name == "at" })

	switch {
	case *mode == "" && atSet:
		return usageError(stderr, "-at names a case of -cases")
	case *mode == "":
		return sweep(ctx, fs.Args(), stdout, stderr)
	case fs.NArg() > 0:
		return usageError(stderr, "-cases checks the cases, not paths")
	case *mode != "core" && *mode != "full":
		return usageError(stderr, fmt.Sprintf("-cases is core or full, not %q", *mode))
	case atSet:
		return checkOne(ctx, fmts, *at, stdout, stderr)
	default:
		return checkCases(ctx, fmts, *mode == "core", stdout, stderr)
	}
}

// usageError writes why the arguments are refused and the usage, and returns
// the exit code for it.
func usageError(stderr io.Writer, why string) int {
	emitf(stderr, "morphic-harness: %s\n%s\n", why, usage)
	return 2
}

// sweep sweeps every path through the harness oracles, writes the combined
// report to stdout, then the summary of what each oracle asked of the specs
// and declined, and returns the exit code: 1 when any spec fails an oracle.
func sweep(ctx context.Context, paths []string, stdout, stderr io.Writer) int {
	if len(paths) == 0 {
		emitf(stderr, "%s\n", usage)
		return 2
	}

	var all []harness.Result
	for _, path := range paths {
		results, err := harness.CheckPath(ctx, path)
		if err != nil {
			emitf(stderr, "morphic-harness: %v\n", err)
			return 2
		}
		all = append(all, results...)
	}

	emitf(stdout, "%s", harness.Report(all))
	if len(all) > 0 {
		emitf(stdout, "\n%s", harness.Summary(all))
	}
	return exitCode(all)
}

// format is one format's schema cases and the record of what each case oracle
// is known to find at its core cases.
type format struct {
	cases  harness.Cases
	record func(harness.Oracle) (harness.Recorded, error)
}

// formats returns every product of schema cases -cases checks, in the order
// -at counts their cases: the OpenAPI schema cases, then the test suite's
// groups written as OpenAPI defaults.
func formats() []format {
	return []format{
		{cases: openapicases.New(), record: openapicases.Record},
		{cases: openapicases.Groups(), record: openapicases.Record},
	}
}

// checkCases applies the case oracles to each format's cases, the core ones
// alone when core is set, writes a report of what each format's records do not
// explain, then what each case oracle asked and declined, and returns the exit
// code: 1 when anything is left to report.
func checkCases(ctx context.Context, fmts []format, core bool, stdout, stderr io.Writer) int {
	var all, left []harness.Result
	for _, f := range fmts {
		pick := func([]int) bool { return true }
		if core {
			pick = harness.Core(f.cases)
		}
		results, err := harness.CheckCases(ctx, f.cases, pick)
		if err != nil {
			emitf(stderr, "morphic-harness: %v\n", err)
			return 2
		}
		records := map[harness.Oracle]harness.Recorded{}
		for _, o := range harness.CaseOracles() {
			if records[o], err = f.record(o); err != nil {
				emitf(stderr, "morphic-harness: %v\n", err)
				return 2
			}
		}
		all, left = append(all, results...), append(left, harness.Unexplained(results, records)...)
	}

	emitf(stdout, "%s", harness.Report(left))
	if len(all) > 0 {
		emitf(stdout, "\n%s", harness.CaseSummary(all))
	}
	if len(left) > 0 {
		return 1
	}
	return 0
}

// checkOne applies the case oracles to the case at index n of the formats'
// cases, counted one format after another, and writes its name, its sources
// and each verdict. It returns 2 for an index naming no case, and 1 when the
// case holds a finding.
func checkOne(ctx context.Context, fmts []format, n int, stdout, stderr io.Writer) int {
	first := 0
	for _, f := range fmts {
		if at, err := harness.Coordinates(f.cases, n-first); err == nil {
			return writeCase(ctx, f.cases, at, stdout)
		}
		count, err := harness.CaseCount(f.cases)
		if err != nil {
			emitf(stderr, "morphic-harness: %v\n", err)
			return 2
		}
		first += count
	}
	emitf(stderr, "morphic-harness: -at %d names none of the %d cases\n", n, first)
	return 2
}

// writeCase applies the case oracles to c's case at and writes its name, its
// sources and each verdict, and returns 1 when the case holds what a run of
// the cases reports with no record to explain it: a finding, a decline a
// record lists as one, or a fault. A case a fault such as a panic ended is
// written with the fault in place of its sources: the fault may lie in Write,
// which writing the case again would raise again.
func writeCase(ctx context.Context, c harness.Cases, at []int, stdout io.Writer) int {
	res, written := harness.CheckCase(ctx, c, at)
	if !written {
		emitf(stdout, "%s writes no document\n", res.Spec)
		return 0
	}
	emitf(stdout, "%s\n", res.Spec)
	if res.Faulted() {
		emitf(stdout, "%s %s\n", res.Outcome, res.Detail)
	} else {
		doc, _ := c.Write(at)
		for _, s := range doc.Sources {
			emitf(stdout, "--- %s\n%s\n", s.Path, s.Data)
		}
	}
	for _, v := range res.Verdicts {
		answer := string(v.Outcome)
		if !v.Asked() {
			answer = "declined: " + string(v.Declined)
		}
		emitf(stdout, "%s\n", strings.TrimSuffix(fmt.Sprintf("%s %s %s", v.Oracle, answer, v.Detail), " "))
	}
	if len(harness.Unexplained([]harness.Result{res}, nil)) > 0 {
		return 1
	}
	return 0
}

// exitCode returns 1 when any result is not OutcomeOK, otherwise 0.
func exitCode(results []harness.Result) int {
	if slices.ContainsFunc(results, func(r harness.Result) bool { return r.Outcome != harness.OutcomeOK }) {
		return 1
	}
	return 0
}

// emitf writes a formatted message to w; write errors on a human-facing stream
// are unactionable and deliberately discarded.
func emitf(w io.Writer, layout string, args ...any) {
	_, _ = fmt.Fprintf(w, layout, args...)
}

const usage = `usage:
  morphic-harness <path>...
  morphic-harness -cases core|full [-at N]

Each path is a spec file or a directory of specs: every file in it a registered
compiler claims, to compile, as a format it names, or as its own and broken,
excluding *.golden.* snapshots and dot-directories.
Every spec is swept through the Morphic oracles (no panic, IR invariants, no
error diagnostic, JSON round-trip, determinism, pass idempotence,
order-invariance, sequence-order-invariance), an error stopping none of them.
Each finding is a line, and a summary after them counts, per oracle, the specs
it asked its question of and those it declined, by reason. Exit status is 0
when every spec passes, 1 when any spec fails an oracle, 2 on a usage or
filesystem error.

With -cases, each format's schema cases are checked instead: core, the cases
the gate runs, or full, every case, as make cases runs them each night. Each
case is a document the case oracles (spelling, presence, instance) judge, the
first two against its neighbours. A finding a format's record lists at the core
case the case projects to is explained; the rest are a line each, and a summary
counts, per case oracle, the cases it asked and declined. With -at N, the case
at index N alone is checked, whichever of core or full is named, and its name,
its sources and each verdict are written. N counts the cases of one product
after another: the OpenAPI schema cases, then the JSON Schema Test Suite's
groups, each written as defaults. A case is written to compile, so an oracle
declining one for an error diagnostic is reported as a finding is. Exit status
is 0 when nothing is left to report, 1 when anything is (with -at, any finding
the case holds, listed or not), 2 on a usage error or an index naming no case.`
