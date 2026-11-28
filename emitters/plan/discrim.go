// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package plan

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/ir/irfacts"
)

// The plan's findings about a discriminator.
const (
	// codeSharedTag reports a tag the IR routes to two types, which
	// [DiscrimFacts.Mapping], keying one type per tag, cannot hold.
	codeSharedTag = "emitters/plan/discriminator-shared-tag"
	// codeUnlocated reports a discriminator that no one locator places.
	codeUnlocated = "emitters/plan/discriminator-unlocated"
)

// The envelope a tag can sit in (ir.Discriminator.Envelope).
const (
	// envelopeObject is a {kind, value} wrapper around the variant's value.
	envelopeObject = "object"
	// defaultEnvelopeValueName is the wire name of an object envelope's value
	// property where the IR names none (ir.Discriminator.EnvelopeValueName).
	defaultEnvelopeValueName = "value"
)

// DiscrimFacts is a union's or a polymorphic base's discriminator with every
// route written out as a type ID, never a name
// (docs/emitters/plan/types.md#a-discriminators-routes).
type DiscrimFacts struct {
	// Mapping maps each wire tag to the one type it routes to; empty = no
	// tag routes. It is the IR's own where a union's holds an entry.
	Mapping map[string]ir.TypeID `json:"mapping,omitempty"`
	// TupleElementIndex is the IR's: the 0-based tuple element carrying the
	// tag where Locator is DiscrimIndex, nil otherwise.
	TupleElementIndex *int `json:"tupleElementIndex,omitzero"`
	// Locator says which of the IR's fields locates the tag.
	Locator DiscrimLocator `json:"locator"`
	// WireKey is the wire name of the tag's property; "" where Locator is
	// DiscrimIndex, or where no flattened member declares the property.
	WireKey string `json:"wireKey,omitempty"`
	// Default is the type an absent or unrecognized tag routes to; "" = none.
	Default ir.TypeID `json:"default,omitempty"`
	// Envelope is "" for a tag inline in the value, or "object" for a
	// {kind, value} wrapper.
	Envelope string `json:"envelope,omitempty"`
	// EnvelopeValueName is the wire name of an object envelope's value
	// property, "value" where the IR names none; "" without an envelope.
	EnvelopeValueName string `json:"envelopeValueName,omitempty"`
}

// DiscrimLocator says which field of an ir.Discriminator locates the tag.
type DiscrimLocator string

// The three locators of a tag.
const (
	// DiscrimProperty is a model hierarchy's tag, at the property
	// ir.Discriminator.Property names.
	DiscrimProperty DiscrimLocator = "property"
	// DiscrimPropertyName is a tag at the wire name
	// ir.Discriminator.PropertyName holds.
	DiscrimPropertyName DiscrimLocator = "property_name"
	// DiscrimIndex is a positional tag, at the tuple element
	// ir.Discriminator.TupleElementIndex holds.
	DiscrimIndex DiscrimLocator = "index"
)

// unionDiscrim returns the discriminator facts of union, nil unless its
// shape is discriminated: its routes are the entries of the IR's Mapping, or,
// where that holds none, each variant under the source name of its type
// (docs/emitters/refine/unions.md#the-tag-a-variants-value-carries).
func unionDiscrim(types ir.TypeRegistry, union *ir.Union) (*DiscrimFacts, []ir.Diagnostic) {
	if unionShape(union) != UnionDiscriminated {
		return nil, nil
	}
	discriminator := union.Discriminator
	set := locators(discriminator)
	locator, located := locate(set, true)
	if !located {
		return nil, []ir.Diagnostic{unlocated(union, set)}
	}

	built := newDiscrim(discriminator, locator)
	if len(discriminator.Mapping) > 0 {
		built.Mapping = discriminator.Mapping
		return built, nil
	}
	// A variant naming no declared type routes nothing, nor does one whose type
	// has no source name, as an anonymous type has a Hint alone.
	routes := routeTable{}
	for _, variant := range union.Variants {
		target := variant.Type.Target
		td, declared := types[target]
		if !declared || ir.IsNilTypeDef(td) || td.Common().Name.Source == "" {
			continue
		}
		routes.add(td.Common().Name.Source, target)
	}
	var diags []ir.Diagnostic
	built.Mapping, diags = routes.mapping(union)
	return built, diags
}

// hierarchyDiscrim returns the discriminator facts of model, whose flattening
// is flat, nil unless it holds a discriminator: its routes are its Mapping's
// entries and the DiscriminatorValue of each subtype facts derive
// (docs/ir/models.md#discriminator).
func hierarchyDiscrim(
	types ir.TypeRegistry, facts *irfacts.DocumentFacts, model *ir.Model, flat ir.Flattening,
) (*DiscrimFacts, []ir.Diagnostic) {
	discriminator := model.Discriminator
	if discriminator == nil {
		return nil, nil
	}
	set := locators(discriminator)
	locator, located := locate(set, false)
	if !located {
		return nil, []ir.Diagnostic{unlocated(model, set)}
	}

	built := newDiscrim(discriminator, locator)
	if locator == DiscrimProperty {
		built.WireKey = wireKeyOf(flat, discriminator.Property)
	}
	routes := routeTable{}
	for tag, target := range discriminator.Mapping {
		routes.add(tag, target)
	}
	for _, id := range facts.Subtypes(model.ID) {
		subtype, isModel := types[id].(*ir.Model)
		if !isModel || subtype == nil || subtype.DiscriminatorValue == "" {
			continue
		}
		routes.add(subtype.DiscriminatorValue, id)
	}
	var diags []ir.Diagnostic
	built.Mapping, diags = routes.mapping(model)
	return built, diags
}

// locators returns the locators discriminator sets, in the order the IR
// declares their fields.
func locators(discriminator *ir.Discriminator) []DiscrimLocator {
	var set []DiscrimLocator
	if discriminator.Property != "" {
		set = append(set, DiscrimProperty)
	}
	if discriminator.PropertyName != "" {
		set = append(set, DiscrimPropertyName)
	}
	if discriminator.TupleElementIndex != nil {
		set = append(set, DiscrimIndex)
	}
	return set
}

// locate returns the one locator of set, false where set holds none or
// several. A union's is no Property, which only a model declares.
func locate(set []DiscrimLocator, union bool) (DiscrimLocator, bool) {
	if len(set) != 1 || union && set[0] == DiscrimProperty {
		return "", false
	}
	return set[0], true
}

// newDiscrim returns the facts of discriminator, located by locator, with
// every field set but its routes and a property's wire key.
func newDiscrim(discriminator *ir.Discriminator, locator DiscrimLocator) *DiscrimFacts {
	built := &DiscrimFacts{
		Locator:           locator,
		TupleElementIndex: discriminator.TupleElementIndex,
		Default:           discriminator.Default,
		Envelope:          discriminator.Envelope,
		EnvelopeValueName: discriminator.EnvelopeValueName,
	}
	if locator == DiscrimPropertyName {
		built.WireKey = discriminator.PropertyName
	}
	if built.Envelope == envelopeObject && built.EnvelopeValueName == "" {
		built.EnvelopeValueName = defaultEnvelopeValueName
	}
	return built
}

// wireKeyOf returns the wire name of the member of flat one of whose
// declarations is property, "" where none is: pass.Validate reports that
// discriminator as pass/discriminator-unknown-property.
func wireKeyOf(flat ir.Flattening, property ir.PropertyID) string {
	for i := range flat.Properties {
		member := &flat.Properties[i]
		if member.Property.ID == property {
			return member.Property.EffectiveWireName()
		}
		for _, overridden := range member.Overridden {
			if overridden.Property.ID == property {
				return member.Property.EffectiveWireName()
			}
		}
	}
	return ""
}

// routeTable holds a discriminator's routes: each tag, and the distinct types
// it routes to.
type routeTable map[string]map[ir.TypeID]bool

// add routes tag to target.
func (r routeTable) add(tag string, target ir.TypeID) {
	if r[tag] == nil {
		r[tag] = map[ir.TypeID]bool{}
	}
	r[tag][target] = true
}

// mapping returns the routes of r whose tag routes to one type, nil where no
// tag does, and, in tag order, a warning about each tag routing to several,
// which it leaves out. holder is the type holding the discriminator.
func (r routeTable) mapping(holder ir.TypeDef) (map[string]ir.TypeID, []ir.Diagnostic) {
	var routes map[string]ir.TypeID
	var diags []ir.Diagnostic
	for _, tag := range slices.Sorted(maps.Keys(r)) {
		targets := slices.Sorted(maps.Keys(r[tag]))
		if len(targets) > 1 {
			diags = append(diags, sharedTag(holder, tag, targets))
			continue
		}
		if routes == nil {
			routes = map[string]ir.TypeID{}
		}
		routes[tag] = targets[0]
	}
	return routes, diags
}

// sharedTag reports that holder's discriminator routes tag to each of
// targets: [DiscrimFacts.Mapping] keys one type per tag, so the plan routes
// the tag to none of them (docs/emitters/plan/types.md#a-discriminators-routes).
func sharedTag(holder ir.TypeDef, tag string, targets []ir.TypeID) ir.Diagnostic {
	names := make([]string, len(targets))
	for i, target := range targets {
		names[i] = string(target)
	}
	common := holder.Common()
	return ir.NewDiagnostic(ir.SeverityWarning, codeSharedTag,
		fmt.Sprintf("the discriminator of %s routes the tag %q to %s, and a plan's mapping keys one type per tag, "+
			"so the plan routes the tag to none of them", common.ID, tag, strings.Join(names, ", ")),
		common.Provenance)
}

// unlocated reports that holder's discriminator sets the locators in set:
// none, several, or, on a union, Property alone.
func unlocated(holder ir.TypeDef, set []DiscrimLocator) ir.Diagnostic {
	located := "no locator"
	if len(set) > 0 {
		names := make([]string, len(set))
		for i, locator := range set {
			names[i] = string(locator)
		}
		located = strings.Join(names, " and ")
	}
	common := holder.Common()
	return ir.NewDiagnostic(ir.SeverityError, codeUnlocated,
		fmt.Sprintf("the discriminator of %s is located by %s, where exactly one locator must place its tag, "+
			"a union's by property_name or index, so the plan routes nothing by it", common.ID, located),
		common.Provenance)
}
