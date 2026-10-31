// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package merge

import (
	"github.com/dexpace/morphic/ir"
)

// conjunct is one model a field composes: the index of the declaration naming
// it, and the model that declaration's type stands for.
type conjunct struct {
	decl  int
	model *ir.Model
}

// composition returns the models the field composes
// (docs/compilers/openapi.md#allof-redeclared-property): those of the declarations
// no other declaration's type holds, each type once, in declaration order. Two
// models say nothing of what they share, but an instance satisfies both, so
// the field's type is the composition of them all. It returns nothing where
// fewer than two remain, since one type then holds every other, or where one
// that remains is no model, which leaves the type to the winner.
func (f *folding) composition() []conjunct {
	judge := typeJudge{Resolve: f.g.Resolve}
	var conjuncts []conjunct
	for i, d := range f.decls {
		if f.heldByAnother(i) {
			continue
		}
		m, ok := judge.model(d.prop.Type)
		if !ok {
			return nil
		}
		conjuncts = append(conjuncts, conjunct{decl: i, model: m})
	}
	if len(conjuncts) < 2 {
		return nil
	}
	return conjuncts
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

// compose mints the model that composes f.conjuncts, as a written allOf of them
// composes: SelectBase picks its Base among those written as a $ref, and the
// rest are Mixins in declaration order. Only the target is carried. A conjunct
// says nothing of null, which is the field's reference's to say (foldType).
func (f *folding) compose() ir.TypeID {
	var refs []int
	for k, c := range f.conjuncts {
		if !hoistedAt(c.model, f.decls[c.decl].prop.Provenance) {
			refs = append(refs, k)
		}
	}
	base, hasBase := SelectBase(refs, func(k int) bool { return f.conjuncts[k].model.Discriminator != nil })
	composed := &ir.Model{}
	for k, c := range f.conjuncts {
		ref := ir.TypeRef{Target: f.decls[c.decl].prop.Type.Target}
		if hasBase && k == base {
			composed.Base = &ref
			continue
		}
		composed.Mixins = append(composed.Mixins, ref)
	}
	return f.g.Mint(f.field, composed)
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
