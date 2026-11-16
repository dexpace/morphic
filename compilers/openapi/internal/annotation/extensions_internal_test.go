// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package annotation

import (
	"slices"
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

// TestExtensionsNamed_ReadsOnlyWhatItIsAsked pins the reader a use site makes of
// its referent: the names it is given, keyed and valued as ExtensionsFrom's so
// one reader takes either map, and nothing where nothing of them is written, as a
// nil map and not an empty one a carrier would have to tell apart.
func TestExtensionsNamed_ReadsOnlyWhatItIsAsked(t *testing.T) {
	t.Parallel()
	s := schemaFromYAML(t, "type: string\nx-a: 1\nx-b: {k: v}\n")

	got := ExtensionsNamed(s.GetExtensions(), slices.Values([]string{"x-a", "x-absent"}))
	require.Len(t, got, 1)
	assert.Equal(t, ir.RawValue("1"), got["openapi:x-a"].Value)
	assert.Equal(t, ir.ReasonVendorExtension, got["openapi:x-a"].Reason)

	assert.Nil(t, ExtensionsNamed(s.GetExtensions(), slices.Values([]string{"x-absent"})))
	assert.Nil(t, ExtensionsNamed(nil, slices.Values([]string{"x-a"})))
	assert.Nil(t, ExtensionsNamed(schemaFromYAML(t, "type: string\n").GetExtensions(), slices.Values([]string{"x-a"})))

	unserializable := schemaFromYAML(t, "type: string\nx-bad: .nan\n")
	assert.Nil(t, ExtensionsNamed(unserializable.GetExtensions(), slices.Values([]string{"x-bad"})),
		"left out, as ExtensionsFrom warns of it where it is written")

	beside := schemaFromYAML(t, "type: string\nx-bad: .nan\nx-good: 1\n")
	got = ExtensionsNamed(beside.GetExtensions(), slices.Values([]string{"x-bad", "x-good"}))
	require.Len(t, got, 1, "one value left out leaves the names after it")
	assert.Equal(t, ir.RawValue("1"), got["openapi:x-good"].Value)
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
	assert.Equal(t, ir.Provenance{Pointer: "/o/x-b", WrittenBy: 1}, got["openapi:x-b"].Provenance,
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

// TestSchemaExtensions_ReportsInTheOrderTheStatementLists pins the schema
// form of ExtensionsUnder to the statement's list: each extension kept as
// ExtensionsUnder keeps it, and two that do not serialize reported in the
// order the schema writes them, which is not their names' order.
func TestSchemaExtensions_ReportsInTheOrderTheStatementLists(t *testing.T) {
	t.Parallel()
	s := standaloneSchema(schemaFromYAML(t, "type: string\nx-b: .nan\nx-c: 1\nx-a: .nan\n"))

	got, diags := SchemaExtensions(s.Extensions, sourced(2), "/o", "xml")

	require.Len(t, got, 1)
	assert.Equal(t, ir.UnmodeledEntry{Reason: ir.ReasonVendorExtension, Value: ir.RawValue("1"),
		Provenance: ir.Provenance{Source: 2, Pointer: "/o/x-c"}}, got["openapi:xml/x-c"])
	require.Len(t, diags, 2)
	assert.Contains(t, diags[0].Message, `"x-b"`)
	assert.Contains(t, diags[1].Message, `"x-a"`)

	got, diags = SchemaExtensions(nil, sourced(0), "/o", "")
	assert.Nil(t, got, "nothing listed is no map")
	assert.Empty(t, diags)
}

// TestSchemaExtensionsNamed_ReadsOnlyWhatItIsAsked is
// TestExtensionsNamed_ReadsOnlyWhatItIsAsked for the statement's list.
func TestSchemaExtensionsNamed_ReadsOnlyWhatItIsAsked(t *testing.T) {
	t.Parallel()
	s := standaloneSchema(schemaFromYAML(t, "type: string\nx-a: 1\nx-bad: .nan\nx-b: {k: v}\n"))

	got := SchemaExtensionsNamed(s.Extensions, slices.Values([]string{"x-b", "x-bad", "x-absent"}))
	require.Len(t, got, 1, "a value that does not serialize is left out")
	assert.Equal(t, ir.UnmodeledEntry{Reason: ir.ReasonVendorExtension, Value: ir.RawValue(`{"k":"v"}`)},
		got["openapi:x-b"])
	assert.Nil(t, SchemaExtensionsNamed(s.Extensions, slices.Values([]string{"x-absent"})))
	assert.Nil(t, SchemaExtensionsNamed(nil, slices.Values([]string{"x-a"})))
}

// TestExtensionPairs_StopsWhenItsReaderDoes pins the iterator over a list to
// its contract: it yields the list in order and stops at the first false.
func TestExtensionPairs_StopsWhenItsReaderDoes(t *testing.T) {
	t.Parallel()
	s := standaloneSchema(schemaFromYAML(t, "x-b: 1\nx-a: 2\n"))
	var names []string
	for name := range extensionPairs(s.Extensions) {
		names = append(names, name)
		if name == "x-b" {
			break
		}
	}
	assert.Equal(t, []string{"x-b"}, names)
}
