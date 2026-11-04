// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package harness

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestOrderInvariant_SkipsASourceOnlyTheJSONReaderReads pins the limit
// docs/compilers/openapi.md#yaml-decoding states in its Status
// (#NEW-ORDER-JSON): the oracle permutes a source through the YAML parser,
// which refuses JSON that writes a surrogate pair, so it skips such a source in
// silence, where it asks its question of the same source written with the
// character. When the oracle reads the source as the compiler does, the pair
// row turns to the literal row's outcome.
func TestOrderInvariant_SkipsASourceOnlyTheJSONReaderReads(t *testing.T) {
	t.Parallel()
	const head = `{"openapi":"3.0.3","info":{"title":"t","version":"1"},"paths":{},` +
		`"components":{"schemas":{"P":{"type":"string","enum":["`
	const tail = `"]}}}}`
	pair := `\u` + "d83d" + `\u` + "de00"

	_, readable := reencodeMappings([]byte(head + "\U0001F600" + tail))
	assert.True(t, readable, "the YAML parser reads the source written with the character")
	_, readable = reencodeMappings([]byte(head + pair + tail))
	assert.False(t, readable, "and refuses the source written with the pair")

	msg, passed := orderInvariant(t.Context(), "pair.json", []byte(head+pair+tail))
	assert.True(t, passed, "the skip reports no finding")
	assert.Empty(t, msg)
}
