package load

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/overlay"
	"github.com/dexpace/morphic/ir"
)

func TestNodeCount_CountsEveryNodeAndAnswersForNothing(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		src  string
		want int
	}{
		// document + mapping + one key + one scalar value.
		{"a one-entry mapping", "a: 1\n", 4},
		// document + mapping + key + sequence + three scalars.
		{"a three-element sequence", "a: [1, 2, 3]\n", 7},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			root, err := decode([]byte(tc.src))
			require.NoError(t, err)
			assert.Equal(t, tc.want, nodeCount(root))
		})
	}
	assert.Equal(t, 0, nodeCount(nil), "an absent tree counts nothing rather than panicking")
}

func TestExceeds_TreatsAnUnsetBudgetAsUnbounded(t *testing.T) {
	t.Parallel()
	assert.False(t, exceeds(1<<40, 0), "zero is no budget, not a budget of zero")
	assert.False(t, exceeds(1<<40, -1))
	assert.False(t, exceeds(4, 4), "the budget is what is allowed, not what is refused")
	assert.True(t, exceeds(5, 4))
}

func TestLoad_SourceByteBudgetBindsOnlyPastTheLimit(t *testing.T) {
	t.Parallel()
	size := len(minimal31)
	tests := []struct {
		name    string
		limit   int
		refused bool
	}{
		{"at the budget", size, false},
		{"one byte past the budget", size - 1, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			src := compilers.Source{Path: "spec.yaml", Data: []byte(minimal31)}
			doc, diags, err := Load(t.Context(), 0, src, Options{MaxSourceBytes: tc.limit})
			require.NoError(t, err, "an over-budget document is a spec problem, not a Go error")

			if !tc.refused {
				require.NotNil(t, doc)
				assert.False(t, diag.HasError(diags))
				return
			}
			assert.Nil(t, doc, "nothing parses an over-budget source")
			require.Len(t, diags, 1)
			assert.Equal(t, diag.BudgetExceeded, diags[0].Code)
			assert.Equal(t, ir.SeverityError, diags[0].Severity)
			assert.Equal(t, fmt.Sprintf("source document is %d bytes, past the %d-byte budget",
				size, tc.limit), diags[0].Message)
		})
	}
}

// TestLoad_SourceNodeBudgetBindsOnlyPastTheLimit pins the node budget at its
// exact boundary, which needs the count the loader itself takes — the compiler's
// public options can ask for a number, never learn one.
func TestLoad_SourceNodeBudgetBindsOnlyPastTheLimit(t *testing.T) {
	t.Parallel()
	root, err := decode([]byte(minimal31))
	require.NoError(t, err)
	nodes := nodeCount(root)
	require.Positive(t, nodes)

	tests := []struct {
		name    string
		limit   int
		refused bool
	}{
		{"at the budget", nodes, false},
		{"one node past the budget", nodes - 1, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			src := compilers.Source{Path: "spec.yaml", Data: []byte(minimal31)}
			doc, diags, err := Load(t.Context(), 0, src, Options{MaxSourceNodes: tc.limit})
			require.NoError(t, err, "an over-budget document is a spec problem, not a Go error")

			if !tc.refused {
				require.NotNil(t, doc)
				assert.False(t, diag.HasError(diags))
				return
			}
			assert.Nil(t, doc, "the model is never built from an over-budget tree")
			require.Len(t, diags, 1)
			assert.Equal(t, diag.BudgetExceeded, diags[0].Code)
			assert.Equal(t, ir.SeverityError, diags[0].Severity)
			assert.Equal(t, fmt.Sprintf("source document parses to %d nodes, past the %d-node budget",
				nodes, tc.limit), diags[0].Message)
			assert.Equal(t, 0, diags[0].Provenance.Source)
		})
	}
}

// TestLoad_SourceNodeBudgetCountsWhatAnOverlayGrafted is why the node budget is
// taken after the overlay is applied rather than beside the byte budget: the
// tree the model is built from is no longer the one the bytes described, and it
// is that tree the budget exists to bound.
func TestLoad_SourceNodeBudgetCountsWhatAnOverlayGrafted(t *testing.T) {
	t.Parallel()
	root, err := decode([]byte(minimal31))
	require.NoError(t, err)
	unpatched := nodeCount(root)

	const patch = `overlay: 1.0.0
info: {title: add a schema, version: "1"}
actions:
  - target: $
    update:
      components:
        schemas:
          Added: {type: object, properties: {a: {type: string}, b: {type: string}}}
`
	opts := Options{
		Overlay:         &overlay.Options{Path: "patch.yaml", Data: []byte(patch)},
		OverlaySrcIndex: 1,
		MaxSourceNodes:  unpatched,
	}
	doc, diags, err := Load(t.Context(), 0,
		compilers.Source{Path: "spec.yaml", Data: []byte(minimal31)}, opts)

	require.NoError(t, err)
	assert.Nil(t, doc, "the grafted nodes count against the budget")
	require.NotEmpty(t, diags)
	assert.Equal(t, diag.BudgetExceeded, diags[len(diags)-1].Code)
}
