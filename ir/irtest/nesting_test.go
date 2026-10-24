package irtest_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
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

// TestNestedListValue_WrapsANumberInDepthLists pins the depth: each level is a
// one-element list, and the number sits at the bottom.
func TestNestedListValue_WrapsANumberInDepthLists(t *testing.T) {
	t.Parallel()
	v := irtest.NestedListValue(3)
	for range 3 {
		require.Equal(t, ir.ValueList, v.Kind)
		require.Len(t, v.List, 1)
		v = v.List[0]
	}
	assert.Equal(t, ir.Value{Kind: ir.ValueNumber, Num: ir.BigVal("0")}, v)
}
