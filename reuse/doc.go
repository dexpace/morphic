// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

// Package reuse holds scratch state a check reads one collection after another
// through, so a collection costs no allocation of its own. It knows no morphic
// concept and imports only the standard library, so both checkers, irverify and
// pass, share one copy of it rather than one each.
package reuse
