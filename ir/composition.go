package ir

import (
	"math"
	"slices"
)

// Supertypes returns the immediate supertypes of the model id names: its Base,
// then each Implements entry, each read through the alias scalars standing in
// for it, since a $ref carrying siblings composes the branch's own alias node,
// not the schema it points at (ir-design §4.3). Mixins are absent on purpose: a
// mixin contributes members without making the model a member of anything, so
// it belongs to [ExposedProps], not to subtype identity. An id naming no live
// model yields nil, and an unset reference is skipped.
func Supertypes(doc *Document, id TypeID) []TypeID {
	left := math.MaxInt
	supers, _ := supertypes(nil, doc, id, &left)
	return supers
}

// supertypes appends to dst the supertypes [Supertypes] returns, taking a step
// from *left for each alias scalar it reads a reference through, and reports
// false when *left ran out first.
func supertypes(dst []TypeID, doc *Document, id TypeID, left *int) ([]TypeID, bool) {
	if doc == nil {
		return dst, true
	}
	m, ok := doc.Types[id].(*Model)
	if !ok || m == nil {
		return dst, true
	}
	if m.Base != nil {
		if dst, ok = appendSupertype(dst, doc, m.Base.Target, left); !ok {
			return dst, false
		}
	}
	for _, r := range m.Implements {
		if dst, ok = appendSupertype(dst, doc, r.Target, left); !ok {
			return dst, false
		}
	}
	return dst, true
}

// appendSupertype appends the type target stands for, read through its alias
// scalars, unless that is no type at all. It reports false when *left ran out
// first.
func appendSupertype(dst []TypeID, doc *Document, target TypeID, left *int) ([]TypeID, bool) {
	t, ok := aliasTarget(doc, target, left)
	if ok && t != "" {
		dst = append(dst, t)
	}
	return dst, ok
}

// take spends a step of *left, and reports false when none is left.
func take(left *int) bool {
	if *left <= 0 {
		return false
	}
	*left--
	return true
}

// AliasBase returns the type the alias scalar id names, and false when id names
// no alias scalar: no Scalar, a typed nil, or one with no Base. An alias stands
// for its Base, and a chain of them for the type at its end (ir-design §4.2).
func AliasBase(types TypeRegistry, id TypeID) (TypeID, bool) {
	s, ok := types[id].(*Scalar)
	if !ok || s == nil || s.Base == nil {
		return "", false
	}
	return s.Base.Target, true
}

// aliasTarget follows a chain of alias scalars to the type it stands for,
// returning id unchanged when it names no alias, and taking a step from *left
// for each scalar it reads through. The IR permits a cyclic chain, so a visited
// set ends the walk, at the first type it meets twice. It reports false when
// *left ran out first.
func aliasTarget(doc *Document, id TypeID, left *int) (TypeID, bool) {
	seen := make(map[TypeID]bool)
	for {
		base, alias := AliasBase(doc.Types, id)
		if !alias || seen[id] {
			return id, true
		}
		if !take(left) {
			return "", false
		}
		seen[id] = true
		id = base
	}
}

// IsSubtype reports whether target is a declared subtype of base at any distance
// along [Supertypes], the transitive closure a base tagging a grandchild needs.
// The relation is not reflexive unless a cycle leads back, and target itself is
// not read through an alias (#844). A visited set ends the walk on a cyclic
// hierarchy.
//
// It is the relation's definition, walked afresh for one question. A caller
// asking many of one document asks irfacts.Index, which is held to it.
func IsSubtype(doc *Document, target, base TypeID) bool {
	sub, _, _ := IsSubtypeWithin(doc, target, base, math.MaxInt)
	return sub
}

// IsSubtypeWithin answers [IsSubtype] by the same walk, in at most limit steps:
// one for target and for each supertype it follows, and one for each alias
// scalar it reads a supertype through. spent is the steps it took, and ok is
// false when the walk needed more than limit and stopped without an answer.
func IsSubtypeWithin(doc *Document, target, base TypeID, limit int) (sub bool, spent int, ok bool) {
	left := limit
	sub, ok = isSubtype(doc, target, base, &left)
	return sub, limit - left, ok
}

// isSubtype walks up from target, taking steps from *left, until it meets base
// or runs out of supertypes or steps.
func isSubtype(doc *Document, target, base TypeID, left *int) (sub, ok bool) {
	seen := map[TypeID]bool{}
	queue := []TypeID{target}
	var supers []TypeID
	for len(queue) > 0 {
		id := queue[len(queue)-1]
		queue = queue[:len(queue)-1]
		if !take(left) {
			return false, false
		}
		if seen[id] {
			continue
		}
		seen[id] = true
		if supers, ok = supertypes(supers[:0], doc, id, left); !ok {
			return false, false
		}
		if slices.Contains(supers, base) {
			return true, true
		}
		queue = append(queue, supers...)
	}
	return false, true
}

// ExposedProps returns the property IDs the type root names exposes: its own
// plus those it composes in through Base, Implements and Mixins (ir-design
// §4.3), the flat set an emitter renders. A root naming an alias scalar exposes
// the parts of the model the alias stands for, and one naming neither that nor
// a live model exposes none. A visited set ends the walk on a cyclic hierarchy.
//
// It is the relation's definition, walked afresh for one root. A caller asking
// many of one document asks irfacts.Index, which is held to it.
func ExposedProps(doc *Document, root TypeID) map[PropID]bool {
	props := map[PropID]bool{}
	left := math.MaxInt
	exposedWalk(doc, root, &left, func(p PropID) bool {
		props[p] = true
		return false
	})
	return props
}

// ExposesWithin answers whether [ExposedProps] holds prop by the same walk,
// which stops at the first type declaring prop, in at most limit steps: one
// for root and for each part source it follows, and one for each property it
// reads. spent is the steps it took, and ok is false when the walk needed more
// than limit and stopped without an answer.
func ExposesWithin(doc *Document, root TypeID, prop PropID, limit int) (exposed bool, spent int, ok bool) {
	left := limit
	ok = exposedWalk(doc, root, &left, func(p PropID) bool {
		exposed = p == prop
		return exposed
	})
	return exposed, limit - left, ok
}

// exposedWalk hands visit each property the types root leads to declare, taking
// steps from *left, and stops when visit returns true. It reports false when
// *left ran out first.
func exposedWalk(doc *Document, root TypeID, left *int, visit func(PropID) bool) bool {
	if doc == nil {
		return true
	}
	seen := map[TypeID]bool{}
	queue := []TypeID{root}
	for len(queue) > 0 {
		id := queue[len(queue)-1]
		queue = queue[:len(queue)-1]
		if !take(left) {
			return false
		}
		if seen[id] {
			continue
		}
		seen[id] = true
		td := doc.Types[id]
		if IsNilTypeDef(td) {
			continue // undeclared, or a typed nil.
		}
		var stop, ok bool
		if queue, stop, ok = partSources(queue, td, left, visit); stop || !ok {
			return ok
		}
	}
	return true
}

// partSources hands visit td's own properties, taking a step from *left for
// each, and appends the nodes the rest come from: a model's composition
// parents, an alias scalar's base. No other kind carries parts or says where
// to find them. stop reports that visit returned true, and ok false that *left
// ran out first.
func partSources(dst []TypeID, td TypeDef, left *int, visit func(PropID) bool) (sources []TypeID, stop, ok bool) {
	switch t := td.(type) {
	case *Model:
		for _, p := range t.Properties {
			if !take(left) {
				return dst, false, false
			}
			if visit(p.ID) {
				return dst, true, true
			}
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
		return dst, false, true
	case *Scalar:
		if t.Base == nil {
			return dst, false, true // an opaque scalar stands for nothing.
		}
		return append(dst, t.Base.Target), false, true
	default:
		return dst, false, true // no other kind exposes parts.
	}
}
