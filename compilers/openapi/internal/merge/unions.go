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
// (boundsBeside), and a closed union within the type where every variant is
// (unionWithin). Two unions, a tagged union, or a type neither holds, are
// unrelated here. A variant is judged on its union's side, against the site
// that union was declared at (declarationSites), so swapping a and b flips the
// verdict and changes nothing else.
func unionArms(typeJudgeValue typeJudge, a ir.TypeRef, at ir.TypeDef, b ir.TypeRef, bt ir.TypeDef, depth int) relation {
	au, aUnion := typeJudgeValue.unaliased(at).(*ir.Union)
	bu, bUnion := typeJudgeValue.unaliased(bt).(*ir.Union)
	if aUnion == bUnion {
		return relUnrelated
	}
	if bUnion {
		against := func(typeRef ir.TypeRef) relation { return typeJudgeValue.relateAt(a, typeRef, depth+1) }
		return byVariants(typeJudgeValue, against, bu, !boundsBeside(typeJudgeValue.registry, bt))
	}
	against := func(typeRef ir.TypeRef) relation { return typeJudgeValue.relateAt(typeRef, b, depth+1).flipped() }
	return byVariants(typeJudgeValue, against, au, !boundsBeside(typeJudgeValue.registry, at)).flipped()
}

// boundsBeside reports whether td, a union read through aliases, or an alias
// on the way holds bounds of its own. Each bounds the values of its kind
// whichever variant holds them (docs/ir/unions.md#bounds-beside-a-union),
// which the variants do not say, so a type within one of them is not shown to
// be within td, and a field taking it would lose them.
func boundsBeside(registryValue registry, td ir.TypeDef) bool {
	chain, end := registryValue.aliasChain(td)
	if union, ok := end.(*ir.Union); ok && union.Constraints != nil {
		return true
	}
	return slices.ContainsFunc(chain, func(scalar *ir.Scalar) bool { return scalar.Constraints != nil })
}

// byVariants relates a type to union, an untagged union, where against is the type
// judged against one of union's variants: narrower where it is within union, which
// holds says it may be, broader where union is within it, and unrelated otherwise.
func byVariants(typeJudgeValue typeJudge, against func(typeRef ir.TypeRef) relation, union *ir.Union, holds bool) relation {
	switch {
	case holds && withinUnion(typeJudgeValue, against, union):
		return firstTypeIsNarrower
	case unionWithin(typeJudgeValue, against, union):
		return secondTypeIsNarrower
	default:
		return relUnrelated
	}
}

// tagged reports whether union is tagged, so its variants' values say nothing of
// its own: a key-tagged union's wrap a variant's, and a discriminator routes by
// a tag a variant's values need not agree with.
func tagged(union *ir.Union) bool { return union.WireTagged || union.Discriminator != nil }

// unionWithin reports whether every value of union, an untagged union, is a value of
// the type against judges: union is closed and has a variant, and each is within
// that type. An open union admits a variant it does not list, which nothing
// holds within the type. A read past the judgment's budget is no proof.
func unionWithin(typeJudgeValue typeJudge, against func(typeRef ir.TypeRef) relation, union *ir.Union) bool {
	if tagged(union) || !union.Closed || len(union.Variants) == 0 {
		return false
	}
	for _, variant := range union.Variants {
		if typeJudgeValue.variants == nil || !typeJudgeValue.variants.Spend(1) {
			return false
		}
		if rel := against(variant.Type); rel != relSame && rel != secondTypeIsNarrower {
			return false
		}
	}
	return true
}

// withinUnion reports whether every value of the type against judges is a
// value of union, an untagged union: the type is within a variant of an inclusive
// one, which admits what any variant does, or within exactly one variant of an
// exclusive one and disjoint from every other, as one admits no value two
// variants share. A read past the judgment's budget is no proof.
func withinUnion(typeJudgeValue typeJudge, against func(typeRef ir.TypeRef) relation, union *ir.Union) bool {
	if tagged(union) {
		return false
	}
	within := 0
	for _, variant := range union.Variants {
		if typeJudgeValue.variants == nil || !typeJudgeValue.variants.Spend(1) {
			return false
		}
		switch rel := against(variant.Type); {
		case rel == relSame || rel == firstTypeIsNarrower:
			if !union.Exclusive {
				return true
			}
			within++
		case union.Exclusive && rel != relDisjoint:
			return false
		}
	}
	return within == 1
}
