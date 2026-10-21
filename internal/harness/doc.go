// Package harness compiles specs through whichever registered compiler takes
// them and applies the bug-catching oracles (no panic/error, irverify
// invariants, JSON round-trip, determinism, order-invariance), returning a
// structured Result per spec. A compiler registered in engine.Compilers is swept
// with no change here. It also defines the annotation-retention vocabulary
// (Annotation, SiteKind, Cell) and the Cells/MissingCells helpers a per-format
// test suite uses to track, cell by cell, which annotation-at-a-site combination
// that format's compiler is known to retain. It is test/tooling infrastructure,
// not part of the compile pipeline.
package harness
