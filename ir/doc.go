// Package ir defines Morphic's spec-agnostic intermediate representation: the
// single contract between spec compilers and generator emitters.
//
// The shapes are normatively specified in docs/ir-design.md; field names and
// struct layouts must match it. Named entities live in flat registries on
// [Document], keyed and cross-referenced by ID, and a Document round-trips
// through JSON deterministically.
//
// The package also owns the shared traversals: [WalkValues], the bounded,
// cycle-guarded, deterministically ordered reflection walk, and
// [DocumentRegistries], which derives what counts as a resolvable reference
// from Document's own shape.
//
// It imports only the standard library and does no I/O; its plain-data types
// are safe for concurrent reads.
package ir
