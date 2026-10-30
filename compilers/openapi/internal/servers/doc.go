// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

// Package servers lowers the servers a document declares, whichever object
// declares them, into one registry keyed by ID, and names the ones a scope uses
// (docs/compilers/openapi.md#servers, docs/compilers/openapi.md#path-item-servers).
//
// It is its own package because the service walk and the operation walk both
// reach it: the first lowers the document's own list, the second a path item's
// and an operation's, each into the Registry the compiler's run assigns to
// Document.Servers.
package servers
