// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package openapicases_test

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/internal/harness"
	"github.com/dexpace/morphic/internal/harness/openapicases"
	"github.com/dexpace/morphic/ir/irtest"
)

// instanceRecord is the file listing what the instance oracle finds, and
// instanceHeader the comment it opens with.
const (
	instanceRecord = "testdata/instances.txt"
	instanceHeader = "What the instance oracle finds at the core cases, and the reason each finding\n" +
		"stands: case name, what was found, reason (Reasons).\n"
)

// TestCore_ValidInstancesAreAdmitted holds each valid instance of the test
// suite's groups, written as a default at each core position, to the type its
// carrier compiles to. An instance the type excludes, and a group the oracle
// cannot ask, is the record's, each line with the reason it stands, so a group
// that starts to decline fails as a new exclusion does. The presence oracle
// declines each case: a group writes no construct.
func TestCore_ValidInstancesAreAdmitted(t *testing.T) {
	t.Parallel()
	results := groupResults(t)
	require.NotEmpty(t, results)
	for _, r := range results {
		require.False(t, r.Faulted(), "%s: %s: %s", r.Spec, r.Outcome, r.Detail)
		assert.Equal(t, harness.Verdict{Oracle: harness.OraclePresence, Declined: harness.DeclineNoConstruct},
			r.Verdicts[1], r.Spec)
	}

	found := harness.Findings(results, harness.OracleInstance)
	record, err := openapicases.Record(harness.OracleInstance)
	require.NoError(t, err)
	if irtest.Update() {
		require.NoError(t, os.WriteFile(instanceRecord, record.Write(instanceHeader, found), 0o644))
		return
	}
	unlisted, changed, stale, unreasoned := record.Judge(found, openapicases.Reasons())
	assert.Empty(t, unlisted, "a finding the record does not list: fix the compiler, or list it with its reason (-update)")
	assert.Empty(t, changed)
	assert.Empty(t, stale, "a line whose finding is gone: delete it (-update)")
	assert.Empty(t, unreasoned)
}
