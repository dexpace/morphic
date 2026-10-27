// Package ir defines Morphic's spec-agnostic intermediate representation: the
// single contract between compilers and emitters.
//
// docs/ir-design.md specifies the shapes normatively; field names and struct
// layouts must match it. Named entities live in flat registries on
// [Document], keyed and cross-referenced by ID, and a Document round-trips
// through JSON deterministically. Passes edit types with an [Editor].
//
// It also owns shared traversals and helpers: [WalkValues], the bounded,
// cycle-guarded, deterministic reflection walk; [IDReferences]; the typed
// [ForEachOperation] and [TypeEdges]; [AliasBase], [Supertypes], [IsSubtype],
// [ExposedProps] and [FlattenedProperties] over composition; [AsMap].
//
// It imports only the standard library and does no I/O; its plain-data types
// are safe for concurrent reads.
package ir
