// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package e2e_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
)

// TestRefConjunction_SilentKeysBesideARefLeaveItTheReference holds that a key
// saying nothing of the instance, a comment, an anchor or the schemas a
// reference reaches, makes no conjunction beside a $ref: a property names the
// target as a bare $ref does, and a component over a string is its alias.
func TestRefConjunction_SilentKeysBesideARefLeaveItTheReference(t *testing.T) {
	t.Parallel()
	for _, key := range []string{"$comment: c", "$anchor: a", "$defs: {D: {type: string}}"} {
		spec := admissionSpec("3.1.0", "", "Obj: {type: object, properties: {a: {type: string}}}",
			"Str: {type: string}",
			"Use: {$ref: '#/components/schemas/Str', "+key+"}",
			"H: {type: object, properties: {p: {$ref: '#/components/schemas/Obj', "+key+"}}}")
		doc, _ := compileUnrefused(t, spec)
		model, ok := kindNode(t, doc, componentID("H")).(*ir.Model)
		require.True(t, ok, key)
		require.Len(t, model.Properties, 1, key)
		assert.Equal(t, componentID("Obj"), model.Properties[0].Type.Target, "%s: the property names the target", key)
		use, ok := kindNode(t, doc, componentID("Use")).(*ir.Scalar)
		require.True(t, ok, "%s: the component is the alias", key)
		assert.Equal(t, componentID("Str"), use.Base.Target, key)
	}
}
