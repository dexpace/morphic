// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package merge

import (
	"slices"

	"github.com/dexpace/morphic/ir"
)

// maxVariantReads bounds the union variants one judgment relates a type to
// (unionArms), which a union nested in a variant multiplies by its own. Stopping
// early only leaves a type unrelated to a union, which the two then compose,
// and that loses nothing.
const maxVariantReads = 64

// unionArms relates a type to an untagged union, each read through aliases, by
// the union's variants: the type is within the union where it is within one
// variant (withinUnion) and nothing on the way bounds the union's values
// (boundsBeside), and the union within the type where every variant is
// (unionWithin). Two unions, a tagged union, or a type neither holds, are
// unrelated here. A variant is judged on its union's side, against the site
// that union was declared at (declarationSites), so swapping a and b flips the
// verdict and changes nothing else.
func unionArms(j typeJudge, a ir.TypeRef, at ir.TypeDef, b ir.TypeRef, bt ir.TypeDef, depth int) relation {
	au, aUnion := j.unaliased(at).(*ir.Union)
	bu, bUnion := j.unaliased(bt).(*ir.Union)
	if aUnion == bUnion {
		return relUnrelated
	}
	if bUnion {
		against := func(v ir.TypeRef) relation { return j.relateAt(a, v, depth+1) }
		return byVariants(j, against, bu, !j.boundsBeside(bt))
	}
	against := func(v ir.TypeRef) relation { return j.relateAt(v, b, depth+1).flipped() }
	return byVariants(j, against, au, !j.boundsBeside(at)).flipped()
}

// boundsBeside reports whether td, a union read through aliases, or an alias
// on the way holds bounds of its own. Each bounds the values of its kind
// whichever variant holds them (docs/ir/unions.md#bounds-beside-a-union),
// which the variants do not say, so a type within one of them is not shown to
// be within td, and a field taking it would lose them.
func (j typeJudge) boundsBeside(td ir.TypeDef) bool {
	chain, end := j.aliasChain(td)
	if u, ok := end.(*ir.Union); ok && u.Constraints != nil {
		return true
	}
	return slices.ContainsFunc(chain, func(s *ir.Scalar) bool { return s.Constraints != nil })
}

// byVariants relates a type to u, an untagged union, where against is the type
// judged against one of u's variants: narrower where it is within u, which
// holds says it may be, broader where u is within it, and unrelated otherwise.
func byVariants(j typeJudge, against func(v ir.TypeRef) relation, u *ir.Union, holds bool) relation {
	switch {
	case holds && withinUnion(j, against, u):
		return relANarrower
	case unionWithin(j, against, u):
		return relBNarrower
	default:
		return relUnrelated
	}
}

// tagged reports whether u is tagged, so its variants' values say nothing of
// its own: a key-tagged union's wrap a variant's, and a discriminator routes by
// a tag a variant's values need not agree with.
func tagged(u *ir.Union) bool { return u.WireTagged || u.Discriminator != nil }

// unionWithin reports whether every value of u, an untagged union, is a value of
// the type against judges: u has a variant, and each is within that type. A
// read past the judgment's budget is no proof.
func unionWithin(j typeJudge, against func(v ir.TypeRef) relation, u *ir.Union) bool {
	if tagged(u) || len(u.Variants) == 0 {
		return false
	}
	for _, v := range u.Variants {
		if j.variants == nil || !j.variants.Spend(1) {
			return false
		}
		if rel := against(v.Type); rel != relSame && rel != relBNarrower {
			return false
		}
	}
	return true
}

// withinUnion reports whether every value of the type against judges is a
// value of u, an untagged union: the type is within a variant of an inclusive
// one, which admits what any variant does, or within exactly one variant of an
// exclusive one and disjoint from every other, as one admits no value two
// variants share. A read past the judgment's budget is no proof.
func withinUnion(j typeJudge, against func(v ir.TypeRef) relation, u *ir.Union) bool {
	if tagged(u) {
		return false
	}
	within := 0
	for _, v := range u.Variants {
		if j.variants == nil || !j.variants.Spend(1) {
			return false
		}
		switch rel := against(v.Type); {
		case rel == relSame || rel == relANarrower:
			if !u.Exclusive {
				return true
			}
			within++
		case u.Exclusive && rel != relDisjoint:
			return false
		}
	}
	return within == 1
}
