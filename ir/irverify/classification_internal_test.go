// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package irverify

import (
	"fmt"
	"maps"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// classificationProblems holds a hand-kept table's keys to the fields the ir
// package declares, and returns one message for each entry the two disagree on,
// in key order: a stale key, which the table lists and ir does not declare,
// names itself and the table (stringLists), and a field the table lacks names the field
// through missing, a format taking its key. Neither names the table's other
// entries, so a failure points at the line to change.
func classificationProblems(listed []string, declared map[string]bool, missing string) []string {
	listedSet := map[string]bool{}
	var problems []string
	for _, key := range slices.Sorted(slices.Values(listed)) {
		listedSet[key] = true
		if !declared[key] {
			problems = append(problems, fmt.Sprintf("stringLists classifies %s, which ir does not declare", key))
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

// TestClassificationProblems_NameTheStaleStringListAndTheMissingField plants
// one stale key and removes one real one from the keys of stringLists: each is
// reported on a line naming it, and no line carries the rest of the table.
func TestClassificationProblems_NameTheStaleStringListAndTheMissingField(t *testing.T) {
	t.Parallel()
	declared := stringListFieldKeys(t)
	require.True(t, declared["Document.Versions"], "Document.Versions is a []string field")
	const missing = "ir.%s is a []string field stringLists does not classify"

	listed := slices.Collect(maps.Keys(stringLists))
	assert.Empty(t, classificationProblems(listed, declared, missing), "the table agrees with ir")

	stale := classificationProblems(append(slices.Clone(listed), "Service.NoSuchField"), declared, missing)
	assert.Equal(t, []string{"stringLists classifies Service.NoSuchField, which ir does not declare"}, stale)

	without := slices.DeleteFunc(slices.Clone(listed), func(key string) bool { return key == "Document.Versions" })
	absent := classificationProblems(without, declared, missing)
	assert.Equal(t, []string{"ir.Document.Versions is a []string field stringLists does not classify"}, absent)
}
