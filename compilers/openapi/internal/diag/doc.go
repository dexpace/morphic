// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

// Package diag holds the OpenAPI compiler's diagnostic vocabulary: the stable
// codes it reports under, the single constructor that builds a diagnostic from
// them, and how one quotes a dependency's error or panic.
//
// It sits at the bottom of the compiler's import graph because every package
// that reports anything reaches it, and it reaches only ir and barrier. The
// codes are format-specific strings and stay here rather than promoting to
// compilers/compile — that rule wants evidence from all three compilers, and
// the GraphQL and Protobuf drafts have not been compared. Whether Newf itself
// later promotes is left open for the same reason.
package diag
