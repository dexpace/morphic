// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package document

import (
	"github.com/dexpace/morphic/compilers/openapi/internal/annotation"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/ids"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/compilers/openapi/internal/nodeview"
	"github.com/dexpace/morphic/ir"
)

// retainedSections are the components sections with no registry. Schemas and
// security schemes are lowered whole and unconditionally; an entry of one of
// these reaches the IR only where a lowering follows a reference to it.
// mediaTypes is OpenAPI 3.2's, which the parser does not model and the load
// phase does (load.MediaTypes).
func retainedSections() []string {
	return []string{"responses", "parameters", "examples", "requestBodies", "headers", "links", "callbacks",
		"pathItems", "mediaTypes"}
}

// retainUnreferencedComponents keeps every entry of a retainedSections section
// that no lowering resolved a reference to verbatim on the document, under
// openapi:components/<section>/<name> with ReasonNoIRHome and no diagnostic
// (GitHub #616). It must run after every other lowering: only they know what
// they followed (lowering.Ctx.Reached), and a $ref string they never follow,
// in a links map, an x- extension or an example's value, carries nothing of
// its target into the IR. A section is read as the parser reads it, through
// `<<` merge keys, so a merged-in entry is kept under its own name.
func retainUnreferencedComponents(c lowering.Ctx) (ir.Unmodeled, []ir.Diagnostic) {
	view := nodeview.New()
	components := view.ChildByToken(nodeview.DocumentRoot(c.Doc.GetRootNode()), "components")
	var out ir.Unmodeled
	var diags []ir.Diagnostic
	for _, section := range retainedSections() {
		for _, entry := range view.MappingPairs(view.ChildByToken(nodeview.Deref(components), section)) {
			pointer := ids.Ptr("components", section, entry.Key)
			if c.Reached(pointer) {
				continue
			}
			_, keptDiags := annotation.PreserveNodeInto(&out, "openapi:"+ids.Scope("components", section, entry.Key),
				entry.Val, ir.ReasonNoIRHome, c.ProvenanceAt(pointer))
			diags = append(diags, keptDiags...)
		}
	}
	if view.Exhausted() {
		diags = append(diags, c.DiagAt(ir.SeverityWarning, diag.DegradedConstruct, ids.Ptr("components"),
			"components merges through `<<` deeper than %d levels; entries past that are not kept", nodeview.MergeDepthLimit))
	}
	return out, diags
}
