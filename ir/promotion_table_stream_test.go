// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package ir_test

import (
	"os"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// recordsIn returns the marker each row filling field records a node of kind
// kept in, "" when no row says so.
func recordsIn(rows []promotionRow, field, kept string) string {
	for _, row := range rows {
		if slices.Contains(row.fields, field) && slices.Contains(row.kept, kept) {
			return row.record
		}
	}
	return ""
}

// TestPromotionTable_StreamFieldsNameWhereTheyAreKept pins the table's rows for
// the two stream fields a promotion fills. A terminator is read from a key on
// the operation or on a streaming content, and each records it in the marker it
// has: the operation in its Provenance.Inferred, the content in its Inferred,
// as it has no Provenance. A condition is read from the operation alone.
func TestPromotionTable_StreamFieldsNameWhereTheyAreKept(t *testing.T) {
	t.Parallel()
	doc, err := os.ReadFile(promotionDoc)
	require.NoError(t, err)
	rows := promotionRows(string(doc))

	assert.Equal(t, "Provenance.Inferred", recordsIn(rows, "StreamDetail.Terminator", "Operation"))
	assert.Equal(t, "Inferred", recordsIn(rows, "StreamDetail.Terminator", "Content"))
	assert.Equal(t, "Provenance.Inferred", recordsIn(rows, "StreamDetail.Condition", "Operation"))
	assert.Empty(t, recordsIn(rows, "StreamDetail.Condition", "Content"), "a condition is read from the operation's key alone")
}
