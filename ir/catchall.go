// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package ir

// CatchAllHolders returns the types holding a catch-all that a member of a
// value of type root falls under, each a key Content.CatchAllEncoding admits
// (docs/ir/payloads.md#a-catch-alls-parts): root, read through alias scalars,
// where it is a map, else each model its flattening reads holding
// AdditionalProps, root's first, as a catch-all is not flattened. A union is
// its caller's to read variant by variant. Each type is read once, so a cyclic
// composition or alias chain ends, and the walk takes one step per composition
// entry of the models it meets.
func CatchAllHolders(types TypeRegistry, root TypeID) []TypeID {
	seen := map[TypeID]bool{}
	id := throughAliases(types, root, seen)
	if m, ok := types[id].(*MapT); ok && m != nil {
		return []TypeID{id}
	}
	var holders []TypeID
	pending := []TypeID{id}
	for len(pending) > 0 {
		id, pending = throughAliases(types, pending[0], seen), pending[1:]
		m, ok := types[id].(*Model)
		if !ok || m == nil || seen[id] {
			continue
		}
		seen[id] = true
		if m.AdditionalProps != nil {
			holders = append(holders, id)
		}
		if m.Base != nil {
			pending = append(pending, m.Base.Target)
		}
		for _, r := range m.Implements {
			pending = append(pending, r.Target)
		}
		for _, r := range m.Mixins {
			pending = append(pending, r.Target)
		}
	}
	return holders
}

// throughAliases returns the type the alias scalars from id stand for, marking
// each in seen, and stops at one seen already, which ends a cyclic chain.
func throughAliases(types TypeRegistry, id TypeID, seen map[TypeID]bool) TypeID {
	for !seen[id] {
		base, alias := AliasBase(types, id)
		if !alias {
			return id
		}
		seen[id] = true
		id = base
	}
	return id
}
