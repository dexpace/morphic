// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package e2e_test

import (
	"encoding/json/v2"
	"maps"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
)

// assertRefChainAnnotations holds ref-chain-annotations: a property takes each
// field from the nearest hop of its chain that writes it.
func assertRefChainAnnotations(t *testing.T, doc *ir.Document, diags []ir.Diagnostic) {
	t.Helper()
	holder, ok := typeByName(doc, "Holder").(*ir.Model)
	require.True(t, ok, "Holder lowers to a model")
	want := map[string]struct{ def, desc string }{
		"f":   {"f", "from-fine"},
		"b":   {"f", "from-fine"},
		"d":   {"l", "from-docs"},
		"own": {"f", "from-use"},
	}
	for wire, w := range want {
		property, found := propertyByWire(holder, wire)
		require.True(t, found, wire)
		require.NotNil(t, property.Default, wire)
		assert.Equal(t, ir.Value{Kind: ir.ValueString, String: w.def}, *property.Default, "%s: the nearest default", wire)
		assert.Equal(t, w.desc, property.Docs.Description, "%s: the nearest description", wire)
	}

	assert.Equal(t, []string{
		"info openapi/degraded-construct /components/schemas/Fine/default",
		"info openapi/degraded-construct /components/schemas/Leaf/default",
	}, findingsAt(diags), "no finding but the two declarations' defaults kept verbatim")
}

// assertKeptRefLandings holds kept-ref-landings: every position a $ref of a
// kept keyword lands on is lowered, a chain of them hop by hop, and the keyword
// is still kept whole.
func assertKeptRefLandings(t *testing.T, doc *ir.Document, diags []ir.Diagnostic) {
	t.Helper()
	sub, ok := doc.Types["type/anonymous/components/schemas/Kept/$defs/Sub"].(*ir.Model)
	require.True(t, ok, "the landing of a kept not is lowered")
	require.Len(t, sub.Properties, 1)
	assert.Equal(t, "kept", sub.Properties[0].WireName)

	mid := doc.Types["type/anonymous/components/schemas/Through/$defs/Mid"]
	require.NotNil(t, mid, "a hop the chain only passes is lowered")
	assert.Equal(t, "mid", mid.Common().Docs.Description, "with what it writes beside its $ref")
	assert.NotNil(t, doc.Types["type/anonymous/components/schemas/Through/$defs/End"], "and so is the end")
	assert.NotNil(t, doc.Types["type/anonymous/components/schemas/Typed/$defs/Sub"], "as is a landing a lowered keyword names")

	kept, ok := typeByName(doc, "Kept").(*ir.Model)
	require.True(t, ok)
	entry := unmodeledEntry(t, kept.Unmodeled, "openapi:not")
	assert.JSONEq(t, `{"$ref":"#/components/schemas/Kept/$defs/Sub"}`, string(entry.Value))

	assert.Equal(t, []string{
		"info openapi/validation-only-keyword /components/schemas/Kept",
		"info openapi/validation-only-keyword /components/schemas/Through",
	}, findingsAt(diags), "nothing but the two kept keywords is reported")
}

// TestConformance_APropertyNamingAnAliasReadsItsDescription pins, in
// allof-redeclared-models, the one property that names the alias MeetingAlias,
// which writes a description beside its $ref: the property reads it, as it reads
// the description of any hop of its chain.
func TestConformance_APropertyNamingAnAliasReadsItsDescription(t *testing.T) {
	t.Parallel()
	doc, _ := parseCorpus(t, "allof-redeclared-models")

	var naming []ir.Property
	for _, td := range doc.Types {
		if model, ok := td.(*ir.Model); ok {
			for _, property := range model.Properties {
				if property.Type.Target == "type/openapi/components/schemas/MeetingAlias" {
					naming = append(naming, property)
				}
			}
		}
	}
	require.Len(t, naming, 1)
	assert.Equal(t, "A meeting under another name.", naming[0].Docs.Description)
}

// assertKeptRefUnrenderable holds kept-ref-unrenderable to
// docs/compilers/openapi.md#allof-refinement: the $refs a value set narrows
// are each kept under openapi:$ref/<rank>, ranked by reference then by entry
// as written, entries written alike one key at the position, and an entry
// that cannot render, ranked by where it is written, alike to none. The
// document is refused for the length, and what it keeps is the same.
func assertKeptRefUnrenderable(t *testing.T, doc *ir.Document, diags []ir.Diagnostic) {
	type kept struct{ key, ref, at string }
	for name, want := range map[string][]kept{
		"Distinct": {{"openapi:$ref/0", "#/components/schemas/A", "/components/schemas/Distinct/allOf/1"},
			{"openapi:$ref/1", "#/components/schemas/B", "/components/schemas/Distinct/allOf/0"}},
		"Alike": {{"openapi:$ref/0", "#/components/schemas/A", "/components/schemas/Alike/allOf/1"},
			{"openapi:$ref/1", "#/components/schemas/A", "/components/schemas/Alike/allOf/0"}},
		"Twice": {{"openapi:$ref/0", "#/components/schemas/A", "/components/schemas/Twice/allOf/0"},
			{"openapi:$ref/1", "#/components/schemas/A", "/components/schemas/Twice/allOf/1"}},
		"Same": {{"openapi:$ref", "#/components/schemas/A", "/components/schemas/Same"}},
	} {
		enum, ok := typeByName(doc, name).(*ir.Enum)
		require.True(t, ok, "%s lowers as its value set", name)
		var got []kept
		for _, key := range slices.Sorted(maps.Keys(enum.Unmodeled)) {
			if !strings.HasPrefix(key, "openapi:$ref") {
				continue
			}
			entry := unmodeledEntry(t, enum.Unmodeled, key)
			var ref string
			require.NoError(t, json.Unmarshal(entry.Value, &ref), name)
			got = append(got, kept{key, ref, string(entry.Provenance.Pointer)})
		}
		assert.Equal(t, want, got, name)
	}
}
