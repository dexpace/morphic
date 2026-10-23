// Package openapi lowers OpenAPI 3.0/3.1/3.2 documents into the Morphic IR. It
// implements compilers.Compiler.
//
// This package is the compiler's public face and the assembly behind it: the
// Compiler, its Options and the text vocabulary DecodeOptions reads, and the
// run that calls the lowerings in order and builds a Document from what they
// return.
//
// Parsing is delegated to github.com/speakeasy-api/openapi. Recognizing a
// source from its bytes, loading it and the lowering itself (identity,
// hoisting, normalization, and lossless preservation of constructs the IR does
// not model structurally) live in the packages under internal/, each of which
// states its own place in the order.
package openapi
