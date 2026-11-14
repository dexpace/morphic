// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package pass_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"

	"github.com/dexpace/morphic/ir"
)

// TestNames_ReadsAnExposedPairAtTheModelThatMetItAlone holds a model that
// composes two others, one of which met the pair already, to no finding of its
// own: the pair is told at the model below which it first met, however many
// models compose that one, and a model that declares one of the two is told
// as well at its own place.
func TestNames_ReadsAnExposedPairAtTheModelThatMetItAlone(t *testing.T) {
	t.Parallel()
	both := &ir.Model{ID: "Both", Name: named("Both"), Mixins: []ir.TypeRef{{Target: "A"}, {Target: "B"}}}
	doc := withTypes(
		extending("A", "", spelled("p/a/0", "userId", "userId")),
		extending("B", "", spelled("p/b/0", "user_id", "user_id")),
		both,
		&ir.Model{ID: "Diamond", Name: named("Diamond"), Mixins: []ir.TypeRef{{Target: "Both"}, {Target: "C"}}},
		extending("C", "", spelled("p/c/0", "other", "other")),
	)
	want := []ir.Diagnostic{exposed("Both", `0 of model A ("userId") and 0 of model B ("user_id")`)}

	for range 16 {
		if !assert.Empty(t, cmp.Diff(want, namesOf(t, doc))) {
			break
		}
	}
}

// TestNames_ReadsAModelsOwnPropertiesOnceWhateverItExtends holds a model that
// extends another and spells one name two ways itself to the report of its own
// scope alone, and a model whose own property meets a pair its base holds to a
// finding of its own: the base's report says the pair, and the model adds the
// third spelling the base does not hold.
func TestNames_ReadsAModelsOwnPropertiesOnceWhateverItExtends(t *testing.T) {
	t.Parallel()
	own := withTypes(
		extending("Base", "", spelled("p/base/0", "other", "other")),
		extending("Child", "Base", spelled("p/child/0", "userId", "userId"), spelled("p/child/1", "user_id", "user_id")),
	)
	third := withTypes(
		extending("Base", "", spelled("p/base/0", "userId", "userId"), spelled("p/base/1", "user_id", "user_id")),
		extending("Child", "Base", spelled("p/child/0", "UserId", "UserId")),
	)
	for _, tc := range []struct {
		name string
		doc  *ir.Document
		want []ir.Diagnostic
	}{
		{"two spellings of its own", own, []ir.Diagnostic{
			collision("Child", `in model Child, properties 0 ("userId") and 1 ("user_id") share the neutral name "user_id"`),
		}},
		{"a third spelling beside a pair of its base", third, []ir.Diagnostic{
			collision("Base", `in model Base, properties 0 ("userId") and 1 ("user_id") share the neutral name "user_id"`),
			exposed("Child", `0 of model Base ("userId"), 1 of model Base ("user_id") and 0 ("UserId")`),
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Empty(t, cmp.Diff(tc.want, namesOf(t, tc.doc)))
		})
	}
}

// TestNames_ReadsAPairAModelsBaseAndInterfaceBringAtTheModel holds a model that
// declares nothing, extending one model and implementing another, to the one
// finding of the pair its base and its interface bring: neither entry alone
// exposes both, so the model is where they first meet.
func TestNames_ReadsAPairAModelsBaseAndInterfaceBringAtTheModel(t *testing.T) {
	t.Parallel()
	doc := withTypes(
		extending("A", "", spelled("p/a/0", "userId", "userId")),
		extending("B", "", spelled("p/b/0", "user_id", "user_id")),
		&ir.Model{ID: "X", Name: named("X"), Base: &ir.TypeRef{Target: "A"}, Implements: []ir.TypeRef{{Target: "B"}}},
	)

	assert.Empty(t, cmp.Diff([]ir.Diagnostic{exposed("X", `0 of model A ("userId") and 0 of model B ("user_id")`)},
		namesOf(t, doc)))
}

// TestNames_ReadsNoPairAgainBelowTheEntryThatMetIt holds a model that declares
// nothing and composes two entries, one of which met a pair already, to no
// finding of its own, whichever entry that is: its base, an interface it
// implements or a mixin. The pair is told at the entry's own scope alone.
func TestNames_ReadsNoPairAgainBelowTheEntryThatMetIt(t *testing.T) {
	t.Parallel()
	held, other := ir.TypeRef{Target: "Pair"}, ir.TypeRef{Target: "Other"}
	want := []ir.Diagnostic{
		collision("Pair", `in model Pair, properties 0 ("userId") and 1 ("user_id") share the neutral name "user_id"`),
	}
	for name, below := range map[string]*ir.Model{
		"its base":     {ID: "Below", Name: named("Below"), Base: &held, Mixins: []ir.TypeRef{other}},
		"an interface": {ID: "Below", Name: named("Below"), Base: &other, Implements: []ir.TypeRef{held}},
		"a mixin":      {ID: "Below", Name: named("Below"), Mixins: []ir.TypeRef{other, held}},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			doc := withTypes(
				extending("Pair", "", spelled("p/pair/0", "userId", "userId"), spelled("p/pair/1", "user_id", "user_id")),
				extending("Other", "", spelled("p/other/0", "other", "other")),
				below,
			)
			assert.Empty(t, cmp.Diff(want, namesOf(t, doc)))
		})
	}
}
