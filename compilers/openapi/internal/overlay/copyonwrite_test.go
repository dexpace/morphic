// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package overlay_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/budget"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/overlay"
)

// head opens each source below: an OpenAPI 3.1 document with no paths.
const head = "openapi: 3.1.0\ninfo: {title: T, version: '1'}\npaths: {}\n"

// aliasedSource anchors schema A, which B names by an alias and C by a merge
// key, so one node is three positions of the value.
const aliasedSource = head + `components:
  schemas:
    A: &a {type: object, properties: {x: {type: string}}}
    B: *a
    C: {<<: *a, description: c}
`

// expandedSource is aliasedSource with every alias and merge written out by
// hand: the value it stands for, with no node shared.
const expandedSource = head + `components:
  schemas:
    A: {type: object, properties: {x: {type: string}}}
    B: {type: object, properties: {x: {type: string}}}
    C: {type: object, properties: {x: {type: string}}, description: c}
`

// schemasAfter returns the components.schemas the three schemas hold after an
// overlay, each written as the flow mapping its position stands for.
func schemasAfter(a, b, c string) string {
	return head + "components:\n  schemas:\n    A: " + a + "\n    B: " + b + "\n    C: " + c + "\n"
}

const (
	plainA = "{type: object, properties: {x: {type: string}}}"
	plainC = "{type: object, properties: {x: {type: string}}, description: c}"
)

// holders returns the position of each mapping under root holding key with
// the value value, each as the keys and indexes that reach it, joined by a
// slash, in document order. An alias is read as the node it names, once for
// each position it stands at.
func holders(root *yaml.Node, key, value string) []string {
	var out []string
	var walk func(n *yaml.Node, at string)
	walk = func(n *yaml.Node, at string) {
		switch n.Kind {
		case yaml.DocumentNode:
			for _, c := range n.Content {
				walk(c, at)
			}
		case yaml.AliasNode:
			walk(n.Alias, at)
		case yaml.MappingNode:
			for i := 0; i+1 < len(n.Content); i += 2 {
				if n.Content[i].Value == key && n.Content[i+1].Value == value {
					out = append(out, at)
				}
				walk(n.Content[i+1], at+"/"+n.Content[i].Value)
			}
		case yaml.SequenceNode:
			for i, c := range n.Content {
				walk(c, fmt.Sprintf("%s/%d", at, i))
			}
		case yaml.ScalarNode:
		}
	}
	walk(root, "")
	return out
}

// nodesIn counts the nodes a Content walk reaches from root: an alias is one
// node, and what it names is not entered, so the count is the size of the
// tree the overlay left and not of the value it stands for.
func nodesIn(root *yaml.Node) int {
	count := 0
	stack := []*yaml.Node{root}
	for len(stack) > 0 {
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		count++
		stack = append(stack, n.Content...)
	}
	return count
}

// ownKeys counts the members named key that the mapping at path holds in its
// own content, a merge key's members not among them.
func ownKeys(t *testing.T, root *yaml.Node, key string, path ...string) int {
	t.Helper()
	_, n := member(t, root, path...)
	count := 0
	for i := 0; i+1 < len(n.Content); i += 2 {
		if n.Content[i].Value == key {
			count++
		}
	}
	return count
}

// updateOf writes the overlay of one update action at version v.
func updateOf(v, target, update string) string {
	return v + "  - target: " + target + "\n    update: " + update + "\n"
}

// TestApply_WritesOnlyThePositionAnUpdateTargets pins copy on write: an
// action that selects a position the source shares with others, an anchored
// node, an alias or a member a merge key brings in, changes that position
// and no other, and draws no warning for the sharing.
func TestApply_WritesOnlyThePositionAnUpdateTargets(t *testing.T) {
	t.Parallel()
	const schemas = "$.components.schemas."
	tests := []struct {
		name, overlay, want string
	}{
		{"the anchored node", updateOf(v11, schemas+"A", "{description: only A}"),
			schemasAfter("{type: object, properties: {x: {type: string}}, description: only A}", plainA, plainC)},
		{"a node under the anchor", updateOf(v11, schemas+"A.properties", "{y: {type: string}}"),
			schemasAfter("{type: object, properties: {x: {type: string}, y: {type: string}}}", plainA, plainC)},
		{"an alias, at 1.0", updateOf(v10, schemas+"B", "{description: b}"),
			schemasAfter(plainA, "{type: object, properties: {x: {type: string}}, description: b}", plainC)},
		{"an alias, at 1.1", updateOf(v11, schemas+"B", "{description: b}"),
			schemasAfter(plainA, "{type: object, properties: {x: {type: string}}, description: b}", plainC)},
		{"a node under an alias", updateOf(v11, schemas+"B.properties", "{y: {type: string}}"),
			schemasAfter(plainA, "{type: object, properties: {x: {type: string}, y: {type: string}}}", plainC)},
		{"a member a merge key brings in", updateOf(v11, schemas+"C.properties", "{y: {type: string}}"),
			schemasAfter(plainA, "{type: object, properties: {x: {type: string}}}",
				"{type: object, properties: {x: {type: string}, y: {type: string}}, description: c}")},
		{"a member a merge key brings in, replaced", updateOf(v11, schemas+"C", "{type: string, x-n: 1}"),
			schemasAfter(plainA, plainA, "{type: string, properties: {x: {type: string}}, description: c, x-n: 1}")},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			origin, root, diags := applied(t, aliasedSource, tc.overlay, false)

			require.True(t, origin.Applied(), "%+v", diags)
			sameCodes(t, nil, diags)
			sameValue(t, tc.want, root)
		})
	}
}

// TestApply_GivesTheSharedValueTheSameTreeAsItsExpansion holds each overlay
// above to the value it must have: the overlay applied to the source with
// every alias and merge written out by hand leaves the same tree, so no
// position holds more or less than the one an overlay selected. The expanded
// twin writes C's type itself, so replacing it draws the info a replaced
// declared value does; the shared source writes it under A alone.
func TestApply_GivesTheSharedValueTheSameTreeAsItsExpansion(t *testing.T) {
	t.Parallel()
	replacedInfo := []string{"info " + diag.OverlayReplacedValue}
	tests := []struct {
		name, action string
		twinCodes    []string
	}{
		{"an update of the anchor", updateOf(v11, "$.components.schemas.A", "{description: only A}"), nil},
		{"an update of an alias", updateOf(v11, "$.components.schemas.B", "{description: only B}"), nil},
		{"an update inside a merge", updateOf(v11, "$.components.schemas.C.properties", "{y: {type: string}}"), nil},
		{"a replaced merged member", updateOf(v11, "$.components.schemas.C", "{type: string, x-n: 1}"), replacedInfo},
		{"an update of every schema", updateOf(v11, "$.components.schemas.*", "{x-all: 1}"), nil},
		{"a remove inside the anchor", v11 + "  - target: $.components.schemas.A.properties.x\n    remove: true\n", nil},
		{"a remove of a merged one", v11 + "  - target: $.components.schemas.C.properties\n    remove: true\n", nil},
		{"a remove of the anchor", v11 + "  - target: $.components.schemas.A\n    remove: true\n", nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, shared, sharedDiags := applied(t, aliasedSource, tc.action, false)
			_, written, writtenDiags := applied(t, expandedSource, tc.action, false)

			sameCodes(t, nil, sharedDiags)
			sameCodes(t, tc.twinCodes, writtenDiags)
			assert.Equal(t, valueOf(t, written), valueOf(t, shared))
		})
	}
}

// TestApply_LeavesAnAnchorsOtherUsesAsTheSourceWroteThem pins what an update of
// the anchor leaves of the nodes that name it: B and C keep the value they
// stood for, a merge key's members and its own description among them, which
// the overlay never wrote.
func TestApply_LeavesAnAnchorsOtherUsesAsTheSourceWroteThem(t *testing.T) {
	t.Parallel()
	_, root, diags := applied(t, aliasedSource, updateOf(v11, "$.components.schemas.A", "{description: only A}"), false)
	require.Empty(t, diags)

	assert.Equal(t, []string{"/components/schemas/A"}, holders(root, "description", "only A"))
	assert.Equal(t, []string{"/components/schemas/C"}, holders(root, "description", "c"))
	assert.Empty(t, holders(root, "description", "only B"), "no use of the anchor was written")
}

// TestApply_AWriteToAMergedMemberLeavesNoSecondKey pins how a merged member
// is replaced: the new value is the mapping's one member of that name, not a
// second beside the merge key's, and what the mapping wrote itself stays.
func TestApply_AWriteToAMergedMemberLeavesNoSecondKey(t *testing.T) {
	t.Parallel()
	_, root, diags := applied(t, aliasedSource, updateOf(v11, "$.components.schemas.C", "{type: string, x-n: 1}"), false)
	require.Empty(t, diags)

	assert.Equal(t, 1, ownKeys(t, root, "type", "components", "schemas", "C"), "one type, not one beside a merge key's")
	assert.Equal(t, 1, ownKeys(t, root, "description", "components", "schemas", "C"), "what C wrote stays")
	_, kind := member(t, root, "components", "schemas", "A", "type")
	assert.Equal(t, "object", kind.Value, "the anchor's own type is not the one C replaced")
}

// TestApply_RemovesAtOnePositionOfASharedNode pins a remove at a shared
// node: of a member the merge brings in, of the anchored node itself, and of
// a member under it, each takes away that position's and no other's.
func TestApply_RemovesAtOnePositionOfASharedNode(t *testing.T) {
	t.Parallel()
	const schemas = "$.components.schemas."
	remove := func(target string) string { return v11 + "  - target: " + target + "\n    remove: true\n" }
	tests := []struct{ name, overlay, want string }{
		{"a merged member", remove(schemas + "C.properties"),
			schemasAfter(plainA, plainA, "{type: object, description: c}")},
		{"the anchored node", remove(schemas + "A"),
			head + "components:\n  schemas:\n    B: " + plainA + "\n    C: " + plainC + "\n"},
		{"a member under an alias", remove(schemas + "B.properties"),
			schemasAfter(plainA, "{type: object}", plainC)},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			origin, root, diags := applied(t, aliasedSource, tc.overlay, false)

			require.True(t, origin.Applied(), "%+v", diags)
			sameCodes(t, nil, diags)
			sameValue(t, tc.want, root)
		})
	}
}

// TestApply_WritesEachPositionOfAWildcardOnce pins an action selecting the
// three positions of one node: each holds the member once, and the tree is as
// large as the one an expanded source leaves, so no position was written
// twice for sharing a node, nor skipped.
func TestApply_WritesEachPositionOfAWildcardOnce(t *testing.T) {
	t.Parallel()
	action := updateOf(v11, "$.components.schemas.*", "{x-all: 1}")

	_, shared, diags := applied(t, aliasedSource, action, false)
	require.Empty(t, diags)
	_, written, _ := applied(t, expandedSource, action, false)

	for _, name := range []string{"A", "B", "C"} {
		assert.Equal(t, 1, ownKeys(t, shared, "x-all", "components", "schemas", name), name)
	}
	assert.Len(t, holders(shared, "x-all", "1"), 3, "three positions written")
	assert.Equal(t, nodesIn(written), nodesIn(shared), "as many nodes as the expanded twin's")
}

// TestApply_ReadsEachActionTheValueTheOneBeforeLeft pins that no selection is
// kept between actions: a remove of the anchor, then an update of its alias,
// writes the alias as the value the anchor's removal left it.
func TestApply_ReadsEachActionTheValueTheOneBeforeLeft(t *testing.T) {
	t.Parallel()
	ov := v11 + "  - target: $.components.schemas.A\n    remove: true\n" +
		"  - target: $.components.schemas.B\n    update: {description: late}\n"

	origin, root, diags := applied(t, aliasedSource, ov, false)

	require.True(t, origin.Applied(), "%+v", diags)
	sameCodes(t, nil, diags)
	sameValue(t, head+"components:\n  schemas:\n    B: {type: object, properties: {x: {type: string}}, description: late}\n"+
		"    C: "+plainC+"\n", root)
}

// TestApply_WritesAPositionWhateverShapeItTargets pins the writes that reach
// no alias at all, over a source that holds one: the root, a scalar, an
// array and a node whose parent is missing, so that sharing elsewhere in the
// tree changes none of them.
func TestApply_WritesAPositionWhateverShapeItTargets(t *testing.T) {
	t.Parallel()
	const source = aliasedSource + "x-list: [1, 2]\n"
	tests := []struct {
		name, overlay string
		lax           bool
		want          string
		codes         []string
	}{
		{"the root", updateOf(v11, "$", "{x-root: 1}"), false,
			source + "x-root: 1\n", nil},
		{"a scalar", updateOf(v11, "$.components.schemas.A.type", "string"), false,
			schemasAfter("{type: string, properties: {x: {type: string}}}", plainA, plainC) + "x-list: [1, 2]\n",
			[]string{"info " + diag.OverlayReplacedValue}},
		{"an array", updateOf(v11, "$['x-list']", "[3]"), false,
			strings.Replace(source, "[1, 2]", "[1, 2, 3]", 1), nil},
		{"no node, strict", updateOf(v11, "$.components.schemas.B.properties.nope.deeper", "{a: 1}"), false,
			source, []string{"error " + diag.OverlayFailed}},
		{"no node, lax", updateOf(v11, "$.components.schemas.B.properties.nope.deeper", "{a: 1}"), true,
			source, []string{"warning " + diag.OverlayAction}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, root, diags := applied(t, source, tc.overlay, tc.lax)

			sameCodes(t, tc.codes, diags)
			sameValue(t, tc.want, root)
		})
	}
}

// TestApply_RefusesACopyThatWouldGrowTheTreePastItsBudget pins the charge made
// before a copy is built: a hundred aliases of a mapping of two thousand
// nodes, each written, would be some two hundred thousand nodes, and a budget
// of fifty thousand refuses the action with nothing built and the tree as it
// was. With no budget it applies, and each of the 101 mappings holds the
// member.
func TestApply_RefusesACopyThatWouldGrowTheTreePastItsBudget(t *testing.T) {
	t.Parallel()
	var src strings.Builder
	src.WriteString(head + "x-big: &m {")
	for i := range 1000 {
		fmt.Fprintf(&src, "k%d: 0, ", i)
	}
	src.WriteString("}\n")
	for i := range 100 {
		fmt.Fprintf(&src, "x-%d: *m\n", i)
	}
	ov := v11 + "  - target: $[?@.k0 == 0]\n    update: {t: 1}\n"

	root := decoded(t, src.String())
	origin, diags := overlay.Apply(t.Context(), overlayIndex, root, overlayDoc(t, ov),
		overlay.Options{Path: "o.yaml", Data: []byte(ov), MaxNodes: budget.AtMost(50_000)})

	assert.False(t, origin.Applied())
	require.NotEmpty(t, diags)
	assert.Equal(t, "error "+diag.BudgetExceeded, codes(diags)[0], "%+v", diags)
	assert.Equal(t, "/actions/0", string(diags[0].Provenance.Pointer), "at the action")
	assert.Empty(t, holders(root, "t", "1"), "nothing was built")
	sameValue(t, src.String(), root)

	origin, root, diags = applied(t, src.String(), ov, false)
	require.True(t, origin.Applied(), "%+v", diags)
	assert.Len(t, holders(root, "t", "1"), 101, "x-big and its hundred aliases each hold the member")
}

// TestApply_AppliesAJSONOverlayThatWritesASurrogatePair pins what a JSON
// overlay says in escapes: a surrogate pair and a solidus read as the
// characters they name, as the YAML spelling of each does.
func TestApply_AppliesAJSONOverlayThatWritesASurrogatePair(t *testing.T) {
	t.Parallel()
	half := `\u` + `d83d`
	pair := half + `\u` + `de00`
	ov := `{"overlay": "1.1.0", "info": {"title": "o", "version": "1"}, "actions": ` +
		`[{"target": "$.info", "update": {"description": "x` + pair + `y\/z"}}]}`

	origin, root, diags := applied(t, head, ov, false)

	require.True(t, origin.Applied(), "%+v", diags)
	_, description := member(t, root, "info", "description")
	assert.Equal(t, "x😀y/z", description.Value)
	assert.Equal(t, []string{"/info"}, holders(root, "description", "x😀y/z"))
	sameCodes(t, nil, diags)
}
