// Command morphic-harness sweeps API specs through Morphic's bug-catching
// oracles, which harness.Check lists in order, each spec run as
// `morphic compile --verify` runs it, and writes a combined report to stdout.
// It is tooling built on internal/harness, not part of the compile pipeline.
//
// Each argument is a spec file or directory, walked recursively for the files a
// registered compiler claims (harness.SpecFiles), skipping dot-directories and
// *.golden.* IR snapshots. Exit code is 0 if every spec passes, 1 if any spec
// fails an oracle, and 2 on a usage or filesystem error, making it usable as a
// CI or script gate.
package main
