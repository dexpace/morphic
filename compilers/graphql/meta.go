package graphql

import (
	"encoding/json"

	"github.com/vektah/gqlparser/v2/ast"

	"github.com/dexpace/morphic/ir"
)

// lowerMeta lowers document-level metadata not part of the type or service
// graph: the schema-level docs, the directive-definition inventory, the
// federation version, and the schema-block directive applications (@link).
func (l *lowerer) lowerMeta() {
	l.out.Docs = docsFrom(l.schemaDescription())
	ext := l.documentExtensions()
	if len(ext) > 0 {
		l.out.Extensions = ext
	}
}

// documentExtensions assembles the document-level extension map from the
// directive inventory, the federation version, and the schema-block directives.
func (l *lowerer) documentExtensions() ir.Extensions {
	ext := ir.Extensions{}
	if !l.opts.OmitDirectiveDefinitions {
		if defs := directiveDefinitionsJSON(l.doc.Directives); defs != nil {
			ext["graphql:directive-definitions"] = defs
		}
	}
	if l.fedVersion != "" {
		raw, _ := json.Marshal(l.fedVersion)
		ext["federation:version"] = raw
	}
	return mergeExtensions(ext, l.schemaDirectiveExtensions())
}

// schemaDirectiveExtensions lowers the directive applications on every schema
// definition and extension block (federation v2's @link lives here) into
// namespaced extensions.
func (l *lowerer) schemaDirectiveExtensions() ir.Extensions {
	var out ir.Extensions
	for _, block := range l.schemaBlocks() {
		out = mergeExtensions(out, lowerDirectives(block.Directives))
	}
	return out
}

// schemaBlocks returns every schema definition and extension block in source
// order.
func (l *lowerer) schemaBlocks() ast.SchemaDefinitionList {
	blocks := make(ast.SchemaDefinitionList, 0, len(l.doc.Schema)+len(l.doc.SchemaExtension))
	blocks = append(blocks, l.doc.Schema...)
	blocks = append(blocks, l.doc.SchemaExtension...)
	return blocks
}
