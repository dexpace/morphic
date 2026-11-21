// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package reftable_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/compilers/openapi/internal/reftable"
)

// TestTable_ANestedResourceOfADocumentNoPassReachesLeadsNowhere pins that the
// graph of which schema resource reaches which holds no edge from a resource
// to one nested in it in a document the pass did not reach: a document read as
// a location before another declared its URI reaches nothing, so the
// `$dynamicAnchor` its outer resource declares does not reach its nested
// one's. The same document reached, it does.
func TestTable_ANestedResourceOfADocumentNoPassReachesLeadsNowhere(t *testing.T) {
	t.Parallel()
	docs := map[string]string{
		"/a.yaml": "S: {$id: 'https://x.test/outer', properties: {a: {$id: 'https://x.test/inner', $dynamicAnchor: n}, " +
			"z: {$dynamicAnchor: n}}}\n$defs: {T: {type: string}}\n",
		"/z.yaml": "S: {$id: 'a.yaml', $defs: {T: {type: string}}}\n",
	}
	const dyn = "    D: {$dynamicAnchor: n}\n"
	cfg := resolving
	cfg.Open = func(name, _ string) (*yaml.Node, error) { return openapitest.YAMLDocument(t, docs[name]), nil }
	anchorOf := func(schemas string) (reftable.DynamicAnchor, bool) {
		table := bound(t, head+"components:\n  schemas:\n"+schemas+dyn, cfg)
		a := docNamed(t, table, "/a.yaml")
		return table.DynamicAnchor(a, "n"), table.Document(a).Unreached
	}

	reached, unreached := anchorOf("    A: {$ref: 'a.yaml#/S/properties/a'}\n")
	require.False(t, unreached)
	require.Equal(t, 2, reached.Count)
	assert.True(t, reached.Reached, "in a reached document the outer resource reaches the nested one")

	passed, unreached := anchorOf("    A: {$ref: 'a.yaml#/$defs/T'}\n    Z: {$ref: 'z.yaml#/S'}\n")
	require.True(t, unreached, "a.yaml is read, and no pass that stands reaches it")
	require.Equal(t, 2, passed.Count)
	assert.False(t, passed.Reached, "a document no pass reaches leads nowhere")
}

// TestTable_AReferenceThatFailsToResolveLeadsNowhere pins that a hop that
// resolves to nothing, a $ref's or a discriminator mapping value's, links its
// resource to no other: it has no target, which a zero position would place
// at the source's own resource, so a resource declaring a `$dynamicAnchor` the
// source declares too does not reach the source's by it. One that does
// resolve to the source's schema does.
func TestTable_AReferenceThatFailsToResolveLeadsNowhere(t *testing.T) {
	t.Parallel()
	const src = head + "components:\n  schemas:\n    D: {$dynamicAnchor: n}\n    U: {$ref: 'o.yaml#/S'}\n"
	tests := []struct {
		name, inside string
		reaches      bool
	}{
		{"an unresolved $ref", "properties: {p: {$ref: '#/missing'}}", false},
		{"an unresolved mapping value", "discriminator: {propertyName: k, mapping: {x: '#/missing'}}", false},
		{"a $ref to the source's schema", "properties: {p: {$ref: 'spec.yaml#/components/schemas/D'}}", true},
		{"a mapping value naming the source's schema", "discriminator: {propertyName: k, mapping: {x: 'spec.yaml#/components/schemas/D'}}", true},
	}
	for _, tc := range tests {
		other := "S: {$dynamicAnchor: n, " + tc.inside + "}\n"
		table := bound(t, src, withDocuments(map[string]string{"o.yaml": other}))

		require.Equal(t, reftable.Resolved, entryAt(t, table, "/components/schemas/U").Hop.Status, tc.name)
		anchor := table.DynamicAnchor(reftable.Source, "n")
		require.Equal(t, 1, anchor.Count, tc.name)
		assert.Equal(t, tc.reaches, anchor.Reached, tc.name)
	}
}
