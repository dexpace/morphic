// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

// Command morphic-harness sweeps API specs through Morphic's bug-catching
// oracles (harness.Check), each spec run as `morphic compile --verify` runs it,
// and writes to stdout a line per finding and what each oracle asked and
// declined (harness.Summary). It is tooling, not part of the compile pipeline.
//
// Each argument is a spec file or directory, walked recursively for the files a
// registered compiler claims (harness.SpecFiles), skipping dot-directories and
// *.golden.* IR snapshots. Exit code is 0 if every spec passes, 1 if any spec
// fails an oracle, and 2 on a usage or filesystem error, making it usable as a
// CI or script gate.
package main
