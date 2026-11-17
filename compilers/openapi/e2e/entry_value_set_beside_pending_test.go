// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

//go:build pending

package e2e_test

// Pending: the warning that the conjuncts share no value reads each conjunct
// as an atom (conjunctAtoms), and a value set written beside the allOf is no
// atom there, so that spelling draws only the info where the entry spelling
// warns. Reading it as one changes what other compositions report, which is
// more than one contained change.

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestEntryValueSet_AValueSetSharingNoKindWithItsRefWarnsInEverySpelling holds
// that a value set elected over a $ref whose instance types it shares none of
// draws the warning naming what the position lowered as wherever the set is
// written, beside the allOf as on an entry
// (docs/compilers/openapi.md#allof-refinement).
func TestEntryValueSet_AValueSetSharingNoKindWithItsRefWarnsInEverySpelling(t *testing.T) {
	t.Parallel()
	for _, version := range []string{"3.0.3", "3.1.0"} {
		t.Run(version, func(t *testing.T) {
			t.Parallel()
			obj := "T: {type: object, properties: {a: {type: string}}}"
			spellings := entrySpellings("enum: [a, b]")
			entryDoc, _ := compileAnnotationSpec(t, "disjoint", conjunctionSpec(version, spellings[0].schema, obj))
			wrapDoc, wrapDiags := compileAnnotationSpec(t, "disjoint", conjunctionSpec(version, spellings[3].schema, obj))
			for _, at := range conjunctionPositions {
				msgs := entryMessagesAt(wrapDiags, at)
				require.Len(t, msgs, 1, "one report at %s", at)
				assert.Contains(t, msgs[0], "which the enum it lowered as cannot state", at)
			}
			assert.Equal(t, entryTree(t, wrapDoc), entryTree(t, entryDoc), "the spelling decided the document")
		})
	}
}
