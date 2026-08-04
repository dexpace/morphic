// Package ir defines Morphic's spec-agnostic intermediate representation: the
// single contract between spec compilers and generator emitters.
//
// The shapes in this package are normatively specified in docs/ir-design.md;
// field names and struct layouts must match that document exactly. All named
// entities live in flat, ID-keyed registries on [Document] and reference each
// other by ID. The whole Document round-trips through JSON deterministically.
//
// Besides the nodes, it owns the traversal every consumer inspecting a whole
// document needs: [WalkValues] is the one bounded, cycle-guarded,
// deterministically-ordered reflection walk, and [DocumentRegistries] derives
// what counts as a resolvable reference from Document's own shape. Both live
// here because a second copy of either is a second answer to keep in step with
// the first.
//
// This package imports only the standard library. It contains no parsing, no
// generation, and no I/O. All types are plain data and safe for concurrent
// reads; nothing in this package mutates package-level state.
package ir
