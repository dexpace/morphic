// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package plan

import "github.com/dexpace/morphic/ir"

// valueRoute is a route a subtype's DiscriminatorValue gives: the tag, and the
// subtype carrying it.
type valueRoute struct {
	tag     string
	subtype ir.TypeID
}

// valueRoutes returns, by the model holding the discriminator each is a tag
// of, the routes the DiscriminatorValue of each model of doc gives, ids being
// doc's type IDs in sorted order, which each holder's routes keep. A value a
// hierarchy's Mapping keys to its subtype is that discriminator's tag, which
// the entry routes, so it gives none. Any other, a name or a tag read off the
// subtype, is taken as the nearest discriminated ancestor's, which the IR
// does not state, so no other discriminator above it routes the value
// (docs/emitters/plan/types.md#a-discriminators-routes, GitHub #2313).
func valueRoutes(doc *ir.Document, ids []ir.TypeID) map[ir.TypeID][]valueRoute {
	keyed := mappedRoutes(doc.Types)
	nearest := newNearestBases(doc, ids).nearest
	var routes map[ir.TypeID][]valueRoute
	for _, id := range ids {
		model, isModel := doc.Types[id].(*ir.Model)
		if !isModel || model == nil || model.DiscriminatorValue == "" {
			continue
		}
		route := valueRoute{tag: model.DiscriminatorValue, subtype: id}
		if keyed[route] {
			continue
		}
		holder, found := nearest[id]
		if !found {
			continue
		}
		if routes == nil {
			routes = map[ir.TypeID][]valueRoute{}
		}
		routes[holder] = append(routes[holder], route)
	}
	return routes
}

// mappedRoutes returns every route the Mapping of a model's discriminator in
// types names, nil where none does. An entry naming a type no subtype of its
// holder is counted too: pass.Validate reports it as
// pass/discriminator-missing-variant.
func mappedRoutes(types ir.TypeRegistry) map[valueRoute]bool {
	var keyed map[valueRoute]bool
	for _, td := range types {
		model, isModel := td.(*ir.Model)
		if !isModel || model == nil || model.Discriminator == nil {
			continue
		}
		for tag, target := range model.Discriminator.Mapping {
			if keyed == nil {
				keyed = map[valueRoute]bool{}
			}
			keyed[valueRoute{tag: tag, subtype: target}] = true
		}
	}
	return keyed
}

// nearestBases holds, for each model a discriminated ancestor is above, the
// nearest of them but itself, level by level up ir.Supertypes, Base before
// Implements: the one fewest hops away and, of those as near, the one reached
// through the earlier supertype. It descends a level at a time from every
// model holding a discriminator, so a cycle of bases ends and a model is
// settled once.
type nearestBases struct {
	nearest map[ir.TypeID]ir.TypeID
	// steps counts the models, supertype edges and subtype reads the descent
	// made, not the alias scalars ir.Supertypes reads a supertype through.
	steps int
}

// subtypeAt is a model naming a supertype, and its position among the
// model's supertypes.
type subtypeAt struct {
	id       ir.TypeID
	position int
}

// newNearestBases returns the nearest bases of the models ids names, doc's
// type IDs in sorted order.
func newNearestBases(doc *ir.Document, ids []ir.TypeID) *nearestBases {
	bases := &nearestBases{nearest: map[ir.TypeID]ir.TypeID{}}
	subtypes := map[ir.TypeID][]subtypeAt{}
	var level []ir.TypeID
	for _, id := range ids {
		bases.steps++
		for position, super := range ir.Supertypes(doc, id) {
			bases.steps++
			subtypes[super] = append(subtypes[super], subtypeAt{id: id, position: position})
		}
		if holdsDiscriminator(doc.Types, id) {
			level = append(level, id)
		}
	}
	// A model enters a level once: the first if it holds a discriminator, or
	// the one after it is settled if it holds none. So no level is empty and
	// none shares a model. Only a first level of one discriminated model,
	// every other model entering a later one, fills as many levels as ids
	// names, and the last of those could settle only that model, which is
	// never its own base: so the loop is held to one turn fewer.
	for turn := 0; len(level) > 0 && turn < len(ids)-1; turn++ {
		level = bases.descend(doc.Types, subtypes, level)
	}
	return bases
}

// descend settles each model a supertype in level names that no nearer level
// settled, and returns the next level: those of them holding no
// discriminator. Each takes the base through the supertype it names first,
// that supertype itself where it holds a discriminator, and never itself, as
// the OpenAPI compiler reads no schema its own ancestor. So a holder in a
// cycle of bases that the cycle's other models take as their nearest gets
// none, though a holder above the cycle they compose is the compiler's
// reading (GitHub #2313).
func (b *nearestBases) descend(types ir.TypeRegistry, subtypes map[ir.TypeID][]subtypeAt, level []ir.TypeID) []ir.TypeID {
	type candidate struct {
		base     ir.TypeID
		position int
	}
	reached := map[ir.TypeID]candidate{}
	for _, super := range level {
		base := super
		if !holdsDiscriminator(types, super) {
			base = b.nearest[super]
		}
		for _, subtype := range subtypes[super] {
			b.steps++
			if _, settled := b.nearest[subtype.id]; settled || subtype.id == base {
				continue
			}
			if best, seen := reached[subtype.id]; seen && best.position <= subtype.position {
				continue
			}
			reached[subtype.id] = candidate{base: base, position: subtype.position}
		}
	}
	var next []ir.TypeID
	for id, best := range reached {
		b.nearest[id] = best.base
		if !holdsDiscriminator(types, id) {
			next = append(next, id)
		}
	}
	return next
}

// holdsDiscriminator reports whether id names a model holding a
// discriminator, located or not: a value read against it is its tag either
// way.
func holdsDiscriminator(types ir.TypeRegistry, id ir.TypeID) bool {
	model, isModel := types[id].(*ir.Model)
	return isModel && model != nil && model.Discriminator != nil
}
