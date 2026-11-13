// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package overlay_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
)

// aliasedSpec anchors A, which B names by an alias and C by a merge key, and
// holds D, which nothing names.
const aliasedSpec = `components:
  schemas:
    A: &a {type: object, required: [x], properties: {x: {type: string}}}
    B: *a
    C: {<<: *a, description: c}
    D: {type: object, properties: {y: {type: string}}}
`

// TestApply_WarnsOfWhatTheSourcesAliasesMakeOfAnAction pins the warnings an
// overlay draws from the source's YAML aliases and merge keys, none of which
// a selector reads through: one for the overlay, that the source writes them,
// and one for each action changing what a node an alias names holds, which
// reaches every alias of it, naming the anchor and the aliases, and none for
// an action that leaves them alone, a remove of the anchor itself among them,
// since each alias then keeps the value it stood for.
func TestApply_WarnsOfWhatTheSourcesAliasesMakeOfAnAction(t *testing.T) {
	t.Parallel()
	const unread, reaching = "warning " + diag.OverlayAction, "warning " + diag.OverlayAction
	tests := map[string]struct {
		action string
		codes  []string
	}{
		"an update of the anchor":          {"  - target: $.components.schemas.A\n    update: {description: d}\n", []string{unread, reaching}},
		"an update inside it":              {"  - target: $.components.schemas.A.properties.x\n    update: {format: f}\n", []string{unread, reaching}},
		"a primitive replaced in it":       {"  - target: $.components.schemas.A.type\n    update: string\n", []string{unread, "info " + diag.OverlayReplacedValue, reaching}},
		"an element appended in it":        {"  - target: $.components.schemas.A.required\n    update: y\n", []string{unread, reaching}},
		"a member removed from it":         {"  - target: $.components.schemas.A.properties.x\n    remove: true\n", []string{unread, reaching}},
		"a member merged into it":          {"  - target: $.components.schemas\n    update: {A: {description: d}}\n", []string{unread, reaching}},
		"an update of a node no one names": {"  - target: $.components.schemas.D\n    update: {description: d}\n", []string{unread}},
		"a remove of the anchor":           {"  - target: $.components.schemas.A\n    remove: true\n", []string{unread}},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			origin, _, diags := applied(t, aliasedSpec, v11+tc.action, false)

			require.True(t, origin.Applied(), "%+v", diags)
			sameCodes(t, tc.codes, diags)
			require.Len(t, diags, len(tc.codes))
			assert.Contains(t, diags[0].Message, "the source writes YAML aliases or `<<` merge keys")
			assert.Contains(t, diags[0].Message, "(GitHub #1834)")
			if last := diags[len(diags)-1]; len(tc.codes) > 1 && last.Code == diag.OverlayAction {
				assert.Contains(t, last.Message, "the node anchored at line 3, column 8 holds, which 2 YAML aliases name")
				assert.Equal(t, "/actions/0", string(last.Provenance.Pointer), "at the action")
			}
		})
	}
}

// TestApply_CountsTheAliasesARemoveLeaves pins the count a warning gives once
// a remove took aliases with it: an alias removed no longer names the anchor,
// and with none left a write into it reaches nothing further.
func TestApply_CountsTheAliasesARemoveLeaves(t *testing.T) {
	t.Parallel()
	const write = "  - target: $.components.schemas.A\n    update: {description: d}\n"
	_, _, diags := applied(t, aliasedSpec, v11+"  - target: $.components.schemas.B\n    remove: true\n"+write, false)
	sameCodes(t, []string{"warning " + diag.OverlayAction, "warning " + diag.OverlayAction}, diags)
	require.Len(t, diags, 2)
	assert.Contains(t, diags[1].Message, "which 1 YAML alias names")

	_, _, diags = applied(t, aliasedSpec, v11+"  - target: $.components.schemas[?@.description == 'c' || @ == 1]\n    remove: true\n"+
		"  - target: $.components.schemas.B\n    remove: true\n"+write, false)
	sameCodes(t, []string{"warning " + diag.OverlayAction}, diags)
}

// TestApply_WarnsOfAMergeKeyAnUpdateWrites pins an update writing a `<<` merge
// key into an object the tree holds: it merges as written, so the members it
// names replace none the object holds, as they would in the value the update
// stands for, and the action is warned of. A source with a merge key and no
// alias draws the overlay's warning alone, and one with neither draws none.
func TestApply_WarnsOfAMergeKeyAnUpdateWrites(t *testing.T) {
	t.Parallel()
	_, _, diags := applied(t, "info: {title: T}\n", v11+"  - target: $.info\n    update: {<<: {title: U}}\n", false)
	sameCodes(t, []string{"warning " + diag.OverlayAction}, diags)
	require.Len(t, diags, 1)
	assert.Contains(t, diags[0].Message, "writes a `<<` merge key into an object")

	_, _, diags = applied(t, "info: {title: T, x-m: {<<: {a: 1}}}\n", v11+"  - target: $.info\n    update: {x-n: 1}\n", false)
	sameCodes(t, []string{"warning " + diag.OverlayAction}, diags)
	require.Len(t, diags, 1)
	assert.Contains(t, diags[0].Message, "the source writes YAML aliases or `<<` merge keys")

	_, _, diags = applied(t, "info: {title: T}\n", v11+"  - target: $.info\n    update: {x-n: {'<<': 1}}\n", false)
	assert.Empty(t, diags, "a quoted << is a member like any other")
}
