// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package pass_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/pass"
)

// cutPoints is how many parts the sweep cuts each document's walk into: it cuts
// at each boundary between two.
const cutPoints = 16

// findingKeys counts diags by code and message, leaving out ir/walk-truncated.
func findingKeys(diags []ir.Diagnostic) map[string]int {
	keys := map[string]int{}
	for _, d := range diags {
		if d.Code != "ir/walk-truncated" {
			keys[d.Code+": "+d.Message]++
		}
	}
	return keys
}

// TestValidate_ACutWalkClaimsNothingTheWholeRunLacks cuts the walk of every IR
// golden under testdata at points spread over what it shows a visitor, and
// holds each cut run to the findings the whole run has, beside the one
// ir/walk-truncated: a check settling against what a stopped walk collected
// would claim an absence the document does not have.
func TestValidate_ACutWalkClaimsNothingTheWholeRunLacks(t *testing.T) {
	t.Parallel()
	for _, sd := range sweepDocuments(t) {
		whole, err := pass.ValidateWith(t.Context(), sd.doc, ir.WalkValuesContext)
		require.NoError(t, err)
		have, shown := findingKeys(whole), shownWhole(sd.doc)
		for k := 1; k < cutPoints; k++ {
			n := shown * k / cutPoints
			cut, err := pass.ValidateWith(t.Context(), sd.doc, cutWalk(n))
			require.NoError(t, err)
			require.Equal(t, 1, countCode(t, cut, "ir/walk-truncated"), "%s cut at %d of %d values", sd.name, n, shown)
			for key, times := range findingKeys(cut) {
				assert.LessOrEqual(t, times, have[key], "%s cut at %d of %d values: %q", sd.name, n, shown, key)
			}
		}
	}
}
