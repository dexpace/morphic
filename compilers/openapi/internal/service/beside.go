// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package service

import (
	"encoding/json/jsontext"
	"strconv"

	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/annotation"
	"github.com/dexpace/morphic/compilers/openapi/internal/componentlookup"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/nodeview"
	"github.com/dexpace/morphic/ir"
)

// Beside returns what the Reference Objects of ref's chain write beside their
// $ref that the use does not read, keyed "openapi:reference/<i>/<key>": i is 0
// for the use written at usePtr, then counts the aliases the chain passes,
// nearest first. folds says the use folds summary and description into docs.
func Beside[T, S any, R chainable[T, S]](serviceContext Ctx, ref R, usePtr jsontext.Pointer, folds bool) (ir.Unmodeled, []ir.Diagnostic) {
	if _, isRef := serviceContext.Bindings().Reference(ref); !isRef {
		return nil, nil // an inline entry writes no $ref to stand beside
	}
	var out ir.Unmodeled
	diags := keepBeside(&out, serviceContext, writtenRoot(ref.GetCoreAny()), usePtr, 0, folds)
	for i, hop := range componentlookup.Hops[T, S](serviceContext.Bindings(), ref) {
		in, _ := InDocument(serviceContext, hop.At.Doc)
		diags = append(diags, keepBeside(&out, in, writtenRoot(hop.Ref.GetCoreAny()), hop.At.Pointer, i+1, folds)...)
	}
	return out, diags
}

// keepBeside keeps on out every key the mapping node, written at at in
// serviceContext's document, writes beside its $ref, as the model reads the
// mapping, under openapi:reference/<i>/: summary and description only where the
// use does not fold them, as documentation with no home, each reported; any
// other as a key OpenAPI gives no effect, which the rule table reports.
func keepBeside(out *ir.Unmodeled, serviceContext Ctx, node *yaml.Node, at jsontext.Pointer, i int, folds bool) []ir.Diagnostic {
	if !writesBeside(nodeview.Deref(node)) {
		return nil
	}
	scope := "reference/" + strconv.Itoa(i)
	var diags []ir.Diagnostic
	for _, pair := range nodeview.Pairs(node) {
		docs := pair.Key == "summary" || pair.Key == "description"
		if pair.Key == "$ref" || (docs && folds) {
			continue
		}
		reason, keyAt := ir.ReasonOutOfScope, at.AppendToken(pair.Key)
		if docs {
			reason = ir.ReasonNoIRHome
		}
		kept, keptDiags := annotation.PreserveNodeInto(out, annotation.EntryKey(scope, pair.Key), pair.Val, reason,
			serviceContext.Place().At(keyAt))
		diags = append(diags, keptDiags...)
		if kept && docs {
			diags = append(diags, serviceContext.DiagAt(ir.SeverityInfo, diag.UnhomedConstruct, keyAt,
				"%s beside this $ref documents the use, which has no docs to hold it; kept verbatim under Unmodeled",
				pair.Key))
		}
	}
	return diags
}

// writesBeside reports whether the mapping node writes a key other than $ref, a
// merge key among them, read off its own pairs: a bare reference, the common
// case, is answered without building a view.
func writesBeside(node *yaml.Node) bool {
	if node == nil || node.Kind != yaml.MappingNode {
		return false
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		if keyNode := expanded(node.Content[i]); keyNode == nil || keyNode.Value != "$ref" {
			return true
		}
	}
	return false
}

// writtenRoot returns the node a model's core was read from, as the library's
// own GetRootNode reads it, and nil for a core holding none.
func writtenRoot(core any) *yaml.Node {
	if c, ok := core.(interface{ GetRootNode() *yaml.Node }); ok {
		return c.GetRootNode()
	}
	return nil
}
