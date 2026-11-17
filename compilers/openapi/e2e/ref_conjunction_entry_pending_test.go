// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

//go:build pending

package e2e_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
)

// TestRefConjunction_AnAllOfOfOneRefToFalseIsTheReference holds that an allOf
// of one $ref saying nothing else is the reference where its target is the
// false schema too, as the bare $ref is, at a property and at a component.
func TestRefConjunction_AnAllOfOfOneRefToFalseIsTheReference(t *testing.T) {
	t.Parallel()
	spec := admissionSpec("3.1.0", "", "F: false",
		"Bare: {$ref: '#/components/schemas/F'}",
		"Wrapped: {allOf: [{$ref: '#/components/schemas/F'}]}",
		"H: {type: object, properties: {bare: {$ref: '#/components/schemas/F'}, "+
			"wrapped: {allOf: [{$ref: '#/components/schemas/F'}]}}}")
	doc, _ := compileUnrefused(t, spec)
	h, ok := kindNode(t, doc, componentID("H")).(*ir.Model)
	require.True(t, ok)
	require.Len(t, h.Properties, 2)
	assert.Equal(t, h.Properties[0].Type.Target, h.Properties[1].Type.Target, "the wrapped use names what the bare one does")
	assert.Equal(t, kindNode(t, doc, componentID("Bare")).Kind(), kindNode(t, doc, componentID("Wrapped")).Kind())
}
