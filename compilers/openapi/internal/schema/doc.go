// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

// Package schema lowers OpenAPI schemas into IR types: the shape walk, its
// compositions, the references between schemas, and what the IR has no field
// for.
//
// A position is lowered from three readings, each made once a run: what its
// schema writes (statement.Table), what that admits (admissionOf) and which
// conjunct of its allOf decides it (decisionOf). It is one package because
// lowering a schema resolves its references and a composition lowers its
// branches (micro-compiler-design §5); internal/archtest pins the recursion by
// name.
//
// Only the entry points the compiler needs, and the facts a carrier lowering
// must agree on, are exported.
package schema
