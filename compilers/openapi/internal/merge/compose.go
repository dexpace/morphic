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
	base, hasBase := SelectBase(refs, func(k int) bool {
		m, ok := cs[k].node.(*ir.Model)
		return ok && m.Discriminator != nil
	})
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
// refs, the entries written as a $ref: the only one, or else the first whose
// target carries a discriminator, and false where none qualifies, which leaves
// them all Mixins (docs/compilers/openapi.md#allof-classification).
func SelectBase(refs []int, discriminated func(i int) bool) (int, bool) {
	if len(refs) == 1 {
		return refs[0], true
	}
	for _, i := range refs {
		if discriminated(i) {
			return i, true
		}
	}
	return 0, false
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
