package load

import (
	"encoding/json/jsontext"
	"iter"

	soa "github.com/speakeasy-api/openapi/openapi"

	"github.com/dexpace/morphic/compilers/openapi/internal/bind"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/ir"
)

// besideRefs reports, at the reference, each one items holds whose chain passes
// through the fields a path item writes beside its $ref (Siblings.Beside). The
// resolver read it through the $ref, while the lowering declares the field
// written there at that very pointer, so taking its answer would give one
// pointer two declarations. The lowering refuses it (resolve.Scope.Beside),
// and this is the report.
//
// items is walkSource's walk. A site where resolved holds the resolver's own
// failure is not reported again: by #767 it can fail a reference it later
// reached. A panic in the walk is reported at the model it reached.
func besideRefs(at func(jsontext.Pointer) ir.Provenance, sibs Siblings, items iter.Seq[soa.WalkItem],
	resolved []ir.Diagnostic,
) []ir.Diagnostic {
	if len(sibs.list) == 0 {
		return nil
	}
	failed := map[ir.Provenance]bool{}
	for _, d := range resolved {
		if d.Code == diag.UnresolvedRef || d.Code == diag.CyclicRef {
			failed[d.Provenance] = true
		}
	}
	var diags []ir.Diagnostic
	site, err := eachModel(items, "path item $ref check", func(site jsontext.Pointer, model any) error {
		if d, beside := besideRef(at, site, sibs, model); beside && !failed[d.Provenance] {
			diags = append(diags, d)
		}
		return nil
	})
	if err != nil {
		diags = append(diags, diag.Newf(ir.SeverityError, diag.Validation, at(site), "%s", err.Error()))
	}
	return diags
}

// besideRef reports the reference object model written at site when a hop of
// its chain passes beside a path item's $ref and finds something other than
// the field written there (Siblings.Writes). The chain is read as the lowering
// reads it (resolve.ObjectAt): hop by hop while each lands in the source. A
// schema's $ref is no reference object; the binding reports its own (bind.Hop).
func besideRef(at func(jsontext.Pointer) ir.Provenance, site jsontext.Pointer, sibs Siblings, model any,
) (ir.Diagnostic, bool) {
	r, isRef := model.(resolvable)
	if !isRef || !r.IsReference() {
		return ir.Diagnostic{}, false
	}
	for _, hop := range resolutionChain(model).records {
		if _, source := (*hop.document).(*soa.OpenAPI); !source {
			break
		}
		if use, key, beside := sibs.Beside(hop.pointer); beside && !sibs.Writes(hop.pointer, hop.object) {
			return diag.Newf(ir.SeverityError, diag.UnresolvedRef, at(site), "unresolved $ref %q: it reaches %s; %s",
				r.GetReference(), hop.pointer, bind.BesideReason(use, key)), true
		}
	}
	return ir.Diagnostic{}, false
}
