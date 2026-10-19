package load

import (
	"encoding/json/jsontext"
	"fmt"
	"math/rand/v2"
	"slices"
	"strings"
	"testing"

	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	yaml "gopkg.in/yaml.v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/scan"
)

// reachSite runs chainCycle over spec with its finding placed by pointer, and
// returns the pointer it names, which it requires.
func reachSite(t *testing.T, spec string) jsontext.Pointer {
	t.Helper()
	doc, root := buildDoc(t, spec)
	d, found := chainCycle(t.Context(), scan.InTree(0, scan.Pointers(root)), root, doc, Siblings{})
	require.True(t, found, "a cycle in\n%s", spec)
	require.Equal(t, diag.CyclicRef, d.Code)
	return d.Provenance.Pointer
}

// TestReachCycle_NamesACycleMemberInEitherOrder is GitHub #912 for the check
// that sees cycles closing through $defs-relative pointers: the reference with
// the least pointer among those the cycle runs through, in either declaration
// order, and never one that only leads into the cycle, as R does here though
// it shares A's spelling.
func TestReachCycle_NamesACycleMemberInEitherOrder(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		members [2]string
		want    jsontext.Pointer
	}{
		{"a loop of two", [2]string{
			"        B: {$ref: '#/$defs/A'}\n", "        A: {$ref: '#/$defs/B'}\n",
		}, "/components/schemas/H/$defs/A"},
		{"a reference into a self-loop", [2]string{
			"        R: {$ref: '#/$defs/A'}\n", "        A: {$ref: '#/$defs/A'}\n",
		}, "/components/schemas/H/$defs/A"},
		{"two self-loops", [2]string{
			"        Z: {$ref: '#/$defs/Z'}\n", "        Y: {$ref: '#/$defs/Y'}\n",
		}, "/components/schemas/H/$defs/Y"},
		{"a lookup landing beside the cycle as well", [2]string{
			"        Z: {$anchor: z, $ref: '#z'}\n", "        Y: {$anchor: z, type: string}\n",
		}, "/components/schemas/H/$defs/Z"},
	}
	const header = "    H:\n      $defs:\n"
	for _, tc := range tests {
		for _, order := range [][2]string{tc.members, {tc.members[1], tc.members[0]}} {
			t.Run(tc.name, func(t *testing.T) {
				t.Parallel()
				assert.Equal(t, tc.want, reachSite(t, schemasDoc(header+order[0]+order[1])))
			})
		}
	}
}

// TestCycleMembers_KeepsTheFirstCycleWhenTheBudgetCutsTheSearch pins what the
// search answers when its budget runs out after it closes a cycle and before
// that cycle's component is whole: the references on the cycle it closed, and
// not T and R leading into it, so a document the search it replaced refused at
// that budget is refused still, at a member of the cycle. Each order is tried.
func TestCycleMembers_KeepsTheFirstCycleWhenTheBudgetCutsTheSearch(t *testing.T) {
	t.Parallel()
	members := []string{
		"        T: {$ref: '#/$defs/R'}\n", "        R: {$ref: '#/$defs/A'}\n",
		"        A: {$ref: '#/$defs/B'}\n", "        B: {$ref: '#/$defs/A'}\n",
	}
	reversed := slices.Clone(members)
	slices.Reverse(reversed)
	for _, order := range [][]string{members, reversed} {
		doc, root := buildDoc(t, schemasDoc("    H:\n      $defs:\n"+strings.TrimSuffix(strings.Join(order, ""), "\n")))
		pointers := scan.Pointers(root)
		cut := cutCycleMembers(t, doc, root)
		got := make([]jsontext.Pointer, 0, len(cut))
		for _, m := range cut {
			p, _ := pointers(m)
			got = append(got, p)
		}
		assert.ElementsMatch(t, []jsontext.Pointer{"/components/schemas/H/$defs/A", "/components/schemas/H/$defs/B"}, got,
			"in the order %q", order)
	}
}

// cutCycleMembers runs the search under the least budget that lets it close
// a cycle, and returns what it names, requiring that the budget cut it short.
func cutCycleMembers(t *testing.T, doc *soa.OpenAPI, root *yaml.Node) []*yaml.Node {
	t.Helper()
	for extra := 1; extra < 64; extra++ {
		r, starts := newReach(t.Context(), maxReachWork, root, doc, Siblings{})
		r.budget.limit = r.budget.spent + extra
		if members := r.cycleMembers(starts); len(members) > 0 {
			require.True(t, r.incomplete(), "the first budget that closes the cycle stops the search there")
			return members
		}
	}
	t.Fatal("no budget below 64 steps closes the cycle")
	return nil
}

// TestCycleMembers_NamesEveryReferenceOnACycleAndNoOther holds the search, with
// budget to spare, to what the graph says by plain reachability: a set member
// is on a cycle exactly when its class reaches back to the set, over 1,500
// generated documents of $defs and anchored references.
func TestCycleMembers_NamesEveryReferenceOnACycleAndNoOther(t *testing.T) {
	t.Parallel()
	r := rand.New(rand.NewPCG(912, 914))
	cyclic := 0
	for range 1500 {
		spec := randomDefsDoc(r)
		doc, root := buildDoc(t, spec)
		search, starts := newReach(t.Context(), maxReachWork, root, doc, Siblings{})
		got := map[*yaml.Node]bool{}
		for _, m := range search.cycleMembers(starts) {
			got[m] = true
		}
		brute, starts := newReach(t.Context(), maxReachWork, root, doc, Siblings{})
		want := onCycles(brute, starts)
		require.Equal(t, want, got, "in\n%s", spec)
		if len(want) > 0 {
			cyclic++
		}
	}
	assert.Greater(t, cyclic, 100, "enough of the documents hold a cycle to compare")
}

// randomDefsDoc is two schemas whose $defs refer to each other's, through
// $defs-relative pointers, pointers from the root, and anchors, two names of
// which several may declare, so one lookup can land on more than one node.
func randomDefsDoc(r *rand.Rand) string {
	names := []string{"A", "B", "C"}
	var b strings.Builder
	for h := range 2 {
		fmt.Fprintf(&b, "    H%d:\n      $defs:\n", h)
		for _, n := range names {
			target := names[r.IntN(len(names))]
			var body string
			switch r.IntN(6) {
			case 0:
				body = "{type: string}"
			case 1, 2:
				body = fmt.Sprintf("{$ref: '#/$defs/%s'}", target)
			case 3:
				body = fmt.Sprintf("{$ref: '#/components/schemas/H%d/$defs/%s'}", r.IntN(2), target)
			case 4:
				body = fmt.Sprintf("{$anchor: a%d, $ref: '#/$defs/%s'}", r.IntN(2), target)
			default:
				body = fmt.Sprintf("{$ref: '#a%d'}", r.IntN(2))
			}
			fmt.Fprintf(&b, "        %s: %s\n", n, body)
		}
	}
	return schemasDoc(strings.TrimSuffix(b.String(), "\n"))
}

// onCycles is, by plain reachability over the graph reach walks from starts,
// every set member whose class reaches the set back.
func onCycles(r *reach, starts []*yaml.Node) map[*yaml.Node]bool {
	type edge struct {
		to  vertex
		via *yaml.Node
	}
	out := map[vertex][]edge{}
	queue := []vertex{}
	for _, s := range starts {
		queue = append(queue, vertex{class: r.classOf(s)})
	}
	for seen := map[vertex]bool{}; len(queue) > 0; queue = queue[1:] {
		v := queue[0]
		if seen[v] {
			continue
		}
		seen[v] = true
		if v.set == nil {
			for _, s := range r.lookups(v.class) {
				out[v] = append(out[v], edge{to: vertex{set: s}})
			}
		} else {
			for _, m := range v.set.members {
				out[v] = append(out[v], edge{to: vertex{class: r.classOf(m)}, via: m})
			}
		}
		for _, e := range out[v] {
			queue = append(queue, e.to)
		}
	}
	reaches := func(from, to vertex) bool {
		seen, stack := map[vertex]bool{}, []vertex{from}
		for len(stack) > 0 {
			v := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			if v == to {
				return true
			}
			if !seen[v] {
				seen[v] = true
				for _, e := range out[v] {
					stack = append(stack, e.to)
				}
			}
		}
		return false
	}
	members := map[*yaml.Node]bool{}
	for v, edges := range out {
		for _, e := range edges {
			if e.via != nil && reaches(e.to, v) {
				members[e.via] = true
			}
		}
	}
	return members
}

// firstCycleWithin is the search cycleMembers replaced, as it stood: a
// three-colour depth-first search from each start in turn, stopping at the
// first vertex it meets on its own path, a step and a charge at a time.
func firstCycleWithin(r *reach, starts []*yaml.Node) bool {
	type frame struct {
		v     vertex
		sets  []*posSet
		nodes []*yaml.Node
	}
	state := map[vertex]int{}
	enter := func(v vertex) frame {
		state[v] = 1
		if v.set != nil {
			return frame{v: v, nodes: v.set.members}
		}
		return frame{v: v, sets: r.lookups(v.class)}
	}
	for _, start := range starts {
		v0 := vertex{class: r.classOf(start)}
		if state[v0] != 0 {
			continue
		}
		stack := []frame{enter(v0)}
		for len(stack) > 0 && r.budget.spend(1) {
			top := &stack[len(stack)-1]
			var next vertex
			switch {
			case len(top.sets) > 0:
				next, top.sets = vertex{set: top.sets[0]}, top.sets[1:]
			case len(top.nodes) > 0:
				next, top.nodes = vertex{class: r.classOf(top.nodes[0])}, top.nodes[1:]
			default:
				state[top.v] = 2
				stack = stack[:len(stack)-1]
				continue
			}
			switch state[next] {
			case 1:
				return true
			case 0:
				stack = append(stack, enter(next))
			}
		}
	}
	return false
}

// TestCycleMembers_ClosesItsFirstCycleWhereTheSearchItReplacedDid holds the
// claim the search's doc makes against the search it replaced: on documents
// reach refuses, under every budget up to the point both find their cycle,
// the two find one under exactly the same budgets, so no document moves
// between refused and warned about at the bound.
func TestCycleMembers_ClosesItsFirstCycleWhereTheSearchItReplacedDid(t *testing.T) {
	t.Parallel()
	specs := make([]string, 0, 3+len(onlyReachCrashFixtures))
	specs = append(specs,
		schemasDoc("    H:\n      $defs:\n        A: {$ref: '#/$defs/B'}\n        B: {$ref: '#/$defs/A'}"),
		schemasDoc("    H:\n      $defs:\n        R: {$ref: '#/$defs/A'}\n        A: {$ref: '#/$defs/A'}"),
		schemasDoc("    H:\n      $defs:\n        Z: {$anchor: z, $ref: '#z'}\n        Y: {$anchor: z, type: string}"),
	)
	for _, f := range onlyReachCrashFixtures {
		specs = append(specs, schemasDoc(f.schemas))
	}
	for _, spec := range specs {
		doc, root := buildDoc(t, spec)
		met := 0
		for extra := 0; met < 2; extra++ {
			require.Less(t, extra, 1<<16, "neither search finishes in\n%s", spec)
			old, starts := newReach(t.Context(), maxReachWork, root, doc, Siblings{})
			old.budget.limit = old.budget.spent + extra
			replaced := firstCycleWithin(old, starts)
			r, starts := newReach(t.Context(), maxReachWork, root, doc, Siblings{})
			r.budget.limit = r.budget.spent + extra
			require.Equal(t, replaced, len(r.cycleMembers(starts)) > 0, "under %d steps, in\n%s", extra, spec)
			if replaced {
				met++
			}
		}
	}
}
