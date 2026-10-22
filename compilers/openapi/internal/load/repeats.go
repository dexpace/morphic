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
// and location (paramRepeats), or a server's name (serverRepeats).
//
// items is walkSource's walk, which visits each list where the source writes
// it, lowered or not, and again wherever a YAML alias mounts it, but never
// through a $ref, so a list only a $ref reaches is not checked (GitHub #880).
// The lists beside a path item's $ref are sibs's model's (writtenItem). A
// panic in the walk is reported at the model it reached.
func listRepeats(at func(jsontext.Pointer) ir.Provenance, sibs Siblings, items iter.Seq[soa.WalkItem],
) []ir.Diagnostic {
	var diags []ir.Diagnostic
	site, err := eachModel(items, "repeat check", func(site jsontext.Pointer, model any) error {
		switch m := model.(type) {
		case *soa.OpenAPI:
			// Placed from "", not site: the library renders the root's location
			// as "/", the pointer to a key named "". Read from the field, since
			// GetServers stands in a server the source never wrote.
			diags = append(diags, serverRepeats(at, m.Servers, "")...)
		case *soa.ReferencedPathItem:
			if item, ok := writtenItem(m, sibs, site); ok {
				diags = append(diags, paramRepeats(at, item.GetParameters(), site)...)
				diags = append(diags, serverRepeats(at, item.GetServers(), site)...)
			}
		case *soa.Operation:
			diags = append(diags, paramRepeats(at, m.GetParameters(), site)...)
			diags = append(diags, serverRepeats(at, m.GetServers(), site)...)
		default:
			// No other model holds a parameter or a servers list.
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
// that model was found at. The model's operations are walked from there alone,
// so its own lists are checked there alone too, wherever else an alias mounts
// it. A $ref with nothing beside it writes no list.
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
// repeat, naming the first holder. The rule table states the rule and leaves
// it here, where every entry is read through its $ref.
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

// serverRepeats reports each entry of the servers list held by the object at
// owner whose name an earlier entry already has, at the repeat's name, naming
// the first holder's. Names compare as written, and an entry without one
// repeats nothing: a URL hint is the lowering's, not a name the source wrote.
// A list shorter than two entries has nothing to compare, so most lists cost
// no map.
func serverRepeats(at func(jsontext.Pointer) ir.Provenance, servers []*soa.Server, owner jsontext.Pointer) []ir.Diagnostic {
	if len(servers) < 2 {
		return nil
	}
	firstAt := make(map[string]jsontext.Pointer, len(servers))
	var diags []ir.Diagnostic
	for i, s := range servers {
		name := s.GetName()
		if name == "" {
			continue
		}
		key := owner.AppendToken("servers").AppendToken(strconv.Itoa(i)).AppendToken("name")
		first, repeated := firstAt[name]
		if !repeated {
			firstAt[name] = key
			continue
		}
		diags = append(diags, diag.Newf(ir.SeverityWarning, diag.DuplicateServerName, at(key),
			"server name %q repeats the name at %s; both servers are kept, and a consumer "+
				"keying servers by name cannot tell them apart", name, first))
	}
	return diags
}
