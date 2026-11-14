// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

// Package pass hosts Morphic's IR-to-IR passes, each a [Pass]: pure analyses
// and transforms over an [ir.Document]. They import only ir and ir/irfacts. A
// pass has no package-level mutable state and no I/O, and reports spec-level
// problems as [ir.Diagnostic] values.
//
// # Diagnostic codes
//
// A code names the defect, not the package that found it: one defect, one code,
// whichever checker a caller runs. A dangling or empty reference is an ir/ code
// because ir/irverify reports it too. A heuristic or a policy judgement keeps
// the pass/ namespace.
//
// pass/dangling-auth-ref became ir/dangling-auth-ref, and pass/param-binding-mismatch
// at warning became pass/param-unbound, breaking for anyone matching either.
package pass
