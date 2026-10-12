package main

import (
	"cmp"
	"fmt"
	"io"
	"os"
	"slices"

	"golang.org/x/tools/cover"
)

// maxReported bounds the failure listing: a broken build can leave hundreds of
// uncovered blocks, and the first screenful is what gets read.
const maxReported = 25

const usage = "usage: morphic-covergate <profile>"

// osExit is os.Exit, a package var so tests can drive main without terminating
// the test process.
var osExit = os.Exit

func main() {
	osExit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// run judges the profile args names and returns the process exit code. Every
// verdict goes to stdout, so a caller capturing only stdout still learns why the
// gate stopped.
func run(args []string, stdout, stderr io.Writer) int {
	if len(args) != 1 {
		emitf(stderr, "%s\n", usage)
		return 2
	}
	// ParseProfiles refuses a missing "mode:" header, a malformed line and a
	// block whose copies disagree on its statement count, and merges the copies
	// of every other block, summing their counts.
	profiles, err := cover.ParseProfiles(args[0])
	if err != nil {
		emitf(stdout, "COVERAGE FAIL: read profile %s: %v\n", args[0], err)
		return 1
	}
	return verdict(stdout, tallyOf(profiles))
}

// gap is a block no test ran.
type gap struct {
	file  string
	block cover.ProfileBlock
}

// tally is what a profile says about the statements it records.
type tally struct {
	total, missed int
	gaps          []gap
}

// tallyOf counts the statements in profiles, and lists the blocks no test ran
// in file and then position order, so a failure reads the same on every run.
// A block with no statements is skipped: go emits one for an empty body, such as
// an unreached "case x:", and it cannot be uncovered.
func tallyOf(profiles []*cover.Profile) tally {
	var t tally
	for _, p := range profiles {
		for _, b := range p.Blocks {
			if b.NumStmt == 0 {
				continue
			}
			t.total += b.NumStmt
			if b.Count == 0 {
				t.missed += b.NumStmt
				t.gaps = append(t.gaps, gap{file: p.FileName, block: b})
			}
		}
	}
	slices.SortFunc(t.gaps, func(a, b gap) int {
		return cmp.Or(cmp.Compare(a.file, b.file),
			cmp.Compare(a.block.StartLine, b.block.StartLine),
			cmp.Compare(a.block.StartCol, b.block.StartCol))
	})
	return t
}

// verdict writes the gate's judgement of t and returns the exit code. A profile
// recording no statements fails: it is what a run that measured nothing leaves.
func verdict(w io.Writer, t tally) int {
	if t.total == 0 {
		emitf(w, "COVERAGE FAIL: the profile records no statements\n")
		return 1
	}
	if t.missed == 0 {
		emitf(w, "Coverage gate passed: all %d statements covered.\n", t.total)
		return 0
	}
	for _, g := range t.gaps[:min(len(t.gaps), maxReported)] {
		b := g.block
		emitf(w, "COVERAGE FAIL: %s:%d.%d,%d.%d (%d statement(s) uncovered)\n",
			g.file, b.StartLine, b.StartCol, b.EndLine, b.EndCol, b.NumStmt)
	}
	if more := len(t.gaps) - maxReported; more > 0 {
		emitf(w, "  ... and %d more uncovered block(s)\n", more)
	}
	emitf(w, "Coverage gate failed: %d of %d statements uncovered; 100%% is required.\n", t.missed, t.total)
	return 1
}

// emitf writes a formatted message to w; write errors on a human-facing stream
// are unactionable and deliberately discarded.
func emitf(w io.Writer, format string, args ...any) {
	_, _ = fmt.Fprintf(w, format, args...)
}
