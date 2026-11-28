// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package plan

import (
	"slices"

	"github.com/dexpace/morphic/ir"
)

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
// model holding a discriminator, and each model keeps the two nearest holders
// it reaches, so one reached back through a supertype taking it as that
// supertype's own nearest takes the next.
type nearestBases struct {
	nearest map[ir.TypeID]ir.TypeID
	// holders are the two nearest holders each model reaches, but itself.
	holders map[ir.TypeID][]ir.TypeID
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

// reach is a holder a model reaches through models holding none. A holder
// reaches itself.
type reach struct {
	model  ir.TypeID
	holder ir.TypeID
}

// newNearestBases returns the nearest bases of the models ids names, doc's
// type IDs in sorted order.
func newNearestBases(doc *ir.Document, ids []ir.TypeID) *nearestBases {
	bases := &nearestBases{nearest: map[ir.TypeID]ir.TypeID{}, holders: map[ir.TypeID][]ir.TypeID{}}
	subtypes := map[ir.TypeID][]subtypeAt{}
	var level []reach
	for _, id := range ids {
		bases.steps++
		for position, super := range ir.Supertypes(doc, id) {
			bases.steps++
			subtypes[super] = append(subtypes[super], subtypeAt{id: id, position: position})
		}
		if holdsDiscriminator(doc.Types, id) {
			level = append(level, reach{model: id, holder: id})
		}
	}
	// A reach at a level is as many hops below its holder through distinct
	// models that each keep it, so it is at most len(ids)-1 below. At that
	// depth every type is such a model, keeping the holder and holding no
	// discriminator, so the level gives no model a holder: the loop stops a
	// turn short of it, and a chain only the first of whose models holds one
	// takes every turn.
	for turn := 0; len(level) > 0 && turn < len(ids)-1; turn++ {
		level = bases.descend(doc.Types, subtypes, level)
	}
	for id, held := range bases.holders {
		bases.nearest[id] = held[0]
	}
	return bases
}

// candidate is a holder a level reaches a model with, and the position of the
// supertype it comes through.
type candidate struct {
	holder   ir.TypeID
	position int
}

// descend gives each model a supertype in level names the holders that level
// reaches it with, nearest first, until it keeps two, and returns the next
// level: each holder it kept, nearest first, where it holds no discriminator
// itself.
func (b *nearestBases) descend(types ir.TypeRegistry, subtypes map[ir.TypeID][]subtypeAt, level []reach) []reach {
	reached := map[ir.TypeID][]candidate{}
	for _, super := range level {
		for _, subtype := range subtypes[super.model] {
			b.steps++
			reached[subtype.id] = append(reached[subtype.id], candidate{holder: super.holder, position: subtype.position})
		}
	}
	var next []reach
	for id, candidates := range reached {
		for _, holder := range b.keep(id, candidates) {
			if !holdsDiscriminator(types, id) {
				next = append(next, reach{model: id, holder: holder})
			}
		}
	}
	return next
}

// keep adds to the holders id keeps the nearest of candidates it does not
// keep yet, until it keeps two, and returns those it added. Of one level's
// candidates the nearest comes through the earlier supertype and, of two
// through the same one, is the first, since descend puts each model's holders
// in the next level nearest first. A model is never its own holder, as the
// OpenAPI compiler reads no schema its own ancestor.
func (b *nearestBases) keep(id ir.TypeID, candidates []candidate) []ir.TypeID {
	var added []ir.TypeID
	for len(b.holders[id]) < 2 {
		best := -1
		for i, found := range candidates {
			if found.holder == id || slices.Contains(b.holders[id], found.holder) {
				continue
			}
			if best < 0 || found.position < candidates[best].position {
				best = i
			}
		}
		if best < 0 {
			break
		}
		added = append(added, candidates[best].holder)
		b.holders[id] = append(b.holders[id], candidates[best].holder)
	}
	return added
}

// holdsDiscriminator reports whether id names a model holding a
// discriminator, located or not: a value read against it is its tag either
// way.
func holdsDiscriminator(types ir.TypeRegistry, id ir.TypeID) bool {
	model, isModel := types[id].(*ir.Model)
	return isModel && model != nil && model.Discriminator != nil
}
