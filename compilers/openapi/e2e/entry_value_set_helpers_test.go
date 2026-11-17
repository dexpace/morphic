// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package e2e_test

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"maps"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
)

// entryIndexed matches a source pointer, or an ID spelling one, that ends at
// an entry of an allOf.
var entryIndexed = regexp.MustCompile(`/allOf/[0-9]+$`)

// requireNoEntryIndexedType holds that no type of doc has an ID or a source
// pointer ending at an entry of an allOf, so none stands at an entry where it
// is the position that owns the node.
func requireNoEntryIndexedType(t *testing.T, doc *ir.Document) {
	t.Helper()
	for _, id := range slices.Sorted(maps.Keys(doc.Types)) {
		assert.False(t, entryIndexed.MatchString(string(id)), "%s stands at an entry of an allOf", id)
		at := string(doc.Types[id].Common().Provenance.Pointer)
		assert.False(t, entryIndexed.MatchString(at), "%s is lowered from the entry at %s", id, at)
	}
}

// entryKeptUnder returns each Unmodeled entry of doc, whatever node holds it,
// whose source is written at or under at.
func entryKeptUnder(doc *ir.Document, at jsontext.Pointer) []string {
	var keys []string
	for _, site := range unmodeledSites(doc) {
		p := string(site.entry.Provenance.Pointer)
		if p == string(at) || strings.HasPrefix(p, string(at)+"/") {
			keys = append(keys, site.key+" at "+p)
		}
	}
	slices.Sort(keys)
	return keys
}

// entryKept is the Unmodeled entries a node is expected to hold, each kept
// for want of a home, keyed as the node keeps them.
func entryKept(keys ...string) map[string]ir.UnmodeledReason {
	kept := map[string]ir.UnmodeledReason{}
	for _, key := range keys {
		kept[key] = ir.ReasonDegradedLowering
	}
	return kept
}

// entryTypesUnder returns, by ID, the JSON of every type of doc lowered from
// the component schema named or from beneath it.
func entryTypesUnder(t *testing.T, doc *ir.Document, name string) map[ir.TypeID]string {
	t.Helper()
	prefixes := []string{
		string(componentID(name)),
		"t/anon/components/schemas/" + name,
		"t/composed/components/schemas/" + name,
	}
	out := map[ir.TypeID]string{}
	for id, td := range doc.Types {
		for _, prefix := range prefixes {
			if string(id) == prefix || strings.HasPrefix(string(id), prefix+"/") {
				raw, err := json.Marshal(td, json.Deterministic(true))
				require.NoError(t, err)
				out[id] = string(raw)
			}
		}
	}
	return out
}

// requireRepeatedUsesLeaveNodesAlone holds that the component Pos, written
// as schema beside the components given, lowers to the same types where one
// property uses it as where two properties, a list's element and a query
// parameter do: a position lowered once per use that reaches it keeps what
// its first visit built.
func requireRepeatedUsesLeaveNodesAlone(t *testing.T, version, schema string, components ...string) {
	t.Helper()
	const use = "{$ref: '#/components/schemas/Pos'}"
	paths := "paths:\n  /x:\n    get:\n      operationId: x\n      parameters:\n" +
		"        - {name: q, in: query, schema: " + use + "}\n      responses: {\"204\": {description: ok}}\n"
	once, _ := compileAnnotationSpec(t, "once", admissionSpec(version, "", append([]string{
		"Pos: " + schema, "Holder: {type: object, properties: {p: " + use + "}}",
	}, components...)...))
	twice, _ := compileAnnotationSpec(t, "twice", admissionSpec(version, paths, append([]string{
		"Pos: " + schema,
		"Holder: {type: object, properties: {p: " + use + ", q: " + use + ", r: {type: array, items: " + use + "}}}",
	}, components...)...))
	want := entryTypesUnder(t, once, "Pos")
	require.NotEmpty(t, want, "the component lowers to a type")
	assert.Equal(t, want, entryTypesUnder(t, twice, "Pos"), "the uses decided what the position's nodes hold")
}
