// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package irfacts

import (
	"maps"
	"math"
	"slices"

	"github.com/dexpace/morphic/ir"
)

// Exposes reports whether the type id names exposes the property property, as
// ir.ExposedProperties answers it: id, or a type its composition or its alias
// scalars lead to, declares property. Its first questions are walked, as
// [DocumentFacts.IsSubtype]'s are. After that a question is a few comparisons
// when one model declares property, unless the comparisons leave the pair open as
// IsSubtype's may be. A property several models declare and an open pair are
// walked.
func (x *DocumentFacts) Exposes(id ir.TypeID, property ir.PropertyID) bool {
	if limit := x.exposureWalks.Load(); limit > 0 {
		exposed, spent, ok := ir.ExposesWithin(x.doc, id, property, int(limit))
		x.exposureWalks.Add(-int64(spent))
		if ok {
			return exposed
		}
	}
	if exposed, settled := x.exposureIndex().exposes(id, property); settled {
		return exposed
	}
	exposed, _, _ := ir.ExposesWithin(x.doc, id, property, math.MaxInt)
	return exposed
}

// sharedProperty is the owner of a property more than one model declares.
const sharedProperty = -1

// exposure indexes the relation ir.ExposedProperties walks. A model leads to
// its Base, Implements and Mixins and an alias scalar to its Base, and a type
// exposes what it and every type it leads to declare, so a property declared by
// one model is exposed by the types that reach that model.
type exposure struct {
	vertex map[ir.TypeID]int     // declared types, in sorted order
	reach  *intervals            // what each type leads to
	owner  map[ir.PropertyID]int // the model declaring each property, or sharedProperty
}

// newExposure indexes the parts of the types in types.
func newExposure(types ir.TypeRegistry) *exposure {
	ids := slices.Sorted(maps.Keys(types))
	exposureValue := &exposure{vertex: make(map[ir.TypeID]int, len(ids)), owner: map[ir.PropertyID]int{}}
	for i, id := range ids {
		exposureValue.vertex[id] = i
	}
	edges := pairsOf[int](mostSources(types))
	var sources []ir.TypeID
	for i, id := range ids {
		sources = exposureValue.read(sources[:0], i, types[id])
		for _, source := range sources {
			if v, declared := exposureValue.vertex[source]; declared {
				edges.add(i, v)
			}
		}
	}
	exposureValue.reach = newIntervals(edges.grouped(len(ids)))
	return exposureValue
}

// mostSources returns how many part sources the types of types name at most:
// a model's Base, Implements and Mixins entries and an alias scalar's Base.
func mostSources(types ir.TypeRegistry) int {
	most := 0
	for _, td := range types {
		switch t := td.(type) {
		case *ir.Model:
			if t != nil {
				most += 1 + len(t.Implements) + len(t.Mixins)
			}
		case *ir.Scalar:
			most++
		default:
			// No other kind names a part source.
		}
	}
	return most
}

// read records the properties td declares as vertex i's, and appends to dst the
// types the rest of its parts come from, as ir.ExposedProperties reads them: a
// model's Base, Implements and Mixins and an alias scalar's Base. A typed nil
// and every other kind have none.
func (e *exposure) read(dst []ir.TypeID, i int, td ir.TypeDef) []ir.TypeID {
	if ir.IsNilTypeDef(td) {
		return dst
	}
	switch t := td.(type) {
	case *ir.Model:
		for j := range t.Properties {
			e.own(t.Properties[j].ID, i)
		}
		if t.Base != nil {
			dst = append(dst, t.Base.Target)
		}
		for _, typeRef := range t.Implements {
			dst = append(dst, typeRef.Target)
		}
		for _, typeRef := range t.Mixins {
			dst = append(dst, typeRef.Target)
		}
	case *ir.Scalar:
		if t.Base != nil {
			dst = append(dst, t.Base.Target)
		}
	default:
		// No other kind declares a property or says where one comes from.
	}
	return dst
}

// own records that vertex i declares property, which another declaring it too makes
// shared.
func (e *exposure) own(property ir.PropertyID, i int) {
	if owner, owned := e.owner[property]; owned && owner != i {
		e.owner[property] = sharedProperty
		return
	}
	e.owner[property] = i
}

// exposes answers [DocumentFacts.Exposes] from the intervals, with settled
// false when they cannot: several models declare property, or the intervals leave
// open whether id reaches the one that does.
func (e *exposure) exposes(id ir.TypeID, property ir.PropertyID) (exposed, settled bool) {
	v, declared := e.vertex[id]
	owner, owned := e.owner[property]
	switch {
	case !declared || !owned:
		return false, true
	case owner == sharedProperty:
		return false, false
	case owner == v:
		return true, true
	default:
		return e.reach.below(v, owner)
	}
}
