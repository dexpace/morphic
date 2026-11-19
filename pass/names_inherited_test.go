// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package pass_test

import (
	"fmt"
	"math"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/pass"
)

// spelled is the property id spelled source, wired as wire.
func spelled(id, source, wire string) ir.Property {
	return ir.Property{ID: ir.PropertyID(id), Name: named(source), WireName: wire}
}

// extending is the model id named after itself, extending base, with
// properties.
func extending(id string, base string, properties ...ir.Property) *ir.Model {
	model := &ir.Model{ID: ir.TypeID(id), Name: named(id), Properties: properties}
	if base != "" {
		model.Base = &ir.TypeRef{Target: ir.TypeID(base)}
	}
	return model
}

// exposed is the warning at the model id about the exposed properties it
// reports, in the words of the scope: every row of them shares user_id.
func exposed(id, parties string) ir.Diagnostic {
	return collision(id, fmt.Sprintf(`in model %s, exposed properties %s share the neutral name "user_id"`, id, parties))
}

// TestNames_ReadsAPropertyMeetingOneItsModelInherits holds a model that exposes
// two properties of one neutral name, one of them inherited, to one finding at
// that model: where a base and a derived model each spell the name their own
// way, and where two mixins do. A model below the first draws none, since the
// model it extends already met the pair, and an override, which a target
// renders as one member, draws none.
func TestNames_ReadsAPropertyMeetingOneItsModelInherits(t *testing.T) {
	t.Parallel()
	const baseUser = `0 of model Base ("userId") and 0 ("user_id")`
	for _, tc := range []struct {
		name string
		doc  *ir.Document
		want []ir.Diagnostic
	}{
		{"a derived model's property beside its base's", withTypes(
			extending("Base", "", spelled("property/base/0", "userId", "userId")),
			extending("Child", "Base", spelled("property/child/0", "user_id", "user_id")),
			extending("GrandChild", "Child"),
		), []ir.Diagnostic{exposed("Child", baseUser)}},
		{"the same, the derived property an override", withTypes(
			extending("Base", "", spelled("property/base/0", "userId", "userId")),
			extending("Child", "Base", spelled("property/child/0", "userId", "userId")),
			extending("GrandChild", "Child"),
		), nil},
		{"two mixins of one model, which declares none", withTypes(
			extending("A", "", spelled("property/a/0", "userId", "userId")),
			extending("B", "", spelled("property/b/0", "user_id", "user_id")),
			&ir.Model{ID: "Both", Name: named("Both"), Mixins: []ir.TypeRef{{Target: "A"}, {Target: "B"}}},
			extending("Below", "Both"),
		), []ir.Diagnostic{exposed("Both", `0 of model A ("userId") and 0 of model B ("user_id")`)}},
		{"the same mixins, the second's property wired as the first's", withTypes(
			extending("A", "", spelled("property/a/0", "userId", "userId")),
			extending("B", "", spelled("property/b/0", "user_id", "userId")),
			&ir.Model{ID: "Both", Name: named("Both"), Mixins: []ir.TypeRef{{Target: "A"}, {Target: "B"}}},
			extending("Below", "Both"),
		), nil},
		{"two properties of one model, which its own scope reports", withTypes(
			extending("Base", "", spelled("property/base/0", "userId", "userId"), spelled("property/base/1", "user_id", "user_id")),
			extending("Child", "Base"),
		), []ir.Diagnostic{collision("Base", `in model Base, properties 0 ("userId") and 1 ("user_id") share the neutral name "user_id"`)}},
		{"a base and a derived model of different names", withTypes(
			extending("Base", "", spelled("property/base/0", "userId", "userId")),
			extending("Child", "Base", spelled("property/child/0", "accountId", "accountId")),
		), nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			for range 16 {
				if !assert.Empty(t, cmp.Diff(tc.want, namesOf(t, tc.doc))) {
					break
				}
			}
		})
	}
}

// chainOf is n models, each extending the one before it and declaring one
// property of a name of its own, so none meets another spelling of a name.
func chainOf(n int) *ir.Document {
	doc := &ir.Document{Types: ir.TypeRegistry{}}
	base := ""
	for i := range n {
		id := fmt.Sprintf("M%03d", i)
		doc.Types[ir.TypeID(id)] = extending(id, base, spelled("property/"+id, fmt.Sprintf("prop%d", i), fmt.Sprintf("prop%d", i)))
		base = id
	}
	return doc
}

// TestNames_ReadingInheritedPropertiesCostsNothingDownAChain holds a chain of
// models that no two spellings of a name meet in to the cost it has without the
// check: allocations do not rise with its length, so no model is flattened
// for a pair that is not there. It is not run in parallel: AllocsPerRun
// measures the process.
func TestNames_ReadingInheritedPropertiesCostsNothingDownAChain(t *testing.T) {
	const short, long = 50, 100
	require.Empty(t, namesOf(t, chainOf(long)), "the chain is sound, or what is measured is a report")

	cost := func(doc *ir.Document) float64 {
		fewest := math.Inf(1)
		for range 20 {
			fewest = min(fewest, testing.AllocsPerRun(1, func() { pass.Names{}.Run(t.Context(), doc) }))
		}
		return fewest
	}
	grown := cost(chainOf(long)) - cost(chainOf(short))

	assert.Less(t, grown, float64(short), "%v allocations more for %d models more", grown, long-short)
}
