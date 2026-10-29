package pass

import (
	"fmt"

	"github.com/dexpace/morphic/ir"
)

// checkDuplicateWireNames reports wire-name collisions within a single model's
// own properties. Every model is read through the same map, so none costs one
// of its own.
func checkDuplicateWireNames(doc *ir.Document) []ir.Diagnostic {
	var diags []ir.Diagnostic
	var names reusedMap[string, bool]
	for _, id := range liveTypeIDs(doc) {
		m, ok := doc.Types[id].(*ir.Model)
		if !ok {
			continue
		}
		seen := names.emptied()
		for _, p := range m.Properties {
			name := p.EffectiveWireName()
			if name == "" {
				continue
			}
			if seen[name] {
				diags = append(diags, diag(ir.SeverityError, "pass/duplicate-wire-name",
					fmt.Sprintf("model %s has more than one property with wire name %q", id, name),
					string(id)))
				continue
			}
			seen[name] = true
		}
	}
	return diags
}

// checkAmbiguousAliases reports a property alias that another property of the
// same model also answers to, by its wire name or by an alias of its own: a
// reader resolving a writer's field by that name has two to choose from, which
// is why Avro holds an alias to the uniqueness of names. The scope is the one
// checkDuplicateWireNames holds, a model's own properties (ir-design §3.2).
//
// A source can write one, so it is a problem in the spec rather than an
// irverify violation. A repeat within one property's own names is irverify's
// (ir/naming-alias-duplicate, ir/naming-alias-redundant).
func checkAmbiguousAliases(doc *ir.Document) []ir.Diagnostic {
	var diags []ir.Diagnostic
	var claims reusedMap[string, int]
	for _, id := range liveTypeIDs(doc) {
		if m, ok := doc.Types[id].(*ir.Model); ok {
			diags = append(diags, ambiguousAliases(id, m.Properties, &claims)...)
		}
	}
	return diags
}

// ambiguousAliases is checkAmbiguousAliases over one model's properties. Every
// wire name is claimed before any alias, so an alias meeting a sibling's wire
// name is reported whichever comes first; an alias meeting a sibling's alias is
// reported at the later one. Either way the alias is reported, since it is the
// name a fix renames or removes, and the message names the sibling. The names
// are read through claims.
func ambiguousAliases(id ir.TypeID, props []ir.Property, claims *reusedMap[string, int]) []ir.Diagnostic {
	claimant := claims.emptied()
	for i, p := range props {
		if name := p.EffectiveWireName(); name != "" {
			if _, taken := claimant[name]; !taken {
				claimant[name] = i
			}
		}
	}
	var diags []ir.Diagnostic
	for i, p := range props {
		for _, alias := range p.Name.Aliases {
			j, taken := claimant[alias]
			switch {
			case alias == "" || (taken && j == i):
				// Answers to nothing, or repeats this property's own name.
			case !taken:
				claimant[alias] = i
			default:
				diags = append(diags, diag(ir.SeverityError, "pass/ambiguous-alias",
					fmt.Sprintf("alias %q of property %q is a name property %q also answers to",
						alias, p.ID, props[j].ID),
					fmt.Sprintf("%s/properties/%d", id, i)))
			}
		}
	}
	return diags
}
