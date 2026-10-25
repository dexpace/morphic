package value_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	yaml "gopkg.in/yaml.v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/nodeview"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/compilers/openapi/internal/value"
	"github.com/dexpace/morphic/compilers/openapi/internal/ynode"
	"github.com/dexpace/morphic/ir"
)

func TestValueFromNode_Scalars(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name, src string
		want      ir.Value
	}{
		{"null", "null", ir.Value{Kind: ir.ValueNull}},
		{"bool", "true", ir.Value{Kind: ir.ValueBool, Bool: true}},
		{"string", `"hi"`, ir.Value{Kind: ir.ValueString, Str: "hi"}},
		{"int precision", "9007199254740993", ir.Value{Kind: ir.ValueNumber, Num: ir.BigVal("9007199254740993")}},
		{"big decimal", "0.30000000000000004", ir.Value{Kind: ir.ValueNumber, Num: ir.BigVal("0.30000000000000004")}},
		{"past a binary float's range", "1e2000000000", ir.Value{Kind: ir.ValueNumber, Num: ir.BigVal("1e2000000000")}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := value.FromNode(openapitest.YAMLNode(t, tc.src))
			require.NoError(t, err)
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestValueFromNode_ObjectPreservesOrder(t *testing.T) {
	t.Parallel()
	got, err := value.FromNode(openapitest.YAMLNode(t, "b: 1\na: 2\n"))
	require.NoError(t, err)
	require.Equal(t, ir.ValueObject, got.Kind)
	require.Len(t, got.Object, 2)
	require.Equal(t, "b", got.Object[0].Name)
	require.Equal(t, "a", got.Object[1].Name)
}

func TestValueFromNode_NilYieldsNull(t *testing.T) {
	t.Parallel()
	got, err := value.FromNode(nil)
	require.NoError(t, err)
	assert.Equal(t, ir.ValueNull, got.Kind)
}

func TestValueFromNode_AliasFollowed(t *testing.T) {
	t.Parallel()
	target := openapitest.ScalarNode("!!str", "hi")
	alias := &yaml.Node{Kind: yaml.AliasNode, Alias: target}
	got, err := value.FromNode(alias)
	require.NoError(t, err)
	assert.Equal(t, ir.ValueString, got.Kind)
	assert.Equal(t, "hi", got.Str)
}

func TestValueFromNode_Sequence(t *testing.T) {
	t.Parallel()
	seq := &yaml.Node{Kind: yaml.SequenceNode, Content: []*yaml.Node{
		openapitest.ScalarNode("!!int", "1"), openapitest.ScalarNode("!!str", "x"),
	}}
	got, err := value.FromNode(seq)
	require.NoError(t, err)
	require.Equal(t, ir.ValueList, got.Kind)
	require.Len(t, got.List, 2)
	assert.Equal(t, ir.BigVal("1"), got.List[0].Num)
	assert.Equal(t, "x", got.List[1].Str)
}

func TestValueFromNode_Binary(t *testing.T) {
	t.Parallel()
	got, err := value.FromNode(openapitest.YAMLNode(t, "!!binary aGVsbG8="))
	require.NoError(t, err)
	require.Equal(t, ir.ValueBytes, got.Kind)
	assert.Equal(t, []byte("hello"), got.Bytes)
}

func TestValueFromNode_Timestamp(t *testing.T) {
	t.Parallel()
	// YAML 1.1 resolves each of these plain scalars to !!timestamp; the exact
	// source spelling must survive as a string, not get parsed into a Go
	// time.Time or dropped.
	cases := []string{"2021-01-01", "2021-01-01T00:00:00Z", "2021-1-1"}
	for _, src := range cases {
		t.Run(src, func(t *testing.T) {
			t.Parallel()
			node := openapitest.YAMLNode(t, src)
			require.Equal(t, "!!timestamp", node.Tag, "precondition: this spelling must resolve to !!timestamp")
			got, err := value.FromNode(node)
			require.NoError(t, err)
			assert.Equal(t, ir.ValueString, got.Kind)
			assert.Equal(t, src, got.Str, "verbatim spelling survives, not a canonicalized or parsed form")
		})
	}
}

func TestValueFromNode_ScalarErrors(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		node *yaml.Node
	}{
		{"bad bool", openapitest.ScalarNode("!!bool", "notabool")},
		{"bad int", openapitest.ScalarNode("!!int", "12abc")},
		{"bad float", openapitest.ScalarNode("!!float", "1.2.3")},
		{"bad binary", openapitest.ScalarNode("!!binary", "@@@not-base64")},
		{"unsupported tag", openapitest.ScalarNode("!custom", "2020-01-01")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := value.FromNode(tc.node)
			require.Error(t, err)
		})
	}
}

func TestValueFromNode_OverflowNumberIsNumber(t *testing.T) {
	t.Parallel()
	// A float64-overflow literal resolves to a plain !!str node; it must be
	// captured as the number it is, canonicalized, not as a string.
	got, err := value.FromNode(openapitest.ScalarNode("!!str", "1.8e308"))
	require.NoError(t, err)
	assert.Equal(t, ir.ValueNumber, got.Kind)
	assert.Equal(t, ir.BigVal("1.8e308"), got.Num)
}

func TestValueFromNode_QuotedNumericStaysString(t *testing.T) {
	t.Parallel()
	// A quoted numeric string is not plain, so it stays a string.
	node := openapitest.ScalarNode("!!str", "123")
	node.Style = yaml.DoubleQuotedStyle
	got, err := value.FromNode(node)
	require.NoError(t, err)
	assert.Equal(t, ir.ValueString, got.Kind)
	assert.Equal(t, "123", got.Str)
}

func TestValueFromNode_UnsupportedNodeKind(t *testing.T) {
	t.Parallel()
	_, err := value.FromNode(&yaml.Node{Kind: yaml.Kind(99)})
	require.Error(t, err)
}

func TestValueFromNode_SequenceChildError(t *testing.T) {
	t.Parallel()
	seq := &yaml.Node{Kind: yaml.SequenceNode, Content: []*yaml.Node{
		openapitest.ScalarNode("!custom", "x"),
	}}
	_, err := value.FromNode(seq)
	require.Error(t, err)
}

func TestValueFromNode_MappingValueError(t *testing.T) {
	t.Parallel()
	m := &yaml.Node{Kind: yaml.MappingNode, Content: []*yaml.Node{
		openapitest.ScalarNode("!!str", "k"), openapitest.ScalarNode("!custom", "x"),
	}}
	_, err := value.FromNode(m)
	require.Error(t, err)
}

func TestValueFromNode_DepthCapExceeded(t *testing.T) {
	t.Parallel()
	n := openapitest.ScalarNode("!!int", "1")
	for range value.MaxValueDepth + 2 {
		n = &yaml.Node{Kind: yaml.SequenceNode, Content: []*yaml.Node{n}}
	}
	_, err := value.FromNode(n)
	require.Error(t, err)
}

// TestValueFromNode_ReadsAMappingAsTheParserDoes pins the members of a mapping
// value to the pairs the parser reads (nodeview.View.MappingPairs): every `<<`
// merged in, first, a key written through an alias read as the key it names,
// and a key that is no scalar as the empty string (GitHub #395, #1089).
func TestValueFromNode_ReadsAMappingAsTheParserDoes(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name, src string
		want      []string
	}{
		{"a merge comes first", "x: &x {a: 1}\nv: {b: 2, <<: *x}\n", []string{"a", "b"}},
		{"every merge merges", "x: &x {a: 1}\ny: &y {c: 3}\nv: {<<: *x, b: 2, <<: *y}\n", []string{"a", "c", "b"}},
		{"an alias key is its scalar", "k: &k rate\nv: {*k : 1, other: 2}\n", []string{"rate", "other"}},
		{"a key no scalar is empty", "v: {? [x] : 1}\n", []string{""}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := value.FromNode(nodeview.Child(openapitest.YAMLNode(t, tc.src), "v"))
			require.NoError(t, err)
			require.Equal(t, ir.ValueObject, got.Kind)
			var names []string
			for _, f := range got.Object {
				names = append(names, f.Name)
			}
			assert.Equal(t, tc.want, names)
		})
	}
}

// TestValueFromNode_RefusesWhatNoObjectHolds pins the two mappings an object
// cannot hold faithfully: one writing a key twice, plainly, through an alias
// (GitHub #1072) or in a mapping it merges, and one merging past
// nodeview.MergeDepthLimit, whose deepest members would be missing.
func TestValueFromNode_RefusesWhatNoObjectHolds(t *testing.T) {
	t.Parallel()
	for name, src := range map[string]string{
		"a key written twice":        "{a: 1, a: 2}",
		"a key repeated as an alias": "{&k rate: 1, *k : 2}",
		"a key a merge writes twice": "{<<: {a: 1, a: 2}}",
	} {
		_, err := value.FromNode(openapitest.YAMLNode(t, src))
		assert.ErrorContains(t, err, "written more than once", name)
	}
	_, err := value.FromNode(ynode.MergeChain(nodeview.MergeDepthLimit + 1))
	assert.ErrorIs(t, err, value.ErrMergedPastBound)
	_, err = value.FromNode(ynode.MergeChain(nodeview.MergeDepthLimit))
	assert.NoError(t, err, "a chain at the bound is read whole")
}
