package load

import (
	"encoding/json/jsontext"
	"iter"
	"strconv"

	soa "github.com/speakeasy-api/openapi/openapi"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	refscope "github.com/dexpace/morphic/compilers/openapi/internal/resolve"
	"github.com/dexpace/morphic/ir"
)

// listRepeats reports each entry of a list written in the source that repeats
// what an earlier entry of the same list is identified by: a parameter's name
// and location (paramRepeats).
//
// items is walkSource's walk, which visits each list once where it is written,
// lowered or not, and never through a $ref, so a list only a $ref reaches is
// not checked (GitHub #880). The lists beside a path item's $ref are sibs's
// model's, checked where that model is walked from. A panic in the walk is
// reported at the model it reached.
func listRepeats(at func(jsontext.Pointer) ir.Provenance, sibs Siblings, items iter.Seq[soa.WalkItem],
) []ir.Diagnostic {
	var diags []ir.Diagnostic
	site, err := eachModel(items, "repeat check", func(site jsontext.Pointer, model any) error {
		switch m := model.(type) {
		case *soa.ReferencedPathItem:
			if item, ok := writtenItem(m, sibs, site); ok {
				diags = append(diags, paramRepeats(at, item.GetParameters(), site)...)
			}
		case *soa.Operation:
			diags = append(diags, paramRepeats(at, m.GetParameters(), site)...)
		default:
			// No other model holds a parameter list.
		}
		return nil
	})
	if err != nil {
		diags = append(diags, diag.Newf(ir.SeverityError, diag.Validation, at(site), "%s", err.Error()))
	}
	return diags
}

// writtenItem returns the path item whose lists rp writes at site: its own
// object, or the model of the fields beside its $ref when site is the position
// that model was found at, so an alias reaching those fields again is not
// checked twice. A $ref with nothing beside it writes no list.
func writtenItem(rp *soa.ReferencedPathItem, sibs Siblings, site jsontext.Pointer) (*soa.PathItem, bool) {
	if !rp.IsReference() {
		return rp.GetObject(), true
	}
	sib, ok := sibs.Of(rp)
	return sib.Item, ok && sib.Pointer == site
}

// paramSlot is what OpenAPI identifies a parameter by within one list: its name
// and location, compared as written.
type paramSlot struct {
	name, in string
}

// paramRepeats reports each entry of the parameter list held by the object at
// owner that repeats an earlier entry's slot, which OpenAPI forbids, at the
// repeat, naming the first holder. The compiler owns the rule (compilerOwned):
// the library read only entries written inline, so it missed a repeat through
// a $ref.
//
// An entry is read as the lowering reads it (refscope.Object), so a repeat
// reported here is one the lowering numbers; one whose $ref resolved to nothing
// has no slot.
func paramRepeats(at func(jsontext.Pointer) ir.Provenance, params []*soa.ReferencedParameter, owner jsontext.Pointer) []ir.Diagnostic {
	firstAt := make(map[paramSlot]jsontext.Pointer, len(params))
	var diags []ir.Diagnostic
	for i, rp := range params {
		p := refscope.Object[soa.Parameter](rp)
		if p == nil {
			continue
		}
		entry := owner.AppendToken("parameters").AppendToken(strconv.Itoa(i))
		slot := paramSlot{name: p.GetName(), in: string(p.GetIn())}
		first, repeated := firstAt[slot]
		if !repeated {
			firstAt[slot] = entry
			continue
		}
		diags = append(diags, diag.Newf(ir.SeverityError, diag.DuplicateParameter, at(entry),
			"parameter %q in %q repeats the entry at %s", slot.name, slot.in, first))
	}
	return diags
}
