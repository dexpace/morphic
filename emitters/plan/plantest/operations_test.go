// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package plantest_test

import (
	"maps"
	"path/filepath"
	"slices"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/emitters/plan"
	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/ir/irfacts"
	"github.com/dexpace/morphic/ir/irtest"
)

// firstOperations returns the first operation ir.ForEachOperation visits in
// doc under each ID.
func firstOperations(doc *ir.Document) map[ir.OperationID]ir.Operation {
	first := map[ir.OperationID]ir.Operation{}
	ir.ForEachOperation(doc, func(operation ir.Operation) {
		if _, seen := first[operation.ID]; !seen {
			first[operation.ID] = operation
		}
	})
	return first
}

// eachCorpusDocument runs check on every IR golden of the corpus, each in a
// parallel subtest of its own.
func eachCorpusDocument(t *testing.T, check func(t *testing.T, doc *ir.Document)) {
	t.Helper()
	root := filepath.Join(irtest.RepoRoot(t), corpus)
	for _, document := range corpusDocuments(t) {
		t.Run(document, func(t *testing.T) {
			t.Parallel()
			doc, err := irtest.ReadGolden(filepath.Join(root, document))
			require.NoError(t, err)
			check(t, doc)
		})
	}
}

// buildUnder plans doc under policy and requires that it reported nothing.
func buildUnder(t *testing.T, doc *ir.Document, policy plan.Policy) *plan.Plan {
	t.Helper()
	built, diags := plan.Build(doc, irfacts.New(doc), policy)
	require.Empty(t, diags)
	return built
}

// TestGolden_OneOpPlanForEachOperation holds Ops, over every corpus document,
// to one entry for each operation ID the walk visits.
func TestGolden_OneOpPlanForEachOperation(t *testing.T) {
	t.Parallel()
	eachCorpusDocument(t, func(t *testing.T, doc *ir.Document) {
		built := buildUnder(t, doc, plan.Policy{})

		want := slices.Sorted(maps.Keys(firstOperations(doc)))
		assert.Equal(t, want, slices.Sorted(maps.Keys(built.Ops)))
	})
}

// TestGolden_ProtocolEffectsMovesOnlyWhatItMarks builds every corpus document
// with ProtocolEffects off and on, and holds the switch to moving only what it
// marks (T-35, docs/emitters/plan/policy.md): an operation is marked exactly
// where it declares no idempotency, and nothing else of the plan, a marked
// operation's Effects and Inferred aside, differs between the two. The zero
// policy marks nothing.
func TestGolden_ProtocolEffectsMovesOnlyWhatItMarks(t *testing.T) {
	t.Parallel()
	eachCorpusDocument(t, func(t *testing.T, doc *ir.Document) {
		off := buildUnder(t, doc, plan.Policy{})
		on := buildUnder(t, doc, plan.Policy{ProtocolEffects: true})

		assert.Empty(t, cmp.Diff(off.Types, on.Types), "the types plan alike (-off +on)")
		require.Len(t, on.Ops, len(off.Ops))
		for id, operation := range firstOperations(doc) {
			offPlan, onPlan := off.Ops[id], on.Ops[id]
			assert.Nil(t, offPlan.Inferred, "%s: the zero policy marks nothing", id)

			_, marked := onPlan.Inferred["Effects"]
			assert.Equal(t, operation.Idempotency.Kind == ir.IdempotencyUnknown, marked,
				"%s is marked exactly where it declares no idempotency", id)
			if marked {
				onPlan.Effects, onPlan.Inferred = offPlan.Effects, nil
			}
			assert.Empty(t, cmp.Diff(offPlan, onPlan), "%s: the switch moves only what it marks (-off +on)", id)
		}
	})
}
