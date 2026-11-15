// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package e2e_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi"
)

// TestCompile_ANumberLikeKeyIsTheTextWritten holds a property named by a key the
// core schema reads as a number, or as a string the parser reads as one, to
// the text written: in its ID, its wire name, the required list naming it and
// a $ref through it.
func TestCompile_ANumberLikeKeyIsTheTextWritten(t *testing.T) {
	t.Parallel()
	keys := []string{"0x10", "1e3", "0b11", "1_000", "01"}
	src := srMin + "components:\n  schemas:\n    S:\n      type: object\n" +
		"      required: [\"0x10\", \"1e3\", \"0b11\", \"1_000\", \"01\"]\n      properties:\n" +
		"        0x10: {type: string}\n        1e3: {type: string}\n        0b11: {type: string}\n" +
		"        1_000: {type: string}\n        01: {type: string}\n" +
		"    T: {$ref: \"#/components/schemas/S/properties/0b11\"}\n"

	doc, diags := compileText(t, "keys.yaml", src, openapi.Options{})

	require.NotNil(t, doc)
	assert.Empty(t, diags)
	tree := irTree(t, doc)
	for _, k := range keys {
		p := findByID(tree, "p/openapi/components/schemas/S/properties/"+k)
		require.NotNil(t, p, k)
		assert.Equal(t, true, p["required"], k)
		assert.Equal(t, k, p["wireName"], k)
		assert.Equal(t, k, p["name"].(map[string]any)["source"], k)
	}
	assert.NotNil(t, findByID(tree, "t/openapi/components/schemas/T"), "the reference through 0b11 resolves")
}
