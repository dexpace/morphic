// Package decls type-checks packages' production sources for tests that hold
// a hand-written list, such as a sealed sum's kinds, or a name's uses to what
// the source declares.
//
// It reads through golang.org/x/tools/go/packages and go/types, so a constant
// typed through an alias, written as an expression or converted from an
// untyped one is read as the compiler reads it, not as a parser would guess.
// It reads a package whole or not at all: a Go file the build leaves out, or a
// pattern naming nothing, is an error rather than a shorter answer.
package decls
