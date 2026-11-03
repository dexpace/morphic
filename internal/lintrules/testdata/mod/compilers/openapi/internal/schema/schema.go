// Package schema stands for a lowering, which may not read the version a
// document declares: it asks the rule table.
package schema

import (
	soa "github.com/speakeasy-api/openapi/openapi"

	"github.com/dexpace/morphic/internal/lintrules/testdata/mod/compilers/openapi/internal/load"
	"github.com/dexpace/morphic/internal/lintrules/testdata/mod/compilers/openapi/internal/oas"
)

// document names the library's type otherwise, so an embedding of it is a
// field named document and the version field is promoted past it.
type document = soa.OpenAPI

// wrapped promotes the library's document field and method.
type wrapped struct{ *document }

func plantVersion(doc *soa.OpenAPI, w wrapped, m soa.Matcher, item soa.WalkItem) {
	_ = doc.OpenAPI                   // want "versionreadings: soa.OpenAPI.OpenAPI reads the OpenAPI version a document declares outside load and oas; ask the rule table, through lowering.Ctx.SchemaRules"
	_ = doc.GetOpenAPI()              // want "versionreadings: soa.OpenAPI.GetOpenAPI reads the OpenAPI version a document declares outside load and oas; ask the rule table, through lowering.Ctx.SchemaRules"
	read := (*soa.OpenAPI).GetOpenAPI // want "versionreadings: soa.OpenAPI.GetOpenAPI reads the OpenAPI version a document declares outside load and oas; ask the rule table, through lowering.Ctx.SchemaRules"
	value := doc.GetOpenAPI           // want "versionreadings: soa.OpenAPI.GetOpenAPI reads the OpenAPI version a document declares outside load and oas; ask the rule table, through lowering.Ctx.SchemaRules"
	_ = w.OpenAPI                     // want "versionreadings: soa.OpenAPI.OpenAPI reads the OpenAPI version a document declares outside load and oas; ask the rule table, through lowering.Ctx.SchemaRules"
	_ = w.GetOpenAPI()                // want "versionreadings: soa.OpenAPI.GetOpenAPI reads the OpenAPI version a document declares outside load and oas; ask the rule table, through lowering.Ctx.SchemaRules"
	_, _ = oas.ParseMinor("3.0.3")    // want "versionreadings: oas.ParseMinor reads the OpenAPI version a document declares outside load and oas; ask the rule table, through lowering.Ctx.SchemaRules"
	_, _ = load.SupportedMinor(doc)   // want "versionreadings: load.SupportedMinor reads the OpenAPI version a document declares outside load and oas; ask the rule table, through lowering.Ctx.SchemaRules"
	_, _ = m.OpenAPI, item.OpenAPI
	_, _ = read, value
}
