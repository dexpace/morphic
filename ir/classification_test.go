// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package ir_test

import (
	"fmt"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
)

// classificationProblems holds a hand-kept table's keys to the fields the ir
// package declares, and returns one message for each entry the two disagree on,
// in key order: a stale key, which the table lists and ir does not declare,
// names itself and the table (idPositions), and a field the table lacks names the field
// through missing, a format taking its key. Neither names the table's other
// entries, so a failure points at the line to change.
func classificationProblems(listed []string, declared map[string]bool, missing string) []string {
	listedSet := map[string]bool{}
	var problems []string
	for _, key := range slices.Sorted(slices.Values(listed)) {
		listedSet[key] = true
		if !declared[key] {
			problems = append(problems, fmt.Sprintf("idPositions classifies %s, which ir does not declare", key))
		}
	}
	for key := range declared {
		if !listedSet[key] {
			problems = append(problems, fmt.Sprintf(missing, key))
		}
	}
	slices.Sort(problems)
	return problems
}

// TestClassificationProblems_NameTheStaleEntryAndTheMissingField plants one
// stale key and removes one real one from the keys of idPositions: each is
// reported on a line naming it, and no line carries the rest of the table.
func TestClassificationProblems_NameTheStaleEntryAndTheMissingField(t *testing.T) {
	t.Parallel()
	declared := identityFieldKeys(t)
	require.True(t, declared["Channel.Messages"], "Channel.Messages is an identity-bearing field")
	const missing = "ir.%s is an identity-bearing field idPositions does not classify"

	listed := ir.IDPositions()
	assert.Empty(t, classificationProblems(listed, declared, missing), "the table agrees with ir")

	stale := classificationProblems(append(slices.Clone(listed), "Service.NoSuchField"), declared, missing)
	assert.Equal(t, []string{"idPositions classifies Service.NoSuchField, which ir does not declare"}, stale)

	without := slices.DeleteFunc(slices.Clone(listed), func(key string) bool { return key == "Channel.Messages" })
	absent := classificationProblems(without, declared, missing)
	assert.Equal(t, []string{"ir.Channel.Messages is an identity-bearing field idPositions does not classify"}, absent)
}
