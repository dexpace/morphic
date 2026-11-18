// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package merge

import (
	"encoding/json/jsontext"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/ir"
)

// TestReportOnce_SaysEachConflictOnce pins what a run's settlement reports:
// a conflict drawn alike at several models, but for the member each names,
// is said once, where it is first, with how many and no member; one drawn at
// one model keeps its member; two differing in what they say or where they
// sit are two; and every other report stays as it is, in order, repeats too.
// A report said for several lists the member each decided, in ascending order
// whatever order the models came in, after what all of them kept, and two
// keeping different declarations are two.
func TestReportOnce_SaysEachConflictOnce(t *testing.T) {
	t.Parallel()
	conflict := func(source int, node, msg string) ir.Diagnostic {
		return ir.Diagnostic{Severity: ir.SeverityWarning, Code: diag.ConflictingRedecl, Message: msg,
			Provenance: ir.Provenance{Source: source, Pointer: "/C/properties/p", Node: node}}
	}
	other := ir.Diagnostic{Severity: ir.SeverityInfo, Code: diag.DegradedConstruct, Message: "o",
		Provenance: ir.Provenance{Pointer: "/M/allOf/0", Node: "p/M"}}
	for name, tc := range map[string]struct{ in, want []ir.Diagnostic }{
		"none": {in: []ir.Diagnostic{other, other}, want: []ir.Diagnostic{other, other}},
		"one model": {
			in:   []ir.Diagnostic{conflict(0, "p/M", "x")},
			want: []ir.Diagnostic{conflict(0, "p/M", "x")},
		},
		"three models": {
			in: []ir.Diagnostic{other, conflict(0, "p/M", "x"), other, conflict(0, "p/N", "x"), conflict(0, "p/O", "x")},
			want: []ir.Diagnostic{other, conflict(0, "", "x; the same holds at each of 3 models composing both"),
				other},
		},
		"what they say differs": {
			in:   []ir.Diagnostic{conflict(0, "p/M", "x"), conflict(0, "p/N", "y")},
			want: []ir.Diagnostic{conflict(0, "p/M", "x"), conflict(0, "p/N", "y")},
		},
		"where they sit differs": {
			in:   []ir.Diagnostic{conflict(0, "p/M", "x"), conflict(1, "p/N", "x")},
			want: []ir.Diagnostic{conflict(0, "p/M", "x"), conflict(1, "p/N", "x")},
		},
		"the members each decided are listed in order": {
			in: []ir.Diagnostic{
				concerning(conflict(0, "p/N", "x"), "/K", "p/N"), other, concerning(conflict(0, "p/M", "x"), "/K", "p/M"),
			},
			want: []ir.Diagnostic{
				concerning(conflict(0, "", "x; the same holds at each of 2 models composing both"), "/K", "p/M", "p/N"), other,
			},
		},
		"one model keeps the member it lists": {
			in:   []ir.Diagnostic{concerning(conflict(0, "p/M", "x"), "/K", "p/M")},
			want: []ir.Diagnostic{concerning(conflict(0, "p/M", "x"), "/K", "p/M")},
		},
		"what they kept differs": {
			in:   []ir.Diagnostic{concerning(conflict(0, "p/M", "x"), "/K", "p/M"), concerning(conflict(0, "p/N", "x"), "/L", "p/N")},
			want: []ir.Diagnostic{concerning(conflict(0, "p/M", "x"), "/K", "p/M"), concerning(conflict(0, "p/N", "x"), "/L", "p/N")},
		},
	} {
		if diff := cmp.Diff(tc.want, ReportOnce(tc.in)); diff != "" {
			t.Errorf("%s (-want +got):\n%s", name, diff)
		}
	}
}

// concerning is d concerning the declaration kept at kept and the members
// decided, in the order a fold and ReportOnce list them.
func concerning(d ir.Diagnostic, kept jsontext.Pointer, decided ...string) ir.Diagnostic {
	d.Related = []ir.Related{{Role: ir.RelatedKept, Provenance: ir.Provenance{Pointer: kept}}}
	for _, node := range decided {
		d.Related = append(d.Related, ir.Related{Role: ir.RelatedDecided, Provenance: ir.Provenance{Source: ir.NoSource, Node: node}})
	}
	return d
}
