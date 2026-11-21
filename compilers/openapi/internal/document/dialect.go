// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package document

import (
	"github.com/dexpace/morphic/compilers/openapi/internal/annotation"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/ids"
	"github.com/dexpace/morphic/compilers/openapi/internal/nodeview"
	"github.com/dexpace/morphic/compilers/openapi/internal/service"
	"github.com/dexpace/morphic/ir"
)

// keepJSONSchemaDialect keeps the document's jsonSchemaDialect verbatim on the
// document, with an info. It is the default `$schema` of the document's Schema
// Objects, and the IR models no dialect, so it is kept as a schema's own
// `$schema` is (annotation.DialectKeywords()): ReasonOutOfScope, read only for
// the dialect a schema naming none of its own is read in.
func keepJSONSchemaDialect(serviceContext service.Ctx, unmodeled *ir.Unmodeled) []ir.Diagnostic {
	at := ids.Ptr("jsonSchemaDialect")
	node := nodeview.Child(serviceContext.Doc.GetRootNode(), "jsonSchemaDialect")
	kept, diags := annotation.PreserveNodeInto(unmodeled, annotation.EntryKey("", "jsonSchemaDialect"), node,
		ir.ReasonOutOfScope, serviceContext.Place().At(at))
	if !kept {
		return diags
	}
	return append(diags, serviceContext.DiagAt(ir.SeverityInfo, diag.DegradedConstruct, at,
		"jsonSchemaDialect names the JSON Schema dialect of the document's schemas; the IR models no "+
			"dialect, so it is kept verbatim under Unmodeled and read only to choose the dialect of each "+
			"schema of a 3.1 or 3.2 document that names none of its own"))
}
