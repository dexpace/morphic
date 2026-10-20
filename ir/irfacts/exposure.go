package irfacts

import (
	"maps"
	"math"
	"slices"

	"github.com/dexpace/morphic/ir"
)

// Exposes reports whether the type id names exposes the property prop, as
// ir.ExposedProps answers it: id, or a type its composition or its alias
// scalars lead to, declares prop. Its first questions are walked, as
// [Index.IsSubtype]'s are. After that a question is a few comparisons when one
// model declares prop, unless the comparisons leave the pair open as
// IsSubtype's may be. A property several models declare and an open pair are
// walked.
func (x *Index) Exposes(id ir.TypeID, prop ir.PropID) bool {
	if limit := x.exposureWalks.Load(); limit > 0 {
		exposed, spent, ok := ir.ExposesWithin(x.doc, id, prop, int(limit))
		x.exposureWalks.Add(-int64(spent))
		if ok {
			return exposed
		}
	}
	if exposed, settled := x.exposureIndex().exposes(id, prop); settled {
		return exposed
	}
	exposed, _, _ := ir.ExposesWithin(x.doc, id, prop, math.MaxInt)
	return exposed
}

// sharedProp is the owner of a property more than one model declares.
const sharedProp = -1

// exposure indexes the relation ir.ExposedProps walks. A model leads to its
// Base, Implements and Mixins and an alias scalar to its Base, and a type
// exposes what it and every type it leads to declare, so a property declared by
// one model is exposed by the types that reach that model.
type exposure struct {
	vertex map[ir.TypeID]int // declared types, in sorted order
	reach  *intervals        // what each type leads to
	owner  map[ir.PropID]int // the model declaring each property, or sharedProp
}

// newExposure indexes the parts of the types in types.
func newExposure(types ir.TypeRegistry) *exposure {
	ids := slices.Sorted(maps.Keys(types))
	e := &exposure{vertex: make(map[ir.TypeID]int, len(ids)), owner: map[ir.PropID]int{}}
	for i, id := range ids {
		e.vertex[id] = i
	}
	edges := make([][]int, len(ids))
	var sources []ir.TypeID
	for i, id := range ids {
		sources = e.read(sources[:0], i, types[id])
		for _, source := range sources {
			if v, declared := e.vertex[source]; declared {
				edges[i] = append(edges[i], v)
			}
		}
	}
	e.reach = newIntervals(edges)
	return e
}

// read records the properties td declares as vertex i's, and appends to dst the
// types the rest of its parts come from, as ir.ExposedProps reads them: a
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
		for _, r := range t.Implements {
			dst = append(dst, r.Target)
		}
		for _, r := range t.Mixins {
			dst = append(dst, r.Target)
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

// own records that vertex i declares prop, which another declaring it too makes
// shared.
func (e *exposure) own(prop ir.PropID, i int) {
	if owner, owned := e.owner[prop]; owned && owner != i {
		e.owner[prop] = sharedProp
		return
	}
	e.owner[prop] = i
}

// exposes answers [Index.Exposes] from the intervals, with settled false when
// they cannot: several models declare prop, or the intervals leave open
// whether id reaches the one that does.
func (e *exposure) exposes(id ir.TypeID, prop ir.PropID) (exposed, settled bool) {
	v, declared := e.vertex[id]
	owner, owned := e.owner[prop]
	switch {
	case !declared || !owned:
		return false, true
	case owner == sharedProp:
		return false, false
	case owner == v:
		return true, true
	default:
		return e.reach.below(v, owner)
	}
}
