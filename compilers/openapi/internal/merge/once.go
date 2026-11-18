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
	for _, d := range diags {
		if d.Code != diag.ConflictingRedecl {
			continue
		}
		key := unnamed(d).Key()
		c := said[key]
		if c == nil {
			c = &conflict{}
			said[key] = c
		}
		c.models++
		c.decided = append(c.decided, decided(d)...)
	}
	if len(said) == 0 {
		return diags
	}
	out := diags[:0]
	for _, d := range diags {
		if d.Code != diag.ConflictingRedecl {
			out = append(out, d)
			continue
		}
		bare := unnamed(d)
		c, first := said[bare.Key()]
		if !first {
			continue
		}
		delete(said, bare.Key())
		if c.models > 1 {
			d = bare
			d.Message += fmt.Sprintf("; the same holds at each of %d models composing both", c.models)
			slices.SortFunc(c.decided, func(a, b ir.Related) int { return strings.Compare(a.Provenance.Node, b.Provenance.Node) })
			d.Related = append(d.Related, c.decided...)
		}
		out = append(out, d)
	}
	return out
}

// conflict is one conflict as every model composing its two declarations
// draws it: how many did, and the member each decided.
type conflict struct {
	models  int
	decided []ir.Related
}

// unnamed is d naming no member, beside its place or among what it concerns:
// what every model composing the two declarations says alike.
func unnamed(d ir.Diagnostic) ir.Diagnostic {
	d.Provenance.Node = ""
	d.Related = slices.DeleteFunc(slices.Clone(d.Related), isDecided)
	return d
}

// decided is the entries of d listing a member it decided.
func decided(d ir.Diagnostic) []ir.Related {
	return slices.DeleteFunc(slices.Clone(d.Related), func(r ir.Related) bool { return !isDecided(r) })
}

// isDecided reports whether r lists a node its finding decided.
func isDecided(r ir.Related) bool { return r.Role == ir.RelatedDecided }
