// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

// Package harness runs specs through the engine as `morphic compile --verify`
// does, the default passes included, and applies the bug-catching oracles
// (Oracles), returning a Result per spec that holds each oracle's Verdict:
// what it found, or why it declined. Summary counts them. CheckCases asks the
// case oracles (CaseOracles) of a format's product of documents (Cases), two
// of them comparing each with its neighbours. It also defines the
// annotation-retention vocabulary (Annotation, SiteKind, Cell) and the
// Cells/MissingCells helpers with which a format's test suite tracks, cell by
// cell, what its compiler retains. It is test/tooling infrastructure, not part
// of the compile pipeline.
package harness
