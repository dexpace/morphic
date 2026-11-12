// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

// Package schema stands for a lowering, which may not read the version a
// document declares: it asks the rule table how the version reads.
package schema

import (
	soa "github.com/speakeasy-api/openapi/openapi"

	"github.com/dexpace/morphic/internal/lintrules/testdata/planted/compilers/openapi/internal/oas"
)

// lowering holds the document, as a lowering's context does.
type lowering struct{ Doc *soa.OpenAPI }

// plantVersion reads the version through the library's field and accessor, a
// method value and expression and a field's document among them, its core
// model's field, and the table's reading, each refused. Another type's field
// of the name, and a literal writing the version, read nothing.
func plantVersion(c lowering, doc *soa.OpenAPI, m soa.Matcher, item soa.WalkItem) {
	_ = c.Doc.GetOpenAPI()            // want "use of `c.Doc.GetOpenAPI` forbidden because \"ask the rule table how a document's version reads, not the version\" (forbidigo)"
	_ = doc.OpenAPI                   // want "use of `doc.OpenAPI` forbidden because \"ask the rule table how a document's version reads, not the version\" (forbidigo)"
	_ = doc.GetOpenAPI()              // want "use of `doc.GetOpenAPI` forbidden because \"ask the rule table how a document's version reads, not the version\" (forbidigo)"
	read := (*soa.OpenAPI).GetOpenAPI // want "use of `(*soa.OpenAPI).GetOpenAPI` forbidden because \"ask the rule table how a document's version reads, not the version\" (forbidigo)"
	value := doc.GetOpenAPI           // want "use of `doc.GetOpenAPI` forbidden because \"ask the rule table how a document's version reads, not the version\" (forbidigo)"
	_, _ = oas.ParseMinor("3.0.3")    // want "use of `oas.ParseMinor` forbidden because \"ask the rule table how a document's version reads, not the version\" (forbidigo)"
	_ = doc.GetCore().OpenAPI.Value   // want "use of `doc.GetCore().OpenAPI` forbidden because \"ask the rule table how a document's version reads, not the version\" (forbidigo)"
	_, _ = m.OpenAPI, item.OpenAPI
	_ = soa.OpenAPI{OpenAPI: "3.0.3"}
	_, _ = read, value
}
