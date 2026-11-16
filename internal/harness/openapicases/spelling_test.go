// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package openapicases_test

import (
	"os"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/internal/harness"
	"github.com/dexpace/morphic/internal/harness/openapicases"
	"github.com/dexpace/morphic/ir/irtest"
)

// spellingRecord is the file listing what the spelling oracle finds, and
// spellingHeader the comment it opens with.
const (
	spellingRecord = "testdata/spelling.txt"
	spellingHeader = "What the spelling oracle finds at the core cases, and the reason each finding\n" +
		"stands: case name, what was found, reason (Reasons).\n"
)

// TestCore_SpellingsHoldTheRecord holds what the spelling oracle finds at the
// core cases to its record: a finding it does not list, one it lists with
// another signature, a line naming no finding and a line whose reason Reasons
// does not name each fail. -update rewrites the record, keeping each listed
// line's reason. No case faults.
func TestCore_SpellingsHoldTheRecord(t *testing.T) {
	results := slices.Concat(coreResults(t), groupResults(t))
	for _, r := range results {
		require.False(t, r.Faulted(), "%s: %s: %s", r.Spec, r.Outcome, r.Detail)
	}
	found := harness.Findings(results, harness.OracleSpelling)
	record, err := openapicases.Record(harness.OracleSpelling)
	require.NoError(t, err)

	if irtest.Update() {
		require.NoError(t, os.WriteFile(spellingRecord, record.Write(spellingHeader, found), 0o644))
		return
	}
	unlisted, changed, stale, unreasoned := record.Judge(found, openapicases.Reasons())
	assert.Empty(t, unlisted, "a finding the record does not list: fix the compiler, or list it with its reason (-update)")
	assert.Empty(t, changed)
	assert.Empty(t, stale, "a line whose finding is gone: delete it (-update)")
	assert.Empty(t, unreasoned)
}
