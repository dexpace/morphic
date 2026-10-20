package ir_test

import (
	"fmt"
	"math/rand/v2"
	"slices"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
)

// factsOracleIsSubtype is the walk ir.IsSubtype ran before Facts existed, kept
// as the reference every answer of the index is held to.
func factsOracleIsSubtype(doc *ir.Document, target, base ir.TypeID) bool {
	seen := map[ir.TypeID]bool{}
	queue := []ir.TypeID{target}
	for len(queue) > 0 {
		id := queue[len(queue)-1]
		queue = queue[:len(queue)-1]
		if seen[id] {
			continue
		}
		seen[id] = true
		supers := ir.Supertypes(doc, id)
		if slices.Contains(supers, base) {
			return true
		}
		queue = append(queue, supers...)
	}
	return false
}

// factsRandomHierarchy builds a registry of up to thirty types: models whose
// Base, Implements and Mixins name any type, an undeclared ID or an alias,
// alias scalars in chains and cycles, types that are no model, and one typed
// nil. It returns the document and every ID worth asking about.
func factsRandomHierarchy(rng *rand.Rand) (*ir.Document, []ir.TypeID) {
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
					m.Implements = append(m.Implements, ir.TypeRef{Target: pick()})
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

// factsQuestion is one thing asked of an index: whether target is a subtype of
// base, or, when listing is set, which types are subtypes of base.
type factsQuestion struct {
	target, base ir.TypeID
	listing      bool
}

func TestFacts_AnswersAsTheReferenceWalkDoes(t *testing.T) {
	t.Parallel()
	var positives, cyclic int
	for seed := range uint64(300) {
		rng := rand.New(rand.NewPCG(seed, seed+1))
		doc, universe := factsRandomHierarchy(rng)

		var questions []factsQuestion
		for _, target := range universe {
			for _, base := range universe {
				questions = append(questions, factsQuestion{target: target, base: base})
			}
		}
		for _, base := range universe {
			questions = append(questions, factsQuestion{base: base, listing: true})
		}
		rng.Shuffle(len(questions), func(i, j int) { questions[i], questions[j] = questions[j], questions[i] })

		facts := ir.NewFacts(doc)
		for _, q := range questions {
			if q.listing {
				var want []ir.TypeID
				for id := range doc.Types {
					if factsOracleIsSubtype(doc, id, q.base) {
						want = append(want, id)
					}
				}
				slices.Sort(want)
				require.Equal(t, want, facts.Subtypes(q.base), "seed %d: Subtypes(%q)", seed, q.base)
				continue
			}
			want := factsOracleIsSubtype(doc, q.target, q.base)
			require.Equal(t, want, facts.IsSubtype(q.target, q.base), "seed %d: IsSubtype(%q, %q)", seed, q.target, q.base)
			require.Equal(t, want, ir.IsSubtype(doc, q.target, q.base), "seed %d: ir.IsSubtype(%q, %q)", seed, q.target, q.base)
			if want {
				positives++
			}
			if want && q.target == q.base {
				cyclic++
			}
		}
	}
	require.Positive(t, positives, "no random document held a subtype, so this compared nothing")
	require.Positive(t, cyclic, "no random document held a cycle, so cycles were not compared")
}

func TestFacts_SubtypesIsSortedFreshAndNilWhenEmpty(t *testing.T) {
	t.Parallel()
	doc := compDoc(
		compModel("root", nil),
		compModel("b", func(m *ir.Model) { m.Base = &ir.TypeRef{Target: "root"} }),
		compModel("a", func(m *ir.Model) { m.Implements = refs("root") }),
		compModel("c", func(m *ir.Model) { m.Base = &ir.TypeRef{Target: "a"} }),
	)
	facts := ir.NewFacts(doc)

	got := facts.Subtypes("root")
	assert.Equal(t, []ir.TypeID{"a", "b", "c"}, got)
	got[0] = "clobbered"
	assert.Equal(t, []ir.TypeID{"a", "b", "c"}, facts.Subtypes("root"), "the caller owns the slice it was handed")

	assert.Nil(t, facts.Subtypes("c"), "a leaf has no subtype")
	assert.Nil(t, facts.Subtypes("ghost"), "an undeclared base has none")
}

func TestFacts_NoDocumentMeansNoFacts(t *testing.T) {
	t.Parallel()
	for name, facts := range map[string]*ir.Facts{
		"a nil document": ir.NewFacts(nil),
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

// factsChainHierarchy is the document of GitHub #850: n+1 models chained by
// Base, factsChainID(0) at the top, and n bases, each routing a discriminator
// mapping to factsChainID(n), the bottom of the chain.
func factsChainHierarchy(n int) *ir.Document {
	defs := make([]ir.TypeDef, 0, 2*n+1)
	defs = append(defs, compModel(factsChainID(0), nil))
	for i := 1; i <= n; i++ {
		defs = append(defs, compModel(factsChainID(i), func(m *ir.Model) {
			m.Base = &ir.TypeRef{Target: factsChainID(i - 1)}
		}))
	}
	for k := range n {
		defs = append(defs, compModel(factsBaseID(k), func(m *ir.Model) {
			m.Discriminator = &ir.Discriminator{Mapping: map[string]ir.TypeID{"x": factsChainID(n)}}
		}))
	}
	return compDoc(defs...)
}

func factsChainID(i int) ir.TypeID { return ir.TypeID(fmt.Sprintf("c%05d", i)) }
func factsBaseID(k int) ir.TypeID  { return ir.TypeID(fmt.Sprintf("m%05d", k)) }

// factsChainSteps asks n bases whether the bottom of an n-deep chain is their
// subtype, as pass.Validate does, and returns the types the index expanded.
func factsChainSteps(n int) int {
	facts := ir.NewFacts(factsChainHierarchy(n))
	for k := range n {
		facts.IsSubtype(factsChainID(n), factsBaseID(k))
	}
	return facts.ClosureSteps()
}

func TestFacts_WorkGrowsLinearlyWithTheQuestions(t *testing.T) {
	t.Parallel()
	small, large := factsChainSteps(1000), factsChainSteps(2000)
	require.Positive(t, small, "no walk was counted, so the bound compares nothing")
	assert.LessOrEqual(t, float64(large), 2.2*float64(small),
		"doubling the chain and the questions must roughly double the work: %d steps at 1000, %d at 2000", small, large)
}

func TestFacts_AnswersEveryQuestionAboutOneTargetFromOneWalk(t *testing.T) {
	t.Parallel()
	const n = 300
	facts := ir.NewFacts(factsChainHierarchy(n))

	assert.False(t, facts.IsSubtype(factsChainID(n), factsBaseID(0)))
	once := facts.ClosureSteps()
	require.Equal(t, n+1, once, "the walk expands the target and each type above it, once")

	for k := range n {
		assert.False(t, facts.IsSubtype(factsChainID(n), factsBaseID(k)))
	}
	assert.True(t, facts.IsSubtype(factsChainID(n), factsChainID(0)))
	assert.Equal(t, once, facts.ClosureSteps(), "a later question about the same target walks nothing")

	assert.True(t, facts.IsSubtype(factsChainID(n-1), factsChainID(0)))
	assert.Equal(t, once+n, facts.ClosureSteps(), "a new target walks the hierarchy above it once")
}

// factsAnswers is what an index says about one type.
type factsAnswers struct {
	usage     ir.UsageFlags
	recursive bool
	component []ir.TypeID
	subtypes  []ir.TypeID
	supers    map[ir.TypeID]bool
}

// factsAsk collects everything facts says about id, asking IsSubtype of each
// base in universe.
func factsAsk(facts *ir.Facts, id ir.TypeID, universe []ir.TypeID) factsAnswers {
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

func TestFacts_EveryMethodIsSafeForConcurrentUse(t *testing.T) {
	t.Parallel()
	doc, universe := factsConcurrentDocument()
	sequential := ir.NewFacts(doc)
	want := map[ir.TypeID]factsAnswers{}
	for _, id := range universe {
		want[id] = factsAsk(sequential, id, universe)
	}

	shared := ir.NewFacts(doc)
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
}

// factsConcurrentDocument builds a document in which every fact has something
// to derive: a discriminated hierarchy used by an operation, a cycle, an
// alias, and a type nothing reaches.
func factsConcurrentDocument() (*ir.Document, []ir.TypeID) {
	doc := compDoc(
		compModel("base", func(m *ir.Model) { m.Discriminator = &ir.Discriminator{} }),
		compModel("sub", func(m *ir.Model) { m.Base = &ir.TypeRef{Target: "alias"} }),
		compScalar("alias", "base"),
		compModel("deep", func(m *ir.Model) { m.Implements = refs("sub") }),
		compModel("loopA", func(m *ir.Model) { m.Properties = []ir.Property{{Type: ir.TypeRef{Target: "loopB"}}} }),
		compModel("loopB", func(m *ir.Model) { m.Properties = []ir.Property{{Type: ir.TypeRef{Target: "loopA"}}} }),
		compModel("orphan", nil),
	)
	doc.Services = []ir.Service{{Groups: []ir.OperationGroup{{Operations: []ir.Operation{{
		Params:    []ir.Parameter{{Type: ir.TypeRef{Target: "loopA"}}},
		Responses: []ir.Response{{Payload: &ir.Payload{Contents: []ir.Content{{Type: ir.TypeRef{Target: "base"}}}}}},
	}}}}}}
	return doc, []ir.TypeID{"base", "sub", "alias", "deep", "loopA", "loopB", "orphan", "ghost"}
}
