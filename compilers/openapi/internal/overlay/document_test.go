// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package overlay_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
)

// TestRead_AppliesTheVersionsTheCompilerKnowsAtAnyPatch pins the versions an
// overlay may declare: 1.0 and 1.1, whose patch the specification asks
// tooling not to tell apart, and no other, each refused before anything
// applies.
func TestRead_AppliesTheVersionsTheCompilerKnowsAtAnyPatch(t *testing.T) {
	t.Parallel()
	const head = "info: {title: O, version: \"1\"}\nactions:\n  - target: $.info\n    update: {description: d}\n"
	for version, applies := range map[string]bool{
		"1.0.0": true, "1.0.1": true, "1.0.10": true, "1.1.0": true, "1.1.7": true,
		"1.2.0": false, "2.0.0": false, "1.0": false, "1.1.x": false, "1.1.0-rc1": false, "0.1.0": false, "1..0": false, "1.1.": false,
	} {
		origin, _, diags := applied(t, "info: {title: T}\n", "overlay: "+version+"\n"+head, false)

		assert.Equal(t, applies, origin.Applied(), "%s: %+v", version, diags)
		if !applies {
			sameCodes(t, []string{"error " + diag.OverlayInvalid}, diags)
			assert.Contains(t, diags[0].Message, version)
		}
	}
}

// TestRead_RefusesWhatTheSpecificationRequiresAndTheOverlayLacks pins each of
// the overlay's required fields, each refused on its own, so an overlay
// lacking several is told of every one.
func TestRead_RefusesWhatTheSpecificationRequiresAndTheOverlayLacks(t *testing.T) {
	t.Parallel()
	tests := map[string]struct {
		overlay string
		says    []string
	}{
		"no version":                   {"info: {title: O, version: \"1\"}\nactions: [{target: $, remove: true}]\n", []string{"declares no overlay version"}},
		"no title and no info version": {"overlay: 1.0.0\ninfo: {}\nactions: [{target: $, remove: true}]\n", []string{"no title", "no version"}},
		"an extends that is no URI":    {"overlay: 1.0.0\ninfo: {title: O, version: \"1\"}\nextends: '%zz'\nactions: [{target: $, remove: true}]\n", []string{"no URI reference"}},
		"no action":                    {"overlay: 1.0.0\ninfo: {title: O, version: \"1\"}\n", []string{"holds no action"}},
		"an action naming no target":   {v10 + "  - update: {a: b}\n", []string{"names no target"}},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			origin, _, diags := applied(t, "info: {title: T}\n", tc.overlay, false)

			assert.False(t, origin.Applied())
			require.Len(t, diags, len(tc.says), "%+v", diags)
			for i, says := range tc.says {
				assert.Equal(t, diag.OverlayInvalid, diags[i].Code)
				assert.Contains(t, diags[i].Message, says)
			}
		})
	}
}

// TestRead_RefusesACopyInAnOverlay10Document pins the field Overlay 1.1
// introduced: an Overlay 1.0 document has no copy, so one writing it is not
// the overlay it declares, and nothing applies.
func TestRead_RefusesACopyInAnOverlay10Document(t *testing.T) {
	t.Parallel()
	origin, root, diags := applied(t, "a: {}\nb: {x: 1}\n", v10+"  - target: $.a\n    copy: $.b\n", false)

	assert.False(t, origin.Applied())
	sameCodes(t, []string{"error " + diag.OverlayInvalid}, diags)
	assert.Contains(t, diags[0].Message, "Overlay 1.1")
	sameValue(t, "a: {}\nb: {x: 1}\n", root)
}

// TestRead_WarnsOfAFieldWithNoEffect pins the fields the specification gives
// no effect: an update beside a remove, a copy beside a remove or an update,
// and an action setting none of the three. Each is applied as the kind it
// takes, remove before update before copy, and warned of.
func TestRead_WarnsOfAFieldWithNoEffect(t *testing.T) {
	t.Parallel()
	const src = "info: {title: T, x-gone: 1}\nx-from: {y: 1}\n"
	tests := []struct {
		name, action, want string
	}{
		{"an update beside a remove", "  - target: $.info['x-gone']\n    remove: true\n    update: {a: b}\n",
			"{info: {title: T}, x-from: {y: 1}}"},
		{"a copy beside a remove", "  - target: $.info['x-gone']\n    remove: true\n    copy: $['x-from']\n",
			"{info: {title: T}, x-from: {y: 1}}"},
		{"a copy beside an update", "  - target: $.info\n    update: {z: 1}\n    copy: $['x-from']\n",
			"{info: {title: T, x-gone: 1, z: 1}, x-from: {y: 1}}"},
		{"no modifier", "  - target: $.info\n", "{info: {title: T, x-gone: 1}, x-from: {y: 1}}"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			origin, root, diags := applied(t, src, v11+tc.action, false)

			require.True(t, origin.Applied(), "%+v", diags)
			sameCodes(t, []string{"warning " + diag.OverlayAction}, diags)
			sameValue(t, tc.want, root)
		})
	}
}
