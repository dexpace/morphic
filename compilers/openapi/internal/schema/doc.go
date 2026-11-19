// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

// Package schema lowers OpenAPI schemas into IR types: shapes, compositions,
// references, and what the IR has no field for.
//
// A position is lowered from three readings, each made once a run: what its
// schema writes (statement.MemoizingSchemaReader), what that admits
// (admissionOf) and which conjunct of its allOf decides it (decisionOf). A
// node's build says which keywords it read (keySet); the rest the node keeps
// verbatim (keepUnread). It is one package because lowering a schema resolves
// its references and a composition lowers its branches (micro-compiler-design
// §5).
//
// It exports only the compiler's entry points and the facts a carrier lowering
// must agree on.
package schema
