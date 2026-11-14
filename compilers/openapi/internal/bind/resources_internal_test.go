// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package bind

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestIdentifiers_AReadingIsTheDocumentsCounted pins what makes passes return
// to a reading: the documents counted, which settle every declaration a pass
// reads against. A document read since and left uncounted declares nothing a
// pass reads, so a pass that would begin with documents counted as one before
// did returns to that reading, and every document read counts from then on.
func TestIdentifiers_AReadingIsTheDocumentsCounted(t *testing.T) {
	t.Parallel()
	ix := newIdentifiers(resource{root: Position{Doc: Source}, uri: "/spec.yaml"})
	ix.begin()
	_, found, _ := ix.lookupURI("u")
	require.False(t, found)

	ix.declared["u"] = []Position{{Doc: 1, Pointer: "/A"}}
	require.True(t, ix.commit(map[DocID]bool{Source: true, 1: true}), "the document read declares u")
	assert.False(t, ix.every, "no pass began with these documents counted")
	ix.begin()

	ix.declared["u"] = append(ix.declared["u"], Position{Doc: 2, Pointer: "/B"})
	require.True(t, ix.commit(map[DocID]bool{Source: true}), "the document that declared u is no longer reached")
	assert.True(t, ix.every, "the first pass began with the source alone counted")
	assert.Len(t, ix.uris["u"], 2, "every document read counts")
}
