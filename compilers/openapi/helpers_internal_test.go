// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package openapi

import "github.com/dexpace/morphic/ir"

// componentID is the stable TypeID of a components-named schema, or of a
// sub-schema beneath one ("Holder/properties/inner").
//
// This one stays a per-package copy where the rest of the vocabulary moved to
// openapitest: the idderivations rule permits a spelled-out ID in a test
// file and refuses one in a production file, and openapitest's files are
// production files. Restating the grammar independently of the compiler is what
// makes these lookups an oracle rather than a tautology, so deriving it through
// compile to satisfy the sweep would be the wrong way out.
func componentID(name string) ir.TypeID {
	return ir.TypeID("t/openapi/components/schemas/" + name)
}

// typeByName returns the named component schema's lowered TypeDef.
func typeByName(doc *ir.Document, name string) ir.TypeDef {
	return doc.Types[componentID(name)]
}
