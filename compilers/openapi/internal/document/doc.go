// Package document lowers what a document says about itself rather than its
// types or operations: its info, root externalDocs and servers, and, kept
// verbatim, what the objects around them write that lowers to no node
// (docs/compilers/openapi.md#document-metadata,
// docs/compilers/openapi.md#unreferenced-components).
//
// It runs after every other lowering, which alone know the components they
// reached, and returns a Meta for the compiler to assign.
//
// Servers are lowered here while the IR holds only the document's. A field for
// a path item's or an operation's (GitHub #285) may move their lowering to a
// package both walks reach, or have the operation walk reach this one.
package document
