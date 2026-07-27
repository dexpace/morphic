// Command morphic-harness sweeps API specs through Morphic's bug-catching
// oracles (no panic/error, IR invariants, JSON round-trip, determinism) and
// writes a combined report to stdout. It is tooling built on
// internal/harness, not part of the compile pipeline.
//
// Each argument is a spec file or directory, walked recursively for
// *.yaml/*.yml/*.json and skipping *.golden.json IR snapshots. Exit code is 0
// if every spec passes, 1 if any spec fails an oracle, and 2 on a usage or
// filesystem error, making it usable as a CI or script gate.
package main
