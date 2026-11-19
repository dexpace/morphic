// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package e2e_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
)

// TestEntryValueSet_AValueSetSharingNoKindWithItsRefWarnsInEverySpelling holds
// that a value set elected over a $ref whose instance types it shares none of
// draws the warning naming what the position lowered as wherever the set is
// written, beside the allOf as on an entry
// (docs/compilers/openapi.md#allof-refinement). The warning names each
// conjunct where it is written, so its text is the one difference.
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
				messages := entryMessagesAt(wrapDiags, at)
				require.Len(t, messages, 1, "one report at %s", at)
				assert.Contains(t, messages[0], "which the enum it lowered as cannot state", at)
			}
			for _, doc := range []*ir.Document{wrapDoc, entryDoc} {
				for i := range doc.Diagnostics {
					if strings.Contains(doc.Diagnostics[i].Message, "share no value (") {
						doc.Diagnostics[i].Message = ""
					}
				}
			}
			assert.Equal(t, entryTree(t, wrapDoc), entryTree(t, entryDoc), "the spelling decided the document")
		})
	}
}
