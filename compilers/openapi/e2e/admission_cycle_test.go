// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package e2e_test

import (
	"encoding/json/v2"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
)

// TestCompile_ATypedCycleBesideBoundsIsCutAlikeFromEveryEntry holds that
// schemas on a cycle of allOf and oneOf entries, one of them writing a type,
// each place their bounds by their own type whichever of them is read first:
// A, a string, keeps its numeric bound and B, writing no type, holds both of
// its own, at every order of the components and of the properties entering
// the cycle. A read remembered from inside an open cycle would hand B the
// string A writes on one entry and not on the other.
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
			for _, holder := range holders {
				for _, order := range [][]string{
					{holder, cycle[0], cycle[1]}, {holder, cycle[1], cycle[0]},
					{cycle[0], cycle[1], holder}, {cycle[1], cycle[0], holder},
				} {
					doc, _ := compileAnnotationSpec(t, "cycle", admissionSpec("3.1.0", "", order...))
					assert.Equal(t, []string{"/components/schemas/A/minimum"}, keptEntries(t, doc, "openapi:minimum"),
						"only A, a string, keeps its numeric bound: %v", order)
					assert.Equal(t, []string{"t/openapi/components/schemas/B"}, boundHomes(t, doc, "min"),
						"B, writing no type, holds its numeric bound: %v", order)
					for _, id := range []string{"A", "B"} {
						raw, err := json.Marshal(doc.Types[ir.TypeID("t/openapi/components/schemas/"+id)], json.Deterministic(true))
						require.NoError(t, err)
						nodes[id] = append(nodes[id], string(raw))
					}
				}
			}
			for _, id := range []string{"A", "B"} {
				require.Len(t, nodes[id], 8)
				assert.Len(t, slices.Compact(nodes[id]), 1, "%s is one node whichever way the cycle is entered", id)
			}
		})
	}
}
