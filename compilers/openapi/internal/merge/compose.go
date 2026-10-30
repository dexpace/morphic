// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package merge

import (
	"github.com/dexpace/morphic/ir"
)

// composition returns the declarations whose models the field composes: those
// no other declaration's type holds, each type once, in declaration order. Two
// models say nothing of what they share, but an instance satisfies both, so
// the field's type is the composition of them all. It returns nothing where
// fewer than two remain, since one type then holds every other, or where one
// that remains is no model, which leaves the type to the winner.
func (f *folding) composition() []int {
	judge := typeJudge{Resolve: f.g.Resolve}
	var parts []int
	for i, d := range f.decls {
		if f.heldByAnother(i) {
			continue
		}
		if _, ok := judge.model(d.prop.Type); !ok {
			return nil
		}
		parts = append(parts, i)
	}
	if len(parts) < 2 {
		return nil
	}
	return parts
}

// heldByAnother reports whether some other declaration's type already says all
// decls[i]'s does: one narrower, or the same one written first.
func (f *folding) heldByAnother(i int) bool {
	for j := range f.decls {
		if f.rel[j][i] == relANarrower || (f.rel[j][i] == relSame && j < i) {
			return true
		}
	}
	return false
}

// compose mints the model that composes parts, as a written allOf of them
// composes: SelectBase picks its Base among those written as a $ref, and the
// rest are Mixins in declaration order. Only the target is carried. A conjunct
// says nothing of null, which is the field's reference's to say (foldType).
func (f *folding) compose() ir.TypeID {
	judge := typeJudge{Resolve: f.g.Resolve}
	models := make(map[int]*ir.Model, len(f.parts))
	var refs []int
	for _, i := range f.parts {
		m, _ := judge.model(f.decls[i].prop.Type)
		models[i] = m
		if !hoistedAt(m, f.decls[i].prop.Provenance) {
			refs = append(refs, i)
		}
	}
	base, hasBase := SelectBase(refs, func(i int) bool { return models[i].Discriminator != nil })
	composed := &ir.Model{}
	for _, i := range f.parts {
		ref := ir.TypeRef{Target: f.decls[i].prop.Type.Target}
		if hasBase && i == base {
			composed.Base = &ref
			continue
		}
		composed.Mixins = append(composed.Mixins, ref)
	}
	return f.g.Mint(f.at(0), composed)
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

// model returns the Model ref names, directly or through aliases, and false
// where it names anything else.
func (j typeJudge) model(ref ir.TypeRef) (*ir.Model, bool) {
	td, ok := j.Resolve(ref.Target)
	if !ok {
		return nil, false
	}
	m, ok := j.unaliased(td).(*ir.Model)
	return m, ok
}
