// Package pass hosts Morphic's IR-to-IR passes: pure analyses and transforms
// that consume an [ir.Document] and emit diagnostics (or, in later passes, a
// rewritten document).
//
// Passes import only the ir package — the layering rule keeps them blind to
// compilers, emitters, and the engine. Every pass is f(input) -> output with no
// package-level mutable state and no I/O; spec-level problems surface as
// [ir.Diagnostic] values, never as panics or stderr writes.
package pass
