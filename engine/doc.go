// Package engine orchestrates the Morphic pipeline: it sniffs the source format,
// dispatches to the registered frontend, and runs IR passes. It is the only
// package that composes frontends and passes together.
package engine
