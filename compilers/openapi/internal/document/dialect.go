package document

import (
	"github.com/dexpace/morphic/compilers/openapi/internal/annotation"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/ids"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/compilers/openapi/internal/nodeview"
	"github.com/dexpace/morphic/ir"
)

// keepJSONSchemaDialect keeps the document's jsonSchemaDialect verbatim on the
// document, with an info. It is the default `$schema` of the document's Schema
// Objects, and the IR models no dialect, so it is kept as a schema's own
// `$schema` is (annotation.DialectKeywords): ReasonOutOfScope, and every schema
// read in the OpenAPI dialect whatever it names.
func keepJSONSchemaDialect(c lowering.Ctx, p *ir.Unmodeled) []ir.Diagnostic {
	at := ids.Ptr("jsonSchemaDialect")
	node := nodeview.Child(c.Doc.GetRootNode(), "jsonSchemaDialect")
	kept, diags := annotation.PreserveNodeInto(p, annotation.EntryKey("", "jsonSchemaDialect"), node,
		ir.ReasonOutOfScope, c.ProvenanceAt(at))
	if !kept {
		return diags
	}
	return append(diags, c.DiagAt(ir.SeverityInfo, diag.DegradedConstruct, at,
		"jsonSchemaDialect names the JSON Schema dialect of the document's schemas; the IR models no "+
			"dialect, so it is kept verbatim under Unmodeled and every schema is read in the OpenAPI dialect"))
}
