// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package dialect

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/specrules"
)

// draft04 names JSON Schema draft 4, a dialect the table has rows for.
const draft04 = "http://json-schema.org/draft-04/schema#"

// TestGovern_NotesEachListOnce pins what keeps the walk linear in a tree
// whose aliases place each list twice in the next, 2^20 placements of the
// innermost: each list is noted, so it is walked once.
func TestGovern_NotesEachListOnce(t *testing.T) {
	t.Parallel()
	const depth = 20
	var b strings.Builder
	b.WriteString("l0: &l0 [{type: integer}]\n")
	for i := 1; i <= depth; i++ {
		fmt.Fprintf(&b, "l%d: &l%d [*l%d, *l%d]\n", i, i, i-1, i-1)
	}
	var tree yaml.Node
	require.NoError(t, yaml.Unmarshal([]byte(b.String()), &tree))
	rows, _ := specrules.Table().SchemaRules("3.1.0", "")
	d := NewBuilder(rows)
	d.Govern(tree.Content[0], rows)
	assert.Len(t, d.t.byNode, depth+3, "the root mapping, each list and the one schema")
}

// TestGovern_ANodeTwoRootsHoldReadsTheFirsts pins that a node two roots
// naming a dialect hold reads the rows of the first to govern it, which the
// load phase asks innermost first, and that each mapping is noted once,
// however many aliases place it, and no scalar at all.
func TestGovern_ANodeTwoRootsHoldReadsTheFirsts(t *testing.T) {
	t.Parallel()
	var tree yaml.Node
	require.NoError(t, yaml.Unmarshal([]byte("A: {x: &n {type: integer}, y: *n}\nB: {z: *n}\n"), &tree))
	root := tree.Content[0]
	a, b := root.Content[1], root.Content[3]
	source, _ := specrules.Table().SchemaRules("3.0.3", "")
	first, _ := specrules.Table().SchemaRules("3.1.0", draft04)
	second, _ := specrules.Table().SchemaRules("3.1.0", "")
	d := NewBuilder(source)
	d.Govern(a, first)
	d.Govern(b, second)
	d.Govern(nil, first)
	table := d.Table()
	// Identity, not equality: two rows of one dialect are equal, and the claim
	// is whose rows the node holds.
	if table.Of(a.Content[1]) != first {
		t.Error("the shared node holds the second root's rows, not the first's")
	}
	if table.Of(b) != second {
		t.Error("B holds another root's rows than its own")
	}
	assert.Len(t, table.byNode, 3, "A, the shared node and B, none of their scalars, and nothing for no tree")
}
