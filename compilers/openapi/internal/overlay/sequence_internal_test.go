// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package overlay

import (
	"bytes"
	"fmt"
	"math"
	"strings"
	"testing"

	soaoverlay "github.com/speakeasy-api/openapi/overlay"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/budget"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/internal/ctxtest"
	"github.com/dexpace/morphic/ir"
)

// sequenceSpec is the tree the differential cases apply to. It has a few of
// every shape a selector here reads: path items, a mapping of schemas, and a
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

// wholeOverlay is how this package applied an overlay before it applied one
// action at a time: the library's own whole-overlay call, its warnings reported
// verbatim. It is the reference the sequence is held to.
func wholeOverlay(doc *soaoverlay.Overlay, root *yaml.Node, at ir.Provenance, lax bool) ([]ir.Diagnostic, bool) {
	if lax {
		if err := doc.ApplyTo(root); err != nil {
			return []ir.Diagnostic{failed(at, err)}, false
		}
		return nil, true
	}
	warnings, err := doc.ApplyToStrict(root)
	out := make([]ir.Diagnostic, 0, len(warnings))
	for _, w := range warnings {
		out = append(out, diag.Newf(ir.SeverityWarning, diag.OverlayAction, at, "%s", w))
	}
	if err != nil {
		return append(out, failed(at, err)), false
	}
	return out, true
}

// applied parses spec and overlay afresh and applies the one to the other, so
// the two sides of a comparison never share a node.
func applied(t *testing.T, ov string, run func(*soaoverlay.Overlay, *yaml.Node) ([]ir.Diagnostic, bool)) (string, []ir.Diagnostic, bool) {
	t.Helper()
	var root yaml.Node
	require.NoError(t, yaml.Unmarshal([]byte(sequenceSpec), &root))
	doc, err := soaoverlay.ParseReader(bytes.NewReader([]byte(ov)))
	require.NoError(t, err)
	diags, ok := run(doc, &root)
	out, err := yaml.Marshal(&root)
	require.NoError(t, err)
	return string(out), diags, ok
}

// differentialOverlays are the overlays the sequence must apply exactly as the
// library does. Each exercises one thing the sequence rebuilds rather than
// inherits: numbering across actions, a selector that matches nothing in the
// middle of an overlay and the error joining several, the filter note said
// once, a failure that ends the loop and drops what came before it, each action
// kind, both overlay versions, and lax application.
func differentialOverlays() map[string]string {
	const v100 = "overlay: 1.0.0\ninfo: {title: o, version: \"1\"}\nactions:\n"
	const v110 = "overlay: 1.1.0\ninfo: {title: o, version: \"1\"}\nactions:\n"
	return map[string]string{
		"one update":                  v100 + "  - target: $.info\n    update: {description: d}\n",
		"an update that does nothing": v100 + "  - target: $.info\n    update: {title: T}\n",
		"every kind in order": v100 +
			"  - target: $.info\n    update: {description: d}\n" +
			"  - target: $.components.schemas.Two\n    copy: $.components.schemas.One\n" +
			"  - target: $.tags[0]\n    remove: true\n",
		"a miss in the middle": v100 +
			"  - target: $.info\n    update: {description: d}\n" +
			"  - target: $.nowhere\n    update: {x: 1}\n" +
			"  - target: $.info\n    update: {summary: s}\n",
		"two misses joined": v100 +
			"  - target: $.nowhere\n    update: {x: 1}\n" +
			"  - target: $.info\n    update: {title: T}\n" +
			"  - target: $.elsewhere\n    update: {y: 2}\n",
		"a filter said once": v100 +
			"  - target: $.paths.*.get[?(@.operationId == 'a')]\n    update: {summary: s}\n" +
			"  - target: $.paths.*.get[?(@.operationId == 'b')]\n    update: {summary: s}\n",
		"a failure that ends the loop": v100 +
			"  - target: $.info\n    update: {title: T}\n" +
			"  - target: $.info\n    copy: $.paths.*\n" +
			"  - target: $.info\n    update: {summary: s}\n",
		"a copy source that is missing": v100 + "  - target: $.info\n    copy: $.nowhere\n",
		"version 1.1.0":                 v110 + "  - target: $.info\n    update: {description: d}\n",
		"an earlier action feeds a later filter": v100 +
			"  - target: $.paths.*.get\n    update: {x-flag: on}\n" +
			"  - target: $.paths.*.get[?(@.x-flag == 'on')]\n    update: {summary: flagged}\n",
		"an action of no kind":           v100 + "  - target: $.info\n",
		"a selector that does not parse": v100 + "  - target: $.info[\n    update: {description: d}\n",
		"a remove":                       v100 + "  - target: $.paths['/a']\n    remove: true\n",
	}
}

// TestSequence_AppliesAnOverlayExactlyAsTheLibraryDoes is the check the design
// rests on. One action at a time lets the budget count each action's matches on
// the tree it runs against, and is safe only if nothing about the outcome
// moves: every case is applied both ways, and the trees and reports must agree
// to the byte.
//
// It also reddens when the library changes the numbering, notes or joined error
// the sequence rebuilds from its strings.
//
// Each case also runs with no budget and under one it never reaches; the latter
// holds that charging an action changes nothing about what it then does.
func TestSequence_AppliesAnOverlayExactlyAsTheLibraryDoes(t *testing.T) {
	t.Parallel()
	at := ir.Provenance{Source: 1}
	for name, ov := range differentialOverlays() {
		for _, lax := range []bool{false, true} {
			for within, limit := range map[string]budget.Limit{"none": budget.Unbounded(), "unreached": budget.AtMost(1 << 20)} {
				t.Run(fmt.Sprintf("%s/lax=%v/budget=%s", name, lax, within), func(t *testing.T) {
					t.Parallel()
					wantTree, wantDiags, wantOK := applied(t, ov, func(d *soaoverlay.Overlay, r *yaml.Node) ([]ir.Diagnostic, bool) {
						return wholeOverlay(d, r, at, lax)
					})
					gotTree, gotDiags, gotOK := applied(t, ov, func(d *soaoverlay.Overlay, r *yaml.Node) ([]ir.Diagnostic, bool) {
						return runSequence(t.Context(), d, r, at, lax, limit)
					})

					assert.Equal(t, wantOK, gotOK, "whether the overlay is usable")
					assert.Equal(t, orNil(wantDiags), orNil(gotDiags), "what is reported, and in what order")
					assert.Equal(t, wantTree, gotTree, "the tree the overlay leaves behind")
				})
			}
		}
	}
}

// orNil forgives the one difference the comparison above allows: the library's
// strict path reported nothing as an empty list and its lax path as nil. Both
// mean nothing to report, the sequence says nil for both, and that is the only
// distinction this erases.
func orNil(diags []ir.Diagnostic) []ir.Diagnostic {
	if len(diags) == 0 {
		return nil
	}
	return diags
}

// TestSequence_TheCorpusExercisesWhatItClaims guards the differential corpus
// against quietly passing on overlays that never reach the paths it names. A
// case that meant to test the joined error but matched everything would agree
// with the library trivially.
func TestSequence_TheCorpusExercisesWhatItClaims(t *testing.T) {
	t.Parallel()
	at := ir.Provenance{Source: 1}
	cases := differentialOverlays()
	strict := func(name string) ([]ir.Diagnostic, bool) {
		_, diags, ok := applied(t, cases[name], func(d *soaoverlay.Overlay, r *yaml.Node) ([]ir.Diagnostic, bool) {
			return runSequence(t.Context(), d, r, at, false, budget.Unbounded())
		})
		return diags, ok
	}
	messages := func(diags []ir.Diagnostic) string {
		var b strings.Builder
		for _, d := range diags {
			b.WriteString(d.Message + "\n")
		}
		return b.String()
	}

	diags, ok := strict("two misses joined")
	require.False(t, ok)
	assert.Contains(t, messages(diags), `"$.nowhere" did not match any targets,selector "$.elsewhere"`,
		"two selectors joined into one error, in order")
	assert.Contains(t, messages(diags), "update action (2 / 3)", "numbered against the whole overlay")

	diags, _ = strict("a filter said once")
	assert.Equal(t, 1, strings.Count(messages(diags), "filter expression"), "the note about filters, once")

	diags, ok = strict("a failure that ends the loop")
	require.False(t, ok)
	require.Len(t, diags, 1, "the failure alone: what came before it is dropped, as the library drops it")
	assert.Contains(t, diags[0].Message, "matched multiple nodes")
}

// budgetedOverlay applies an overlay to sequenceSpec under a node budget.
func budgetedOverlay(t *testing.T, ov string, limit budget.Limit) (*yaml.Node, []ir.Diagnostic, bool) {
	t.Helper()
	var root yaml.Node
	require.NoError(t, yaml.Unmarshal([]byte(sequenceSpec), &root))
	doc, err := soaoverlay.ParseReader(bytes.NewReader([]byte(ov)))
	require.NoError(t, err)
	diags, ok := runSequence(t.Context(), doc, &root, ir.Provenance{Source: 1}, false, limit)
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
	later := "  - target: $.paths.*.get[?(@.x-flag == 'on')]\n    update: {x-bulk: " + wideUpdate(100) + "}\n"

	var fresh yaml.Node
	require.NoError(t, yaml.Unmarshal([]byte(sequenceSpec), &fresh))
	doc, err := soaoverlay.ParseReader(bytes.NewReader([]byte(head + later)))
	require.NoError(t, err)
	path, err := doc.NewPath(doc.Actions[0].Target, nil)
	require.NoError(t, err)
	require.Empty(t, path.Query(&fresh), "on the source, the later filter matches nothing")

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
	assert.Contains(t, last.Message, fmt.Sprintf("action 2 of 2 would grow the document to at least %d nodes", left+charge))
}

// TestSequence_AppliesNoActionOnceItsContextRefused cancels a sequence at
// every check of its context a whole one makes, the recounts its overstated
// charges take among them, and requires the tree it leaves to be the tree as
// it stood at the refusal: no action is applied once a check has refused,
// since a charge a cancel cut short may have counted too few nodes. Each
// action writes a title of its own, so one applied late shows.
func TestSequence_AppliesNoActionOnceItsContextRefused(t *testing.T) {
	t.Parallel()
	var b strings.Builder
	b.WriteString("overlay: 1.0.0\ninfo: {title: o, version: \"1\"}\nactions:\n")
	for i := range 20 {
		fmt.Fprintf(&b, "  - target: $.info\n    update: {title: T%d}\n", i)
	}
	doc, err := soaoverlay.ParseReader(bytes.NewReader([]byte(b.String())))
	require.NoError(t, err)
	tree := func() *yaml.Node {
		var root yaml.Node
		require.NoError(t, yaml.Unmarshal([]byte(sequenceSpec), &root))
		return &root
	}
	limit := budget.AtMost(countNodes(t.Context(), tree(), budget.AtMost(maxNodes)) + 10)
	at := ir.Provenance{Source: 1}
	whole := &ctxtest.Countdown{Context: t.Context(), Left: math.MaxInt}
	_, ok := runSequence(whole, doc, tree(), at, false, limit)
	require.True(t, ok)
	require.Greater(t, whole.Calls, 1+len(doc.Actions),
		"beside the first count and the check after each charge, a recount asks too")

	for left := range whole.Calls {
		root := tree()
		var refusedOn []byte
		ctx := &ctxtest.Countdown{Context: t.Context(), Left: left}
		ctx.OnRefuse = func() { refusedOn, err = yaml.Marshal(root) }

		diags, ok := runSequence(ctx, doc, root, at, false, limit)

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
// library's clone: an alias costs itself and everything it names, since the
// clone follows it; an anchor named twice is walked once but counted twice,
// since the clone copies it twice; and a cycle weighs past any budget, since
// the clone would never finish.
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
	const head = "overlay: 1.0.0\ninfo: {title: o, version: \"1\", x-a: &a {k: v, w: [1, 2]}, x-b: &b {p: *a, q: *a}}\nactions:\n"
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
			doc, err := soaoverlay.ParseReader(bytes.NewReader([]byte(head + action)))
			require.NoError(t, err)

			s := &sequence{doc: doc, root: &root, size: budget.New(budget.AtMost(1 << 20))}
			charged := s.cost(doc.Actions[0])
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

// TestActionType_NamesByTheLibrarysPrecedence holds the names the renumbering
// matches on, and the order they are decided in, to the library's: an action that
// sets more than one kind is the first of remove, update, copy. The library
// reports the name only as the prefix of the warnings it files for an action, so
// each action here makes it file one, and the name is read off that prefix. A
// name that disagreed would leave the action's warnings numbered as if it were
// the only one, since the prefix would never match. The want column pins today's
// library.
func TestActionType_NamesByTheLibrarysPrecedence(t *testing.T) {
	t.Parallel()
	const v100 = "overlay: 1.0.0\ninfo: {title: o, version: \"1\"}\nactions:\n"
	tests := []struct {
		name, action, want string
	}{
		// $.paths./a is no RFC 9535 selector, which the library warns of for a remove.
		{"every kind at once", "  - target: $.paths./a\n    remove: true\n    update: {title: T}\n    copy: $.nowhere\n", "remove"},
		{"remove and copy", "  - target: $.paths./a\n    remove: true\n    copy: $.components.schemas.One\n", "remove"},
		{"update and copy", "  - target: $.info\n    update: {title: T}\n    copy: $.nowhere\n", "update"},
		{"update alone", "  - target: $.info\n    update: {title: T}\n", "update"},
		{"copy alone", "  - target: $.components.schemas.Two\n    copy: $.components.schemas.One\n", "copy"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var root yaml.Node
			require.NoError(t, yaml.Unmarshal([]byte(sequenceSpec), &root))
			doc, err := soaoverlay.ParseReader(strings.NewReader(v100 + tc.action))
			require.NoError(t, err)
			require.Len(t, doc.Actions, 1)

			warnings, err := doc.ApplyToStrict(&root)
			require.NoError(t, err)
			require.Len(t, warnings, 1, "the one warning the action files")
			name, _, found := strings.Cut(warnings[0], " action (1 / 1) target=")
			require.True(t, found, "the warning opens with the action's name: %q", warnings[0])

			assert.Equal(t, tc.want, name, "the library's name for the action")
			assert.Equal(t, name, actionType(doc.Actions[0]))
		})
	}
}

// TestActionType_NamesAnActionOfNoKindUnknown pins the name for an action that
// sets no kind. The library fails the overlay on it and files no warning, so it
// never reports a name for one and this is the compiler's own.
func TestActionType_NamesAnActionOfNoKindUnknown(t *testing.T) {
	t.Parallel()
	var root yaml.Node
	require.NoError(t, yaml.Unmarshal([]byte(sequenceSpec), &root))
	doc, err := soaoverlay.ParseReader(strings.NewReader(
		"overlay: 1.0.0\ninfo: {title: o, version: \"1\"}\nactions:\n  - target: $.info\n"))
	require.NoError(t, err)
	require.Len(t, doc.Actions, 1)

	warnings, err := doc.ApplyToStrict(&root)

	require.Error(t, err, "the library fails the overlay on an action of no kind")
	assert.Empty(t, warnings, "and files no warning to carry a name")
	assert.Equal(t, "unknown", actionType(doc.Actions[0]))
}
