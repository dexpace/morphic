// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package lowering

import (
	"encoding/json/jsontext"
	"strconv"

	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/annotation"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/nodeview"
	"github.com/dexpace/morphic/compilers/openapi/internal/resolve"
	"github.com/dexpace/morphic/ir"
)

// Beside returns what the Reference Objects of ref's chain write beside their
// $ref that the use does not read, keyed "openapi:reference/<i>/<key>": i is 0
// for the use written at usePtr, then counts the aliases the chain passes,
// nearest first. folds says the use folds summary and description into docs.
func Beside[T, S any, R chainable[T, S]](c Ctx, ref R, usePtr jsontext.Pointer, folds bool) (ir.Unmodeled, []ir.Diagnostic) {
	if _, isRef := c.Bindings().Reference(ref); !isRef {
		return nil, nil // an inline entry writes no $ref to stand beside
	}
	var out ir.Unmodeled
	diags := keepBeside(&out, c, writtenRoot(ref.GetCoreAny()), usePtr, 0, folds)
	for i, hop := range resolve.Hops[T, S](c.Bindings(), ref) {
		in, _ := InDocument(c, hop.At.Doc)
		diags = append(diags, keepBeside(&out, in, writtenRoot(hop.Ref.GetCoreAny()), hop.At.Pointer, i+1, folds)...)
	}
	return out, diags
}

// keepBeside keeps on out every key the mapping n, written at at in c's
// document, writes beside its $ref, as the model reads the mapping, under
// openapi:reference/<i>/: summary and description only where the use does not
// fold them, as documentation with no home, each reported; any other as a key
// OpenAPI gives no effect, which the rule table reports.
func keepBeside(out *ir.Unmodeled, c Ctx, n *yaml.Node, at jsontext.Pointer, i int, folds bool) []ir.Diagnostic {
	if !writesBeside(nodeview.Deref(n)) {
		return nil
	}
	scope := "reference/" + strconv.Itoa(i)
	var diags []ir.Diagnostic
	for _, p := range nodeview.Pairs(n) {
		docs := p.Key == "summary" || p.Key == "description"
		if p.Key == "$ref" || (docs && folds) {
			continue
		}
		reason, keyAt := ir.ReasonOutOfScope, at.AppendToken(p.Key)
		if docs {
			reason = ir.ReasonNoIRHome
		}
		kept, keptDiags := annotation.PreserveNodeInto(out, annotation.EntryKey(scope, p.Key), p.Val, reason,
			c.Place().At(keyAt))
		diags = append(diags, keptDiags...)
		if kept && docs {
			diags = append(diags, c.DiagAt(ir.SeverityInfo, diag.UnhomedConstruct, keyAt,
				"%s beside this $ref documents the use, which has no docs to hold it; kept verbatim under Unmodeled",
				p.Key))
		}
	}
	return diags
}

// writesBeside reports whether the mapping n writes a key other than $ref, a
// merge key among them, read off its own pairs: a bare reference, the common
// case, is answered without building a view.
func writesBeside(n *yaml.Node) bool {
	if n == nil || n.Kind != yaml.MappingNode {
		return false
	}
	for i := 0; i+1 < len(n.Content); i += 2 {
		if k := expanded(n.Content[i]); k == nil || k.Value != "$ref" {
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
