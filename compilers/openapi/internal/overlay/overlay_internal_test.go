package overlay

import (
	"encoding/json/jsontext"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	yaml "gopkg.in/yaml.v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/nodeview"
	"github.com/dexpace/morphic/compilers/openapi/internal/ynode"
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

	key, value := &yaml.Node{Kind: yaml.ScalarNode, Value: "b"}, &yaml.Node{Kind: yaml.ScalarNode, Value: "2"}
	root.Content[0].Content = append(root.Content[0].Content, key, value)

	_, _, ok := attribute(&root, before, 2)
	assert.False(t, ok, "three nodes do not fit a budget of two")

	pointers, nodes, ok := attribute(&root, before, maxNodes)
	require.True(t, ok, "and the same tree fits a real one — the budget is what differed")
	assert.Equal(t, map[jsontext.Pointer]bool{"/b": true}, pointers,
		"which is also the answer the exhausted walk withheld")
	assert.Equal(t, map[*yaml.Node]jsontext.Pointer{key: "/b", value: "/b"}, nodes,
		"and both nodes of the new member sit at that pointer")
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

	pointers, nodes, ok := attribute(root, before, maxNodes)
	require.True(t, ok)
	assert.Empty(t, pointers, "and none to attribute")
	assert.Empty(t, nodes)
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

// TestApplyWithin_RefusesAGraftItCannotExpandWithinBudget pins the one graft
// failure that refuses rather than degrades.
//
// Running out of tree to walk says only that the document is past what this
// package reads, which is what the attribution walks already say and already
// degrade for. Running out of room to substitute says a graft was found and
// could not be made safe, and a tree repaired in part is worse than either
// outcome — so the compile refuses instead of passing it on.
//
// The budget is set between the two: large enough to walk this small source,
// small enough that resolving the alias the overlay grafts runs past it.
func TestApplyWithin_RefusesAGraftItCannotExpandWithinBudget(t *testing.T) {
	t.Parallel()
	var root yaml.Node
	require.NoError(t, yaml.Unmarshal([]byte("openapi: 3.1.0\npaths: {}\n"), &root))
	const ov = "overlay: 1.0.0\ninfo: {title: o, version: \"1\", x-t: &t {a: 1, b: 2, c: 3, d: 4}}\n" +
		"actions:\n  - target: $.paths\n    update: {p: *t}\n"

	origin, diags := applyWithin(1, &root, Options{Data: []byte(ov)}, 12)

	assert.False(t, origin.Applied())
	require.True(t, diag.HasError(diags), "the compile refuses: %+v", diags)
	last := diags[len(diags)-1]
	assert.Equal(t, diag.OverlayFailed, last.Code)
	assert.Contains(t, last.Message, "expands past")
}

// TestRepairGrafts_ReportsWalkingAndSubstitutingApart pins that the two budget
// failures are told apart at the source, since applyWithin can only show one of
// them at a time.
func TestRepairGrafts_ReportsWalkingAndSubstitutingApart(t *testing.T) {
	t.Parallel()
	var root yaml.Node
	require.NoError(t, yaml.Unmarshal([]byte("a: {b: 1, c: 2}\n"), &root))

	normalized, safe := repairGrafts(&root, maxNodes)
	assert.True(t, normalized, "a tree with no graft in it is already normal")
	assert.True(t, safe)

	normalized, safe = repairGrafts(&root, 1)
	assert.False(t, normalized, "a tree it cannot walk is one it cannot say anything about")
	assert.True(t, safe, "and nothing it could not substitute was found")

	// A document node holding nothing has no content to walk, which is neither
	// a failure nor a graft.
	normalized, safe = repairGrafts(&yaml.Node{Kind: yaml.DocumentNode}, maxNodes)
	assert.True(t, normalized)
	assert.True(t, safe)
}

// TestExpandAlias_DeclinesAnAliasNamingNothing pins the shape a parse never
// produces and a caller assembling nodes can: an alias with no target stands
// for no content, so there is nothing to graft in its place and the caller
// refuses rather than writing an empty node into the document.
func TestExpandAlias_DeclinesAnAliasNamingNothing(t *testing.T) {
	t.Parallel()
	budget := maxNodes
	got, ok := expandAlias(&yaml.Node{Kind: yaml.AliasNode}, &budget)
	assert.False(t, ok)
	assert.Nil(t, got)
}

// TestRepairGrafts_TakesShapesAParseNeverProduces pins the guards the graft
// repair carries for trees it did not parse. Nodes are built here rather than
// decoded because that is the whole point: yaml.v3 hands back neither a nil
// child nor one node in two places, and the walks would loop or fault on either
// — so the guards are what make this package's correctness independent of what
// the library grafting into the tree happens to do.
func TestRepairGrafts_TakesShapesAParseNeverProduces(t *testing.T) {
	t.Parallel()

	shared := ynode.Map(ynode.Scalar("k"), ynode.Scalar("v"))
	twice := ynode.Map(ynode.Scalar("a"), shared, ynode.Scalar("b"), shared)
	reachable, ok := reachableNodes(twice, maxNodes)
	require.True(t, ok, "one node in two places is walked, not walked forever")
	assert.True(t, reachable[shared], "and counted once")

	withNil := ynode.Map(ynode.Scalar("a"), nil)
	reachable, ok = reachableNodes(withNil, maxNodes)
	require.True(t, ok, "a nil child is skipped, not dereferenced")
	budget := maxNodes
	assert.True(t, substituteGrafts(withNil, reachable, &budget), "and skipped again on the way back")
}

// TestSubstituteGrafts_StopsAtItsOwnBudget pins the bound on the walk that
// finds grafts, as against the one on the expansion that replaces them. A tree
// wide enough to exhaust the budget before any graft is met stops there rather
// than walking on: the caller reads that as a graft it could not make safe,
// which is the safe direction when it no longer knows whether one is left.
func TestSubstituteGrafts_StopsAtItsOwnBudget(t *testing.T) {
	t.Parallel()
	wide := ynode.Map()
	for i := range 8 {
		wide.Content = append(wide.Content, ynode.Scalar(strconv.Itoa(i)), ynode.Scalar("v"))
	}

	budget := 4
	assert.False(t, substituteGrafts(wide, map[*yaml.Node]bool{}, &budget),
		"sixteen children do not fit a budget of four")

	budget = maxNodes
	assert.True(t, substituteGrafts(wide, map[*yaml.Node]bool{}, &budget),
		"and the same tree fits a real one — the budget is what differed")
}
