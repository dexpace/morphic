// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package overlay

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/budget"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/jsonpath"
	"github.com/dexpace/morphic/compilers/openapi/internal/ynode"
	"github.com/dexpace/morphic/ir"
)

// TestLocator_RefusesAPathTheTreeNoLongerHolds pins the steps the locator
// takes from a selection the tree no longer holds, which no sequence hands
// it: a member no mapping writes, even once its merges are its own, an element
// past an array's end, and a root holding no value.
func TestLocator_RefusesAPathTheTreeNoLongerHolds(t *testing.T) {
	t.Parallel()
	list := ynode.Seq(ynode.Scalar("x"))
	root := &yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{ynode.Map(ynode.Scalar("a"), list)}}
	steps := budget.New(budget.Unbounded())
	l := newLocator(newGrafter(v11, root), &steps)

	_, err := l.slot(t.Context(), l.prefixes[0].node, jsonpath.Step{Name: "missing", Member: true}, false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), `the member "missing"`)

	_, err = l.slot(t.Context(), list, jsonpath.Step{Index: 5}, false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "the element 5")

	_, err = l.slot(t.Context(), list, jsonpath.Step{Name: "x", Member: true}, false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), `the member "x"`, "an array has no member")

	l.prefixes[0].node = nil
	_, err = l.own(t.Context(), 0)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "the document holds none")
}

// TestGrafter_RefusesToWriteWhatIsNoValue pins the writes no target the
// sequence makes its own reaches: a document node and an alias.
func TestGrafter_RefusesToWriteWhatIsNoValue(t *testing.T) {
	t.Parallel()
	for _, target := range []*yaml.Node{{Kind: yaml.DocumentNode}, ynode.Alias(ynode.Map())} {
		_, err := newGrafter(v10, nil).write(t.Context(), []*yaml.Node{target}, ynode.Map())
		require.Error(t, err)
		assert.Contains(t, err.Error(), "which no update writes")
	}
}

// TestSequence_RefusesASelectionPastTheNodelistsBound pins the finding for a
// selector whose nodelist passes jsonpath.MaxResults, a bound a test cannot
// reach with a tree of a test's size.
func TestSequence_RefusesASelectionPastTheNodelistsBound(t *testing.T) {
	t.Parallel()
	s := &sequence{}
	a := action{index: 0, of: 1, target: "$..*", kind: kindUpdate, overlay: ir.Provenance{Source: 1}}

	d := s.unselectable(a, errors.Join(jsonpath.ErrResults))

	assert.Equal(t, diag.BudgetExceeded, d.Code)
	assert.Equal(t, ir.SeverityError, d.Severity)
	assert.Contains(t, d.Message, "nodes one selection may hold")
}

// TestLocator_ChargesAnIndexBeforeBuildingIt pins the charge for locating a
// member: a mapping's index costs a step a member, spent before it is built,
// so a budget one short refuses it, as it refuses the index built again once
// a merge's members are made the mapping's own.
func TestLocator_ChargesAnIndexBeforeBuildingIt(t *testing.T) {
	t.Parallel()
	m := ynode.Map(ynode.Scalar("a"), ynode.Scalar("1"), ynode.Scalar("b"), ynode.Scalar("2"))
	root := &yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{m}}
	var merged yaml.Node
	require.NoError(t, yaml.Unmarshal([]byte("m: &m {a: 1, b: 2}\nx: {<<: *m, c: 3}\n"), &merged))
	merging := merged.Content[0].Content[3]

	twoSteps := budget.New(budget.AtMost(2))
	_, err := newLocator(newGrafter(v11, &merged), &twoSteps).slot(t.Context(), merging, jsonpath.Step{Name: "a", Member: true}, false)
	require.ErrorIs(t, err, jsonpath.ErrSteps, "the index rebuilt once the merge is flattened is charged too")

	short := budget.New(budget.AtMost(1))
	_, err = newLocator(newGrafter(v11, root), &short).slot(t.Context(), m, jsonpath.Step{Name: "b", Member: true}, false)
	require.ErrorIs(t, err, jsonpath.ErrSteps)

	enough := budget.New(budget.AtMost(2))
	slot, err := newLocator(newGrafter(v11, root), &enough).slot(t.Context(), m, jsonpath.Step{Name: "b", Member: true}, false)
	require.NoError(t, err)
	assert.Equal(t, 3, slot)
}
