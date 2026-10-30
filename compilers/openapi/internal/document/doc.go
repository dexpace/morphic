// Package document lowers what a document says about itself rather than its
// types or operations: its info and root externalDocs, and, kept
// verbatim, what the objects around them write that lowers to no node
// (docs/compilers/openapi.md#document-metadata,
// docs/compilers/openapi.md#unreferenced-components).
//
// It runs after every other lowering, which alone know the components they
// reached, and returns a Meta for the compiler to assign. The document's
// servers are not lowered here: the service names them, so the service walk
// lowers them, through the package a path item's and an operation's go
// through too (servers).
package document
