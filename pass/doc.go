// Package pass hosts Morphic's IR-to-IR passes: pure analyses and transforms
// that consume an [ir.Document] and emit diagnostics (or, in later passes, a
// rewritten document).
//
// Passes import only the ir package — the layering rule keeps them blind to
// compilers, emitters, and the engine. Every pass is f(input) -> output with no
// package-level mutable state and no I/O; spec-level problems surface as
// [ir.Diagnostic] values, never as panics or stderr writes.
//
// # Diagnostic codes
//
// A code is namespaced for the defect, not for the package that found it. A
// dangling reference is an ir/ code because ir/irverify reports the identical
// defect and the two must agree: one defect, one code, whichever checker a
// caller runs. Codes a pass owns outright — a heuristic or a policy judgement —
// keep the pass/ namespace.
//
// That rule renamed pass/dangling-auth-ref to ir/dangling-auth-ref. A code is a
// stable string a caller may match on, so this is a breaking change for anyone
// filtering diagnostics by it, and it is the only code the rule moved.
package pass
