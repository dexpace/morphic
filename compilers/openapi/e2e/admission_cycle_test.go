// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package e2e_test

import (
	"encoding/json/v2"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
)

// TestCompile_ATypedCycleBesideBoundsIsCutAlikeFromEveryEntry holds that
// schemas on a cycle of allOf and oneOf entries, one of them writing a type,
// each place or keep their bounds alike whichever of them is read first, at
// every order of the components and of the properties entering the cycle. A
// read remembered from inside an open cycle would hand B the string A writes
// on one entry and not on the other, and B's bounds would move with the order.
func TestCompile_ATypedCycleBesideBoundsIsCutAlikeFromEveryEntry(t *testing.T) {
	t.Parallel()
	cycles := map[string][]string{
		"allOf": {
			"A: {type: string, allOf: [{$ref: '#/components/schemas/B'}], minLength: 2, minimum: 3}",
			"B: {allOf: [{$ref: '#/components/schemas/A'}], minimum: 4, maxLength: 5}",
		},
		"oneOf": {
			"A: {type: string, oneOf: [{$ref: '#/components/schemas/B'}], minLength: 2, minimum: 3}",
			"B: {allOf: [{$ref: '#/components/schemas/A'}], minimum: 4, maxLength: 5}",
		},
	}
	holders := []string{
		"Holder: {type: object, properties: {a: {$ref: '#/components/schemas/A'}, b: {$ref: '#/components/schemas/B'}}}",
		"Holder: {type: object, properties: {b: {$ref: '#/components/schemas/B'}, a: {$ref: '#/components/schemas/A'}}}",
	}
	for name, cycle := range cycles {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			nodes := map[string][]string{}
			var kept, held []string
			for _, holder := range holders {
				for _, order := range [][]string{
					{holder, cycle[0], cycle[1]}, {holder, cycle[1], cycle[0]},
					{cycle[0], cycle[1], holder}, {cycle[1], cycle[0], holder},
				} {
					doc, _ := compileAnnotationSpec(t, "cycle", admissionSpec("3.1.0", "", order...))
					kept = append(kept, strings.Join(keptEntries(t, doc, "openapi:minimum"), " "))
					held = append(held, strings.Join(boundHomes(t, doc, "min"), " "))
					for _, id := range []string{"A", "B"} {
						raw, err := json.Marshal(doc.Types[ir.TypeID("t/openapi/components/schemas/"+id)], json.Deterministic(true))
						require.NoError(t, err)
						nodes[id] = append(nodes[id], string(raw))
					}
				}
			}
			require.Len(t, kept, 8)
			assert.Len(t, slices.Compact(kept), 1, "a numeric bound is kept at the same schemas in every order: %v", kept)
			assert.Len(t, slices.Compact(held), 1, "a numeric bound is held by the same nodes in every order: %v", held)
			assert.Contains(t, kept[0], "/components/schemas/A/minimum", "A, a string, keeps its numeric bound")
			for _, id := range []string{"A", "B"} {
				require.Len(t, nodes[id], 8)
				assert.Len(t, slices.Compact(nodes[id]), 1, "%s is one node whichever way the cycle is entered", id)
			}
		})
	}
}
