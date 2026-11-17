// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package e2e_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRefConjunction_ASplitEntryKeptWholeHoldsItsWordsAlone holds that an
// allOf entry writing a $ref beside a keyword the conjuncts disagree on keeps
// the keyword whole without the $ref, which the composition already holds as a
// mixin, as the same keyword on an entry of its own is kept.
func TestRefConjunction_ASplitEntryKeptWholeHoldsItsWordsAlone(t *testing.T) {
	t.Parallel()
	a := "{type: object, properties: {a: {type: string}}}"
	b := "{type: object, properties: {b: {type: string}}}"
	for name, schema := range map[string]string{
		"beside the $ref": "{allOf: [{$ref: '#/components/schemas/A', description: d}, {$ref: '#/components/schemas/B', description: e}]}",
		"on an entry":     "{allOf: [{$ref: '#/components/schemas/A'}, {description: d}, {$ref: '#/components/schemas/B'}, {description: e}]}",
	} {
		doc, _ := compileUnrefused(t, admissionSpec("3.1.0", "", "A: "+a, "B: "+b, "Use: "+schema))
		kept := kindNode(t, doc, componentID("Use")).Common().Unmodeled
		require.Len(t, kept, 2, name)
		for key, entry := range kept {
			assert.NotContains(t, string(entry.Value), "$ref", "%s: %s holds the keyword alone", name, key)
		}
	}
}
