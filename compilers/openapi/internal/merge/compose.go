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
	for i, member := range p.members {
		if p.heldByAnother(i) {
			continue
		}
		node, ok := reg.composable(member.ref)
		if !ok {
			return nil
		}
		_, isModel := node.(*ir.Model)
		model = model || isModel
		conjuncts = append(conjuncts, conjunct{member: member, node: node})
	}
	if len(conjuncts) < 2 || !model {
		return nil
	}
	return conjuncts
}

// heldByAnother reports whether some other member's type already says all
// member i's does: one narrower, or the same one standing for it (stands).
func (p position) heldByAnother(i int) bool {
	for j := range p.members {
		if p.rel[j][i] == firstTypeIsNarrower || (p.rel[j][i] == relSame && p.stands(j, i)) {
			return true
		}
	}
	return false
}

// compose returns the model that composes cs, minted at pointer, as a written
// allOf of them composes: SelectBase picks its Base among those written as a
// $ref, and the rest are Mixins in declaration order, so the model's own
// members settle in that order too. Only the target is carried. A conjunct says
// nothing of null, which the reference to the model says (position.null). The
// pointer is the declaration last to widen the field (fieldSpan), so swapping
// two entries renames the model, and moves a report on a member it settles
// (GitHub #1921, #1992).
func (b *builder) compose(cs []conjunct, pointer jsontext.Pointer) ir.TypeID {
	var refs []int
	for k, conjunct := range cs {
		if !hoistedAt(conjunct.node, b.f.decls[conjunct.decl].sites()) {
			refs = append(refs, k)
		}
	}
	base, hasBase := SelectBase(refs, func(k int) bool { return inHierarchy(b.f.g.Reg, cs[k].node) })
	composed := &ir.Model{}
	var brought []jsontext.Pointer
	for k, conjunct := range cs {
		ref := ir.TypeRef{Target: conjunct.ref.Target}
		if hasBase && k == base {
			composed.Base = &ref
			brought = append([]jsontext.Pointer{b.f.decls[conjunct.decl].reportAt()}, brought...)
			continue
		}
		composed.Mixins = append(composed.Mixins, ref)
		brought = append(brought, b.f.decls[conjunct.decl].reportAt())
	}
	return b.mint(pointer, composed, brought)
}

// SelectBase returns which entry of a composition becomes Model.Base, given
// refs, the entries written as a $ref in the order the caller ranks them: the
// only one, or else the first of those whose target takes part in a
// discriminator hierarchy, and false where none does, which leaves them all
// Mixins (docs/compilers/openapi.md#allof-classification).
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
		model, ok := td.(*ir.Model)
		if !ok || model == nil || seen[model.ID] {
			continue
		}
		seen[model.ID] = true
		for _, id := range ir.Supertypes(doc, model.ID) {
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
	var discriminator *ir.Discriminator
	switch n := td.(type) {
	case *ir.Model:
		discriminator = n.Discriminator
	case *ir.Union:
		discriminator = n.Discriminator
	default:
		return false
	}
	return discriminator != nil && !discriminator.Inferred
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
