// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package overlay_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/ir"
)

// invalidAt returns the pointer of each invalid-overlay finding in diags, in
// order.
func invalidAt(diags []ir.Diagnostic) []string {
	var out []string
	for _, d := range diags {
		if d.Code == diag.OverlayInvalid {
			out = append(out, string(d.Provenance.Pointer))
		}
	}
	return out
}

// reusable is an Overlay 1.2 document whose one reference is written as ref,
// naming one of the two reusable actions it holds.
func reusable(ref, extra string) string {
	return "overlay: 1.2.0\ninfo: {title: o, version: \"1\"}\n" + extra + `components:
  actions:
    describe:
      description: reusable
      fields:
        description: from fields
        update: {description: d}
    'a/b~c':
      fields:
        update: {description: escaped}
actions:
  - $ref: '` + ref + `'
    target: $.info
`
}

// TestApply_AppliesAReusableActionAtTheReferencesTarget pins Overlay 1.2's
// reusable action: a reference applies what the component's fields write, at
// the reference's own target, and the overlay's entry in Document.Sources
// names the version it applied and the $self it wrote.
func TestApply_AppliesAReusableActionAtTheReferencesTarget(t *testing.T) {
	t.Parallel()
	ov := reusable("#/components/actions/describe", "$self: https://example.com/o.yaml\n")

	origin, root, diags := applied(t, head, ov, false)

	require.True(t, origin.Applied(), "%+v", diags)
	sameCodes(t, nil, diags)
	_, description := member(t, root, "info", "description")
	assert.Equal(t, "d", description.Value, "the component's update, not its description field")
	assert.Equal(t, "overlay@1.2.0", origin.Source().Format)
	assert.Equal(t, "https://example.com/o.yaml", origin.Source().SelfURI)
}

// TestApply_ReadsTheKeyAReferenceNamesDecoded pins the reference's key: RFC
// 6901 decodes it, so a component whose name holds a slash and a tilde is
// named by their escapes and by nothing else, and a reference that names no
// component, or one of another document, is refused at the reference.
func TestApply_ReadsTheKeyAReferenceNamesDecoded(t *testing.T) {
	t.Parallel()
	origin, root, diags := applied(t, head, reusable("#/components/actions/a~1b~0c", ""), false)
	require.True(t, origin.Applied(), "%+v", diags)
	_, description := member(t, root, "info", "description")
	assert.Equal(t, "escaped", description.Value)

	for name, ref := range map[string]string{
		"the key undecoded":            "#/components/actions/a/b~c",
		"a component nothing declares": "#/components/actions/none",
		"another document's":           "other.yaml#/components/actions/describe",
	} {
		origin, root, diags := applied(t, head, reusable(ref, ""), false)

		assert.False(t, origin.Applied(), name)
		assert.Equal(t, []string{"/actions/0/$ref"}, invalidAt(diags), "%s: %+v", name, diags)
		sameValue(t, head, root)
	}
}

// TestApply_RefusesWhatAReusableActionAndAReferenceMayNotWrite pins the
// fields each may not write at Overlay 1.2, each refused at the field and
// nothing applied, and the one rule 1.1 does not state: an extends holding a
// fragment is read there.
func TestApply_RefusesWhatAReusableActionAndAReferenceMayNotWrite(t *testing.T) {
	t.Parallel()
	const ref = "#/components/actions/describe"
	tests := map[string]struct{ overlay, at string }{
		"a reference writing an update": {
			"overlay: 1.2.0\ninfo: {title: o, version: \"1\"}\ncomponents:\n  actions:\n    describe:\n      fields: {update: {a: b}}\n" +
				"actions:\n  - $ref: '" + ref + "'\n    target: $.info\n    update: {x: 1}\n", "/actions/0/update"},
		"a reference writing a remove": {
			"overlay: 1.2.0\ninfo: {title: o, version: \"1\"}\ncomponents:\n  actions:\n    describe:\n      fields: {update: {a: b}}\n" +
				"actions:\n  - $ref: '" + ref + "'\n    target: $.info\n    remove: true\n", "/actions/0/remove"},
		"fields writing a target": {
			"overlay: 1.2.0\ninfo: {title: o, version: \"1\"}\ncomponents:\n  actions:\n    describe:\n      fields: {target: $, update: {a: b}}\n" +
				"actions:\n  - $ref: '" + ref + "'\n    target: $.info\n", "/components/actions/describe/fields/target"},
		"a $self holding a fragment": {
			"overlay: 1.2.0\ninfo: {title: o, version: \"1\"}\n$self: 'https://example.com/o.yaml#f'\n" +
				"actions:\n  - target: $.info\n    update: {a: b}\n", "/$self"},
		"an extends holding a fragment": {
			"overlay: 1.2.0\ninfo: {title: o, version: \"1\"}\nextends: 'a.yaml#f'\n" +
				"actions:\n  - target: $.info\n    update: {a: b}\n", "/extends"},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			origin, root, diags := applied(t, head, tc.overlay, false)

			assert.False(t, origin.Applied())
			assert.Equal(t, []string{tc.at}, invalidAt(diags), "%+v", diags)
			sameValue(t, head, root)
		})
	}

	origin, _, diags := applied(t, head, "overlay: 1.1.0\ninfo: {title: o, version: \"1\"}\nextends: 'a.yaml#f'\n"+
		"actions:\n  - target: $.info\n    update: {a: b}\n", false)
	assert.True(t, origin.Applied(), "1.1 states no rule for an extends fragment: %+v", diags)
	assert.Empty(t, invalidAt(diags))
}

// TestApply_RefusesAFieldTheVersionDoesNotDefine pins the reading of an
// overlay document by the fields its version gives each object: a misspelled
// field, a field of the wrong type and a field no object has are an invalid
// overlay at the field, and nothing applies, so a misspelled update does not
// apply as nothing.
func TestApply_RefusesAFieldTheVersionDoesNotDefine(t *testing.T) {
	t.Parallel()
	const info = "info: {title: o, version: \"1\"}\n"
	tests := map[string]struct{ overlay, at string }{
		"a misspelled update": {"overlay: 1.1.0\n" + info + "actions:\n  - target: $.info\n    udpate: {description: d}\n", "/actions/0/udpate"},
		"a title that is no string": {"overlay: 1.1.0\ninfo: {title: 100, version: \"1\"}\nactions:\n  - target: $.info\n    update: {description: d}\n",
			"/info/title"},
		"an action description that is no string": {"overlay: 1.1.0\n" + info +
			"actions:\n  - target: $.info\n    description: 10\n    update: {description: d}\n", "/actions/0/description"},
		"a field of the root no object has": {"overlay: 1.1.0\n" + info + "invalidProperty: true\n" +
			"actions:\n  - target: $.info\n    update: {description: d}\n", "/invalidProperty"},
		"a field of an info no object has, at 1.0": {"overlay: 1.0.0\ninfo: {title: o, version: \"1\", summary: s}\n" +
			"actions:\n  - target: $.info\n    update: {description: d}\n", "/info/summary"},
		"a description of an info, at 1.0": {"overlay: 1.0.0\ninfo: {title: o, version: \"1\", description: d}\n" +
			"actions:\n  - target: $.info\n    update: {description: d}\n", "/info/description"},
		"a copy of an action, at 1.0": {"overlay: 1.0.0\n" + info + "actions:\n  - target: $.info\n    copy: $['x-from']\n", "/actions/0/copy"},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			origin, root, diags := applied(t, head, tc.overlay, false)

			assert.False(t, origin.Applied())
			assert.Equal(t, []string{tc.at}, invalidAt(diags), "%+v", diags)
			sameValue(t, head, root)
		})
	}
}

// TestApply_ReadsEachVersionOfAnOverlayAsItsOwnRulesDo pins the three versions
// a document may declare: 1.0, 1.1 and 1.2 each apply an update, an extension
// is read at each object whatever the version, and 1.1 and 1.2 read what 1.0
// does not define, a description of the info and a copy of an action.
func TestApply_ReadsEachVersionOfAnOverlayAsItsOwnRulesDo(t *testing.T) {
	t.Parallel()
	for _, version := range []string{"1.0.0", "1.1.0", "1.2.0"} {
		ov := "overlay: " + version + "\ninfo: {title: o, version: \"1\", x-n: 1}\nx-root: 1\n" +
			"actions:\n  - target: $.info\n    x-a: 1\n    update: {description: d}\n"

		origin, root, diags := applied(t, head, ov, false)

		require.True(t, origin.Applied(), "%s: %+v", version, diags)
		_, description := member(t, root, "info", "description")
		assert.Equal(t, "d", description.Value, version)
		assert.Equal(t, "overlay@"+version, origin.Source().Format)
	}
	for _, version := range []string{"1.1.0", "1.2.0"} {
		ov := "overlay: " + version + "\ninfo: {title: o, version: \"1\", description: about}\n" +
			"actions:\n  - target: $.info\n    copy: $['x-from']\n"

		origin, _, diags := applied(t, head+"x-from: {k: v}\n", ov, true)

		assert.True(t, origin.Applied(), "%s reads an info description and a copy: %+v", version, diags)
	}
}
