// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package merge

import (
	"encoding/json/jsontext"

	"github.com/dexpace/morphic/ir"
)

// conjunct is one type a position composes: the member naming it, and the node
// that member's type stands for.
type conjunct struct {
	member
	node ir.TypeDef
}

// composition returns the types the position composes
// (docs/compilers/openapi.md#allof-redeclared-property): those of the members
// no other member's type holds, each once, in declaration order. Two models,
// or a model and a map or a union, say nothing of what they share, but an
// instance satisfies both, so the type there is a model composing them all.
// It returns nothing where fewer than two remain, since one type then holds
// every other, where none is a model, or where one is no model, map or union,
// such as a kind, which leaves the type to the winner.
func (p position) composition(reg registry) []conjunct {
	var conjuncts []conjunct
	model := false
	for i, m := range p.members {
		if p.heldByAnother(i) {
			continue
		}
		node, ok := reg.composable(m.ref)
		if !ok {
			return nil
		}
		_, isModel := node.(*ir.Model)
		model = model || isModel
		conjuncts = append(conjuncts, conjunct{member: m, node: node})
	}
	if len(conjuncts) < 2 || !model {
		return nil
	}
	return conjuncts
}

// heldByAnother reports whether some other member's type already says all
// member i's does: one narrower, or the same one written first.
func (p position) heldByAnother(i int) bool {
	for j := range p.members {
		if p.rel[j][i] == relANarrower || (p.rel[j][i] == relSame && j < i) {
			return true
		}
	}
	return false
}

// compose returns the model that composes cs, minted at pointer, as a written
// allOf of them composes: SelectBase picks its Base among those written as a
// $ref, and the rest are Mixins in declaration order. Only the target is
// carried. A conjunct says nothing of null, which the reference to the model
// says (position.null).
func (b *builder) compose(cs []conjunct, pointer jsontext.Pointer) ir.TypeID {
	var refs []int
	for k, c := range cs {
		if !hoistedAt(c.node, b.f.decls[c.decl].prop.Provenance) {
			refs = append(refs, k)
		}
	}
	base, hasBase := SelectBase(refs, func(k int) bool { return inHierarchy(b.f.g.Reg, cs[k].node) })
	composed := &ir.Model{}
	for k, c := range cs {
		ref := ir.TypeRef{Target: c.ref.Target}
		if hasBase && k == base {
			composed.Base = &ref
			continue
		}
		composed.Mixins = append(composed.Mixins, ref)
	}
	return b.mint(pointer, composed)
}

// SelectBase returns which entry of a composition becomes Model.Base, given
// refs, the entries written as a $ref: the only one, or else the first, in
// declaration order, whose target takes part in a discriminator hierarchy, and
// false where none does, which leaves them all Mixins
// (docs/compilers/openapi.md#allof-classification).
func SelectBase(refs []int, takesPart func(i int) bool) (int, bool) {
	if len(refs) == 1 {
		return refs[0], true
	}
	for _, i := range refs {
		if takesPart(i) {
			return i, true
		}
	}
	return 0, false
}

// inHierarchy reports whether node takes part in a discriminator hierarchy: a
// Model or Union holding a discriminator its source declares, or a Model one of
// whose supertypes does at any depth (ir.Supertypes), as a composition the
// source writes elects its Base. A visited set ends the walk on a cyclic
// hierarchy. A member lowering to a Union that holds no discriminator reads as
// none here, though the election over the source counts it (GitHub #1901).
func inHierarchy(reg ir.TypeRegistry, node ir.TypeDef) bool {
	doc := &ir.Document{Types: reg}
	seen := map[ir.TypeID]bool{}
	queue := []ir.TypeDef{node}
	for len(queue) > 0 {
		td := queue[len(queue)-1]
		queue = queue[:len(queue)-1]
		if !ir.IsNilTypeDef(td) && declaresDiscriminator(td) {
			return true
		}
		m, ok := td.(*ir.Model)
		if !ok || m == nil || seen[m.ID] {
			continue
		}
		seen[m.ID] = true
		for _, id := range ir.Supertypes(doc, m.ID) {
			queue = append(queue, reg[id])
		}
	}
	return false
}

// declaresDiscriminator reports whether td, which is not nil, holds a
// discriminator its source declares: a Model's, or a Union's, as a union written
// with structure holds the one written with it. One a policy inferred is no
// source's, so it makes no hierarchy, as the election over the source reads none.
func declaresDiscriminator(td ir.TypeDef) bool {
	var d *ir.Discriminator
	switch n := td.(type) {
	case *ir.Model:
		d = n.Discriminator
	case *ir.Union:
		d = n.Discriminator
	default:
		return false
	}
	return d != nil && !d.Inferred
}

// composable returns the node ref names, directly or through aliases, where
// it is a Model, a MapT or a Union, and false where it names anything else.
func (r registry) composable(ref ir.TypeRef) (ir.TypeDef, bool) {
	td, ok := r.resolve(ref.Target)
	if !ok {
		return nil, false
	}
	switch node := r.unaliased(td).(type) {
	case *ir.Model, *ir.MapT, *ir.Union:
		return node, true
	default:
		return nil, false
	}
}
