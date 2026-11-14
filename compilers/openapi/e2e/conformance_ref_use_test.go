// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package e2e_test

import (
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
		p, found := propByWire(holder, wire)
		require.True(t, found, wire)
		require.NotNil(t, p.Default, wire)
		assert.Equal(t, ir.Value{Kind: ir.ValueString, Str: w.def}, *p.Default, "%s: the nearest default", wire)
		assert.Equal(t, w.desc, p.Docs.Description, "%s: the nearest description", wire)
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
	sub, ok := doc.Types["t/anon/components/schemas/Kept/$defs/Sub"].(*ir.Model)
	require.True(t, ok, "the landing of a kept not is lowered")
	require.Len(t, sub.Properties, 1)
	assert.Equal(t, "kept", sub.Properties[0].WireName)

	mid := doc.Types["t/anon/components/schemas/Through/$defs/Mid"]
	require.NotNil(t, mid, "a hop the chain only passes is lowered")
	assert.Equal(t, "mid", mid.Common().Docs.Description, "with what it writes beside its $ref")
	assert.NotNil(t, doc.Types["t/anon/components/schemas/Through/$defs/End"], "and so is the end")
	assert.NotNil(t, doc.Types["t/anon/components/schemas/Typed/$defs/Sub"], "as is a landing a lowered keyword names")

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
		if m, ok := td.(*ir.Model); ok {
			for _, p := range m.Properties {
				if p.Type.Target == "t/openapi/components/schemas/MeetingAlias" {
					naming = append(naming, p)
				}
			}
		}
	}
	require.Len(t, naming, 1)
	assert.Equal(t, "A meeting under another name.", naming[0].Docs.Description)
}
