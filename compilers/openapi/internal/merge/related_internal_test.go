// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package merge

import (
	"encoding/json/jsontext"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/ir"
)

// settledModel builds the model id in reg, written at at, as compose does, by
// the entries its steps compose, settles it, and returns what it reports.
func settledModel(t *testing.T, reg ir.TypeRegistry, id ir.TypeID, at string, steps ...func(idx *ModelPropertyDeclarations, at string)) []ir.Diagnostic {
	t.Helper()
	m := &ir.Model{ID: id, Provenance: ptrAt(jsontext.Pointer(at))}
	reg[id] = m
	idx := NewModelPropertyDeclarations(m)
	for _, step := range steps {
		step(idx, at)
	}
	g, _ := stubMerger(reg)
	return settleFlat(g, idx, reg)
}

// partAt composes target as the model's allOf entry n.
func partAt(target ir.TypeID, n string) func(idx *ModelPropertyDeclarations, at string) {
	return func(idx *ModelPropertyDeclarations, at string) {
		idx.m.Mixins = append(idx.m.Mixins, ir.TypeRef{Target: target})
		idx.Compose(target, jsontext.Pointer(at+"/allOf/"+n))
	}
}

// ownAt declares wire as target in the model's own entry n.
func ownAt(wire string, target ir.TypeID, n string) func(idx *ModelPropertyDeclarations, at string) {
	return func(idx *ModelPropertyDeclarations, at string) {
		idx.MergeProperty(declared(wire, target, at+"/allOf/"+n+"/properties/"+wire), written(`{"own":true}`), "")
	}
}

// TestReportOnce_AConflictListsWhatItKeptAndThenWhatItDecided holds the order
// this compiler gives a conflict report's related entries, which the IR leaves
// to the producer (docs/ir/provenance.md#related-places): exactly one kept
// place, first, then every member the report decided, ascending by ID, however
// the declarations meet: two or three, a $ref chain, a model's own declaration
// against an entry's, one model or five settled out of order, two members of
// one pair, and a model composing one that settled the pair.
func TestReportOnce_AConflictListsWhatItKeptAndThenWhatItDecided(t *testing.T) {
	t.Parallel()
	registry := func() ir.TypeRegistry {
		reg := membersRegistry()
		reg["t/E"] = &ir.Model{ID: "t/E", Properties: []ir.Property{declared("p", "t/prim/bool", "/E/p")}}
		reg["t/prim/bool"] = &ir.Primitive{ID: "t/prim/bool", Prim: ir.PrimBool}
		return reg
	}
	for name, tc := range map[string]struct {
		settle  func(reg ir.TypeRegistry) []ir.Diagnostic
		reports int
		decided []int
	}{
		"two declarations": {
			settle: func(reg ir.TypeRegistry) []ir.Diagnostic {
				return settledModel(t, reg, "t/M", "/M", partAt("t/A", "0"), partAt("t/B", "1"))
			},
			reports: 1, decided: []int{1},
		},
		"three declarations": {
			settle: func(reg ir.TypeRegistry) []ir.Diagnostic {
				return settledModel(t, reg, "t/M", "/M", partAt("t/A", "0"), partAt("t/B", "1"), partAt("t/E", "2"))
			},
			reports: 2, decided: []int{1, 1},
		},
		"a $ref chain": {
			settle: func(reg ir.TypeRegistry) []ir.Diagnostic {
				return settledModel(t, reg, "t/M", "/M", partAt(chainTo(reg, "t/B", 3), "0"), partAt("t/A", "1"))
			},
			reports: 1, decided: []int{1},
		},
		"a model's own declaration against an entry's": {
			settle: func(reg ir.TypeRegistry) []ir.Diagnostic {
				return settledModel(t, reg, "t/M", "/M", ownAt("p", "t/prim/string", "0"), partAt("t/B", "1"))
			},
			reports: 1, decided: []int{1},
		},
		"five models settled out of order": {
			settle: func(reg ir.TypeRegistry) []ir.Diagnostic {
				var diags []ir.Diagnostic
				for _, m := range []string{"M3", "M1", "M5", "M2", "M4"} {
					diags = append(diags, settledModel(t, reg, ir.TypeID("t/"+m), "/"+m, partAt("t/A", "0"), partAt("t/B", "1"))...)
				}
				return diags
			},
			reports: 1, decided: []int{5},
		},
		"two members of one pair": {
			settle: func(reg ir.TypeRegistry) []ir.Diagnostic {
				return settledModel(t, reg, "t/M", "/M", partAt("t/C", "0"), partAt("t/D", "1"))
			},
			reports: 2, decided: []int{1, 1},
		},
		"a model composing one that settled the pair": {
			settle: func(reg ir.TypeRegistry) []ir.Diagnostic {
				diags := settledModel(t, reg, "t/N", "/N", partAt("t/A", "0"), partAt("t/B", "1"))
				return append(diags, settledModel(t, reg, "t/M", "/M", partAt("t/N", "0"), partAt("t/E", "1"))...)
			},
			reports: 2, decided: []int{1, 1},
		},
	} {
		got := ReportOnce(tc.settle(registry()))

		var counts []int
		for _, d := range got {
			require.Equal(t, diag.ConflictingRedecl, d.Code, name)
			require.NotEmpty(t, d.Related, name)
			assert.Equal(t, ir.RelatedKept, d.Related[0].Role, "%s: the kept place comes first", name)
			assert.NotEmpty(t, d.Related[0].Provenance.Pointer, "%s: the kept place is a declaration's", name)
			decided := d.Related[1:]
			nodes := make([]string, len(decided))
			for i, r := range decided {
				assert.Equal(t, ir.RelatedDecided, r.Role, "%s: one kept place, then the members decided", name)
				assert.Equal(t, ir.Provenance{Source: ir.NoSource, Node: r.Provenance.Node}, r.Provenance, name)
				nodes[i] = r.Provenance.Node
			}
			assert.True(t, slices.IsSorted(nodes), "%s: the members ascend by ID: %v", name, nodes)
			assert.Len(t, slices.Compact(slices.Clone(nodes)), len(nodes), "%s: each member once", name)
			counts = append(counts, len(decided))
		}
		assert.Len(t, got, tc.reports, name)
		assert.Equal(t, tc.decided, counts, name)
	}
}
