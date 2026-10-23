package oas_test

import (
	"encoding/json/jsontext"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	yaml "gopkg.in/yaml.v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/oas"
	"github.com/dexpace/morphic/ir"
)

// formatFindings runs CheckFormat over doc, declaring version, each finding
// placed at the pointer it was read at.
func formatFindings(t *testing.T, version, doc string, budget int) []finding {
	t.Helper()
	var n yaml.Node
	require.NoError(t, yaml.Unmarshal([]byte(doc), &n))
	diags := oas.CheckFormat(&n, version, oas.Options{
		Budget: budget,
		Locate: func(p jsontext.Pointer) ir.Provenance { return ir.Provenance{Pointer: p} },
	})
	out := make([]finding, 0, len(diags))
	for _, d := range diags {
		out = append(out, finding{Code: d.Code, At: string(d.Provenance.Pointer)})
	}
	sortFindings(out)
	return out
}

// TestCheckFormat_AKeyIsAString pins the Format section's rule on keys over
// the tree as written, objects, values and extensions alike: a key that is no
// scalar, aliased or not, is placed at the pointer the parser reads it at,
// under the empty key; a `<<` naming no mapping, by itself or in a list, is
// placed at the mapping it merges into; a pair a merge writes in place is the
// merging mapping's. A node an alias mounts twice is reported once.
func TestCheckFormat_AKeyIsAString(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name, doc string
		want      []finding
	}{
		{"a well-formed document draws nothing",
			"a: {b: 1}\nm: &m {k: v}\nn: {<<: *m}\nl: {<<: [*m, *m]}\n", []finding{}},
		{"a mapping key", "x-e:\n  ? {k: v}\n  : 1\n", []finding{at(mismatch, "/x-e/")}},
		{"a list key", "x-e:\n  ? [k]\n  : 1\n", []finding{at(mismatch, "/x-e/")}},
		{"an alias to a mapping as a key", "m: &m {k: v}\nx-e: {*m : 1}\n", []finding{at(mismatch, "/x-e/")}},
		{"a merge of a scalar", "x-e: {<<: 5}\n", []finding{at(mismatch, "/x-e")}},
		{"a merge of an alias to a scalar", "s: &s five\nx-e: {<<: *s}\n", []finding{at(mismatch, "/x-e")}},
		{"a scalar in a merge list", "m: &m {k: v}\nx-e: {<<: [*m, 5]}\n", []finding{at(mismatch, "/x-e")}},
		{"a scalar in an aliased merge list", "m: &m {k: v}\nl: &l [*m, 5]\nx-e: {<<: *l}\n",
			[]finding{at(mismatch, "/x-e")}},
		{"a key no scalar in a merged mapping is the merging mapping's",
			"x-e: {<<: {? [k] : 1}}\n", []finding{at(mismatch, "/x-e/")}},
		{"in a merged list too", "x-e: {<<: [{? [k] : 1}]}\n", []finding{at(mismatch, "/x-e/")}},
		{"a node an alias mounts twice is reported once", "m: &m {? [k] : 1}\na: *m\nb: *m\n",
			[]finding{at(mismatch, "/m/")}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := formatFindings(t, "3.1.0", tc.doc, 0)
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("findings (-want +got):\n%s", diff)
			}
		})
	}
}

// TestCheckFormat_SaysWhatItRead pins the messages: the minor and the section
// the rule comes from, what the key or the merge names, and what the parser
// makes of it.
func TestCheckFormat_SaysWhatItRead(t *testing.T) {
	t.Parallel()
	var n yaml.Node
	require.NoError(t, yaml.Unmarshal([]byte("a: {? [k] : 1, <<: 5}\n"), &n))
	diags := oas.CheckFormat(&n, "3.0.3", oas.Options{})
	require.Len(t, diags, 2)
	assert.Equal(t, ir.SeverityError, diags[0].Severity)
	assert.Equal(t, "a mapping key must be a string in OpenAPI 3.0 (Format), and this one is a list; "+
		"it is read as the empty string", diags[0].Message)
	assert.Equal(t, "a `<<` merge key must name a mapping or a list of mappings, and this one names a scalar; "+
		"it merges nothing", diags[1].Message)
}

// TestCheckFormat_IsBoundedAndKeyedToTheMinor pins the two edges: a version
// the table does not cover is held to nothing, and a tree past the budget is
// read as far as it and reported, an unset budget being the default one.
func TestCheckFormat_IsBoundedAndKeyedToTheMinor(t *testing.T) {
	t.Parallel()
	const doc = "x-e:\n  ? [k]\n  : 1\n"
	assert.Empty(t, formatFindings(t, "2.0", doc, 0))
	assert.Equal(t, []finding{at(diag.BudgetExceeded, "")}, formatFindings(t, "3.1.0", doc, 2))
	assert.Equal(t, []finding{at(mismatch, "/x-e/")}, formatFindings(t, "3.1.0", doc, 0))
}

// TestCheckFormat_NamesEachKind pins kindName over what a merge can name: a
// mapping is no finding, and nothing at all, which no parse produces, is
// named as such.
func TestCheckFormat_NamesEachKind(t *testing.T) {
	t.Parallel()
	merge := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!merge", Value: "<<"}
	root := &yaml.Node{Kind: yaml.MappingNode, Content: []*yaml.Node{merge, {Kind: yaml.AliasNode}}}
	diags := oas.CheckFormat(root, "3.1.0", oas.Options{})
	require.Len(t, diags, 1)
	assert.Contains(t, diags[0].Message, "names nothing")
}
