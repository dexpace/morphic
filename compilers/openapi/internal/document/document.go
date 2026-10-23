package document

import (
	"github.com/dexpace/morphic/compilers/openapi/internal/annotation"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/ir"
)

// Meta is the document-level metadata: what ir.Document carries that belongs
// to neither the type graph nor the service graph.
//
// It exists so the lowering can return what it read instead of writing into the
// document as it goes. The caller owns the document and does the assigning,
// which is what lets every function below be checked against a literal context
// and nothing else.
type Meta struct {
	Name           string
	Version        string
	TermsOfService string
	Docs           ir.Docs
	Contact        *ir.Contact
	License        *ir.License
	Servers        []ir.Server
	Unmodeled      ir.Unmodeled
}

// Lower lowers the document-level metadata that is not part of the type or
// service graph: info, servers, and the extensions of every object around them
// that lowers to no node of its own (ir-design §10, §12).
func Lower(c lowering.Ctx) (Meta, []ir.Diagnostic) {
	m := lowerInfo(c)

	ext, diags := documentExtensions(c)
	m.Unmodeled = ext

	retained, retainDiags := retainUnreferencedComponents(c)
	m.Unmodeled = annotation.MergeUnmodeled(m.Unmodeled, retained)
	diags = append(diags, retainDiags...)

	servers, serverDiags := lowerServers(c)
	m.Servers = servers
	diags = append(diags, serverDiags...)

	diags = append(diags, keepComponentMediaTypes(c, &m.Unmodeled)...)
	diags = append(diags, keepJSONSchemaDialect(c, &m.Unmodeled)...)
	return m, append(diags, documentUnknownKeys(c, &m.Unmodeled)...)
}
