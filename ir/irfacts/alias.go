// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package irfacts

import "github.com/dexpace/morphic/ir"

// aliasTargets holds, for each alias scalar of a registry, the type it stands
// for, as ir.Supertypes reads a reference: along ir.AliasBase to a type that is
// no alias, or on a cyclic chain to the first type the walk meets twice. Each
// scalar is resolved once, so a chain named from many places costs its length
// once rather than once per reference.
type aliasTargets map[ir.TypeID]ir.TypeID

// newAliasTargets resolves every alias scalar of types. A walk from a scalar
// stops at a type already resolved, at one that is no alias, or at one on its
// own path, which closes a cycle; each type it passed is then settled. So a
// later walk finds a type an earlier one left in walked resolved first.
func newAliasTargets(types ir.TypeRegistry) aliasTargets {
	a := aliasTargets{}
	walked := map[ir.TypeID]int{} // each type's position on the walk that met it
	var path []ir.TypeID
	for id := range types {
		path = path[:0]
		for cur := id; ; {
			if t, done := a[cur]; done {
				a.settle(path, t)
				break
			}
			if at, cyclic := walked[cur]; cyclic {
				a.settleCycle(path, at)
				break
			}
			base, alias := ir.AliasBase(types, cur)
			if !alias {
				a.settle(path, cur)
				break
			}
			walked[cur] = len(path)
			path = append(path, cur)
			cur = base
		}
	}
	return a
}

// settle records target for every scalar on path.
func (a aliasTargets) settle(path []ir.TypeID, target ir.TypeID) {
	for _, id := range path {
		a[id] = target
	}
}

// settleCycle records the scalars of a path whose last one names path[at]
// again. Each scalar on the cycle is the first type its own walk meets twice,
// so it stands for itself; each before the cycle meets path[at] twice first.
func (a aliasTargets) settleCycle(path []ir.TypeID, at int) {
	a.settle(path[:at], path[at])
	for _, id := range path[at:] {
		a[id] = id
	}
}

// target returns the type id stands for, which is id itself when it names no
// alias scalar.
func (a aliasTargets) target(id ir.TypeID) ir.TypeID {
	if t, ok := a[id]; ok {
		return t
	}
	return id
}
