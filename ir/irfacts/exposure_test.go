// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package irfacts_test

import (
	"fmt"
	"math/rand/v2"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/ir/irfacts"
	"github.com/dexpace/morphic/ir/irtest"
)

// exposureRandom builds a registry of up to thirty types: models declaring up
// to two properties and naming any type, an undeclared ID or an alias as Base,
// Implements and Mixins; alias scalars in chains and cycles; scalars with no
// Base; types that are no model; and one typed nil. With forest set a model
// names at most one part source and declares properties no other declares, so
// the relation is a forest; otherwise properties are drawn from a shared pool,
// so some are declared twice. It returns the document, every type ID and every
// property ID worth asking about.
func exposureRandom(rng *rand.Rand, forest bool) (*ir.Document, []ir.TypeID, []ir.PropertyID) {
	n := 1 + rng.IntN(30)
	types := make([]ir.TypeID, 0, n+4)
	for i := range n {
		types = append(types, ir.TypeID(fmt.Sprintf("t%02d", i)))
	}
	declared := types[:n:n]
	types = append(types, "ghost", "nilm", "")
	pick := func() ir.TypeID { return types[rng.IntN(len(types))] }
	props := []ir.PropertyID{"property/ghost", ""}
	doc := irtest.DocOf()
	for _, id := range declared {
		switch r := rng.IntN(10); {
		case r < 6:
			m := irtest.ModelOf(id, nil)
			for j := range rng.IntN(3) {
				prop := ir.PropertyID(fmt.Sprintf("property/%s/%d", id, j))
				if !forest {
					prop = ir.PropertyID(fmt.Sprintf("property/shared%d", rng.IntN(6)))
				}
				m.Properties = append(m.Properties, ir.Property{ID: prop})
				props = append(props, prop)
			}
			exposureSources(rng, m, pick, forest)
			doc.Types[id] = m
		case r < 8:
			doc.Types[id] = irtest.ScalarOf(id, pick())
		case r == 8:
			doc.Types[id] = &ir.Scalar{ID: id}
		default:
			doc.Types[id] = &ir.Enum{ID: id}
		}
	}
	doc.Types["nilm"] = (*ir.Model)(nil)
	if rng.IntN(4) == 0 {
		doc.Types[""] = irtest.ModelOf("", func(m *ir.Model) { m.Properties = []ir.Property{{ID: "property/empty"}} })
		props = append(props, "property/empty")
	}
	return doc, types, props
}

// exposureSources gives m its part sources: with forest set at most one, as
// Base, an Implements entry or a mixin; otherwise any number of each.
func exposureSources(rng *rand.Rand, m *ir.Model, pick func() ir.TypeID, forest bool) {
	if forest {
		switch rng.IntN(4) {
		case 0:
			m.Base = &ir.TypeRef{Target: pick()}
		case 1:
			m.Implements = irtest.RefsTo(pick())
		case 2:
			m.Mixins = irtest.RefsTo(pick())
		default:
			// A model of its own properties alone.
		}
		return
	}
	if rng.IntN(2) == 0 {
		m.Base = &ir.TypeRef{Target: pick()}
	}
	for range rng.IntN(2) {
		m.Implements = append(m.Implements, ir.TypeRef{Target: pick()})
	}
	for range rng.IntN(3) {
		m.Mixins = append(m.Mixins, ir.TypeRef{Target: pick()})
	}
}

func TestIndex_ExposesAsTheReferenceWalkDoes(t *testing.T) {
	t.Parallel()
	for name, forest := range map[string]bool{"a forest": true, "with shared sources and properties": false} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			var positive, settled, walked, walkedNo int
			for seed := range uint64(300) {
				doc, types, props := exposureRandom(rand.New(rand.NewPCG(seed, seed+7)), forest)
				facts := irfacts.New(doc)
				for _, id := range types {
					want := ir.ExposedProperties(doc, id)
					for _, prop := range props {
						require.Equal(t, want[prop], facts.Exposes(id, prop), "seed %d: Exposes(%q, %q)", seed, id, prop)
						exposed, ok := facts.ExposesIndexAnswer(id, prop)
						switch {
						case ok:
							require.Equal(t, want[prop], exposed, "seed %d: the index settled Exposes(%q, %q) wrongly", seed, id, prop)
							settled++
						case !want[prop]:
							walked++
							walkedNo++
						default:
							walked++
						}
						if want[prop] {
							positive++
						}
					}
				}
			}
			require.Positive(t, positive, "no random document exposed a property, so this compared nothing")
			require.Positive(t, settled, "the index answered nothing alone")
			if forest {
				assert.Zero(t, walked, "in a forest of properties declared once the index settles every pair")
				return
			}
			assert.Positive(t, walkedNo, "no pair the index could not rule out went unexposed, so the walk it falls back to was not shown needed")
		})
	}
}

func TestIndex_ExposesWhatCompositionAndAliasesLeadTo(t *testing.T) {
	t.Parallel()
	doc := irtest.DocOf(
		irtest.ModelOf("top", func(m *ir.Model) { m.Properties = []ir.Property{{ID: "property/top"}} }),
		irtest.ModelOf("mixin", func(m *ir.Model) { m.Properties = []ir.Property{{ID: "property/mixin"}} }),
		irtest.ScalarOf("topAlias", "top"),
		irtest.ModelOf("mid", func(m *ir.Model) {
			m.Base = &ir.TypeRef{Target: "topAlias"}
			m.Mixins = irtest.RefsTo("mixin")
			m.Properties = []ir.Property{{ID: "property/mid"}}
		}),
		irtest.ModelOf("leaf", func(m *ir.Model) { m.Implements = irtest.RefsTo("mid") }),
		irtest.ScalarOf("leafAlias", "leaf"),
		irtest.ModelOf("other", func(m *ir.Model) { m.Properties = []ir.Property{{ID: "property/other"}} }),
	)
	facts := irfacts.New(doc)
	for _, root := range []ir.TypeID{"leaf", "leafAlias"} {
		for _, prop := range []ir.PropertyID{"property/top", "property/mixin", "property/mid"} {
			assert.True(t, facts.Exposes(root, prop), "%s exposes %s", root, prop)
		}
		assert.False(t, facts.Exposes(root, "property/other"), "%s composes nothing of other", root)
	}
	assert.False(t, facts.Exposes("top", "property/mid"), "a base exposes nothing of what composes it")
	assert.False(t, facts.Exposes("ghost", "property/top"), "an undeclared type exposes nothing")
	assert.False(t, facts.Exposes("leaf", "property/ghost"), "no type exposes a property nothing declares")
}

func TestIndex_ExposesAChainsPropertiesWithoutWalking(t *testing.T) {
	t.Parallel()
	const n = 300
	doc := irtest.DocOf(factsChain(n, func(i int, m *ir.Model) {
		m.Properties = []ir.Property{{ID: factsPropID(i)}}
		m.Discriminator = &ir.Discriminator{Property: factsPropID(0)}
	})...)
	facts := irfacts.New(doc)
	for i := 0; i <= n; i++ {
		exposed, settled := facts.ExposesIndexAnswer(factsChainID(i), factsPropID(0))
		assert.True(t, exposed && settled, "%s exposes the top's tag", factsChainID(i))
		exposed, settled = facts.ExposesIndexAnswer(factsChainID(n), factsPropID(i))
		assert.True(t, exposed && settled, "the bottom exposes %s", factsPropID(i))
		exposed, settled = facts.ExposesIndexAnswer(factsChainID(0), factsPropID(i))
		assert.Equal(t, i == 0, exposed, "the top exposes only its own")
		assert.True(t, settled)
	}
}

func factsPropID(i int) ir.PropertyID { return ir.PropertyID(fmt.Sprintf("property/c%06d", i)) }
