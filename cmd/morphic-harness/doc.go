// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

// Command morphic-harness sweeps API specs through Morphic's bug-catching
// oracles (harness.Check), each spec run as `morphic compile --verify` runs it,
// and writes a line per finding and what each oracle asked and declined
// (harness.Summary). Each argument is a spec file or a directory, walked for
// the files a registered compiler claims (harness.SpecFiles). With -cases, it
// checks each format's schema cases instead (harness.CheckCases), reporting
// what the format's records do not explain; with -at N, the case at index N,
// its sources and verdicts. It exits 0 when nothing is found, 1 when anything
// is, and 2 on a usage or filesystem error.
package main
