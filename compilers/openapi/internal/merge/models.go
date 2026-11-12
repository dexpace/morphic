// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package merge

import (
	"reflect"
	"slices"

	"github.com/dexpace/morphic/compilers/openapi/internal/budget"
	"github.com/dexpace/morphic/ir"
)

// maxModelPairs bounds the pairs of models one judgment compares member by
// member (models). A $ref to a sibling position adds width at no cost in
// document size, so nested models compared without it could cost their width to
// the power of the depth. Stopping early only leaves two models unrelated,
// which compose, and that loses nothing.
const maxModelPairs = 64

// modelPairs is a fresh budget for the model pairs one judgment compares.
func modelPairs() *budget.Budget {
	b := budget.New(budget.AtMost(maxModelPairs))
	return &b
}

// models relates two models alike in everything but where they and the nodes
// they name were declared (modelSkeleton), as the copy rule relates identical
// nodes (sameNodes): where every type one names relates to the other's as the
// same type, or as that rule relates the two models, the one it picks loses
// nothing of the other. Two models declared apart overlap, and so compose. Any
// other pair is unrelated here, as is one past the judgment's budget.
func (j typeJudge) models(a ir.TypeRef, am *ir.Model, b ir.TypeRef, bm *ir.Model, depth int) relation {
	if j.modelPairs == nil || !j.modelPairs.Spend(1) {
		return relUnrelated
	}
	as, aok := modelSkeleton(am)
	bs, bok := modelSkeleton(bm)
	if !aok || !bok || as.tag != bs.tag || len(as.refs) != len(bs.refs) || !reflect.DeepEqual(as.model, bs.model) {
		return relUnrelated
	}
	if (membersKeepReference(am) || membersKeepReference(bm)) && !j.sites.walk.Alike(am.Provenance, bm.Provenance) {
		return relUnrelated
	}
	outer := j.sites.copies(a, am, b, bm)
	for i := range as.refs {
		rel := j.relateAt(as.refs[i], bs.refs[i], depth+1)
		if rel != relSame && (rel != outer || outer == relOverlap) {
			return relUnrelated
		}
	}
	return outer
}

// skeleton is a model with what names where it and its members were declared
// cleared, as siteless clears a node's: its own site, each property's ID and
// provenance and those of what it keeps under Unmodeled. Each type it names is
// set apart in refs, in order, with only its target cleared in model, and the
// discriminator's property is read as its position, tag, or -1 where it has
// none.
type skeleton struct {
	model ir.Model
	refs  []ir.TypeRef
	tag   int
}

// modelSkeleton returns m's skeleton, and false where its discriminator names
// no property of m.
func modelSkeleton(m *ir.Model) (skeleton, bool) {
	sk := skeleton{model: *m, tag: -1}
	take := func(r ir.TypeRef) ir.TypeRef {
		sk.refs = append(sk.refs, r)
		r.Target = ""
		return r
	}
	sk.model.TypeCommon = siteless(m.TypeCommon)
	sk.model.Properties = make([]ir.Property, len(m.Properties))
	for i, p := range m.Properties {
		p.ID, p.Provenance, p.Unmodeled = "", ir.Provenance{}, sitelessUnmodeled(p.Unmodeled)
		p.Type = take(p.Type)
		sk.model.Properties[i] = p
	}
	if m.Base != nil {
		sk.model.Base = new(take(*m.Base))
	}
	sk.model.Implements = takeAll(m.Implements, take)
	sk.model.Mixins = takeAll(m.Mixins, take)
	if m.AdditionalProps != nil {
		sk.model.AdditionalProps = additionalSkeleton(*m.AdditionalProps, take)
	}
	if d := m.Discriminator; d != nil && d.Property != "" {
		sk.tag = slices.IndexFunc(m.Properties, func(p ir.Property) bool { return p.ID == d.Property })
		if sk.tag < 0 {
			return skeleton{}, false
		}
		own := *d
		own.Property = ""
		sk.model.Discriminator = &own
	}
	return sk, true
}

// takeAll applies take to each of refs, in order, into a new slice.
func takeAll(refs []ir.TypeRef, take func(ir.TypeRef) ir.TypeRef) []ir.TypeRef {
	if refs == nil {
		return nil
	}
	out := make([]ir.TypeRef, len(refs))
	for i, r := range refs {
		out[i] = take(r)
	}
	return out
}

// additionalSkeleton is modelSkeleton's reading of a catch-all: each type it
// names, value, key and each pattern's value in that order, taken by take.
func additionalSkeleton(ap ir.AdditionalProps, take func(ir.TypeRef) ir.TypeRef) *ir.AdditionalProps {
	if ap.Value != nil {
		ap.Value = new(take(*ap.Value))
	}
	if ap.Key != nil {
		ap.Key = new(take(*ap.Key))
	}
	patterns := slices.Clone(ap.Patterns)
	for i := range patterns {
		patterns[i].Value = take(patterns[i].Value)
	}
	ap.Patterns = patterns
	return &ap
}

// sitelessUnmodeled copies um with each entry's provenance cleared, as siteless
// clears a node's.
func sitelessUnmodeled(um ir.Unmodeled) ir.Unmodeled {
	return siteless(ir.TypeCommon{Unmodeled: um}).Unmodeled
}

// membersKeepReference reports whether a property of m keeps a reference under
// Unmodeled (keepsReference), which reads as the same text only against one
// base.
func membersKeepReference(m *ir.Model) bool {
	return slices.ContainsFunc(m.Properties, func(p ir.Property) bool {
		return keepsReference(&ir.Scalar{Unmodeled: p.Unmodeled})
	})
}
