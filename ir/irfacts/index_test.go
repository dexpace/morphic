package irfacts_test

import (
	"fmt"
	"math/rand/v2"
	"slices"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/ir/irfacts"
)

// factsRandomHierarchy builds a registry of up to thirty types: models whose
// Base, Mixins and, unless forest is set, Implements name any type, an
// undeclared ID or an alias; alias scalars in chains and cycles; types that are
// no model; and one typed nil. With forest set no model has two supertypes, so
// the condensed hierarchy is a forest. It returns the document and every ID
// worth asking about.
func factsRandomHierarchy(rng *rand.Rand, forest bool) (*ir.Document, []ir.TypeID) {
	n := 1 + rng.IntN(30)
	universe := make([]ir.TypeID, 0, n+5)
	for i := range n {
		universe = append(universe, ir.TypeID(fmt.Sprintf("t%02d", i)))
	}
	declared := slices.Clone(universe)
	universe = append(universe, "ghost0", "ghost1", "nilm", "")
	pick := func() ir.TypeID { return universe[rng.IntN(len(universe))] }

	defs := make([]ir.TypeDef, 0, n)
	for _, id := range declared {
		switch r := rng.IntN(10); {
		case r < 6:
			defs = append(defs, compModel(id, func(m *ir.Model) {
				if rng.IntN(2) == 0 {
					m.Base = &ir.TypeRef{Target: pick()}
				}
				for range rng.IntN(3) {
					if !forest {
						m.Implements = append(m.Implements, ir.TypeRef{Target: pick()})
					}
				}
				if rng.IntN(2) == 0 {
					m.Mixins = refs(pick())
				}
			}))
		case r < 8:
			defs = append(defs, compScalar(id, pick()))
		case r == 8:
			defs = append(defs, compScalar(id, ""))
		default:
			defs = append(defs, &ir.Enum{ID: id})
		}
	}
	doc := compDoc(defs...)
	doc.Types["nilm"] = (*ir.Model)(nil)
	return doc, universe
}

// factsTally counts how the hierarchy index answered the questions of
// TestIndex_AnswersAsTheReferenceWalkDoes.
type factsTally struct {
	positive, cyclic int // the walk said yes; and of those, target == base
	settled, walked  int // the index answered alone; or left the pair to the walk
	walkedNo         int // the walk said no to a pair the index could not rule out
}

// factsCompare asks facts every pair of universe, and which subtypes each base
// has, and holds each answer to the walk ir.IsSubtype runs.
func factsCompare(t *testing.T, seed uint64, doc *ir.Document, universe []ir.TypeID, tally *factsTally) {
	t.Helper()
	facts := irfacts.New(doc)
	for _, base := range universe {
		var want []ir.TypeID
		for _, target := range universe {
			walk := ir.IsSubtype(doc, target, base)
			require.Equal(t, walk, facts.IsSubtype(target, base), "seed %d: IsSubtype(%q, %q)", seed, target, base)
			sub, settled := facts.SubtypeIndexAnswer(target, base)
			if settled {
				require.Equal(t, walk, sub, "seed %d: the index settled IsSubtype(%q, %q) wrongly", seed, target, base)
				tally.settled++
			} else {
				tally.walked++
				if !walk {
					tally.walkedNo++
				}
			}
			if _, declared := doc.Types[target]; walk && declared {
				want = append(want, target)
			}
			if walk {
				tally.positive++
				if target == base {
					tally.cyclic++
				}
			}
		}
		slices.Sort(want)
		require.Equal(t, want, facts.Subtypes(base), "seed %d: Subtypes(%q)", seed, base)
	}
}

func TestIndex_AnswersAsTheReferenceWalkDoes(t *testing.T) {
	t.Parallel()
	for name, forest := range map[string]bool{"a forest": true, "with Implements": false} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			var tally factsTally
			for seed := range uint64(300) {
				doc, universe := factsRandomHierarchy(rand.New(rand.NewPCG(seed, seed+1)), forest)
				factsCompare(t, seed, doc, universe, &tally)
			}
			require.Positive(t, tally.positive, "no random document held a subtype, so this compared nothing")
			require.Positive(t, tally.cyclic, "no random document held a cycle, so cycles were not compared")
			require.Positive(t, tally.settled, "the index answered nothing alone")
			if forest {
				assert.Zero(t, tally.walked, "in a forest the index settles every pair")
				return
			}
			assert.Positive(t, tally.walkedNo, "no pair the index could not rule out was unrelated, so the walk it falls back to was not shown needed")
		})
	}
}

// factsRandomAliases builds a registry of up to twenty types, each an alias
// scalar naming another, an undeclared ID or the empty ID, a scalar with no
// Base, a typed nil or a model, so chains, cycles and tails into cycles all
// occur. The empty ID is sometimes declared as an alias itself.
func factsRandomAliases(rng *rand.Rand) (*ir.Document, []ir.TypeID) {
	n := 1 + rng.IntN(20)
	universe := make([]ir.TypeID, 0, n+2)
	universe = append(universe, "ghost", "")
	for i := range n {
		universe = append(universe, ir.TypeID(fmt.Sprintf("a%02d", i)))
	}
	pick := func() ir.TypeID { return universe[rng.IntN(len(universe))] }
	doc := compDoc()
	for _, id := range universe[2:] {
		switch r := rng.IntN(10); {
		case r < 7:
			doc.Types[id] = &ir.Scalar{ID: id, Base: &ir.TypeRef{Target: pick()}} // the empty target among them
		case r == 7:
			doc.Types[id] = &ir.Scalar{ID: id}
		case r == 8:
			doc.Types[id] = (*ir.Scalar)(nil)
		default:
			doc.Types[id] = compModel(id, nil)
		}
	}
	if rng.IntN(3) == 0 {
		doc.Types[""] = &ir.Scalar{Base: &ir.TypeRef{Target: pick()}}
	}
	return doc, universe
}

// aliasRead returns what ir.Supertypes reads the reference id as, by asking it
// of a model whose Base is id: the type id's alias scalars stand for, or ""
// when that names no type.
func aliasRead(doc *ir.Document, id ir.TypeID) ir.TypeID {
	const probe = "probe/model"
	doc.Types[probe] = compModel(probe, func(m *ir.Model) { m.Base = &ir.TypeRef{Target: id} })
	defer delete(doc.Types, probe)
	if supers := ir.Supertypes(doc, probe); len(supers) == 1 {
		return supers[0]
	}
	return ""
}

func TestIndex_AliasIndexAnswersAsTheOneQuestionReadingDoes(t *testing.T) {
	t.Parallel()
	var cycles int
	for seed := range uint64(500) {
		doc, universe := factsRandomAliases(rand.New(rand.NewPCG(seed, seed+3)))
		index := irfacts.AliasTargetsOf(doc.Types)
		for _, id := range universe {
			want := aliasRead(doc, id)
			got, indexed := index[id]
			if !indexed {
				got = id
			}
			require.Equal(t, want, got, "seed %d: the alias %q", seed, id)
			if s, ok := doc.Types[want].(*ir.Scalar); ok && s != nil && s.Base != nil {
				cycles++ // a reading that ends on an alias ended on a cycle.
			}
		}
	}
	require.Positive(t, cycles, "no random registry held an alias cycle, so cycles were not compared")
}

func TestIndex_SubtypesIsSortedFreshAndNilWhenEmpty(t *testing.T) {
	t.Parallel()
	doc := compDoc(
		compModel("root", nil),
		compModel("b", func(m *ir.Model) { m.Base = &ir.TypeRef{Target: "root"} }),
		compModel("a", func(m *ir.Model) { m.Implements = refs("root") }),
		compModel("c", func(m *ir.Model) { m.Base = &ir.TypeRef{Target: "a"} }),
	)
	facts := irfacts.New(doc)

	got := facts.Subtypes("root")
	assert.Equal(t, []ir.TypeID{"a", "b", "c"}, got)
	got[0] = "clobbered"
	assert.Equal(t, []ir.TypeID{"a", "b", "c"}, facts.Subtypes("root"), "the caller owns the slice it was handed")

	assert.Nil(t, facts.Subtypes("c"), "a leaf has no subtype")
	assert.Nil(t, facts.Subtypes("ghost"), "an undeclared base has none")
}

func TestIndex_NoDocumentMeansNoFacts(t *testing.T) {
	t.Parallel()
	for name, facts := range map[string]*irfacts.Index{
		"a nil document": irfacts.New(nil),
		"the zero value": {},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assert.False(t, facts.IsSubtype("a", "b"))
			assert.Nil(t, facts.Subtypes("a"))
			assert.Zero(t, facts.Usage("a"))
			assert.False(t, facts.Recursive("a"))
			assert.Nil(t, facts.Component("a"))
		})
	}
}

func factsChainID(i int) ir.TypeID { return ir.TypeID(fmt.Sprintf("c%06d", i)) }
func factsBaseID(k int) ir.TypeID  { return ir.TypeID(fmt.Sprintf("m%06d", k)) }

// factsChain returns n+1 models chained by Base, factsChainID(0) at the top,
// each shaped by mutate.
func factsChain(n int, mutate func(i int, m *ir.Model)) []ir.TypeDef {
	defs := make([]ir.TypeDef, 0, n+1)
	for i := 0; i <= n; i++ {
		defs = append(defs, compModel(factsChainID(i), func(m *ir.Model) {
			if i > 0 {
				m.Base = &ir.TypeRef{Target: factsChainID(i - 1)}
			}
			if mutate != nil {
				mutate(i, m)
			}
		}))
	}
	return defs
}

// factsWorkShape is a document of size n and the questions asked of it, which
// return how many came out true.
type factsWorkShape struct {
	doc func(n int) *ir.Document
	ask func(f *irfacts.Index, n int) int
}

// factsWorkShapes are the documents whose subtype or usage questions have cost
// more than linear time or memory: GitHub #850's, a chain each of whose models
// routes to the next (a closure per target is quadratic there), a base routing
// to every model below it, and usage on a chain of discriminated bases.
func factsWorkShapes() map[string]factsWorkShape {
	return map[string]factsWorkShape{
		"many bases routing to the bottom of one chain": {
			doc: func(n int) *ir.Document {
				defs := factsChain(n, nil)
				for k := range n {
					defs = append(defs, compModel(factsBaseID(k), nil))
				}
				return compDoc(defs...)
			},
			ask: func(f *irfacts.Index, n int) (yes int) {
				for k := range n {
					yes += factsCount(f.IsSubtype(factsChainID(n), factsBaseID(k)))
				}
				return yes
			},
		},
		"each model of a chain routing to the next": {
			doc: func(n int) *ir.Document { return compDoc(factsChain(n, nil)...) },
			ask: func(f *irfacts.Index, n int) (yes int) {
				for i := range n {
					yes += factsCount(f.IsSubtype(factsChainID(i+1), factsChainID(i)))
				}
				return yes
			},
		},
		"the top of a chain routing to every model below it": {
			doc: func(n int) *ir.Document { return compDoc(factsChain(n, nil)...) },
			ask: func(f *irfacts.Index, n int) (yes int) {
				for i := 1; i <= n; i++ {
					yes += factsCount(f.IsSubtype(factsChainID(i), factsChainID(0)))
				}
				return yes
			},
		},
		"usage of a chain of discriminated bases": {
			doc: func(n int) *ir.Document {
				defs := factsChain(n, func(_ int, m *ir.Model) { m.Discriminator = &ir.Discriminator{PropertyName: "k"} })
				return useDoc(ir.Operation{Responses: []ir.Response{{Payload: useBody("application/json", factsChainID(0))}}}, defs...)
			},
			ask: func(f *irfacts.Index, n int) (yes int) {
				for i := 0; i <= n; i++ {
					yes += factsCount(f.Usage(factsChainID(i)) == irfacts.UsageOutput)
				}
				return yes - 1
			},
		},
	}
}

func factsCount(b bool) int {
	if b {
		return 1
	}
	return 0
}

// TestIndex_WorkGrowsLinearly counts the allocations of building a Facts and
// asking a shape's questions at three sizes, and holds the growth from 2n to 4n
// to at most 2.5 times the growth from n to 2n: fixed costs cancel, linear work
// gives 2 and quadratic 4. It does not run in parallel, since the count is
// process-wide.
func TestIndex_WorkGrowsLinearly(t *testing.T) {
	for name, shape := range factsWorkShapes() {
		t.Run(name, func(t *testing.T) {
			var allocs [3]float64
			for k, n := range []int{400, 800, 1600} {
				doc := shape.doc(n)
				var yes int
				allocs[k] = testing.AllocsPerRun(1, func() { yes = shape.ask(irfacts.New(doc), n) })
				require.Contains(t, []int{0, n}, yes, "the questions must all have one answer, or the sizes compare different work")
			}
			assert.LessOrEqual(t, allocs[2]-allocs[1], 2.5*(allocs[1]-allocs[0]),
				"allocations at 400, 800, 1600: %v", allocs)
		})
	}
}

func TestIndex_AForestNeedsNoWalk(t *testing.T) {
	t.Parallel()
	doc := factsWorkShapes()["many bases routing to the bottom of one chain"].doc(300)
	facts := irfacts.New(doc)
	for k := range 300 {
		_, settled := facts.SubtypeIndexAnswer(factsChainID(300), factsBaseID(k))
		assert.True(t, settled, "an unrelated base")
	}
	for i := 1; i <= 300; i++ {
		sub, settled := facts.SubtypeIndexAnswer(factsChainID(i), factsChainID(0))
		assert.True(t, sub && settled, "a model below the top of the chain")
	}
}

func TestIndex_ASupertypeNamedTwiceIsOneParent(t *testing.T) {
	t.Parallel()
	for name, doc := range map[string]*ir.Document{
		"by Base and Implements": compDoc(compModel("top", nil), compModel("both", func(m *ir.Model) {
			m.Base = &ir.TypeRef{Target: "top"}
			m.Implements = refs("top")
		})),
		"directly and through an alias": compDoc(compModel("top", nil), compScalar("topAlias", "top"), compModel("both", func(m *ir.Model) {
			m.Base = &ir.TypeRef{Target: "topAlias"}
			m.Implements = refs("top")
		})),
		"by two members of one cycle": compDoc(compModel("top", nil),
			compModel("a", func(m *ir.Model) { m.Base = &ir.TypeRef{Target: "b"}; m.Implements = refs("top") }),
			compModel("b", func(m *ir.Model) { m.Base = &ir.TypeRef{Target: "a"}; m.Implements = refs("top") })),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			facts := irfacts.New(doc)
			for _, sub := range []ir.TypeID{"both", "a", "b"} {
				if _, declared := doc.Types[sub]; !declared {
					continue
				}
				got, settled := facts.SubtypeIndexAnswer(sub, "top")
				assert.True(t, got && settled, "%s has one parent, so the hierarchy is still a forest", sub)
			}
		})
	}
}

// factsAnswers is what an index says about one type.
type factsAnswers struct {
	usage     irfacts.UsageFlags
	recursive bool
	component []ir.TypeID
	subtypes  []ir.TypeID
	supers    map[ir.TypeID]bool
}

// factsAsk collects everything facts says about id, asking IsSubtype of each
// base in universe.
func factsAsk(facts *irfacts.Index, id ir.TypeID, universe []ir.TypeID) factsAnswers {
	got := factsAnswers{
		usage:     facts.Usage(id),
		recursive: facts.Recursive(id),
		component: facts.Component(id),
		subtypes:  facts.Subtypes(id),
		supers:    map[ir.TypeID]bool{},
	}
	for _, base := range universe {
		got.supers[base] = facts.IsSubtype(id, base)
	}
	return got
}

func TestIndex_EveryMethodIsSafeForConcurrentUse(t *testing.T) {
	t.Parallel()
	doc, universe := factsConcurrentDocument()
	sequential := irfacts.New(doc)
	want := map[ir.TypeID]factsAnswers{}
	for _, id := range universe {
		want[id] = factsAsk(sequential, id, universe)
	}

	shared := irfacts.New(doc)
	var wg sync.WaitGroup
	for worker := range 16 {
		wg.Go(func() {
			ids := slices.Clone(universe)
			rand.New(rand.NewPCG(uint64(worker), 7)).Shuffle(len(ids), func(i, j int) { ids[i], ids[j] = ids[j], ids[i] })
			for _, id := range ids {
				assert.Equal(t, want[id], factsAsk(shared, id, universe), "answers about %q", id)
			}
		})
	}
	wg.Wait()
	assert.NotZero(t, want["base"].usage, "the document gives the index usage to derive")
	assert.True(t, want["loopA"].recursive, "the document gives the index a cycle to find")
	assert.True(t, want["deep"].supers["mid"] && !want["deep"].supers["orphan"], "the document gives the index a hierarchy to walk")
}

// factsConcurrentDocument builds a document in which every fact has something
// to derive: a discriminated hierarchy used by an operation, with a model of
// two supertypes so that some questions walk; a cycle; an alias; and a type
// nothing reaches.
func factsConcurrentDocument() (*ir.Document, []ir.TypeID) {
	doc := compDoc(
		compModel("base", func(m *ir.Model) { m.Discriminator = &ir.Discriminator{} }),
		compModel("sub", func(m *ir.Model) { m.Base = &ir.TypeRef{Target: "alias"} }),
		compScalar("alias", "base"),
		compModel("mid", func(m *ir.Model) { m.Base = &ir.TypeRef{Target: "sub"} }),
		compModel("deep", func(m *ir.Model) { m.Base = &ir.TypeRef{Target: "mid"}; m.Implements = refs("sub") }),
		compModel("loopA", func(m *ir.Model) { m.Properties = []ir.Property{{Type: ir.TypeRef{Target: "loopB"}}} }),
		compModel("loopB", func(m *ir.Model) { m.Properties = []ir.Property{{Type: ir.TypeRef{Target: "loopA"}}} }),
		compModel("orphan", nil),
	)
	doc.Services = []ir.Service{{Groups: []ir.OperationGroup{{Operations: []ir.Operation{{
		Params:    []ir.Parameter{{Type: ir.TypeRef{Target: "loopA"}}},
		Responses: []ir.Response{{Payload: &ir.Payload{Contents: []ir.Content{{Type: ir.TypeRef{Target: "base"}}}}}},
	}}}}}}
	return doc, []ir.TypeID{"base", "sub", "alias", "mid", "deep", "loopA", "loopB", "orphan", "ghost"}
}
