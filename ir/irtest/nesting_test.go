package irtest_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir/irtest"
)

// TestNestedOverloadDoc_NestsTheDeclarationAsDeepAsAsked pins the fixture's
// shape: the overload sits in the first group, and its target is depth groups
// down the second.
func TestNestedOverloadDoc_NestsTheDeclarationAsDeepAsAsked(t *testing.T) {
	t.Parallel()
	doc, target := irtest.NestedOverloadDoc(3)
	require.Len(t, doc.Services, 1)
	groups := doc.Services[0].Groups
	require.Len(t, groups, 2)
	require.Len(t, groups[0].Operations, 1)
	assert.Equal(t, &target, groups[0].Operations[0].OverloadOf)

	g := groups[1]
	for range 3 {
		require.Len(t, g.Groups, 1)
		g = g.Groups[0]
	}
	require.Len(t, g.Operations, 1)
	assert.Equal(t, target, g.Operations[0].ID)
}
