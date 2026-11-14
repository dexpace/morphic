// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package overlay_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
)

// TestApply_RefusesATreeTheOverlayTypeCannotDecode holds an overlay document
// read into a tree of the wrong shape to one finding, which says the overlay
// could not be read, and to nothing applied.
func TestApply_RefusesATreeTheOverlayTypeCannotDecode(t *testing.T) {
	t.Parallel()

	origin, _, diags := applied(t, spec, `{"overlay": ["1.0.0"], "info": 3, "actions": {"target": "$"}}`, false)

	assert.False(t, origin.Applied())
	sameCodes(t, []string{"error " + diag.OverlayInvalid}, diags)
	assert.Contains(t, diags[0].Message, "cannot parse overlay: ")
}
