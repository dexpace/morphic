// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package overlay_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/ir"
)

// mergeSpec is the source the merge cases write into: an object, an array of
// objects, an array of primitives, a primitive, and a schema holding all
// three below it.
const mergeSpec = `info: {title: T, version: "1"}
tags: [{name: a}]
x-list: [1, 2]
components:
  schemas:
    R:
      type: object
      required: [original]
      properties: {original: {type: string}}
      example: {a: 1}
`

// mergeCase is one action applied to mergeSpec: the tree it leaves, written
// as the whole document, or the codes it reports with the tree left as it
// fails.
type mergeCase struct {
	name, overlay, want string
	codes               []string
}

// mergeCases are the Overlay Specification's rules for writing an update, at
// each version that writes them differently: at the node the target selects,
// by its shape and the update's, and below it, member by member.
func mergeCases() []mergeCase {
	refused := []string{"error " + diag.OverlayFailed}
	return []mergeCase{
		{name: "an object merges into an object", overlay: v10 + "  - target: $.info\n    update: {description: d}\n",
			want: `{info: {title: T, version: "1", description: d}, tags: [{name: a}], x-list: [1, 2],
				components: {schemas: {R: {type: object, required: [original], properties: {original: {type: string}}, example: {a: 1}}}}}`},
		{name: "a nested object merges member by member",
			overlay: v10 + "  - target: $.components.schemas.R\n    update: {properties: {added: {type: integer}}}\n",
			want: `{info: {title: T, version: "1"}, tags: [{name: a}], x-list: [1, 2],
				components: {schemas: {R: {type: object, required: [original],
				properties: {original: {type: string}, added: {type: integer}}, example: {a: 1}}}}}`},
		{name: "a nested array is concatenated, at 1.0",
			overlay: v10 + "  - target: $.components.schemas.R\n    update: {required: [replacement]}\n",
			want: `{info: {title: T, version: "1"}, tags: [{name: a}], x-list: [1, 2],
				components: {schemas: {R: {type: object, required: [original, replacement],
				properties: {original: {type: string}}, example: {a: 1}}}}}`},
		{name: "a nested array is concatenated, at 1.1",
			overlay: v11 + "  - target: $.components.schemas.R\n    update: {required: [replacement]}\n",
			want: `{info: {title: T, version: "1"}, tags: [{name: a}], x-list: [1, 2],
				components: {schemas: {R: {type: object, required: [original, replacement],
				properties: {original: {type: string}}, example: {a: 1}}}}}`},
		{name: "an object is appended to an array", overlay: v10 + "  - target: $.tags\n    update: {name: b}\n",
			want: `{info: {title: T, version: "1"}, tags: [{name: a}, {name: b}], x-list: [1, 2],
				components: {schemas: {R: {type: object, required: [original], properties: {original: {type: string}}, example: {a: 1}}}}}`},
		{name: "an array is concatenated with an array, at 1.0", overlay: v10 + "  - target: $['x-list']\n    update: [3, 4]\n",
			want: `{info: {title: T, version: "1"}, tags: [{name: a}], x-list: [1, 2, 3, 4],
				components: {schemas: {R: {type: object, required: [original], properties: {original: {type: string}}, example: {a: 1}}}}}`},
		{name: "an array is concatenated with an array, at 1.1", overlay: v11 + "  - target: $['x-list']\n    update: [3, 4]\n",
			want: `{info: {title: T, version: "1"}, tags: [{name: a}], x-list: [1, 2, 3, 4],
				components: {schemas: {R: {type: object, required: [original], properties: {original: {type: string}}, example: {a: 1}}}}}`},
		{name: "a primitive is appended to an array, at 1.1", overlay: v11 + "  - target: $['x-list']\n    update: 3\n",
			want: `{info: {title: T, version: "1"}, tags: [{name: a}], x-list: [1, 2, 3],
				components: {schemas: {R: {type: object, required: [original], properties: {original: {type: string}}, example: {a: 1}}}}}`},
		{name: "a primitive replaces a selected primitive, at 1.1", overlay: v11 + "  - target: $.info.title\n    update: New\n",
			want: `{info: {title: New, version: "1"}, tags: [{name: a}], x-list: [1, 2],
				components: {schemas: {R: {type: object, required: [original], properties: {original: {type: string}}, example: {a: 1}}}}}`,
			codes: []string{"info " + diag.OverlayReplacedValue}},
		{name: "a primitive is no target, at 1.0", overlay: v10 + "  - target: $.info.title\n    update: New\n",
			want: mergeSpec, codes: refused},
		{name: "an object takes no primitive", overlay: v11 + "  - target: $.info\n    update: New\n",
			want: mergeSpec, codes: refused},
		{name: "a primitive takes no object, at 1.1", overlay: v11 + "  - target: $.info.title\n    update: {a: b}\n",
			want: mergeSpec, codes: refused},
		{name: "an object member takes no primitive", overlay: v10 + "  - target: $.components.schemas.R\n    update: {example: 5}\n",
			want: mergeSpec, codes: refused},
		{name: "a primitive member takes no object", overlay: v10 + "  - target: $.components.schemas.R\n    update: {type: {a: b}}\n",
			want: mergeSpec, codes: refused},
		{name: "an array member takes no object", overlay: v11 + "  - target: $.components.schemas.R\n    update: {required: {a: b}}\n",
			want: mergeSpec, codes: refused},
		{name: "a null is a primitive no object takes", overlay: v10 + "  - target: $.components.schemas.R\n    update: {properties: null}\n",
			want: mergeSpec, codes: refused},
		{name: "nodes of two kinds take one update, at 1.0", overlay: v10 + "  - target: $['info','tags']\n    update: {x-a: 1}\n",
			want: `{info: {title: T, version: "1", x-a: 1}, tags: [{name: a}, {x-a: 1}], x-list: [1, 2],
				components: {schemas: {R: {type: object, required: [original], properties: {original: {type: string}}, example: {a: 1}}}}}`},
		{name: "nodes of two kinds take no update, at 1.1", overlay: v11 + "  - target: $['info','tags']\n    update: {x-a: 1}\n",
			want: mergeSpec, codes: refused},
		{name: "a node selected twice is written once", overlay: v10 + "  - target: $['tags','tags']\n    update: {name: b}\n",
			want: `{info: {title: T, version: "1"}, tags: [{name: a}, {name: b}], x-list: [1, 2],
				components: {schemas: {R: {type: object, required: [original], properties: {original: {type: string}}, example: {a: 1}}}}}`},
		{name: "the root is no node a remove removes", overlay: v10 + "  - target: $\n    remove: true\n",
			want: mergeSpec, codes: refused},
	}
}

// TestGrafter_WritesAnUpdateAsTheSpecificationSays pins the rules for an
// update by the shapes of the node selected and of the update, at both
// versions: an object merges into an object member by member, a primitive
// replaces a primitive, an array is concatenated with an array, nested or
// selected, an object or a primitive is appended to a selected array, and a
// selected primitive is no target at 1.0. Every other pair is refused, and the
// tree is left as it was.
func TestGrafter_WritesAnUpdateAsTheSpecificationSays(t *testing.T) {
	t.Parallel()
	for _, tc := range mergeCases() {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, root, diags := applied(t, mergeSpec, tc.overlay, false)

			sameCodes(t, tc.codes, diags)
			sameValue(t, tc.want, root)
		})
	}
}

// TestGrafter_TellsA10PrimitiveRemoveFromAnUpdate pins the asymmetry Overlay
// 1.0 leaves for a primitive: a remove of one applies, as 1.0 has no other
// spelling for removing a primitive member, while an update of one is refused
// naming both of the spellings that write it, since one exists.
func TestGrafter_TellsA10PrimitiveRemoveFromAnUpdate(t *testing.T) {
	t.Parallel()
	_, root, diags := applied(t, mergeSpec, v10+"  - target: $.info.version\n    remove: true\n", false)
	require.Empty(t, diags)
	_, info := member(t, root, "info")
	var got map[string]any
	require.NoError(t, info.Decode(&got))
	assert.Equal(t, map[string]any{"title": "T"}, got, "the version is removed")

	_, _, diags = applied(t, mergeSpec, v10+"  - target: $.info.title\n    update: New\n", false)
	sameCodes(t, []string{"error " + diag.OverlayFailed}, diags)
	assert.Contains(t, diags[0].Message, "target the object holding it and update the member, or declare overlay: 1.1.0")
}

// TestGrafter_QuotesATargetAsWritten pins the action a refusal names: its
// target as the overlay writes it, quoted, a `%` in it included, which a
// message built from it as a format would read as a verb, and a line break,
// which RFC 9535 reads as blank space and which would split the report.
func TestGrafter_QuotesATargetAsWritten(t *testing.T) {
	t.Parallel()
	_, _, diags := applied(t, mergeSpec, v11+"  - target: \"$['info','%d']\"\n    update: 1\n", false)

	sameCodes(t, []string{"error " + diag.OverlayFailed}, diags)
	assert.Equal(t, `overlay action 1 of 1 (update "$['info','%d']"): it writes a primitive into the object at line 1, `+
		"column 7, which only an object merges into", diags[0].Message)

	_, _, diags = applied(t, mergeSpec, v11+"  - target: \"$\\n.info\"\n    update: 1\n", false)
	sameCodes(t, []string{"error " + diag.OverlayFailed}, diags)
	assert.Contains(t, diags[0].Message, `(update "$\n.info")`)
	assert.NotContains(t, diags[0].Message, "\n")
}

// TestGrafter_AppliesActionsInOrderToWhatTheOnesBeforeLeft pins the
// specification's order: each action applies to the tree the ones before it
// left, so a later action selects what an earlier one added, finds nothing
// where an earlier one removed, and re-creates what an earlier one removed.
func TestGrafter_AppliesActionsInOrderToWhatTheOnesBeforeLeft(t *testing.T) {
	t.Parallel()
	const src = "info: {title: T, version: '1', description: old}\ntags: [{name: a}]\n"
	tests := []struct {
		name, actions, want string
		codes               []string
	}{
		{name: "an update into what an earlier update added",
			actions: "  - target: $.info\n    update: {x-a: {b: 1}}\n  - target: $.info['x-a']\n    update: {c: 2}\n",
			want:    "{info: {title: T, version: '1', description: old, x-a: {b: 1, c: 2}}, tags: [{name: a}]}"},
		{name: "a remove of what an earlier update added",
			actions: "  - target: $.info\n    update: {x-a: 1}\n  - target: $.info['x-a']\n    remove: true\n",
			want:    "{info: {title: T, version: '1', description: old}, tags: [{name: a}]}"},
		{name: "an update re-creating what an earlier remove removed",
			actions: "  - target: $.info.description\n    remove: true\n  - target: $.info\n    update: {description: new}\n",
			want:    "{info: {title: T, version: '1', description: new}, tags: [{name: a}]}"},
		{name: "a filter selecting what an earlier update set",
			actions: "  - target: $.tags.*\n    update: {x-flag: on}\n  - target: $.tags[?@['x-flag'] == 'on']\n    update: {x-seen: true}\n",
			want:    "{info: {title: T, version: '1', description: old}, tags: [{name: a, x-flag: on, x-seen: true}]}"},
		{name: "an update of what an earlier remove removed",
			actions: "  - target: $.info.description\n    remove: true\n  - target: $.info.description\n    update: x\n",
			want:    "{info: {title: T, version: '1'}, tags: [{name: a}]}",
			codes:   []string{"error " + diag.OverlayFailed}},
		{name: "a later write over what an earlier one wrote",
			actions: "  - target: $.info\n    update: {description: first}\n  - target: $.info\n    update: {description: second}\n",
			want:    "{info: {title: T, version: '1', description: second}, tags: [{name: a}]}",
			codes:   []string{"info " + diag.OverlayReplacedValue}},
		{name: "removes of what updates grafted after an earlier remove",
			actions: "  - target: $.info.description\n    remove: true\n" +
				"  - target: $.info\n    update: {x-a: {b: 1, c: 2}, x-d: 1}\n  - target: $.tags\n    update: [{name: b}]\n" +
				"  - target: $.info['x-a'].b\n    remove: true\n  - target: $.info['x-d']\n    remove: true\n" +
				"  - target: $.tags[1]\n    remove: true\n",
			want: "{info: {title: T, version: '1', x-a: {c: 2}}, tags: [{name: a}]}"},
		{name: "a move: an update ensuring the target, a copy, and a remove",
			actions: "  - target: $\n    update: {x-new: {}}\n  - target: $['x-new']\n    copy: $.info\n  - target: $.info\n    remove: true\n",
			want:    "{x-new: {title: T, version: '1', description: old}, tags: [{name: a}]}"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, root, diags := applied(t, src, v11+tc.actions, false)

			sameCodes(t, tc.codes, diags)
			sameValue(t, tc.want, root)
		})
	}
}

// TestGrafter_ReportsAValueTheSourceDeclaresReplaced pins the report an
// overlay that overrides the source draws, and the writes that draw none: an
// info at the value that replaced it, naming both values and where the
// source writes the one replaced, so a caller treating overlays as
// gap-fillers can promote it, while adding a value the source lacks, writing
// the value already there, and writing over a value an earlier action wrote
// report no replacement.
func TestGrafter_ReportsAValueTheSourceDeclaresReplaced(t *testing.T) {
	t.Parallel()
	const src = "info: {title: T, version: '1', description: upstream wrote this}\ntags: [{name: a}]\n"
	_, _, diags := applied(t, src, v10+
		"  - target: $.info\n    update: {description: overlay wrote this, summary: overlay filled a gap}\n", false)

	sameCodes(t, []string{"info " + diag.OverlayReplacedValue}, diags)
	assert.Equal(t, ir.Provenance{Source: overlayIndex, Pointer: "/actions/0", Position: ir.Position{Line: 5, Column: 27}},
		diags[0].Provenance, "at the value that replaced it, in the overlay")
	assert.Equal(t, `overlay action 1 of 1 (update "$.info") replaces "upstream wrote this", which the source declares `+
		`at line 1, column 45, with "overlay wrote this"`, diags[0].Message)

	for name, ov := range map[string]string{
		"the value already there": v10 + "  - target: $.info\n    update: {description: upstream wrote this}\n",
		"over an earlier action's value": v10 + "  - target: $.info\n    update: {x-a: 1}\n" +
			"  - target: $.info\n    update: {x-a: 2}\n",
	} {
		_, _, diags := applied(t, src, ov, false)
		for _, d := range diags {
			assert.NotEqual(t, diag.OverlayReplacedValue, d.Code, "%s: %+v", name, d)
		}
	}
}

// TestGrafter_ReportsAValueACopyReplaced pins the report for a copy, whose
// replacing value is the source's own: it is placed at the action, since the
// overlay writes the value nowhere.
func TestGrafter_ReportsAValueACopyReplaced(t *testing.T) {
	t.Parallel()
	const src = "info: {title: T, version: '1'}\nx-from: {title: U}\n"
	_, _, diags := applied(t, src, v11+"  - target: $.info\n    copy: $['x-from']\n", false)

	sameCodes(t, []string{"info " + diag.OverlayReplacedValue}, diags)
	assert.Equal(t, ir.Provenance{Source: overlayIndex, Pointer: "/actions/0"}, diags[0].Provenance)
	assert.Contains(t, diags[0].Message, `replaces "T", which the source declares at line 1, column 15, with "U"`)
}

// TestGrafter_TakesACopyOfTheSourceAsTheActionBegan pins the template a copy
// takes: a source that holds a target is copied as it stood, not as writing
// it into the target left it, and a target that holds the source takes it
// whole.
func TestGrafter_TakesACopyOfTheSourceAsTheActionBegan(t *testing.T) {
	t.Parallel()
	_, root, diags := applied(t, "a: {c: 1, b: {}}\n", v11+"  - target: $.a.b\n    copy: $.a\n", false)
	require.Empty(t, diags)
	sameValue(t, "a: {c: 1, b: {c: 1, b: {}}}\n", root)

	_, root, diags = applied(t, "a: {b: {c: 1}}\n", v11+"  - target: $.a\n    copy: $.a.b\n", false)
	require.Empty(t, diags)
	sameValue(t, "a: {b: {c: 1}, c: 1}\n", root)
}

// TestGrafter_RefusesACopyOfManyNodes pins a copy selector selecting more
// than the one node a copy copies, which the specification makes one: the
// action cannot apply, in either mode, whatever its target selects.
func TestGrafter_RefusesACopyOfManyNodes(t *testing.T) {
	t.Parallel()
	for _, target := range []string{"$.a", "$.nowhere"} {
		for _, lax := range []bool{false, true} {
			_, root, diags := applied(t, "a: {}\nb: {x: 1}\nc: {y: 1}\n", v11+"  - target: "+target+"\n    copy: $['b','c']\n", lax)

			sameCodes(t, []string{"error " + diag.OverlayFailed}, diags)
			assert.Contains(t, diags[0].Message, "selects 2 nodes", "%s, lax=%v", target, lax)
			sameValue(t, "a: {}\nb: {x: 1}\nc: {y: 1}\n", root)
		}
	}
}

// TestGrafter_RefusesToWriteThroughAnAlias pins what an action selecting a
// YAML alias, or merging into a member the source writes as one, does: it is
// refused, since the alias shares its anchor's node and writing it would
// change every use of the anchor.
func TestGrafter_RefusesToWriteThroughAnAlias(t *testing.T) {
	t.Parallel()
	const src = "x-a: &a {k: v}\nx-b: *a\nx-c: {d: *a}\n"
	for name, action := range map[string]string{
		"a target that is an alias":  "  - target: $['x-b']\n    update: {k: w}\n",
		"a member that is an alias":  "  - target: $['x-c']\n    update: {d: {k: w}}\n",
		"an array target appends to": "  - target: $['x-b']\n    update: [1]\n",
	} {
		_, root, diags := applied(t, src, v11+action, false)

		sameCodes(t, []string{"warning " + diag.OverlayAction, "error " + diag.OverlayFailed}, diags)
		require.Len(t, diags, 2, name)
		assert.Contains(t, diags[1].Message, "YAML alias at line", name)
		assert.Contains(t, diags[1].Message, "whose anchor shares its value with every use of it", name)
		_, k := member(t, root, "x-a", "k")
		assert.Equal(t, "v", k.Value, "%s: the anchor is as the source wrote it", name)
	}
}

// TestGrafter_WritesWhatAnAliasInTheUpdateNames pins an alias the overlay
// writes in its update: it stands for its anchor's value, so what is written
// is a copy of that value, holding no alias, with no anchor of its own.
func TestGrafter_WritesWhatAnAliasInTheUpdateNames(t *testing.T) {
	t.Parallel()
	const ov = "overlay: 1.0.0\ninfo: {title: O, version: \"1\", x-in: &in {deep: 1}, x-out: &out {a: *in}}\n" +
		"actions:\n  - target: $\n    update: {p: *out, q: [*in]}\n"
	_, root, diags := applied(t, "x: 1\n", ov, false)
	require.Empty(t, diags)

	sameValue(t, "{x: 1, p: {a: {deep: 1}}, q: [{deep: 1}]}", root)
	_, p := member(t, root, "p")
	assert.Empty(t, p.Anchor, "the copy does not answer to the name of the node it came from")
	assert.Zero(t, aliasNodes(root), "and no node of the tree is an alias")
}

// TestGrafter_LeavesTheSourcesOwnAliasesAlone is the control: an alias the
// source writes and the overlay does not select is ordinary YAML, which every
// reading copes with by design, and expanding it would grow what the node
// budget measures.
func TestGrafter_LeavesTheSourcesOwnAliasesAlone(t *testing.T) {
	t.Parallel()
	_, root, diags := applied(t, "x-t: &s {description: d}\nx-use: *s\ninfo: {title: T}\n",
		v10+"  - target: $.info\n    update: {description: d}\n", false)
	sameCodes(t, []string{"warning " + diag.OverlayAction}, diags)
	assert.Contains(t, diags[0].Message, "do not read through", "the source's aliases are warned of, once")

	_, use := member(t, root, "x-use")
	assert.Equal(t, yaml.AliasNode, use.Kind, "the alias is still an alias, not the subtree it names expanded in place")
}

// aliasNodes counts the alias nodes a Content walk from root reaches.
func aliasNodes(root *yaml.Node) int {
	count := 0
	stack := []*yaml.Node{root}
	for len(stack) > 0 {
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if n.Kind == yaml.AliasNode {
			count++
		}
		stack = append(stack, n.Content...)
	}
	return count
}

// TestRepairAliases_CopiesWhatARemoveLeftAnAliasNaming pins what a remove of
// an anchored node leaves at each alias of it: a copy of the value the alias
// stands for, holding no alias, so every reader walking the tree sees what the
// parser follows (GitHub #477). An alias reached through a `<<` merge key and
// one inside another anchored value are copied alike, and one both the anchor
// and the alias of which were removed leaves nothing.
func TestRepairAliases_CopiesWhatARemoveLeftAnAliasNaming(t *testing.T) {
	t.Parallel()
	const src = "x-t: &s {description: d}\nx-use: *s\nx-merged: {<<: *s, k: 1}\nx-outer: &o {inner: *s}\nx-twice: *o\n" +
		"x-gone: {a: &g {z: 1}, b: *g}\n"
	_, root, diags := applied(t, src, v11+"  - target: $['x-t']\n    remove: true\n  - target: $['x-gone']\n    remove: true\n", false)
	sameCodes(t, []string{"warning " + diag.OverlayAction}, diags)

	sameValue(t, "{x-use: {description: d}, x-merged: {<<: {description: d}, k: 1}, x-outer: {inner: {description: d}}, "+
		"x-twice: {inner: {description: d}}}", root)
	assert.Zero(t, aliasNodesNaming(root, func(target *yaml.Node) bool { return !reaches(root, target) }),
		"no alias is left naming a node the tree does not hold")
	_, use := member(t, root, "x-use")
	assert.Equal(t, yaml.MappingNode, use.Kind, "the alias is a copy of what it named")
	assert.Equal(t, 1, use.Line, "keeping the line the source writes the value at")
	_, twice := member(t, root, "x-twice")
	assert.Equal(t, yaml.AliasNode, twice.Kind, "an alias whose anchor the tree still holds is left alone")
}

// TestRepairAliases_KeepsTheWriterOfWhatItCopies pins the attribution of a
// copy a remove's repair makes: a node the overlay wrote into the anchor
// before removing it is the overlay's at the alias's position, and one the
// source declares stays the source's, as the value the alias stands for is the
// source's.
func TestRepairAliases_KeepsTheWriterOfWhatItCopies(t *testing.T) {
	t.Parallel()
	origin, _, diags := applied(t, "x-r: &r {k: v}\nx-meta: *r\n",
		v11+"  - target: $['x-r']\n    update: {k2: w}\n  - target: $['x-r']\n    remove: true\n", false)
	sameCodes(t, []string{"warning " + diag.OverlayAction, "warning " + diag.OverlayAction}, diags)

	assert.Equal(t, overlayIndex, origin.Attribution().WriterAt("/x-meta/k2"), "the overlay wrote k2")
	assert.Equal(t, 0, origin.Attribution().WriterAt("/x-meta/k"), "the source declares k")
	assert.Equal(t, 0, origin.Attribution().WriterAt("/x-meta"), "and the mapping holding both")
}

// aliasNodesNaming counts the alias nodes a Content walk from root reaches
// whose target names says of.
func aliasNodesNaming(root *yaml.Node, names func(*yaml.Node) bool) int {
	count := 0
	stack := []*yaml.Node{root}
	for len(stack) > 0 {
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if n.Kind == yaml.AliasNode && names(n.Alias) {
			count++
		}
		stack = append(stack, n.Content...)
	}
	return count
}

// reaches reports whether a Content walk from root reaches n.
func reaches(root, n *yaml.Node) bool {
	stack := []*yaml.Node{root}
	for len(stack) > 0 {
		top := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if top == n {
			return true
		}
		stack = append(stack, top.Content...)
	}
	return false
}
