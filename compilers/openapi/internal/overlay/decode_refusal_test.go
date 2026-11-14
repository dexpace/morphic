// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package overlay_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/ir"
)

// TestApply_RefusesATreeOfTheWrongShape holds an overlay document read into a
// tree of the wrong shape to findings that are each an invalid overlay, one
// for every field that is of another shape than its object's, and to nothing
// applied.
func TestApply_RefusesATreeOfTheWrongShape(t *testing.T) {
	t.Parallel()

	origin, _, diags := applied(t, spec, `{"overlay": ["1.0.0"], "info": 3, "actions": {"target": "$"}}`, false)

	assert.False(t, origin.Applied())
	require.NotEmpty(t, diags)
	for _, d := range diags {
		assert.Equal(t, diag.OverlayInvalid, d.Code, "%+v", d)
		assert.Equal(t, ir.SeverityError, d.Severity)
		assert.Equal(t, overlayIndex, d.Provenance.Source, "about the overlay")
	}
}
