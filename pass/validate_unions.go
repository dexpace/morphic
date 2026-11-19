// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package pass

import (
	"fmt"

	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/scratchstate"
)

// checkRepeatedVariants reports each variant of a union naming a type an
// earlier variant names, which nothing in a value tells apart from it
// (docs/ir/unions.md#a-type-listed-twice): a value of the type matches both,
// so an exclusive union refuses it, a warning, and in an inclusive one neither
// variant admits a value the other does not, null aside, an info, each under a
// code of its own. A key-tagged union tells its variants apart by key, so it is
// left alone. Every union is read through the same map, so none costs one of
// its own.
func checkRepeatedVariants(doc *ir.Document, live []ir.TypeID) []ir.Diagnostic {
	var diags []ir.Diagnostic
	var first scratchstate.Map[ir.TypeID, int]
	for _, id := range live {
		union, isUnion := doc.Types[id].(*ir.Union)
		if !isUnion || (union.WireTagged && union.Discriminator == nil) {
			continue
		}
		seen := first.Emptied()
		for i, variant := range union.Variants {
			earlier, repeated := seen[variant.Type.Target]
			if !repeated {
				seen[variant.Type.Target] = i
				continue
			}
			diags = append(diags, repeatedVariant(union, earlier, i))
		}
	}
	return diags
}

// repeatedVariant reports that variant later of union names the type variant
// earlier does.
func repeatedVariant(union *ir.Union, earlier, later int) ir.Diagnostic {
	target := union.Variants[later].Type.Target
	at := fmt.Sprintf("%s/variants/%d", union.ID, later)
	if union.Exclusive {
		return diag(ir.SeverityWarning, "pass/exclusive-union-repeated-variant",
			fmt.Sprintf("variants %d and %d of union %s both name %s, so a value of it matches both, "+
				"and the union, which admits a value only one variant matches, refuses it", earlier, later, union.ID, target),
			at)
	}
	return diag(ir.SeverityInfo, "pass/inclusive-union-redundant-variant",
		fmt.Sprintf("variants %d and %d of union %s both name %s, so neither admits a value the other does not, "+
			"null aside", earlier, later, union.ID, target),
		at)
}
