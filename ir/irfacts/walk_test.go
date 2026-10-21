package irfacts_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/ir/irfacts"
)

// walkChain is a chain of n+1 models, each declaring one property, beside
// extra models nothing relates to it.
func walkChain(n, extra int) *ir.Document {
	defs := factsChain(n, func(i int, m *ir.Model) { m.Properties = []ir.Property{{ID: factsPropID(i)}} })
	for k := range extra {
		defs = append(defs, compModel(factsBaseID(k), nil))
	}
	return compDoc(defs...)
}

func TestIndex_AFewCheapQuestionsAreWalked(t *testing.T) {
	t.Parallel()
	facts := irfacts.New(walkChain(3, 1000))
	assert.True(t, facts.IsSubtype(factsChainID(3), factsChainID(0)))
	assert.False(t, facts.IsSubtype(factsChainID(0), factsChainID(3)))
	assert.True(t, facts.Exposes(factsChainID(3), factsPropID(0)))
	assert.False(t, facts.Exposes(factsChainID(0), factsPropID(3)))

	hierarchy, exposure := facts.Indexed()
	assert.False(t, hierarchy || exposure, "questions this cheap cost less than indexing a registry of a thousand types")
	subtype, exposed := facts.WalkStepsLeft()
	assert.Less(t, subtype, int64(1004), "a walked subtype question spends steps")
	assert.Less(t, exposed, int64(1004), "a walked exposure question spends steps")
}

func TestIndex_WalksPastTheRegistrysSizeGiveWayToTheIndex(t *testing.T) {
	t.Parallel()
	const n = 200
	facts := irfacts.New(walkChain(n, 0))
	for i := 1; i <= n; i++ {
		require.True(t, facts.IsSubtype(factsChainID(i), factsChainID(0)), "%s below the top", factsChainID(i))
		require.True(t, facts.Exposes(factsChainID(i), factsPropID(0)), "%s exposes the top's property", factsChainID(i))
	}
	hierarchy, exposure := facts.Indexed()
	assert.True(t, hierarchy && exposure, "walking the chain from every model costs more than indexing it")
	subtype, exposed := facts.WalkStepsLeft()
	assert.Zero(t, subtype, "subtype questions read the index now")
	assert.Zero(t, exposed, "exposure questions read the index now")
}

func TestIndex_AnIndexBuiltForAnyReasonEndsTheWalking(t *testing.T) {
	t.Parallel()
	facts := irfacts.New(walkChain(3, 0))
	facts.Usage(factsChainID(0))
	hierarchy, exposure := facts.Indexed()
	require.True(t, hierarchy && !exposure, "usage indexes the hierarchy and nothing else")
	subtype, exposed := facts.WalkStepsLeft()
	assert.Zero(t, subtype, "subtype questions read the hierarchy usage indexed")
	assert.Positive(t, exposed, "exposure questions still walk")
	assert.True(t, facts.IsSubtype(factsChainID(3), factsChainID(0)))

	// No fact but Exposes builds the exposure index yet; the index's own answer
	// does, as a fact that needs it would.
	_, _ = facts.ExposesIndexAnswer(factsChainID(3), factsPropID(0))
	_, exposed = facts.WalkStepsLeft()
	assert.Zero(t, exposed, "exposure questions read the index once it is built")
}
