// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package e2e_test

import (
	"encoding/json/jsontext"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
)

// entryMessagesAt returns the messages of the findings of diags at at.
func entryMessagesAt(diags []ir.Diagnostic, at jsontext.Pointer) []string {
	var out []string
	for _, diagnostic := range diags {
		if diagnostic.Provenance.Pointer == at {
			out = append(out, diagnostic.Message)
		}
	}
	return out
}

// TestEntryValueSet_WhatTheSetWritesStaysWhereItIsWritten holds that where an
// entry's value set stands at the position, what lies beneath it does not:
// the reports on its members are at the members as the entry writes them,
// and the literals an enum of objects lowers to, with the info saying so,
// stand at the entry, at every position.
func TestEntryValueSet_WhatTheSetWritesStaysWhereItIsWritten(t *testing.T) {
	t.Parallel()
	t.Run("members", func(t *testing.T) {
		t.Parallel()
		schema := "{allOf: [" + entryRef + ", {type: string, enum: [a, a, null]}]}"
		_, diags := compileAnnotationSpec(t, "written", conjunctionSpec("3.1.0", schema, "T: {type: string}"))
		for _, at := range conjunctionPositions {
			entry := at + "/allOf/1"
			require.Len(t, entryMessagesAt(diags, entry+"/enum/1"), 1, "the repeat is reported where written at %s", at)
			assert.Contains(t, entryMessagesAt(diags, entry+"/enum/1")[0], "repeats the entry at index 0")
			require.Len(t, entryMessagesAt(diags, entry+"/enum/2"), 1, "the null is reported where written at %s", at)
			assert.Contains(t, entryMessagesAt(diags, entry+"/enum/2")[0], "null admits no instance")
			assert.Empty(t, entryMessagesAt(diags, at+"/enum/1"), "nothing is reported at the position's own members")
			assert.Empty(t, entryMessagesAt(diags, at+"/enum/2"), "nothing is reported at the position's own members")
		}
	})
	t.Run("literals", func(t *testing.T) {
		t.Parallel()
		schema := "{allOf: [" + entryRef + ", {enum: [{a: x}, {a: y}]}]}"
		doc, diags := compileAnnotationSpec(t, "written", conjunctionSpec("3.1.0", schema, "T: {type: object}"))
		for _, at := range conjunctionPositions {
			entry := at + "/allOf/1"
			union, ok := kindNode(t, doc, usedAt(t, doc, at)).(*ir.Union)
			require.True(t, ok, "the enum of objects at %s is a union of literals", at)
			require.Len(t, union.Variants, 2, at)
			for i, variant := range union.Variants {
				want := entry + jsontext.Pointer("/enum/"+strconv.Itoa(i))
				assert.Equal(t, ir.TypeID("type/anonymous"+string(want)), variant.Type.Target, "the literal stands at the entry")
				assert.Equal(t, want, kindNode(t, doc, variant.Type.Target).Common().Provenance.Pointer, at)
			}
			assert.Equal(t, []string{"heterogeneous or non-scalar enum lowered as a union of literals"},
				entryMessagesAt(diags, entry), "the info is at the entry writing the enum, at %s", at)
		}
	})
}
