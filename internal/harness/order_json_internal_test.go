// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package harness

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestOrderInvariant_DeclinesASourceOnlyTheJSONReaderReads pins the limit
// docs/compilers/openapi.md#yaml-decoding states in its Status (#1768): the
// oracle permutes a source through the YAML parser, which refuses JSON that
// writes a surrogate pair, so it declines such a source as not YAML, where it
// asks its question of the same source written with the character. When the
// oracle reads the source as the compiler does, the pair row turns to the
// literal row's outcome.
func TestOrderInvariant_DeclinesASourceOnlyTheJSONReaderReads(t *testing.T) {
	t.Parallel()
	const head = `{"openapi":"3.0.3","info":{"title":"t","version":"1"},"paths":{},` +
		`"components":{"schemas":{"P":{"type":"string","enum":["`
	const tail = `"]}}}}`
	pair := `\u` + "d83d" + `\u` + "de00"

	_, why := reencodeMappings([]byte(head + "\U0001F600" + tail))
	assert.Empty(t, why, "the YAML parser reads the source written with the character")
	_, why = reencodeMappings([]byte(head + pair + tail))
	assert.Equal(t, DeclineNotYAML, why, "and refuses the source written with the pair")

	v := orderInvariant(t.Context(), "pair.json", []byte(head+pair+tail), true)
	assert.Equal(t, DeclineNotYAML, v.Declined, "the oracle asks the source nothing")
}

// TestSequenceOrderInvariant_DeclinesASourceOnlyTheJSONReaderReads pins the
// same limit for the sequence-order oracle, which permutes through the same
// parser: a source writing a surrogate pair is declined as not YAML, where the
// same source written with the character has its sequence swapped.
func TestSequenceOrderInvariant_DeclinesASourceOnlyTheJSONReaderReads(t *testing.T) {
	t.Parallel()
	const head = `{"openapi":"3.0.3","info":{"title":"t","version":"1"},"paths":{},` +
		`"components":{"schemas":{"P":{"allOf":[{"title":"t"},{"type":"string","enum":["`
	const tail = `"]}]}}}}`
	pair := `\u` + "d83d" + `\u` + "de00"

	asked := sequenceOrderInvariant(t.Context(), "char.json", []byte(head+"\U0001F600"+tail), true)
	assert.True(t, asked.Asked(), "the oracle asks the source written with the character: %+v", asked)
	v := sequenceOrderInvariant(t.Context(), "pair.json", []byte(head+pair+tail), true)
	assert.Equal(t, declined(OracleSequenceOrder, DeclineNotYAML), v, "and nothing of the source written with the pair")
}
