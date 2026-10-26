// Package harness runs specs through the engine as `morphic compile` does, the
// default passes included, and applies the bug-catching oracles (no
// panic/error, irverify invariants, JSON round-trip, determinism, pass
// idempotence, order-invariance), returning a structured Result per spec. A
// compiler registered in engine.Compilers, or a pass in engine.Passes, is swept
// with no change here. It also defines the annotation-retention vocabulary
// (Annotation, SiteKind, Cell) and the Cells/MissingCells helpers a per-format
// test suite uses to track, cell by cell, which annotation-at-a-site combination
// that format's compiler is known to retain. It is test/tooling infrastructure,
// not part of the compile pipeline.
package harness
