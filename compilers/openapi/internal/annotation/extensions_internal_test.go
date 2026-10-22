package annotation

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
)

// TestExtensionsFrom_KeepsEachAndLocatesItAtItsOwnKey pins the vendor-extension
// reader: every x-* key is kept under its own pointer with the reason that says
// the format assigns it no semantics.
func TestExtensionsFrom_KeepsEachAndLocatesItAtItsOwnKey(t *testing.T) {
	t.Parallel()
	s := schemaFromYAML(t, "type: string\nx-a: 1\nx-b: {k: v}\n")

	got, diags := ExtensionsFrom(s.GetExtensions(), sourced(3), "/components/schemas/S")

	assert.Empty(t, diags)
	require.Len(t, got, 2)
	assert.Equal(t, ir.RawValue("1"), got["openapi:x-a"].Value)
	assert.Equal(t, ir.ReasonVendorExtension, got["openapi:x-a"].Reason)
	assert.Equal(t, ir.Provenance{Source: 3, Pointer: "/components/schemas/S/x-a"},
		got["openapi:x-a"].Provenance)
	assert.JSONEq(t, `{"k":"v"}`, string(got["openapi:x-b"].Value))
}

// TestExtensionsFrom_NothingWrittenIsNotAnEmptyMap pins that an absent or empty
// extension map yields no Unmodeled at all. An empty map would serialize as an
// `unmodeled: {}` the source never wrote.
func TestExtensionsFrom_NothingWrittenIsNotAnEmptyMap(t *testing.T) {
	t.Parallel()
	got, diags := ExtensionsFrom(nil, sourced(0), "/x")
	assert.Nil(t, got)
	assert.Empty(t, diags)

	got, diags = ExtensionsFrom(schemaFromYAML(t, "type: string\n").GetExtensions(), sourced(0), "/x")
	assert.Nil(t, got)
	assert.Empty(t, diags)
}

// TestExtensionsFrom_UnserializableIsWarnedNotKept pins the degradation path,
// including the case where it is the only extension written: the result must be
// a nil map rather than an empty one, with the warning still reported.
func TestExtensionsFrom_UnserializableIsWarnedNotKept(t *testing.T) {
	t.Parallel()
	s := schemaFromYAML(t, "type: string\nx-bad: .nan\n")

	got, diags := ExtensionsFrom(s.GetExtensions(), sourced(0), "/x")

	assert.Nil(t, got, "nothing was kept, so there is no map to emit")
	require.Len(t, diags, 1)
	assert.Equal(t, ir.SeverityWarning, diags[0].Severity)
	assert.Contains(t, diags[0].Message, `"x-bad"`)
}

// TestExtensionsUnder_KeysBeneathTheScope pins the scoped spelling: an object
// with no Unmodeled map of its own keys its entries under the path that says
// which object wrote them, while the entry's provenance still points at the
// extension itself.
func TestExtensionsUnder_KeysBeneathTheScope(t *testing.T) {
	t.Parallel()
	s := schemaFromYAML(t, "type: string\nx-a: 1\n")

	got, diags := ExtensionsUnder(s.GetExtensions(), sourced(2), "/info/contact", "info/contact")

	assert.Empty(t, diags)
	require.Len(t, got, 1)
	require.Contains(t, got, "openapi:info/contact/x-a")
	assert.Equal(t, ir.Provenance{Source: 2, Pointer: "/info/contact/x-a"},
		got["openapi:info/contact/x-a"].Provenance)
}

// TestExtensionsUnder_AttributesEachEntryAtItsOwnKey pins GitHub #522: an
// overlay that rewrites one extension on an owner must not carry its
// attribution onto a sibling extension the overlay never touched, since both
// entries are located through the same owner pointer plus their own key.
func TestExtensionsUnder_AttributesEachEntryAtItsOwnKey(t *testing.T) {
	t.Parallel()
	s := schemaFromYAML(t, "type: string\nx-a: 1\nx-b: 2\n")

	got, diags := ExtensionsUnder(s.GetExtensions(), overlaid("/o/x-b"), "/o", "")

	assert.Empty(t, diags)
	require.Len(t, got, 2)
	assert.Equal(t, ir.Provenance{Source: 0, Pointer: "/o/x-a"}, got["openapi:x-a"].Provenance,
		"the untouched sibling stays with the base")
	assert.Equal(t, ir.Provenance{Source: 1, Pointer: "/o/x-b"}, got["openapi:x-b"].Provenance,
		"the overlaid entry names the overlay")
}

// TestExtensionsAt_FoldsEverySiteWithoutCollision is the reason the scope
// exists: two objects writing the same x-* key onto one carrier must both
// survive, which one unscoped key cannot do.
func TestExtensionsAt_FoldsEverySiteWithoutCollision(t *testing.T) {
	t.Parallel()
	info := schemaFromYAML(t, "type: string\nx-a: 1\n")
	license := schemaFromYAML(t, "type: string\nx-a: 2\n")

	got, diags := ExtensionsAt(sourced(0),
		ExtensionSite{Scope: "info", Owner: "/info", Ext: info.GetExtensions()},
		ExtensionSite{Scope: "info/license", Owner: "/info/license", Ext: license.GetExtensions()},
		ExtensionSite{Scope: "components", Owner: "/components", Ext: nil},
	)

	assert.Empty(t, diags)
	require.Len(t, got, 2, "one key spelling, two objects, two entries")
	assert.Equal(t, ir.RawValue("1"), got["openapi:info/x-a"].Value)
	assert.Equal(t, ir.RawValue("2"), got["openapi:info/license/x-a"].Value)
}

// TestExtensionsAt_ReportsEverySiteThatFailed holds the diagnostics to the same
// completeness as the entries: a site whose extension cannot be serialized keeps
// nothing, and the fold must still carry its warning out (GitHub #144's rule,
// applied to the multi-site form).
func TestExtensionsAt_ReportsEverySiteThatFailed(t *testing.T) {
	t.Parallel()
	bad := schemaFromYAML(t, "type: string\nx-bad: .nan\n")

	got, diags := ExtensionsAt(sourced(0), ExtensionSite{Scope: "info", Owner: "/info", Ext: bad.GetExtensions()})

	assert.Nil(t, got, "nothing was kept, so there is no map to emit")
	require.Len(t, diags, 1)
	assert.Equal(t, ir.SeverityWarning, diags[0].Severity)
}

// TestMergeUnmodeled_AllocatesOnlyOnFirstWrite pins the overlay: merging nothing
// leaves the destination exactly as it was — including nil, which must not
// become an empty map.
func TestMergeUnmodeled_AllocatesOnlyOnFirstWrite(t *testing.T) {
	t.Parallel()
	assert.Nil(t, MergeUnmodeled(nil, nil))
	assert.Nil(t, MergeUnmodeled(nil, ir.Unmodeled{}))

	src := ir.Unmodeled{"a": {Reason: ir.ReasonOutOfScope}}
	assert.Equal(t, src, MergeUnmodeled(nil, src), "a nil destination is allocated to hold the first entry")

	dst := ir.Unmodeled{"b": {Reason: ir.ReasonNoIRHome}}
	assert.Equal(t, ir.Unmodeled{
		"a": {Reason: ir.ReasonOutOfScope}, "b": {Reason: ir.ReasonNoIRHome},
	}, MergeUnmodeled(dst, src))
}
