package load

import (
	"encoding/json/jsontext"
	"testing"

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
// that cycle's component is whole: the references on the path it closed, so a
// document the search it replaced refused at that budget is refused still.
func TestCycleMembers_KeepsTheFirstCycleWhenTheBudgetCutsTheSearch(t *testing.T) {
	t.Parallel()
	doc, root := buildDoc(t, schemasDoc("    H:\n      $defs:\n        A: {$ref: '#/$defs/B'}\n        B: {$ref: '#/$defs/A'}"))
	fresh := func() (*reach, []*yaml.Node) {
		return newReach(t.Context(), maxReachWork, root, doc, Siblings{})
	}
	whole, starts := fresh()
	require.Len(t, whole.cycleMembers(starts), 4, "both references, from the cycle and from its component")

	for extra := 1; extra < 64; extra++ {
		r, starts := fresh()
		r.budget.limit = r.budget.spent + extra
		members := r.cycleMembers(starts)
		if len(members) == 0 {
			continue
		}
		assert.True(t, r.incomplete(), "the first budget that closes the cycle stops the search there")
		assert.Len(t, members, 2, "the path's two references, and nothing from a component left open")
		return
	}
	t.Fatal("no budget below 64 steps closes the cycle")
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
