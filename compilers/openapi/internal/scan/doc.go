// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

// Package scan refuses a source document before any of it is lowered, for
// shapes the parser library cannot decode: a YAML alias cycle or fan-out,
// which overflows its stack or exhausts memory
// (docs/parser-library.md#alias-cycle, docs/parser-library.md#alias-expansion),
// and nesting so deep that its cost per node, growing with depth, adds up past
// use (docs/parser-library.md#nesting-cost). Each is read from the raw text,
// through the sourceindex.Index the caller supplies. A $ref is no concern of
// it: the compiler resolves schema $refs itself (internal/bind), and the
// parser's resolver reports a loop of reference objects
// (docs/parser-library.md#reference-object-cycle).
package scan
