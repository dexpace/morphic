// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package operation

import (
	"testing"

	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/speakeasy-api/openapi/sequencedmap"
	"github.com/stretchr/testify/assert"
)

// TestElectContentEntry_SkipsANilEntry holds the election to an entry that can
// lower: a nil one, which no parse yields, sorts first and is not taken, in
// either order.
func TestElectContentEntry_SkipsANilEntry(t *testing.T) {
	t.Parallel()
	entry := &soa.MediaType{}
	empty := sequencedmap.NewElem("a/a", (*soa.MediaType)(nil))
	written := sequencedmap.NewElem("b/b", entry)
	for _, content := range []*sequencedmap.Map[string, *soa.MediaType]{
		sequencedmap.New(empty, written), sequencedmap.New(written, empty),
	} {
		mt, media, ok := electContentEntry(content)
		assert.True(t, ok)
		assert.Equal(t, "b/b", mt)
		assert.Same(t, entry, media)
	}
}

// TestElectContentEntry_NoEntryElectsNothing is the other way an election ends:
// no map, or one that holds nothing that can lower.
func TestElectContentEntry_NoEntryElectsNothing(t *testing.T) {
	t.Parallel()
	for name, content := range map[string]*sequencedmap.Map[string, *soa.MediaType]{
		"no map":    nil,
		"empty map": sequencedmap.New[string, *soa.MediaType](),
		"nil entry": sequencedmap.New(sequencedmap.NewElem("a/a", (*soa.MediaType)(nil))),
	} {
		_, _, ok := electContentEntry(content)
		assert.False(t, ok, name)
	}
}
