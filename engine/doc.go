// Package engine orchestrates the Morphic pipeline: it asks the registered
// compilers which of them recognizes the source, dispatches to that one, and
// runs IR passes. It is the only package that composes compilers and passes
// together, and it holds no knowledge of any source format — what a spec looks
// like and what its options are called are the compiler's to answer.
package engine
