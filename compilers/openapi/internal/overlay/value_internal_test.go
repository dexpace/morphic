// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package overlay

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/jsonpath"
	"github.com/dexpace/morphic/compilers/openapi/internal/ynode"
	"github.com/dexpace/morphic/ir"
)

// TestOwn_RefusesAPathTheTreeNoLongerHolds pins the steps own takes from a
// selection the tree no longer holds, which no sequence hands it: a member
// no mapping writes, even once its merges are its own, and an element past an
// array's end.
func TestOwn_RefusesAPathTheTreeNoLongerHolds(t *testing.T) {
	t.Parallel()
	root := &yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{
		ynode.Map(ynode.Scalar("a"), ynode.Seq(ynode.Scalar("x"))),
	}}
	g := newGrafter(v11, root)

	_, err := g.own(t.Context(), []jsonpath.Step{{Name: "missing", Member: true}}, true)
	require.Error(t, err)
	assert.Contains(t, err.Error(), `the member "missing"`)

	_, err = g.own(t.Context(), []jsonpath.Step{{Name: "a", Member: true}, {Index: 5}}, true)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "the element 5")
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
