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
	"github.com/dexpace/morphic/compilers/openapi/internal/nodeview"
	"github.com/dexpace/morphic/compilers/openapi/internal/overlay"
)

// TestApply_KeepsTheAliasesACopyHolds pins a copy of an anchored node that
// holds an alias of its own: the copy keeps the alias, which goes on naming
// what the source anchors, and the alias of the copied node keeps its value.
func TestApply_KeepsTheAliasesACopyHolds(t *testing.T) {
	t.Parallel()
	const src = "x-b: &b {q: 1}\nx-a: &a {p: *b}\nx-c: *a\n"

	origin, root, diags := applied(t, src, v11+"  - target: $['x-a']\n    update: {r: 1}\n", false)

	require.True(t, origin.Applied(), "%+v", diags)
	sameCodes(t, nil, diags)
	sameValue(t, "x-b: {q: 1}\nx-a: {p: {q: 1}, r: 1}\nx-c: {p: {q: 1}}\n", root)
}

// TestApply_KeepsTheWriterOfAMergedMemberItWrote pins a merge the overlay
// wrote: a later write into the mapping holding it makes its members the
// mapping's own, and those the overlay wrote stay the overlay's.
func TestApply_KeepsTheWriterOfAMergedMemberItWrote(t *testing.T) {
	t.Parallel()
	ov := v11 + "  - target: $.info\n    update: {x-m: {<<: {k: v}}}\n" +
		"  - target: $.info['x-m']\n    update: {z: 1}\n"

	origin, root, diags := applied(t, head, ov, false)

	require.True(t, origin.Applied(), "%+v", diags)
	sameValue(t, strings.Replace(head, "version: '1'}", "version: '1', x-m: {k: v, z: 1}}", 1), root)
	assert.Equal(t, overlayIndex, origin.Attribution().WriterAt("/info/x-m/k"), "the overlay wrote k")
}

// deepMerge returns a source whose x-m merges a chain of `<<` keys one level
// past what the compiler expands, and x-a an anchored mapping x-c merges.
func deepMerge() string {
	var text strings.Builder
	text.WriteString(head + "x-d0: &d0 {k0: 0}\n")
	for i := 1; i <= nodeview.MergeDepthLimit+2; i++ {
		fmt.Fprintf(&text, "x-d%d: &d%d {<<: *d%d}\n", i, i, i-1)
	}
	fmt.Fprintf(&text, "x-m: {<<: *d%d}\n", nodeview.MergeDepthLimit+2)
	text.WriteString("x-a: &a {k: {" + strings.Repeat("a: {", 20) + strings.Repeat("}", 20) + "}}\nx-c: {<<: *a, own: 1}\n")
	return text.String()
}

// TestApply_RefusesAWriteIntoWhatItCannotMakeItsOwn pins the two ways making a
// mapping's merged members its own fails: merges past the depth the compiler
// expands, and copies past the node budget, each refusing the overlay.
func TestApply_RefusesAWriteIntoWhatItCannotMakeItsOwn(t *testing.T) {
	t.Parallel()
	src := deepMerge()

	origin, _, diags := applied(t, src, v11+"  - target: $['x-m']\n    update: {z: 1}\n", false)
	assert.False(t, origin.Applied())
	require.NotEmpty(t, diags)
	assert.Equal(t, diag.OverlayFailed, diags[0].Code, "%+v", diags)
	assert.Contains(t, diags[0].Message, "merges run deeper")

	ov := v11 + "  - target: $['x-c'].k\n    update: {z: 1}\n"
	root := decoded(t, src)
	limit := budget.AtMost(nodesIn(root) + 10)
	origin, diags = overlay.Apply(t.Context(), overlayIndex, root, overlayDoc(t, ov),
		overlay.Options{Path: "o.yaml", Data: []byte(ov), MaxNodes: limit})
	assert.False(t, origin.Applied())
	require.NotEmpty(t, diags)
	assert.Equal(t, diag.BudgetExceeded, diags[0].Code, "%+v", diags)
}

// TestApply_RefusesASelectorThroughMergesPastTheDepthItReads pins a target
// whose selection reads a mapping merging past the depth the compiler
// expands: the action is refused in the overlay's words, naming the bound.
func TestApply_RefusesASelectorThroughMergesPastTheDepthItReads(t *testing.T) {
	t.Parallel()
	origin, _, diags := applied(t, deepMerge(), v11+"  - target: $['x-m'].*\n    update: 1\n", false)

	assert.False(t, origin.Applied())
	require.Len(t, diags, 1, "%+v", diags)
	assert.Equal(t, diag.OverlayFailed, diags[0].Code)
	assert.Equal(t, fmt.Sprintf(`overlay action 1 of 1 (update "$['x-m'].*"): it selects through a mapping whose `+
		"`<<` merges run deeper than %d levels", nodeview.MergeDepthLimit), diags[0].Message)
}

// TestApply_RefusesAnOverlayDocumentOfTheWrongShape pins the reader's
// refusals of objects and references that are no such thing, each at the
// field it is about.
func TestApply_RefusesAnOverlayDocumentOfTheWrongShape(t *testing.T) {
	t.Parallel()
	const v12 = "overlay: 1.2.0\ninfo: {title: o, version: \"1\"}\n"
	var chain strings.Builder
	chain.WriteString("overlay: 1.1.0\nx-i0: &i0 {title: o, version: \"1\"}\n")
	for i := 1; i <= nodeview.MergeDepthLimit+2; i++ {
		fmt.Fprintf(&chain, "x-i%d: &i%d {<<: *i%d}\n", i, i, i-1)
	}
	fmt.Fprintf(&chain, "info: {<<: *i%d}\nactions:\n  - target: $.info\n    update: {a: b}\n", nodeview.MergeDepthLimit+2)
	tests := map[string]struct{ overlay, at string }{
		"an action that is no object": {v11 + "  - 1\n", "/actions/0"},
		"an info merging too deep":    {chain.String(), "/info"},
		"a $ref that is no string":    {v12 + "actions:\n  - $ref: 1\n    target: $.info\n", "/actions/0/$ref"},
		"a key escaping ~ otherwise":  {v12 + "components: {actions: {a: {fields: {update: {}}}}}\nactions:\n  - $ref: '#/components/actions/~2a'\n    target: $.info\n", "/actions/0/$ref"},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			origin, _, diags := applied(t, head, tc.overlay, false)

			assert.False(t, origin.Applied())
			assert.Equal(t, []string{tc.at}, invalidAt(diags), "%+v", diags)
		})
	}

	origin, root, diags := applied(t, head, v12+"components: {}\nactions:\n  - target: $.info\n    update: {x-a: 1}\n", false)
	assert.True(t, origin.Applied(), "a components object declaring no action: %+v", diags)
	sameValue(t, strings.Replace(head, "version: '1'}", "version: '1', x-a: 1}", 1), root)

	ov := v12 + "components: {actions: {d: {fields: {update: {description: d}}}}}\n" +
		"actions:\n  - $ref: '#/components/actions/d'\n    target: $.info\n    description: the reference's own\n"
	origin, _, diags = applied(t, head, ov, false)
	assert.True(t, origin.Applied(), "a reference writing its own description: %+v", diags)
}

// TestApply_WritesAMergedMemberAsTheMappingsOwn pins a write into a mapping
// whose merge brings in the member written, and a remove of a member that
// shadows a merged one: the update merges into the member's value rather than
// shadowing it with a member of its own, and the remove leaves no merged
// member in its place.
func TestApply_WritesAMergedMemberAsTheMappingsOwn(t *testing.T) {
	t.Parallel()
	const src = head + "components:\n  schemas:\n    A: &a {type: object, properties: {x: {type: string}}}\n" +
		"    C: {<<: *a, type: string}\n"
	tests := map[string]struct{ action, c string }{
		"an update merging into a merged member": {"  - target: $.components.schemas.C\n    update: {properties: {y: {type: string}}}\n",
			"{type: string, properties: {x: {type: string}, y: {type: string}}}"},
		"a remove of a member shadowing a merged one": {"  - target: $.components.schemas.C.type\n    remove: true\n",
			"{properties: {x: {type: string}}}"},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			origin, root, diags := applied(t, src, v11+tc.action, false)

			require.True(t, origin.Applied(), "%+v", diags)
			sameValue(t, head+"components:\n  schemas:\n    A: "+plainA+"\n    C: "+tc.c+"\n", root)
		})
	}
}

// TestApply_RemovesEveryCopyOfAKeyWrittenTwice pins a remove of a member whose
// key the mapping writes twice: the value reads the last copy, and removing
// the member removes the earlier one too, which would otherwise stand in its
// place.
func TestApply_RemovesEveryCopyOfAKeyWrittenTwice(t *testing.T) {
	t.Parallel()
	const src = head + "x-m: {a: 1, b: 2, a: 3}\n"

	origin, root, diags := applied(t, src, v11+"  - target: $['x-m'].a\n    remove: true\n", false)

	require.True(t, origin.Applied(), "%+v", diags)
	sameValue(t, head+"x-m: {b: 2}\n", root)
}

// TestApply_KeepsAKeyThatIsNoScalarWhereItFlattensAMerge pins a write into a
// mapping that merges members and writes a key that is no scalar: making the
// mapping's members its own keeps that key, which names no member, for the
// reading that refuses it, by an update and by a remove alike.
func TestApply_KeepsAKeyThatIsNoScalarWhereItFlattensAMerge(t *testing.T) {
	t.Parallel()
	const src = head + "x-b: &b {p: 1, q: 2}\nx-m: {<<: *b, ? [k] : v, r: 3}\n"
	for name, action := range map[string]string{
		"an update of a merged member": "  - target: $['x-m'].p\n    update: 9\n",
		"a remove of a merged member":  "  - target: $['x-m'].q\n    remove: true\n",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			origin, root, diags := applied(t, src, v11+action, false)

			require.True(t, origin.Applied(), "%+v", diags)
			doc := nodeview.DocumentRoot(root)
			require.Equal(t, "x-m", doc.Content[8].Value)
			m := doc.Content[9]
			var sequences int
			for i := 0; i+1 < len(m.Content); i += 2 {
				if m.Content[i].Kind == yaml.SequenceNode {
					sequences++
					assert.Equal(t, "v", m.Content[i+1].Value)
				}
			}
			assert.Equal(t, 1, sequences, "the key that is no scalar stays in x-m")
		})
	}
}

// TestApply_KeepsAMergeTheSourceRefusesWhereItFlattensOne pins a write into a
// mapping whose `<<` reaches what the source's format check refuses: a merge
// naming no mapping, or a key that is no scalar in a mapping it brings in, at
// any depth. Making the mapping's members its own keeps that merge key, for
// the reading that refuses it, where a merge the source accepts is replaced.
func TestApply_KeepsAMergeTheSourceRefusesWhereItFlattensOne(t *testing.T) {
	t.Parallel()
	for value, kept := range map[string]bool{
		"5":                      true,
		"[{p: 1}, 5]":            true,
		"{p: 1, ? [k] : v}":      true,
		"{<<: 5, p: 1}":          true,
		"{<<: {? [k] : v}}":      true,
		"{p: 1}":                 false,
		"[{p: 1}, {<<: {q: 2}}]": false,
	} {
		t.Run(value, func(t *testing.T) {
			t.Parallel()
			src := head + "x-m: {<<: " + value + ", r: 3}\n"
			origin, root, diags := applied(t, src, v11+"  - target: $['x-m']\n    update: {s: 4}\n", false)

			require.True(t, origin.Applied(), "%+v", diags)
			doc := nodeview.DocumentRoot(root)
			require.Equal(t, "x-m", doc.Content[6].Value)
			m := doc.Content[7]
			merges := 0
			for i := 0; i+1 < len(m.Content); i += 2 {
				if nodeview.IsMergeKey(m.Content[i]) {
					merges++
				}
			}
			assert.Equal(t, map[bool]int{true: 1, false: 0}[kept], merges, "the merge key in x-m")
		})
	}
}
