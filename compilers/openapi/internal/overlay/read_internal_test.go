// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package overlay

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
)

// TestRead_DecodesTheTreeAsTheLibraryDecodesItsOwn holds the decode of an
// overlay's tree to the library's defaults: a field the overlay type does not
// declare is ignored, as the library's own parse ignores it, not refused.
func TestRead_DecodesTheTreeAsTheLibraryDecodesItsOwn(t *testing.T) {
	t.Parallel()
	doc := docOf(t, "overlay: 1.0.0\ninfo: {title: o, version: \"1\", note: n}\nnote: n\n"+
		"actions:\n  - target: $.info\n    note: n\n    update: {description: d}\n")

	p, diags := read(ir.Provenance{Source: 1}, doc)

	assert.Empty(t, diags)
	require.Len(t, p.actions, 1)
	assert.Equal(t, "$.info", p.actions[0].target)
}
