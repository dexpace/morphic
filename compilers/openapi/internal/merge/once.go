// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package merge

import (
	"fmt"
	"slices"
	"strings"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/ir"
)

// ReportOnce returns diags, a run's settlement reports, with each conflict
// between two declarations said once, where it is first. Every model composing
// both draws it alike, at the declaration that lost (folding.disagree), but for
// the member each decided (naming, deciding). One said for several models
// names how many and no member beside its place, as it decided one at each,
// and lists them all among what it concerns, in ascending order of their IDs,
// so no order the models settled in shows. Every other report stays, in order.
// It reuses diags' array.
func ReportOnce(diags []ir.Diagnostic) []ir.Diagnostic {
	said := map[ir.DiagnosticKey]*conflict{}
	for _, diagnostic := range diags {
		if diagnostic.Code != diag.ConflictingRedecl {
			continue
		}
		key := unnamed(diagnostic).Key()
		conflictValue := said[key]
		if conflictValue == nil {
			conflictValue = &conflict{}
			said[key] = conflictValue
		}
		conflictValue.models++
		conflictValue.decided = append(conflictValue.decided, decided(diagnostic)...)
	}
	if len(said) == 0 {
		return diags
	}
	out := diags[:0]
	for _, diagnostic := range diags {
		if diagnostic.Code != diag.ConflictingRedecl {
			out = append(out, diagnostic)
			continue
		}
		bare := unnamed(diagnostic)
		conflict, first := said[bare.Key()]
		if !first {
			continue
		}
		delete(said, bare.Key())
		if conflict.models > 1 {
			diagnostic = bare
			diagnostic.Message += fmt.Sprintf("; the same holds at each of %d models composing both", conflict.models)
			slices.SortFunc(conflict.decided, func(a, b ir.Related) int { return strings.Compare(a.Provenance.Node, b.Provenance.Node) })
			diagnostic.Related = append(diagnostic.Related, conflict.decided...)
		}
		out = append(out, diagnostic)
	}
	return out
}

// conflict is one conflict as every model composing its two declarations
// draws it: how many did, and the member each decided.
type conflict struct {
	models  int
	decided []ir.Related
}

// unnamed is diagnostic naming no member, beside its place or among what it concerns:
// what every model composing the two declarations says alike.
func unnamed(diagnostic ir.Diagnostic) ir.Diagnostic {
	diagnostic.Provenance.Node = ""
	diagnostic.Related = slices.DeleteFunc(slices.Clone(diagnostic.Related), isDecided)
	return diagnostic
}

// decided is the entries of diagnostic listing a member it decided.
func decided(diagnostic ir.Diagnostic) []ir.Related {
	return slices.DeleteFunc(slices.Clone(diagnostic.Related), func(related ir.Related) bool { return !isDecided(related) })
}

// isDecided reports whether related lists a node its finding decided.
func isDecided(related ir.Related) bool { return related.Role == ir.RelatedDecided }
