// Package openapi lowers OpenAPI 3.0/3.1/3.2 documents into the Morphic IR. It
// implements compilers.Compiler. Parsing is delegated to
// github.com/speakeasy-api/openapi; this package owns identity (pointer-derived
// IDs), hoisting, normalization (nullable spellings, allOf classification), and
// lossless preservation of constructs the IR does not model structurally.
package openapi
