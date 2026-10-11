// Package pass hosts Morphic's IR-to-IR passes: pure analyses and transforms
// that consume an [ir.Document] and emit diagnostics. They import only the ir
// package. A pass has no package-level mutable state and no I/O, and reports
// spec-level problems as [ir.Diagnostic] values.
//
// # Diagnostic codes
//
// A code names the defect, not the package that found it: one defect, one code,
// whichever checker a caller runs. A dangling reference is an ir/ code because
// ir/irverify reports the identical defect. Codes a pass owns outright, a
// heuristic or a policy judgement, keep the pass/ namespace.
//
// pass/dangling-auth-ref became ir/dangling-auth-ref, breaking for anyone
// matching on the code.
package pass
