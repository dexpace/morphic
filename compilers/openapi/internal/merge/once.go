// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package merge

import (
	"fmt"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/ir"
)

// ReportOnce returns diags, a run's settlement reports, with each conflict
// between two declarations said once, where it is first. Every model composing
// both draws it alike, at the declaration that lost (folding.disagree), but for
// the member each names (naming). One said for several models names how many
// and no member, as it decided one at each. Every other report stays, in
// order. It reuses diags' array.
func ReportOnce(diags []ir.Diagnostic) []ir.Diagnostic {
	folds := map[ir.Diagnostic]int{}
	for _, d := range diags {
		if d.Code == diag.ConflictingRedecl {
			folds[unnamed(d)]++
		}
	}
	if len(folds) == 0 {
		return diags
	}
	out := diags[:0]
	for _, d := range diags {
		if d.Code != diag.ConflictingRedecl {
			out = append(out, d)
			continue
		}
		key := unnamed(d)
		switch n := folds[key]; {
		case n == 0:
			continue
		case n > 1:
			d = key
			d.Message += fmt.Sprintf("; the same holds at each of %d models composing both", n)
		}
		folds[key] = 0
		out = append(out, d)
	}
	return out
}

// unnamed is d naming no member.
func unnamed(d ir.Diagnostic) ir.Diagnostic {
	d.Provenance.Node = ""
	return d
}
