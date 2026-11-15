// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package doctree_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/budget"
	"github.com/dexpace/morphic/compilers/openapi/internal/doctree"
)

// pairText is the twelve characters a pair of surrogate escapes is written
// with.
const pairText = bs + "uD83D" + bs + "uDE00"

// TestRead_ASurrogatePairIsTheCharacterOnlyInADoubleQuotedScalar holds the
// pair to the character in a double-quoted scalar, and to the twelve
// characters as written in a single-quoted, a plain and a block scalar and in
// a comment, whose text no escape reads.
func TestRead_ASurrogatePairIsTheCharacterOnlyInADoubleQuotedScalar(t *testing.T) {
	t.Parallel()
	doc := read(t, "t: \"x"+pairText+"\"\nl: \"x"+bs+"ud83d"+bs+"ude00\"\na: 'x"+pairText+"'\nb: x"+pairText+
		"\nblock: |\n  x"+pairText+"\nc: 1 # "+bs+"ud83d"+bs+"ude00\n")

	assert.Equal(t, "x\U0001F600", at(t, doc.Root, "t").Value)
	assert.Equal(t, "x\U0001F600", at(t, doc.Root, "l").Value, "the hex digits in lower case read alike")
	assert.Equal(t, "x"+pairText, at(t, doc.Root, "a").Value)
	assert.Equal(t, "x"+pairText, at(t, doc.Root, "b").Value)
	assert.Equal(t, "x"+pairText+"\n", at(t, doc.Root, "block").Value)
	assert.Equal(t, "# "+bs+"ud83d"+bs+"ude00", at(t, doc.Root, "c").LineComment, "a comment keeps the case written")
}

// TestRead_ASurrogatePairAfterAnotherTriggerIsTheCharacter holds a pair written
// after an escaped slash or a line-break character, whose placeholders are
// wider in bytes than what they stand for, to the character, the text before
// it as written.
func TestRead_ASurrogatePairAfterAnotherTriggerIsTheCharacter(t *testing.T) {
	t.Parallel()
	for name, before := range map[string]struct{ text, value string }{
		"an escaped slash": {bs + "/", "/"},
		"a NEL":            {"\u0085", "\u0085"},
		"a line separator": {" ", " "},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			doc := read(t, "t: \"a"+before.text+"b"+pairText+"c\"\nu: 'd"+pairText+"'\n")

			assert.Equal(t, "a"+before.value+"b\U0001F600c", at(t, doc.Root, "t").Value)
			assert.Equal(t, "d"+pairText, at(t, doc.Root, "u").Value)
		})
	}
}

// TestRead_ASurrogatePairAfterAnEvenRunOfBackslashesIsNotOne holds the pair
// whose first backslash is itself escaped: the escaped backslash then the
// text uD83D then a lone low half, which the lexer refuses as it does today.
func TestRead_ASurrogatePairAfterAnEvenRunOfBackslashesIsNotOne(t *testing.T) {
	t.Parallel()

	_, err := doctree.Read(t.Context(), []byte("d: \""+bs+bs+"uD83D"+bs+"uDE00\"\n"), budget.Unbounded())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "found invalid Unicode character escape code")
}

// TestRead_AnUnpairedHalfIsRefusedAsItIsToday holds each text a pair is not,
// in a double-quoted scalar, to the refusal the lexer gives.
func TestRead_AnUnpairedHalfIsRefusedAsItIsToday(t *testing.T) {
	t.Parallel()
	for name, text := range map[string]string{
		"high alone":      bs + "uD83D",
		"low alone":       bs + "uDE00",
		"low then high":   bs + "uDE00" + bs + "uD83D",
		"high, text, low": bs + "uD83Dx" + bs + "uDE00",
		"two highs":       bs + "uD83D" + bs + "uD83D",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := doctree.Read(t.Context(), []byte("t: \""+text+"\"\n"), budget.Unbounded())

			require.Error(t, err)
			assert.Contains(t, err.Error(), "found invalid Unicode character escape code")
		})
	}
}

// TestRead_ASurrogatePairInAKeyIsTheCharacter holds the pair in a double-quoted
// mapping key to the character, and the same key in a single-quoted spelling to
// its twelve characters.
func TestRead_ASurrogatePairInAKeyIsTheCharacter(t *testing.T) {
	t.Parallel()
	doc := read(t, "? \"k"+pairText+"\"\n: 1\n? 'k"+pairText+"'\n: 2\n")

	keys := at(t, doc.Root).Content
	require.Len(t, keys, 4)
	assert.Equal(t, "k\U0001F600", keys[0].Value)
	assert.Equal(t, "k"+pairText, keys[2].Value)
}
