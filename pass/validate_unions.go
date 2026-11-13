// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package pass

import (
	"fmt"

	"github.com/dexpace/morphic/ir"
)

// checkRepeatedVariants reports each variant of a union naming a type an
// earlier variant names, which nothing in a value tells apart from it
// (docs/ir/unions.md#a-type-listed-twice): a value of the type matches both,
// so an exclusive union refuses it, a warning, and in an inclusive one neither
// variant admits a value the other does not, null aside, an info, each under a
// code of its own. A key-tagged union tells its variants apart by key, so it is
// left alone. Every union is read through the same map, so none costs one of
// its own.
func checkRepeatedVariants(doc *ir.Document) []ir.Diagnostic {
	var diags []ir.Diagnostic
	var first reusedMap[ir.TypeID, int]
	for _, id := range liveTypeIDs(doc) {
		u, isUnion := doc.Types[id].(*ir.Union)
		if !isUnion || (u.WireTagged && u.Discriminator == nil) {
			continue
		}
		seen := first.emptied()
		for i, v := range u.Variants {
			earlier, repeated := seen[v.Type.Target]
			if !repeated {
				seen[v.Type.Target] = i
				continue
			}
			diags = append(diags, repeatedVariant(u, earlier, i))
		}
	}
	return diags
}

// repeatedVariant reports that variant later of u names the type variant
// earlier does.
func repeatedVariant(u *ir.Union, earlier, later int) ir.Diagnostic {
	target := u.Variants[later].Type.Target
	at := fmt.Sprintf("%s/variants/%d", u.ID, later)
	if u.Exclusive {
		return diag(ir.SeverityWarning, "pass/exclusive-union-repeated-variant",
			fmt.Sprintf("variants %d and %d of union %s both name %s, so a value of it matches both, "+
				"and the union, which admits a value only one variant matches, refuses it", earlier, later, u.ID, target),
			at)
	}
	return diag(ir.SeverityInfo, "pass/inclusive-union-redundant-variant",
		fmt.Sprintf("variants %d and %d of union %s both name %s, so neither admits a value the other does not, "+
			"null aside", earlier, later, u.ID, target),
		at)
}
