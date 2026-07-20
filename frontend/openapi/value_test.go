package openapi

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/require"
	yaml "gopkg.in/yaml.v3"

	"github.com/dexpace/morphic/ir"
)

// yamlNode parses a YAML snippet and returns its root value node (the
// document node's single content child), matching what schema fields expose.
func yamlNode(t *testing.T, src string) *yaml.Node {
	t.Helper()
	var doc yaml.Node
	require.NoError(t, yaml.Unmarshal([]byte(src), &doc))
	require.Len(t, doc.Content, 1, "expected a single document node")
	return doc.Content[0]
}

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
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := valueFromNode(yamlNode(t, tc.src))
			require.NoError(t, err)
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestValueFromNode_ObjectPreservesOrder(t *testing.T) {
	t.Parallel()
	got, err := valueFromNode(yamlNode(t, "b: 1\na: 2\n"))
	require.NoError(t, err)
	require.Equal(t, ir.ValueObject, got.Kind)
	require.Len(t, got.Object, 2)
	require.Equal(t, "b", got.Object[0].Name)
	require.Equal(t, "a", got.Object[1].Name)
}
