// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package overlay

import (
	"fmt"
	"math"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/budget"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/ynode"
	"github.com/dexpace/morphic/internal/ctxtest"
	"github.com/dexpace/morphic/ir"
)

// sequenceSpec is the tree the budget cases apply to. It has a few of every
// shape a selector here reads: path items, a mapping of schemas, and a
// sequence of tags.
const sequenceSpec = `openapi: 3.1.0
info:
  title: T
  version: "1"
tags:
  - name: a
  - name: b
paths:
  /a: {get: {operationId: a, x-flag: off}}
  /b: {get: {operationId: b, x-flag: off}}
  /c: {get: {operationId: c, x-flag: off}}
components:
  schemas:
    One: {type: string}
    Two: {type: string}
`

// planOf reads ov as an overlay to apply, requiring that it reads.
func planOf(t *testing.T, ov string) plan {
	t.Helper()
	p, diags := read(ir.Provenance{Source: 1}, []byte(ov))
	require.False(t, diag.HasError(diags), "%+v", diags)
	return p
}

// budgetedOverlay applies an overlay to sequenceSpec under a node budget.
func budgetedOverlay(t *testing.T, ov string, limit budget.Limit) (*yaml.Node, []ir.Diagnostic, bool) {
	t.Helper()
	var root yaml.Node
	require.NoError(t, yaml.Unmarshal([]byte(sequenceSpec), &root))
	_, diags, ok := runSequence(t.Context(), planOf(t, ov), &root, false, limit)
	return &root, diags, ok
}

// wideUpdate returns an update mapping of n keys, the payload that fans out.
func wideUpdate(n int) string {
	keys := make([]string, n)
	for i := range keys {
		keys[i] = fmt.Sprintf("k%d: v", i)
	}
	return "{" + strings.Join(keys, ", ") + "}"
}

// TestSequence_RefusesFanOutBeforeBuildingIt pins GitHub #491: an update copied
// into every node its selector matches costs its size times the matches, and
// that is refused before the library copies anything — not, as it was, after a
// tree the node budget then refused had already been built.
func TestSequence_RefusesFanOutBeforeBuildingIt(t *testing.T) {
	t.Parallel()
	const head = "overlay: 1.0.0\ninfo: {title: o, version: \"1\"}\nactions:\n"
	ov := head + "  - target: $.paths.*\n    update: {x-bulk: " + wideUpdate(100) + "}\n"

	var fresh yaml.Node
	require.NoError(t, yaml.Unmarshal([]byte(sequenceSpec), &fresh))
	before := countNodes(t.Context(), &fresh, budget.AtMost(maxNodes))

	root, diags, ok := budgetedOverlay(t, ov, budget.AtMost(before+300))

	require.False(t, ok, "three path items times a 200-node update is past a budget with room for 300")
	last := diags[len(diags)-1]
	assert.Equal(t, diag.BudgetExceeded, last.Code)
	assert.Contains(t, last.Message, "action 1 of 1")
	assert.Equal(t, before, countNodes(t.Context(), root, budget.AtMost(maxNodes)), "and nothing was built: the refusal came first")

	_, diags, ok = budgetedOverlay(t, ov, budget.AtMost(before+1000))
	assert.True(t, ok, "the same overlay fits a budget with room for it: %+v", diags)
}

// TestSequence_CountsEachActionOnTheTreeItRunsAgainst pins why the count is
// taken action by action. An earlier action can make a later selector match
// nodes it would not have matched on the source — here by setting the flag a
// later filter selects on — so a count taken up front understates the later
// action's reach, and would let it build past the budget.
func TestSequence_CountsEachActionOnTheTreeItRunsAgainst(t *testing.T) {
	t.Parallel()
	const head = "overlay: 1.0.0\ninfo: {title: o, version: \"1\"}\nactions:\n"
	flag := "  - target: $.paths.*.get\n    update: {x-flag: 'on'}\n"
	later := "  - target: $.paths.*[?@['x-flag'] == 'on']\n    update: {x-bulk: " + wideUpdate(100) + "}\n"

	var fresh yaml.Node
	require.NoError(t, yaml.Unmarshal([]byte(sequenceSpec), &fresh))
	require.Empty(t, planOf(t, head+later).actions[0].targets.Query(&fresh), "on the source, the later filter matches nothing")

	_, diags, ok := budgetedOverlay(t, head+flag+later, budget.AtMost(countNodes(t.Context(), &fresh, budget.AtMost(maxNodes))+300))

	require.False(t, ok, "on the tree the first action leaves, it matches every path item")
	last := diags[len(diags)-1]
	assert.Equal(t, diag.BudgetExceeded, last.Code)
	assert.Contains(t, last.Message, "action 2 of 2", "and it is the second action that is refused")
}

// TestSequence_DoesNotRefuseOnAnOverstatedCharge pins the recount. Each action
// is charged what it could add, and an update to keys that already exist adds
// nothing, so the running size overstates; refusing on it would turn down an
// overlay that fits. The real count is taken before any refusal.
func TestSequence_DoesNotRefuseOnAnOverstatedCharge(t *testing.T) {
	t.Parallel()
	const head = "overlay: 1.0.0\ninfo: {title: o, version: \"1\"}\nactions:\n"
	var b strings.Builder
	b.WriteString(head)
	for range 20 {
		b.WriteString("  - target: $.info\n    update: {title: T}\n")
	}

	var fresh yaml.Node
	require.NoError(t, yaml.Unmarshal([]byte(sequenceSpec), &fresh))
	before := countNodes(t.Context(), &fresh, budget.AtMost(maxNodes))

	root, diags, ok := budgetedOverlay(t, b.String(), budget.AtMost(before+10))

	assert.True(t, ok, "twenty charges of two nodes each exceed ten, and none of them adds a node: %+v", diags)
	assert.Equal(t, before, countNodes(t.Context(), root, budget.AtMost(maxNodes)))
}

// TestSequence_ChargesAnActionToTheBudgetsEdge pins where a charge crosses: an
// update whose charge brings the tree to exactly the budget is applied, and
// one node of budget fewer refuses it, before it builds anything. The charge
// is the payload's weight, three nodes here, though merging into info adds two.
func TestSequence_ChargesAnActionToTheBudgetsEdge(t *testing.T) {
	t.Parallel()
	const head = "overlay: 1.0.0\ninfo: {title: o, version: \"1\"}\nactions:\n"
	ov := head + "  - target: $.info\n    update: {x-a: 1}\n"
	var fresh yaml.Node
	require.NoError(t, yaml.Unmarshal([]byte(sequenceSpec), &fresh))
	const charge = 3
	before := countNodes(t.Context(), &fresh, budget.AtMost(maxNodes))

	_, diags, ok := budgetedOverlay(t, ov, budget.AtMost(before+charge))
	assert.True(t, ok, "a charge to the budget is within it: %+v", diags)

	root, diags, ok := budgetedOverlay(t, ov, budget.AtMost(before+charge-1))
	require.False(t, ok)
	last := diags[len(diags)-1]
	assert.Equal(t, diag.BudgetExceeded, last.Code)
	assert.Contains(t, last.Message, fmt.Sprintf("at least %d nodes, past the %d-node budget", before+charge, before+charge-1))
	assert.Equal(t, before, countNodes(t.Context(), root, budget.AtMost(maxNodes)), "and nothing was built")
}

// TestSequence_RefusesNoActionThatBuildsNothing pins that an action adding no
// node is applied however large the tree it runs on: here a remove, on a tree
// already past the budget, which it brings back within. Refused, it was
// reported as growing the document.
func TestSequence_RefusesNoActionThatBuildsNothing(t *testing.T) {
	t.Parallel()
	const head = "overlay: 1.0.0\ninfo: {title: o, version: \"1\"}\nactions:\n"
	ov := head + "  - target: $.paths\n    remove: true\n"

	var fresh yaml.Node
	require.NoError(t, yaml.Unmarshal([]byte(sequenceSpec), &fresh))
	before := countNodes(t.Context(), &fresh, budget.AtMost(maxNodes))

	root, diags, ok := budgetedOverlay(t, ov, budget.AtMost(before-1))

	require.True(t, ok, "a remove builds nothing, so no budget refuses it: %+v", diags)
	assert.Empty(t, diags)
	assert.Less(t, countNodes(t.Context(), root, budget.AtMost(maxNodes)), before-1, "and it was applied")
}

// TestSequence_ChargesAnActionAfterARemoveOnTheTreeItLeft pins that an action
// that builds, after a remove on a tree past the budget, is charged against
// the tree the remove left, counted afresh: a budget of that tree and the
// action's charge admits it, and one node fewer refuses it. The charge is the
// payload's weight, three nodes here.
func TestSequence_ChargesAnActionAfterARemoveOnTheTreeItLeft(t *testing.T) {
	t.Parallel()
	const head = "overlay: 1.0.0\ninfo: {title: o, version: \"1\"}\nactions:\n"
	const remove = "  - target: $.paths\n    remove: true\n"
	ov := head + remove + "  - target: $.info\n    update: {x-a: 1}\n"
	const charge = 3

	var fresh yaml.Node
	require.NoError(t, yaml.Unmarshal([]byte(sequenceSpec), &fresh))
	before := countNodes(t.Context(), &fresh, budget.AtMost(maxNodes))
	removed, _, ok := budgetedOverlay(t, head+remove, budget.Unbounded())
	require.True(t, ok)
	left := countNodes(t.Context(), removed, budget.AtMost(maxNodes))
	require.Less(t, left+charge, before, "sanity: the source alone is past both budgets below")

	_, diags, ok := budgetedOverlay(t, ov, budget.AtMost(left+charge))
	assert.True(t, ok, "the tree the remove left, and the charge, fit: %+v", diags)

	_, diags, ok = budgetedOverlay(t, ov, budget.AtMost(left+charge-1))
	require.False(t, ok)
	last := diags[len(diags)-1]
	assert.Equal(t, diag.BudgetExceeded, last.Code)
	assert.Contains(t, last.Message, fmt.Sprintf(`action 2 of 2 (update "$.info") would grow the document to at least %d nodes`, left+charge))
}

// TestSequence_AppliesNoActionOnceItsContextRefused cancels a sequence at
// every check of its context a whole one makes, the recounts its overstated
// charges take and the index a remove reads among them, and requires the tree
// it leaves to be the tree as it stood at the refusal: no action is applied
// once a check has refused, since a charge or an index a cancel cut short may
// be partial. Each action writes a title of its own, so one applied late shows.
func TestSequence_AppliesNoActionOnceItsContextRefused(t *testing.T) {
	t.Parallel()
	var b strings.Builder
	b.WriteString("overlay: 1.0.0\ninfo: {title: o, version: \"1\"}\nactions:\n")
	b.WriteString("  - target: $.tags[0]\n    remove: true\n")
	for i := range 20 {
		fmt.Fprintf(&b, "  - target: $.info\n    update: {title: T%d}\n", i)
	}
	p := planOf(t, b.String())
	tree := func() *yaml.Node {
		var root yaml.Node
		require.NoError(t, yaml.Unmarshal([]byte(sequenceSpec), &root))
		return &root
	}
	limit := budget.AtMost(countNodes(t.Context(), tree(), budget.AtMost(maxNodes)) + 10)
	whole := &ctxtest.Countdown{Context: t.Context(), Left: math.MaxInt}
	_, _, ok := runSequence(whole, p, tree(), false, limit)
	require.True(t, ok)
	require.Greater(t, whole.Calls, 1+len(p.actions),
		"beside the first count and the check after each charge, a recount asks too")

	for left := range whole.Calls {
		root := tree()
		var (
			refusedOn []byte
			err       error
		)
		ctx := &ctxtest.Countdown{Context: t.Context(), Left: left}
		ctx.OnRefuse = func() { refusedOn, err = yaml.Marshal(root) }

		_, diags, ok := runSequence(ctx, p, root, false, limit)

		require.NoError(t, err)
		require.False(t, ok, "check %d", left+1)
		require.Empty(t, diags, "check %d", left+1)
		after, err := yaml.Marshal(root)
		require.NoError(t, err)
		require.Equal(t, string(refusedOn), string(after), "check %d: an action was applied after the refusal", left+1)
	}
}

// TestSequence_AppliesWithoutABudget pins that an unbounded budget turns the
// check off rather than refusing everything, matching the rest of the
// compiler's budgets.
func TestSequence_AppliesWithoutABudget(t *testing.T) {
	t.Parallel()
	const head = "overlay: 1.0.0\ninfo: {title: o, version: \"1\"}\nactions:\n"
	ov := head + "  - target: $.paths.*\n    update: {x-bulk: " + wideUpdate(100) + "}\n"
	_, diags, ok := budgetedOverlay(t, ov, budget.Unbounded())
	assert.True(t, ok, "%+v", diags)
}

// TestWeigher_CountsWhatTheCloneBuilds pins the payload count against the
// copy an action writes: an alias costs itself and everything it names, since
// the copy follows it; an anchor named twice is walked once but counted twice,
// since the copy copies it twice; and a cycle weighs past any budget, since
// the copy would never finish.
func TestWeigher_CountsWhatTheCloneBuilds(t *testing.T) {
	t.Parallel()
	decode := func(src string) *yaml.Node {
		var n yaml.Node
		require.NoError(t, yaml.Unmarshal([]byte(src), &n))
		return n.Content[0]
	}

	plain := decode("{a: 1, b: 2}")
	assert.Equal(t, 5, newWeigher(budget.AtMost(1000)).weigh(plain, 0), "a mapping, two keys, two values")

	twice := decode("{x: &s {k: v}, y: *s, z: *s}")
	// The mapping, its three keys, x's value of three nodes, and each alias:
	// itself plus the three it names, which the clone copies again each time.
	assert.Equal(t, 1+3+3+2*(1+3), newWeigher(budget.AtMost(1000)).weigh(twice, 0))

	cycle := decode("&a [*a]")
	assert.Equal(t, 11, newWeigher(budget.AtMost(10)).weigh(cycle, 0), "a cycle weighs the first count past the limit")

	assert.Equal(t, 11, newWeigher(budget.AtMost(10)).weigh(decode(wideUpdate(50)), 0),
		"and so does anything past it, without counting on")
	assert.Equal(t, 0, newWeigher(budget.AtMost(10)).weigh(nil, 0))
}

// TestSequence_NeverChargesLessThanItBuilds is the property the budget stands
// on. The charge is taken before an action runs and the tree is measured after,
// and a charge below what the action built would let an overlay build past the
// budget one action at a time. So the charge must bound what is built, for
// every payload shape the library copies: plain, aliased, aliases within
// aliases, and a copy action. It may overstate — an update merges into its
// target rather than being added, and an alias the loader's repair later
// substitutes counts once — but never understate.
func TestSequence_NeverChargesLessThanItBuilds(t *testing.T) {
	t.Parallel()
	const head = "overlay: 1.1.0\ninfo: {title: o, version: \"1\", x-a: &a {k: v, w: [1, 2]}, x-b: &b {p: *a, q: *a}}\nactions:\n"
	for name, action := range map[string]string{
		"a plain update":             "  - target: $.paths.*\n    update: {x-new: {a: 1, b: [1, 2, 3]}}\n",
		"an aliased update":          "  - target: $.paths.*\n    update: {x-new: *a}\n",
		"aliases within aliases":     "  - target: $.paths.*\n    update: {x-new: *b, x-too: *a}\n",
		"a copy":                     "  - target: $.paths.*\n    copy: $.components.schemas.One\n",
		"an update into keys it has": "  - target: $.paths.*.get\n    update: {operationId: z}\n",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			var root yaml.Node
			require.NoError(t, yaml.Unmarshal([]byte(sequenceSpec), &root))
			p := planOf(t, head+action)

			s := &sequence{plan: p, root: &root, size: budget.New(budget.AtMost(1 << 20))}
			targets, sources := s.selected(p.actions[0])
			charged := s.cost(p.actions[0], targets, sources)
			before := countNodes(t.Context(), &root, budget.AtMost(maxNodes))

			_, diags := Apply(t.Context(), 1, &root, Options{Data: []byte(head + action)})
			require.False(t, diag.HasError(diags), "%+v", diags)

			built := countNodes(t.Context(), &root, budget.AtMost(maxNodes)) - before
			assert.GreaterOrEqual(t, charged, built, "charged %d, built %d", charged, built)
		})
	}
}

// TestCountNodes_SkipsWhatAParseNeverProduces pins the one guard countNodes
// carries for a tree it did not parse: a nil child is skipped rather than
// dereferenced. The node is built because yaml.v3 never hands one back.
func TestCountNodes_SkipsWhatAParseNeverProduces(t *testing.T) {
	t.Parallel()
	root := &yaml.Node{Kind: yaml.MappingNode, Content: []*yaml.Node{{Kind: yaml.ScalarNode, Value: "a"}, nil}}
	assert.Equal(t, 2, countNodes(t.Context(), root, budget.AtMost(maxNodes)), "the mapping and its one real child")
}

// TestCountNodes_TheLargestLimitCountsTheWholeTree pins that a limit of
// math.MaxInt, past which no node lies, and no limit at all count every node
// rather than overflowing the bound one past it.
func TestCountNodes_TheLargestLimitCountsTheWholeTree(t *testing.T) {
	t.Parallel()
	root := &yaml.Node{Kind: yaml.MappingNode, Content: []*yaml.Node{{Kind: yaml.ScalarNode}, {Kind: yaml.ScalarNode}}}
	assert.Equal(t, 3, countNodes(t.Context(), root, budget.AtMost(math.MaxInt)))
	assert.Equal(t, 3, countNodes(t.Context(), root, budget.Unbounded()))
}

// TestSequence_RefusesACopyItCannotTakeATemplateOf pins a copy whose source
// nests past maxPayloadDepth: the template it would take first is refused,
// and the action with it, before any target is written.
func TestSequence_RefusesACopyItCannotTakeATemplateOf(t *testing.T) {
	t.Parallel()
	p := planOf(t, "overlay: 1.1.0\ninfo: {title: o, version: \"1\"}\nactions:\n  - target: $.a\n    copy: $.b\n")
	a := ynode.Map()
	root := &yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{
		ynode.Map(ynode.Scalar("a"), a, ynode.Scalar("b"), nested(maxPayloadDepth+1)),
	}}

	_, diags, ok := runSequence(t.Context(), p, root, false, budget.Unbounded())

	assert.False(t, ok)
	require.Len(t, diags, 1)
	assert.Contains(t, diags[0].Message, "nested past")
	assert.Empty(t, a.Content, "no target was written")
}

// TestSequence_ChargesNoCopyOfManyOrNoNodes pins the charge for a copy whose
// source is not one node: nothing, since the sequence refuses or skips it
// without copying anything.
func TestSequence_ChargesNoCopyOfManyOrNoNodes(t *testing.T) {
	t.Parallel()
	p := planOf(t, "overlay: 1.1.0\ninfo: {title: o, version: \"1\"}\nactions:\n  - target: $.a\n    copy: $.b\n")
	s := &sequence{plan: p, size: budget.New(budget.AtMost(10))}
	targets := []*yaml.Node{ynode.Map()}

	assert.Zero(t, s.cost(p.actions[0], targets, nil))
	assert.Zero(t, s.cost(p.actions[0], targets, []*yaml.Node{ynode.Map(), ynode.Map()}))
	assert.Equal(t, 3, s.cost(p.actions[0], targets, []*yaml.Node{ynode.Map(ynode.Scalar("k"), ynode.Scalar("v"))}))
}
