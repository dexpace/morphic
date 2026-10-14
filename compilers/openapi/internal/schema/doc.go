// Package schema lowers OpenAPI schemas into IR types: the shape walk, its
// compositions, the references between schemas, and the preservation of what
// the IR has no field for.
//
// It is one package because those are one cycle: lowering a schema resolves its
// references, resolving a reference lowers what it names, and a composition
// lowers its branches (micro-compiler-design §5). internal/archtest pins the
// mutual recursion by name.
//
// Only the entry points the rest of the compiler needs, and the few facts a
// carrier lowering must agree on, are exported, so no caller can enter the walk
// halfway down.
package schema
