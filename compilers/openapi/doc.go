// Package openapi lowers OpenAPI 3.0/3.1/3.2 documents into the Morphic IR. It
// implements compilers.Compiler.
//
// What is here is the compiler's public face and the assembly behind it: the
// Compiler, its Options, the document metadata, and the run that calls the
// lowerings in order and builds a Document out of what they return.
//
// Parsing is delegated to github.com/speakeasy-api/openapi. Everything the
// compiler itself decides — identity (pointer-derived IDs), hoisting,
// normalization (nullable spellings, allOf classification), and the lossless
// preservation of constructs the IR does not model structurally — lives in the
// packages under internal/, each of which states its own place in the order.
package openapi
