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

// TestApply_DrawsNoFindingForTheAliasesTheSourceWrites pins that a source's
// YAML aliases and merge keys are no warning: an overlay reads the value they
// stand for and writes one position of it, so an action on an anchor, on
// something inside it, on a node no one names or on the anchor's removal each
// applies with nothing to say, but for a primitive it replaces.
func TestApply_DrawsNoFindingForTheAliasesTheSourceWrites(t *testing.T) {
	t.Parallel()
	replaced := []string{"info " + diag.OverlayReplacedValue}
	tests := map[string]struct {
		action string
		codes  []string
	}{
		"an update of the anchor":          {"  - target: $.components.schemas.A\n    update: {description: d}\n", nil},
		"an update inside it":              {"  - target: $.components.schemas.A.properties.x\n    update: {format: f}\n", nil},
		"a primitive replaced in it":       {"  - target: $.components.schemas.A.type\n    update: string\n", replaced},
		"an element appended in it":        {"  - target: $.components.schemas.A.required\n    update: y\n", nil},
		"a member removed from it":         {"  - target: $.components.schemas.A.properties.x\n    remove: true\n", nil},
		"a member merged into it":          {"  - target: $.components.schemas\n    update: {A: {description: d}}\n", nil},
		"an update of a node no one names": {"  - target: $.components.schemas.D\n    update: {description: d}\n", nil},
		"a remove of the anchor":           {"  - target: $.components.schemas.A\n    remove: true\n", nil},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			origin, _, diags := applied(t, aliasedSpec, v11+tc.action, false)

			require.True(t, origin.Applied(), "%+v", diags)
			sameCodes(t, tc.codes, diags)
		})
	}
}

// TestApply_RemovesAnAliasAndWritesTheAnchorWithoutAFinding pins a remove of
// one alias followed by an update of the anchor, and a remove of a node that
// holds the aliases of another: the later action reads what the earlier left,
// and neither draws a finding for the count of aliases that remain.
func TestApply_RemovesAnAliasAndWritesTheAnchorWithoutAFinding(t *testing.T) {
	t.Parallel()
	const write = "  - target: $.components.schemas.A\n    update: {description: d}\n"
	_, root, diags := applied(t, aliasedSpec, v11+"  - target: $.components.schemas.B\n    remove: true\n"+write, false)
	sameCodes(t, nil, diags)
	assert.Equal(t, []string{"/components/schemas/A"}, holders(root, "description", "d"), "the anchor is described, and the alias removed is gone")

	_, _, diags = applied(t, aliasedSpec, v11+"  - target: $.components.schemas[?@.description == 'c' || @ == 1]\n    remove: true\n"+
		"  - target: $.components.schemas.B\n    remove: true\n"+write, false)
	sameCodes(t, nil, diags)
}

// TestApply_WritesAMergeKeyAnUpdateNamesWithoutAFinding pins an update that
// writes a `<<` key into an object the tree holds, and a source with a merge
// key and no alias: neither draws a finding, and the overlay applies.
func TestApply_WritesAMergeKeyAnUpdateNamesWithoutAFinding(t *testing.T) {
	t.Parallel()
	origin, _, diags := applied(t, "info: {title: T}\n", v11+"  - target: $.info\n    update: {<<: {title: U}}\n", false)
	assert.True(t, origin.Applied(), "%+v", diags)
	sameCodes(t, nil, diags)

	_, _, diags = applied(t, "info: {title: T, x-m: {<<: {a: 1}}}\n", v11+"  - target: $.info\n    update: {x-n: 1}\n", false)
	sameCodes(t, nil, diags)

	_, _, diags = applied(t, "info: {title: T}\n", v11+"  - target: $.info\n    update: {x-n: {'<<': 1}}\n", false)
	assert.Empty(t, diags, "a quoted << is a member like any other")
}
