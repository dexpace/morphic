// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

// Package typemap holds the type table every target maps the IR's leaf types
// through (docs/emitters/contract/type-table.md): its shape, its strict decode,
// the merge that lays a user's rows over the built-in ones, the rows an options
// version moved, and the lookup with its fallback. It holds no target's rows
// and reads no plan, since a refine helper resolves a reference to its key.
package typemap
