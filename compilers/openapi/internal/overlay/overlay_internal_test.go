package overlay

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	yaml "gopkg.in/yaml.v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/nodeview"
	"github.com/dexpace/morphic/ir"
)

// budgetSpec and budgetOverlay are a document and a patch small enough to hold
// in a sentence and still more than a two-node budget: the point of the cases
// below is which walk runs out, not what either one is looking at.
const (
	budgetSpec = `openapi: 3.1.0
info: {title: T, version: "1"}
`
	budgetOverlay = "overlay: 1.0.0\ninfo: {title: O, version: \"1\"}\n" +
		"actions:\n  - target: $.info\n    update: {description: d}\n"
)

// TestApplyWithin_DegradesToTheSourceAtTheNodeBudget pins what exhausting the
// budget costs and what it must not: the overlay still applies, and attribution
// is abandoned wholesale rather than half-taken.
//
// Abandoning it is the load-bearing part. A snapshot cut short holds only the
// nodes it reached, so every node past the cut looks freshly introduced — an
// attribution built on it would blame the overlay for most of the document,
// which is worse than the answer a compile with no overlay gives. Reporting the
// degradation rather than silently taking it is the other half: a caller reading
// provenance has no other way to tell the two apart.
func TestApplyWithin_DegradesToTheSourceAtTheNodeBudget(t *testing.T) {
	t.Parallel()
	var root yaml.Node
	require.NoError(t, yaml.Unmarshal([]byte(budgetSpec), &root))

	origin, diags := applyWithin(1, &root, Options{Data: []byte(budgetOverlay)}, 2)

	assert.False(t, origin.Applied(), "no position is attributed to the overlay")
	assert.Equal(t, 9, origin.IndexAt("/info/description", 9), "not even one it did introduce")

	require.Len(t, diags, 1)
	assert.Equal(t, diag.OverlayOriginIncomplete, diags[0].Code)
	assert.Equal(t, ir.SeverityWarning, diags[0].Severity, "the compile still proceeds")

	assert.Equal(t, "d", nodeAt(t, &root, "info", "description"),
		"the overlay applied; only the attribution of it was given up")
}

// TestAttribute_DegradesWhenOnlyTheSecondWalkRunsOut pins the other order. The
// snapshot is taken before the overlay applies and the attribution walk after,
// so the tree the second walk covers is the larger of the two and can exceed a
// budget the first fit inside. Driving the walks directly is what separates the
// two exhaustion points, which applyWithin alone cannot do.
func TestAttribute_DegradesWhenOnlyTheSecondWalkRunsOut(t *testing.T) {
	t.Parallel()
	var root yaml.Node
	require.NoError(t, yaml.Unmarshal([]byte("a: 1\n"), &root))

	before, complete := snapshot(&root, maxNodes)
	require.True(t, complete)

	root.Content[0].Content = append(root.Content[0].Content,
		&yaml.Node{Kind: yaml.ScalarNode, Value: "b"},
		&yaml.Node{Kind: yaml.ScalarNode, Value: "2"})

	_, ok := attribute(&root, before, 2)
	assert.False(t, ok, "three nodes do not fit a budget of two")

	pointers, ok := attribute(&root, before, maxNodes)
	require.True(t, ok, "and the same tree fits a real one — the budget is what differed")
	assert.Equal(t, map[string]bool{"/b": true}, pointers,
		"which is also the answer the exhausted walk withheld")
}

// TestSnapshot_RecordsEveryNodeAgainstItsValue pins the record the attribution
// walk reads back. Identity alone cannot answer "was this rewritten", so a
// snapshot that stored presence would silently disable the rewritten-scalar half
// of the attribution while every introduced-node test kept passing.
func TestSnapshot_RecordsEveryNodeAgainstItsValue(t *testing.T) {
	t.Parallel()
	var root yaml.Node
	require.NoError(t, yaml.Unmarshal([]byte("a: 1\n"), &root))

	before, complete := snapshot(&root, maxNodes)
	require.True(t, complete)

	mapping := root.Content[0]
	assert.Equal(t, "a", before[mapping.Content[0]], "the key node, by its value")
	assert.Equal(t, "1", before[mapping.Content[1]], "and the value node by its own")
	assert.Contains(t, before, mapping, "a mapping is recorded too, holding no value")
	assert.NotContains(t, before, &root, "the document node is not one of them")
}

// TestApplyWithin_RecoversALibraryPanic pins the no-panics-escape invariant on
// the overlay side, the way the barriers around the parser and the resolver pin
// it on theirs.
//
// A document node holding no root is the shape that provokes it: yamlpath
// indexes the first child of what it is handed without checking there is one, so
// the selector faults before any action is applied. The refusal must leave as a
// diagnostic like every other overlay problem, and nothing may be attributed to
// an overlay that never ran.
//
// yaml.v3 does not produce this shape today — an empty source leaves a
// zero-valued node, which the library tolerates — so the node is built rather
// than decoded. That is what holds the barrier to its claim instead of resting
// on a third-party parser continuing to avoid the input a third-party selector
// cannot take.
func TestApplyWithin_RecoversALibraryPanic(t *testing.T) {
	t.Parallel()
	root := &yaml.Node{Kind: yaml.DocumentNode}
	require.Nil(t, nodeview.DocumentRoot(root), "the shape under test: a document with no root")

	origin, diags := applyWithin(1, root, Options{Data: []byte(budgetOverlay), Lax: true}, maxNodes)

	assert.False(t, origin.Applied(), "an overlay that faulted attributes nothing")
	require.Len(t, diags, 1)
	assert.Equal(t, diag.OverlayFailed, diags[0].Code)
	assert.Contains(t, diags[0].Message, "panicked", "the fault is named, not swallowed")
}

// TestSnapshotAndAttribute_TakeADocumentWithNoRoot pins the walks themselves
// against that same shape. The barrier above returns before either runs, so
// without driving them directly nothing holds their nil guards to anything —
// and a walk that faulted here would do so on any tree the library later learns
// to accept.
func TestSnapshotAndAttribute_TakeADocumentWithNoRoot(t *testing.T) {
	t.Parallel()
	root := &yaml.Node{Kind: yaml.DocumentNode}

	before, complete := snapshot(root, maxNodes)
	require.True(t, complete)
	assert.Empty(t, before, "no node to record")

	pointers, ok := attribute(root, before, maxNodes)
	require.True(t, ok)
	assert.Empty(t, pointers, "and none to attribute")
}

// nodeAt reads the scalar at a path of mapping keys, so a test can assert on the
// tree the overlay left behind rather than on a re-serialisation of it.
func nodeAt(t *testing.T, root *yaml.Node, keys ...string) string {
	t.Helper()
	n := root.Content[0]
	for _, key := range keys {
		found := false
		for i := 0; i+1 < len(n.Content); i += 2 {
			if n.Content[i].Value == key {
				n, found = n.Content[i+1], true
				break
			}
		}
		require.True(t, found, "no %q under the path %v", key, keys)
	}
	return n.Value
}
