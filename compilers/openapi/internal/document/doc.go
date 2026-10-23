// Package document lowers what a document says about itself rather than about
// its types or operations: its info, root externalDocs and servers, and, kept
// verbatim, what the objects around them write that lowers to no node of its
// own (docs/compilers/openapi.md#document-metadata,
// docs/compilers/openapi.md#unreferenced-components).
//
// It runs after every other lowering, since only they know which components
// they reached, and returns a Meta for the compiler to assign.
//
// Servers are lowered here while the IR holds only the document's own. A field
// for a path item's or an operation's (GitHub #285) would move their lowering
// to a package both walks reach.
package document
