package overlay

import (
	"bytes"
	"fmt"
	"strings"
	"testing"

	soaoverlay "github.com/speakeasy-api/openapi/overlay"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	yaml "gopkg.in/yaml.v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
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
// rests on. Applying one action at a time is what lets a budget count each
// action's matches on the tree it actually runs against, and it is only safe if
// nothing about the outcome moves: every case here is applied both ways, and
// the resulting trees and everything reported must agree to the byte.
//
// It is also what would notice the library changing: the sequence rebuilds the
// library's numbering, notes and joined error from its strings, and a change to
// any of them reddens here rather than drifting in silence.
//
// Each case runs twice more than the reference: with no budget, and under a
// budget it never reaches. The second is the one that counts every action's
// cost, so it is what holds that charging an action — querying its selector,
// weighing what it copies — changes nothing about what the action then does.
func TestSequence_AppliesAnOverlayExactlyAsTheLibraryDoes(t *testing.T) {
	t.Parallel()
	at := ir.Provenance{Source: 1}
	for name, ov := range differentialOverlays() {
		for _, lax := range []bool{false, true} {
			for _, budget := range []int{0, 1 << 20} {
				t.Run(fmt.Sprintf("%s/lax=%v/budget=%d", name, lax, budget), func(t *testing.T) {
					t.Parallel()
					wantTree, wantDiags, wantOK := applied(t, ov, func(d *soaoverlay.Overlay, r *yaml.Node) ([]ir.Diagnostic, bool) {
						return wholeOverlay(d, r, at, lax)
					})
					gotTree, gotDiags, gotOK := applied(t, ov, func(d *soaoverlay.Overlay, r *yaml.Node) ([]ir.Diagnostic, bool) {
						return runSequence(d, r, at, lax, budget)
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
			return runSequence(d, r, at, false, 0)
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
func budgetedOverlay(t *testing.T, ov string, maxNodes int) (*yaml.Node, []ir.Diagnostic, bool) {
	t.Helper()
	var root yaml.Node
	require.NoError(t, yaml.Unmarshal([]byte(sequenceSpec), &root))
	doc, err := soaoverlay.ParseReader(bytes.NewReader([]byte(ov)))
	require.NoError(t, err)
	diags, ok := runSequence(doc, &root, ir.Provenance{Source: 1}, false, maxNodes)
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
	before := countNodes(&fresh)

	root, diags, ok := budgetedOverlay(t, ov, int(before)+300)

	require.False(t, ok, "three path items times a 200-node update is past a budget with room for 300")
	last := diags[len(diags)-1]
	assert.Equal(t, diag.BudgetExceeded, last.Code)
	assert.Contains(t, last.Message, "action 1 of 1")
	assert.Equal(t, before, countNodes(root), "and nothing was built: the refusal came first")

	_, diags, ok = budgetedOverlay(t, ov, int(before)+1000)
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

	_, diags, ok := budgetedOverlay(t, head+flag+later, int(countNodes(&fresh))+300)

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
	before := int(countNodes(&fresh))

	root, diags, ok := budgetedOverlay(t, b.String(), before+10)

	assert.True(t, ok, "twenty charges of two nodes each exceed ten, and none of them adds a node: %+v", diags)
	assert.Equal(t, int64(before), countNodes(root))
}

// TestSequence_AppliesWithoutABudget pins that a zero or negative budget turns
// the check off rather than refusing everything, matching the rest of the
// compiler's budgets.
func TestSequence_AppliesWithoutABudget(t *testing.T) {
	t.Parallel()
	const head = "overlay: 1.0.0\ninfo: {title: o, version: \"1\"}\nactions:\n"
	ov := head + "  - target: $.paths.*\n    update: {x-bulk: " + wideUpdate(100) + "}\n"
	for _, budget := range []int{0, -1} {
		_, diags, ok := budgetedOverlay(t, ov, budget)
		assert.True(t, ok, "budget %d: %+v", budget, diags)
	}
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
	assert.Equal(t, int64(5), newWeigher(1000).weigh(plain, 0), "a mapping, two keys, two values")

	twice := decode("{x: &s {k: v}, y: *s, z: *s}")
	// The mapping, its three keys, x's value of three nodes, and each alias:
	// itself plus the three it names, which the clone copies again each time.
	assert.Equal(t, int64(1+3+3+2*(1+3)), newWeigher(1000).weigh(twice, 0))

	cycle := decode("&a [*a]")
	assert.Equal(t, int64(11), newWeigher(10).weigh(cycle, 0), "a cycle weighs the ceiling")

	assert.Equal(t, int64(11), newWeigher(10).weigh(decode(wideUpdate(50)), 0),
		"and so does anything past it, without counting on")
	assert.Equal(t, int64(0), newWeigher(10).weigh(nil, 0))
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

			s := &sequence{doc: doc, root: &root, maxNodes: 1 << 20}
			charged := s.cost(doc.Actions[0], 1<<20)
			before := countNodes(&root)

			_, diags := Apply(1, &root, Options{Data: []byte(head + action)})
			require.False(t, diag.HasError(diags), "%+v", diags)

			built := countNodes(&root) - before
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
	assert.Equal(t, int64(2), countNodes(root), "the mapping and its one real child")
}

// TestActionType_NamesByTheLibrarysPrecedence pins the names the renumbering
// matches on and the order they are decided in, which is the library's: an
// action that sets more than one kind is the first of remove, update, copy. A
// name that disagreed with the library's would leave that action's warnings
// numbered as if it were the only one, since the prefix would never match.
func TestActionType_NamesByTheLibrarysPrecedence(t *testing.T) {
	t.Parallel()
	update := yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	for want, action := range map[string]soaoverlay.Action{
		"remove":  {Remove: true, Update: update, Copy: "$.x"},
		"update":  {Update: update, Copy: "$.x"},
		"copy":    {Copy: "$.x"},
		"unknown": {Target: "$.x"},
	} {
		assert.Equal(t, want, actionType(action))
	}
}
