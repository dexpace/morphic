// Package engine orchestrates the Morphic pipeline: it sniffs the source format,
// dispatches to the registered compiler, and runs IR passes. It is the only
// package that composes compilers and passes together.
package engine
