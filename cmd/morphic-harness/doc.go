// Command morphic-harness sweeps API specs through the Morphic bug-catching
// oracles (no panic/error, IR invariants, JSON round-trip, determinism) and
// reports the outcome per spec.
//
// Each argument is a spec file or a directory of specs; directories are walked
// recursively for *.yaml, *.yml, and *.json inputs, excluding *.golden.json IR
// snapshots. The combined report is written to stdout. The process exits 0 when
// every spec passes, 1 when any spec fails an oracle, and 2 on a usage or
// filesystem error, so it is usable as a CI or script gate.
//
// It is tooling built on internal/harness, not part of the compile pipeline.
package main
