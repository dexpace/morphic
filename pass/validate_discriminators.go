// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package pass

import (
	"fmt"
	"slices"

	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/ir/irfacts"
)

// checkDiscriminators reports discriminator mappings whose target either does
// not exist or is not a legal variant/subtype, and a model discriminator whose
// tag property the model does not expose. facts indexes doc.
func checkDiscriminators(doc *ir.Document, facts *irfacts.Index) []ir.Diagnostic {
	var diags []ir.Diagnostic
	var read mappingReads
	for _, id := range liveTypeIDs(doc) {
		switch t := doc.Types[id].(type) {
		case *ir.Union:
			diags = append(diags, checkUnionDiscriminator(doc, t, &read)...)
		case *ir.Model:
			diags = append(diags, checkModelDiscriminator(doc, facts, t, &read)...)
		default:
			// Only Union and Model can declare a discriminator; every other
			// TypeDef kind has nothing to check. A new discriminator-bearing
			// kind must add a case above rather than fall through here.
		}
	}
	return diags
}

// mappingReads is what checkDiscriminators reads each discriminator through,
// so none costs a collection of its own: a union's variant targets, and a
// mapping's keys, sorted.
type mappingReads struct {
	variants reusedMap[ir.TypeID, bool]
	keys     []string
}

// checkUnionDiscriminator requires each mapping target to be one of the union's
// variant targets.
func checkUnionDiscriminator(doc *ir.Document, u *ir.Union, read *mappingReads) []ir.Diagnostic {
	if u.Discriminator == nil {
		return nil
	}
	variants := read.variants.emptied()
	for _, v := range u.Variants {
		variants[v.Type.Target] = true
	}
	return checkMapping(doc, u.Discriminator, string(u.ID), read, func(target ir.TypeID) bool {
		return variants[target]
	})
}

// checkModelDiscriminator requires each mapping target to be a declared subtype
// of the discriminated base model, and the tag property to be one this model
// exposes. Its routes and tag are asked of facts, the one index of the run.
func checkModelDiscriminator(doc *ir.Document, facts *irfacts.Index, m *ir.Model, read *mappingReads) []ir.Diagnostic {
	if m.Discriminator == nil {
		return nil
	}
	diags := checkDiscriminatorProperty(facts, m)
	return append(diags, checkMapping(doc, m.Discriminator, string(m.ID), read, func(target ir.TypeID) bool {
		return facts.IsSubtype(target, m.ID)
	})...)
}

// checkDiscriminatorProperty requires a model discriminator's tag property to be
// one the model exposes — its own, or one it composes in.
//
// checkPropIDRefs already holds every PropID to naming a property the document
// declares somewhere. This is the tighter claim available where the root is
// written down: a tag on a property of some unrelated model routes nothing, and
// an emitter building a decoder reads the tag off *this* model's instance. A
// tag naming a declaration the model's flattening overrides passes, though
// docs/ir/models.md#the-flattening has it name the one taken, as
// appendUnknownPartDiags says.
func checkDiscriminatorProperty(facts *irfacts.Index, m *ir.Model) []ir.Diagnostic {
	prop := m.Discriminator.Property
	if prop == "" || facts.Exposes(m.ID, prop) {
		return nil
	}
	where := string(m.ID)
	return []ir.Diagnostic{diag(ir.SeverityError, "pass/discriminator-unknown-property",
		fmt.Sprintf("discriminator on %s tags property %q, which the model neither declares nor composes in", where, prop),
		where)}
}

// checkMapping validates every routing target a discriminator names — each
// wire-value mapping entry and the Default an unrecognized tag falls back to.
// All of them must resolve in the registry and satisfy member. Default names
// the type an instance deserializes into, so it is held to the same claim as a
// mapping entry.
//
// A target that resolves nowhere is reported here and again by
// checkDanglingRefs, deliberately: ir/dangling-type-ref says the document is
// not referentially closed, while pass/discriminator-missing-variant says this
// discriminator cannot route that wire value, which a polymorphic decoder's
// emitter subscribes to. Neither consumer should need the other's code.
func checkMapping(doc *ir.Document, d *ir.Discriminator, where string, read *mappingReads,
	member func(ir.TypeID) bool,
) []ir.Diagnostic {
	var diags []ir.Diagnostic
	// Asked in ascending key order, sorted in read's list, so map iteration
	// reaches neither the output nor the questions member asks.
	read.keys = read.keys[:0]
	for key := range d.Mapping {
		read.keys = append(read.keys, key)
	}
	slices.Sort(read.keys)
	for _, key := range read.keys {
		target := d.Mapping[key]
		if legal, why := mappingTarget(doc, target, member); !legal {
			diags = append(diags, missingVariant(fmt.Sprintf("mapping %q", key), where, target, why))
		}
	}
	if d.Default == "" {
		return diags
	}
	if legal, why := mappingTarget(doc, d.Default, member); !legal {
		diags = append(diags, missingVariant("default", where, d.Default, why))
	}
	return diags
}

// missingVariant reports that the discriminator on where routes position to
// target, which why says is no legal target.
func missingVariant(position, where string, target ir.TypeID, why string) ir.Diagnostic {
	return diag(ir.SeverityError, "pass/discriminator-missing-variant",
		fmt.Sprintf("discriminator %s on %s references %q, which %s", position, where, target, why),
		where)
}

// mappingTarget reports whether target is a legal mapping target and, when it is
// not, the clause naming why. The two failures read differently on purpose: a
// target no type declares is a broken reference, while a declared one that fails
// member is a well-formed reference to the wrong type, and the reader should not
// have to cross-check the registry to tell them apart.
func mappingTarget(doc *ir.Document, target ir.TypeID, member func(ir.TypeID) bool) (legal bool, why string) {
	if _, declared := doc.Types[target]; !declared {
		return false, "no type in the document declares"
	}
	if !member(target) {
		return false, "is not a variant of it"
	}
	return true, ""
}
