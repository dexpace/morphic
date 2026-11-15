// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package overlay_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
)

// TestApply_RefusesATreeOfTheWrongShape holds an overlay document read into a
// tree of the wrong shape to findings that are each an invalid overlay at the
// pointer of the field of another shape than its object's, in document order,
// and to nothing applied. A version that is no string is the one finding, as
// the version decides what the rest may write.
func TestApply_RefusesATreeOfTheWrongShape(t *testing.T) {
	t.Parallel()
	tests := map[string]struct {
		overlay string
		at      []string
	}{
		"a version that is no string": {`{"overlay": ["1.0.0"], "info": 3, "actions": {"target": "$"}}`, []string{"/overlay"}},
		"an info and actions of another shape": {`{"overlay": "1.0.0", "info": 3, "actions": {"target": "$"}}`,
			[]string{"/info", "/actions"}},
		"fields of the info and of each action": {
			`{"overlay": "1.1.0", "info": {"title": 1, "version": []}, "actions": [1, {"target": 2}]}`,
			[]string{"/info/title", "/info/version", "/actions/0", "/actions/1/target"}},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			origin, _, diags := applied(t, spec, tc.overlay, false)

			assert.False(t, origin.Applied())
			assert.Equal(t, tc.at, invalidAt(diags), "%+v", diags)
			require.Len(t, diags, len(tc.at))
			for _, d := range diags {
				assert.Equal(t, ir.SeverityError, d.Severity)
				assert.Equal(t, overlayIndex, d.Provenance.Source, "about the overlay")
			}
		})
	}
}
