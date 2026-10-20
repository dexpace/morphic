package ir_test

import (
	"fmt"
	"math/rand/v2"
	"slices"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
)

// containmentArity is how many types a value of each kind is made of when every
// field is filled: a decision per kind, so a new kind cannot join the sum
// without someone saying whether it holds other types.
var containmentArity = map[ir.TypeKind]int{
	ir.KindModel: 7, ir.KindUnion: 1, ir.KindList: 1, ir.KindMap: 2, ir.KindTuple: 2, ir.KindScalar: 1,
	ir.KindPrimitive: 0, ir.KindEnum: 0, ir.KindLiteral: 0, ir.KindExternal: 0, ir.KindAny: 0,
}

func TestFacts_EveryKindDecidesItsContainment(t *testing.T) {
	t.Parallel()
	assert.Len(t, containmentArity, len(allKinds), "containmentArity classifies a kind the sum no longer has")
	for _, k := range allKinds {
		t.Run(string(k), func(t *testing.T) {
			t.Parallel()
			arity, decided := containmentArity[k]
			require.True(t, decided, "%s holds other types or it does not: say which in containmentArity", k)

			td := newFilled(t, k)
			held := ir.AppendContained(nil, td)
			assert.Len(t, held, arity)
			assert.Len(t, slices.Compact(slices.Sorted(slices.Values(held))), len(held), "every filled field names its own type")
			edges := slices.Collect(ir.TypeEdges(td))
			for _, id := range held {
				assert.Contains(t, edges, id, "containment is a subset of the references the type makes")
			}
		})
	}
}

func TestFacts_SelfContainmentIsRecursive(t *testing.T) {
	t.Parallel()
	for name, edge := range usageEdgeCases() {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			doc := compDoc(edge.build("x"))
			facts := ir.NewFacts(doc)
			assert.True(t, facts.Recursive("x"), "a type holding itself by this edge is recursive")
			assert.Equal(t, []ir.TypeID{"x"}, facts.Component("x"))
		})
	}
}

func TestFacts_ReferencesThatAreNotContainmentAreNotRecursive(t *testing.T) {
	t.Parallel()
	self := ir.TypeRef{Target: "x"}
	selfValue := &ir.Value{Kind: ir.ValueRefKind, Ref: &ir.ValueRef{Type: "x"}}
	for name, td := range map[string]ir.TypeDef{
		"model discriminator mapping": compModel("x", func(m *ir.Model) {
			m.Discriminator = &ir.Discriminator{Mapping: map[string]ir.TypeID{"a": "x"}, Default: "x"}
		}),
		"union discriminator mapping": &ir.Union{ID: "x", Discriminator: &ir.Discriminator{Mapping: map[string]ir.TypeID{"a": "x"}}},
		"property default":            compModel("x", func(m *ir.Model) { m.Properties = []ir.Property{{Default: selfValue}} }),
		"property encoding": compModel("x", func(m *ir.Model) {
			m.Properties = []ir.Property{{Encoding: &ir.Encoding{WireType: &self, Schema: &self}}}
		}),
		"scalar encoding":      &ir.Scalar{ID: "x", Encoding: &ir.Encoding{WireType: &self}},
		"list encoding":        &ir.List{ID: "x", Encoding: &ir.Encoding{WireType: &self}},
		"literal value":        &ir.Literal{ID: "x", Value: *selfValue},
		"example value":        &ir.Any{ID: "x", Examples: []ir.Example{{Value: selfValue}}},
		"template argument":    &ir.Any{ID: "x", Instantiation: &ir.TemplateInstantiation{Args: []ir.TemplateArg{{Type: &self}}}},
		"availability history": &ir.Any{ID: "x", Availability: &ir.Availability{TypeChangedFrom: []ir.VersionedType{{Type: self}}}},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			facts := ir.NewFacts(compDoc(td))
			assert.False(t, facts.Recursive("x"), "the reference names a type without making a value hold one")
			assert.Equal(t, []ir.TypeID{"x"}, facts.Component("x"))
		})
	}
}

func TestFacts_MutualContainmentIsOneComponent(t *testing.T) {
	t.Parallel()
	doc := compDoc(
		useHolder("a", "items"),
		&ir.List{ID: "items", Elem: useRef("b")},
		useHolder("b", "a", "leaf"),
		useType("leaf"),
	)
	facts := ir.NewFacts(doc)
	for _, id := range []ir.TypeID{"a", "b", "items"} {
		assert.True(t, facts.Recursive(id), id)
		assert.Equal(t, []ir.TypeID{"a", "b", "items"}, facts.Component(id), id)
	}
	assert.False(t, facts.Recursive("leaf"))
	assert.Equal(t, []ir.TypeID{"leaf"}, facts.Component("leaf"))
}

func TestFacts_ABaseAndItsRoutedSubtypeAreNotRecursive(t *testing.T) {
	t.Parallel()
	doc := compDoc(
		compModel("base", func(m *ir.Model) {
			m.Discriminator = &ir.Discriminator{Mapping: map[string]ir.TypeID{"s": "sub"}}
		}),
		compModel("sub", func(m *ir.Model) { m.Base = &ir.TypeRef{Target: "base"} }),
	)
	facts := ir.NewFacts(doc)
	for _, id := range []ir.TypeID{"base", "sub"} {
		assert.False(t, facts.Recursive(id), id)
		assert.Equal(t, []ir.TypeID{id}, facts.Component(id), id)
	}
}

func TestFacts_AnAcyclicDiamondIsNotRecursive(t *testing.T) {
	t.Parallel()
	doc := compDoc(useHolder("top", "left", "right"), useHolder("left", "bottom"), useHolder("right", "bottom"), useType("bottom"))
	facts := ir.NewFacts(doc)
	for _, id := range []ir.TypeID{"top", "left", "right", "bottom"} {
		assert.False(t, facts.Recursive(id), id)
		assert.Equal(t, []ir.TypeID{id}, facts.Component(id), id)
	}
}

func TestFacts_ADanglingReferenceIsIgnored(t *testing.T) {
	t.Parallel()
	facts := ir.NewFacts(compDoc(useHolder("a", "ghost")))
	assert.False(t, facts.Recursive("a"))
	assert.Equal(t, []ir.TypeID{"a"}, facts.Component("a"))
	assert.False(t, facts.Recursive("ghost"), "the document declares no such type")
	assert.Nil(t, facts.Component("ghost"))
	assert.Nil(t, facts.Component(""))
}

func TestFacts_ATypedNilEntryIsATypeHoldingNothing(t *testing.T) {
	t.Parallel()
	doc := compDoc(useHolder("a", "nilm"))
	doc.Types["nilm"] = (*ir.Model)(nil)
	facts := ir.NewFacts(doc)
	assert.False(t, facts.Recursive("nilm"))
	assert.Equal(t, []ir.TypeID{"nilm"}, facts.Component("nilm"))
}

func TestFacts_ComponentIsSortedAndTheCallersOwn(t *testing.T) {
	t.Parallel()
	// The search from "a" closes the ring with "c" ahead of "b", the reverse of
	// the order the component must be reported in.
	doc := compDoc(useHolder("a", "c"), useHolder("c", "b"), useHolder("b", "a"))
	facts := ir.NewFacts(doc)

	got := facts.Component("b")
	require.Equal(t, []ir.TypeID{"a", "b", "c"}, got)
	got[0] = "clobbered"
	assert.Equal(t, []ir.TypeID{"a", "b", "c"}, facts.Component("a"), "the index keeps its own copy")
}

// factsLongContainment builds n models, t000000 first, each holding the next
// through a property, and the last holding nothing.
func factsLongContainment(n int) *ir.Document {
	defs := make([]ir.TypeDef, 0, n)
	for i := range n {
		defs = append(defs, compModel(factsLongID(i), func(m *ir.Model) {
			if i+1 < n {
				m.Properties = []ir.Property{useProp(factsLongID(i + 1))}
			}
		}))
	}
	return compDoc(defs...)
}

func factsLongID(i int) ir.TypeID { return ir.TypeID(fmt.Sprintf("t%06d", i)) }

func TestFacts_AVeryLongChainNeedsNoRecursion(t *testing.T) {
	t.Parallel()
	const n = 100_000
	doc := factsLongContainment(n)

	chain := ir.NewFacts(doc)
	assert.False(t, chain.Recursive(factsLongID(0)))
	assert.Equal(t, []ir.TypeID{factsLongID(n / 2)}, chain.Component(factsLongID(n/2)))

	// Closing the chain on itself makes every model one component.
	last := doc.Types[factsLongID(n-1)].(*ir.Model)
	last.Properties = []ir.Property{useProp(factsLongID(0))}
	ring := ir.NewFacts(doc)
	assert.True(t, ring.Recursive(factsLongID(n-1)))
	members := ring.Component(factsLongID(n / 2))
	require.Len(t, members, n)
	assert.True(t, slices.IsSorted(members))
	assert.Equal(t, factsLongID(0), members[0])
	assert.Equal(t, factsLongID(n-1), members[n-1])
}

// factsRandomContainment builds up to twenty-five models holding each other,
// and undeclared types, through random properties. It returns the document and
// the edges it drew among the declared models.
func factsRandomContainment(rng *rand.Rand) (*ir.Document, []ir.TypeID, map[ir.TypeID][]ir.TypeID) {
	n := 1 + rng.IntN(25)
	ids := make([]ir.TypeID, n)
	for i := range n {
		ids[i] = ir.TypeID(fmt.Sprintf("t%02d", i))
	}
	pick := func() ir.TypeID {
		if i := rng.IntN(n + 2); i < n {
			return ids[i]
		}
		return "ghost"
	}
	edges := map[ir.TypeID][]ir.TypeID{}
	defs := make([]ir.TypeDef, 0, n)
	for _, id := range ids {
		held := make([]ir.TypeID, rng.IntN(4))
		for i := range held {
			held[i] = pick()
		}
		for _, h := range held {
			if h != "ghost" {
				edges[id] = append(edges[id], h)
			}
		}
		defs = append(defs, useHolder(id, held...))
	}
	return compDoc(defs...), ids, edges
}

// factsReaches reports, for each declared type, the declared types a chain of
// one or more edges leads to, by repeated relaxation.
func factsReaches(ids []ir.TypeID, edges map[ir.TypeID][]ir.TypeID) map[ir.TypeID]map[ir.TypeID]bool {
	reach := map[ir.TypeID]map[ir.TypeID]bool{}
	for _, id := range ids {
		reach[id] = map[ir.TypeID]bool{}
		for _, to := range edges[id] {
			reach[id][to] = true
		}
	}
	for changed := true; changed; {
		changed = false
		for _, a := range ids {
			for b := range reach[a] {
				for c := range reach[b] {
					if !reach[a][c] {
						reach[a][c] = true
						changed = true
					}
				}
			}
		}
	}
	return reach
}

func TestFacts_ComponentsAgreeWithReachability(t *testing.T) {
	t.Parallel()
	var cyclic int
	for seed := range uint64(300) {
		rng := rand.New(rand.NewPCG(seed, seed+1))
		doc, ids, edges := factsRandomContainment(rng)
		reach := factsReaches(ids, edges)
		facts := ir.NewFacts(doc)
		for _, a := range ids {
			want := []ir.TypeID{a}
			for _, b := range ids {
				if b != a && reach[a][b] && reach[b][a] {
					want = append(want, b)
				}
			}
			slices.Sort(want)
			require.Equal(t, want, facts.Component(a), "seed %d: Component(%q)", seed, a)
			require.Equal(t, reach[a][a], facts.Recursive(a), "seed %d: Recursive(%q)", seed, a)
			if reach[a][a] {
				cyclic++
			}
		}
	}
	require.Positive(t, cyclic, "no random document held a cycle, so this compared nothing")
}

func TestFacts_ComponentsDoNotDependOnMapOrder(t *testing.T) {
	t.Parallel()
	rng := rand.New(rand.NewPCG(5, 6))
	doc, ids, _ := factsRandomContainment(rng)
	first, second := ir.NewFacts(doc), ir.NewFacts(doc)
	for _, id := range ids {
		assert.Empty(t, cmp.Diff(first.Component(id), second.Component(id)), id)
		assert.Equal(t, first.Recursive(id), second.Recursive(id), id)
		assert.True(t, slices.IsSorted(first.Component(id)), id)
	}
}
