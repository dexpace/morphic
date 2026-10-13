// Package decls type-checks packages' production sources for tests that hold a
// hand-written list to what the source declares: an enum's members, a sealed
// sum's kinds, the fields of a given shape.
//
// It reads through golang.org/x/tools/go/packages and go/types, so a constant
// typed through an alias, written as an expression or converted from an
// untyped one is read as the compiler reads it rather than as a parser would
// have to guess. It is test infrastructure outside the compile pipeline.
package decls
