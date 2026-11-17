// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

//go:build pending

package e2e_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestEntryValueSet_AKeywordBesideTheAllOfLandsAsTheWrapLandsIt holds that a
// keyword a position writes beside an allOf whose entry's value set the
// position lowers to lands as it does where the value set is written beside
// the allOf: kept on the node standing at the position, with the info naming
// that node's kind. A bound the value set's kind takes is among them: no
// carrier holds a bound of a use owning its node.
func TestEntryValueSet_AKeywordBesideTheAllOfLandsAsTheWrapLandsIt(t *testing.T) {
	t.Parallel()
	for _, beside := range []string{"pattern: '^a'", "maxLength: 0", "minItems: 2", "format: date"} {
		keyword, _, _ := strings.Cut(beside, ":")
		for _, version := range []string{"3.0.3", "3.1.0"} {
			t.Run(fmt.Sprintf("%s/%s", version, keyword), func(t *testing.T) {
				t.Parallel()
				entry := "{allOf: [" + entryRef + ", {enum: [a, b]}], " + beside + "}"
				wrap := "{allOf: [" + entryRef + "], enum: [a, b], " + beside + "}"
				entryDoc, _ := compileAnnotationSpec(t, "beside", conjunctionSpec(version, entry, "T: {type: string}"))
				wrapDoc, _ := compileAnnotationSpec(t, "beside", conjunctionSpec(version, wrap, "T: {type: string}"))
				for _, at := range conjunctionPositions {
					node := kindNode(t, entryDoc, usedAt(t, entryDoc, at))
					_, kept := node.Common().Unmodeled["openapi:"+keyword]
					require.True(t, kept, "the node at %s keeps %s", at, keyword)
				}
				assert.Equal(t, entryTree(t, wrapDoc), entryTree(t, entryDoc), "the spelling decided the document")
			})
		}
	}
}

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
