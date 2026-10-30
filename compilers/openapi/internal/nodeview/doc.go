// Package nodeview is the compiler's one reading of a raw YAML tree: a mapping
// read the way the parser reads it, through aliases and `<<` merge keys, each
// key and each key written twice settled as the parser's model settles them
// (docs/parser-library.md#merge-keys, #duplicate-key, #non-scalar-key), and
// one bounded, iterative walk of a tree as written (Walk).
//
// A reader that wants the parser's reading takes it from here, so no two
// disagree; one mirroring another reader on purpose, as load.treeReads
// mirrors GetTarget, says so. It is neither a scan nor a lowering, so it sits
// below both.
package nodeview
