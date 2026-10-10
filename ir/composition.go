package ir

import "slices"

// Supertypes returns the immediate supertypes of the model id names: its Base,
// then each Implements entry, every one read through the alias scalars standing
// in for it. Mixins are absent on purpose. A mixin contributes members without
// making the model a member of anything, so it belongs to [ExposedProps] and not
// to subtype identity. An id naming no live model yields nil.
func Supertypes(doc *Document, id TypeID) []TypeID {
	if doc == nil {
		return nil
	}
	m, ok := doc.Types[id].(*Model)
	if !ok || m == nil {
		return nil
	}
	var supers []TypeID
	if m.Base != nil {
		supers = append(supers, aliasTarget(doc, m.Base.Target))
	}
	for _, r := range m.Implements {
		supers = append(supers, aliasTarget(doc, r.Target))
	}
	return supers
}

// aliasTarget follows a chain of alias scalars, each a Scalar whose Base names
// another type, to the type it stands for, returning id unchanged when it names
// no alias. The IR permits a cyclic chain, so a visited set ends the walk.
func aliasTarget(doc *Document, id TypeID) TypeID {
	seen := make(map[TypeID]bool)
	for {
		s, ok := doc.Types[id].(*Scalar)
		if !ok || s == nil || s.Base == nil || seen[id] {
			return id
		}
		seen[id] = true
		id = s.Base.Target
	}
}

// IsSubtype reports whether target is a declared subtype of base at any distance
// along [Supertypes], the transitive closure a base tagging a grandchild needs.
// The relation is not reflexive unless a cycle leads back, and target itself is
// not read through an alias. A visited set ends the walk on a cyclic hierarchy.
func IsSubtype(doc *Document, target, base TypeID) bool {
	seen := map[TypeID]bool{}
	queue := []TypeID{target}
	for len(queue) > 0 {
		id := queue[len(queue)-1]
		queue = queue[:len(queue)-1]
		if seen[id] {
			continue
		}
		seen[id] = true
		supers := Supertypes(doc, id)
		if slices.Contains(supers, base) {
			return true
		}
		queue = append(queue, supers...)
	}
	return false
}

// ExposedProps returns the property IDs the type root names exposes: its own
// plus those it composes in through Base, Implements and Mixins (ir-design
// §4.3), the flat set an emitter renders. An alias scalar exposes the parts of
// the model its Base names. A root naming no live model exposes none. A visited
// set ends the walk on a cyclic hierarchy.
func ExposedProps(doc *Document, root TypeID) map[PropID]bool {
	props := map[PropID]bool{}
	if doc == nil {
		return props
	}
	seen := map[TypeID]bool{}
	queue := []TypeID{root}
	for len(queue) > 0 {
		id := queue[len(queue)-1]
		queue = queue[:len(queue)-1]
		if seen[id] {
			continue
		}
		seen[id] = true
		td := doc.Types[id]
		if IsNilTypeDef(td) {
			continue // undeclared, or a typed nil.
		}
		queue = partSources(queue, props, td)
	}
	return props
}

// partSources records td's own properties in props and appends the nodes the
// rest come from: a model's composition parents, an alias scalar's base. No
// other kind carries parts or says where to find them.
func partSources(dst []TypeID, props map[PropID]bool, td TypeDef) []TypeID {
	switch t := td.(type) {
	case *Model:
		for _, p := range t.Properties {
			props[p.ID] = true
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
		return dst
	case *Scalar:
		if t.Base == nil {
			return dst // an opaque scalar stands for nothing.
		}
		return append(dst, t.Base.Target)
	default:
		return dst // no other kind exposes parts.
	}
}
