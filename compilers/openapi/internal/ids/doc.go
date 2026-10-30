// Package ids builds the RFC 6901 pointer that names a position — a
// jsontext.Pointer from construction on, read back through its methods, never
// split on '/' — and derives IR identifiers and namespaces from it.
//
// The path is OpenAPI's and stays here — a JSON Pointer is not a GraphQL
// structural path or a protobuf fully-qualified name, and nothing above this
// package can compute one. The grammar wrapped around it belongs to the
// framework, which is the only thing this package reaches besides ir.
package ids
