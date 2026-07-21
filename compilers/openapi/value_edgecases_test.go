package openapi

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	yaml "gopkg.in/yaml.v3"

	"github.com/dexpace/morphic/ir"
)

func scalarNode(tag, val string) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: tag, Value: val}
}

func TestValueFromNode_NilYieldsNull(t *testing.T) {
	t.Parallel()
	got, err := valueFromNode(nil)
	require.NoError(t, err)
	assert.Equal(t, ir.ValueNull, got.Kind)
}

func TestValueFromNode_AliasFollowed(t *testing.T) {
	t.Parallel()
	target := scalarNode("!!str", "hi")
	alias := &yaml.Node{Kind: yaml.AliasNode, Alias: target}
	got, err := valueFromNode(alias)
	require.NoError(t, err)
	assert.Equal(t, ir.ValueString, got.Kind)
	assert.Equal(t, "hi", got.Str)
}

func TestValueFromNode_Sequence(t *testing.T) {
	t.Parallel()
	seq := &yaml.Node{Kind: yaml.SequenceNode, Content: []*yaml.Node{
		scalarNode("!!int", "1"), scalarNode("!!str", "x"),
	}}
	got, err := valueFromNode(seq)
	require.NoError(t, err)
	require.Equal(t, ir.ValueList, got.Kind)
	require.Len(t, got.List, 2)
	assert.Equal(t, ir.BigVal("1"), got.List[0].Num)
	assert.Equal(t, "x", got.List[1].Str)
}

func TestValueFromNode_Binary(t *testing.T) {
	t.Parallel()
	got, err := valueFromNode(yamlNode(t, "!!binary aGVsbG8="))
	require.NoError(t, err)
	require.Equal(t, ir.ValueBytes, got.Kind)
	assert.Equal(t, []byte("hello"), got.Bytes)
}

func TestValueFromNode_ScalarErrors(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		node *yaml.Node
	}{
		{"bad bool", scalarNode("!!bool", "notabool")},
		{"bad int", scalarNode("!!int", "12abc")},
		{"bad float", scalarNode("!!float", "1.2.3")},
		{"bad binary", scalarNode("!!binary", "@@@not-base64")},
		{"unsupported tag", scalarNode("!!timestamp", "2020-01-01")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := valueFromNode(tc.node)
			require.Error(t, err)
		})
	}
}

func TestValueFromNode_OverflowNumberIsNumber(t *testing.T) {
	t.Parallel()
	// A float64-overflow literal resolves to a plain !!str node; it must be
	// captured as the number it is, canonicalized, not as a string.
	got, err := valueFromNode(scalarNode("!!str", "1.8e308"))
	require.NoError(t, err)
	assert.Equal(t, ir.ValueNumber, got.Kind)
	assert.Equal(t, ir.BigVal("1.8e308"), got.Num)
}

func TestValueFromNode_QuotedNumericStaysString(t *testing.T) {
	t.Parallel()
	// A quoted numeric string is not plain, so it stays a string.
	node := scalarNode("!!str", "123")
	node.Style = yaml.DoubleQuotedStyle
	got, err := valueFromNode(node)
	require.NoError(t, err)
	assert.Equal(t, ir.ValueString, got.Kind)
	assert.Equal(t, "123", got.Str)
}

func TestValueFromNode_UnsupportedNodeKind(t *testing.T) {
	t.Parallel()
	_, err := valueFromNode(&yaml.Node{Kind: yaml.Kind(99)})
	require.Error(t, err)
}

func TestValueFromNode_SequenceChildError(t *testing.T) {
	t.Parallel()
	seq := &yaml.Node{Kind: yaml.SequenceNode, Content: []*yaml.Node{
		scalarNode("!!timestamp", "x"),
	}}
	_, err := valueFromNode(seq)
	require.Error(t, err)
}

func TestValueFromNode_MappingValueError(t *testing.T) {
	t.Parallel()
	m := &yaml.Node{Kind: yaml.MappingNode, Content: []*yaml.Node{
		scalarNode("!!str", "k"), scalarNode("!!timestamp", "x"),
	}}
	_, err := valueFromNode(m)
	require.Error(t, err)
}

func TestValueFromNode_DepthCapExceeded(t *testing.T) {
	t.Parallel()
	n := scalarNode("!!int", "1")
	for range maxValueDepth + 2 {
		n = &yaml.Node{Kind: yaml.SequenceNode, Content: []*yaml.Node{n}}
	}
	_, err := valueFromNode(n)
	require.Error(t, err)
}
