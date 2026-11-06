// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package bind

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestIsAnchorName pins the names a $anchor can declare, by JSON Schema
// 2020-12 section 8.2.2: none empty, none opening with a digit, '-' or '.'.
func TestIsAnchorName(t *testing.T) {
	t.Parallel()
	for name, want := range map[string]bool{
		"a": true, "_": true, "Z9-_.": true, "": false, "1a": false, "-a": false, ".a": false, "a b": false,
		"a/b": false, "é": false,
	} {
		assert.Equal(t, want, isAnchorName(name), "%q", name)
	}
}

// TestIsPlainName pins the plain names JSON Schema drafts 6, 7 and 2019-09
// declare an anchor by: a letter, then letters, digits, '-', '_', ':' and '.',
// so a colon is one and a leading '_' is not. fragmentName reads by it an
// identifier naming a fragment alone where the rule says so, and anchorNamed
// an `$anchor` where the rule says so, and by 2020-12's grammar any other,
// `$dynamicAnchor` among them.
func TestIsPlainName(t *testing.T) {
	t.Parallel()
	plain := DialectRule{Keyword: "$id", Fragments: true, PlainFragments: true}
	for name, want := range map[string]bool{
		"a": true, "a:b": true, "Z9-_.:": true, "": false, "_": false, "1a": false, "-a": false, "a b": false,
		"a/b": false, "é": false,
	} {
		assert.Equal(t, want, isPlainName(name), "%q", name)
		declared, ok := fragmentName(plain, "#"+name, true)
		assert.Equal(t, want, ok, "%q", name)
		assert.Equal(t, want, ok && declared == name, "%q declares %q", name, declared)
		assert.Equal(t, isAnchorName(name), anchorNamed(plain, "$anchor", name), "%q", name)
		assert.Equal(t, want, anchorNamed(DialectRule{PlainNames: true}, "$anchor", name), "%q", name)
		assert.Equal(t, isAnchorName(name), anchorNamed(DialectRule{PlainNames: true}, "$dynamicAnchor", name), "%q", name)
	}
}

// TestFragmentName_DeclaresAnyNameDraft4Writes pins an identifier naming a
// fragment alone in draft 4, which states no grammar for one: any fragment no
// JSON pointer spells declares the name a $ref's fragment reads as, escapes
// decoded. A pointer, an empty fragment, one with a document part or no '#',
// one decoding to no UTF-8, and one not written declare none.
func TestFragmentName_DeclaresAnyNameDraft4Writes(t *testing.T) {
	t.Parallel()
	draft4 := DialectRule{Keyword: "id", Fragments: true}
	for id, want := range map[string]string{
		"#a": "a", "#_a": "_a", "#1x": "1x", "#a:b": "a:b", "#a%20b": "a b", "#é": "é",
		"#/a": "", "#%2Fa": "", "#": "", "x.json#a": "", "a": "", "#%FF": "", "#a#x": "",
	} {
		declared, ok := fragmentName(draft4, id, true)
		assert.Equal(t, want != "", ok, "%q", id)
		assert.Equal(t, want, map[bool]string{true: declared}[ok], "%q", id)
	}
	_, ok := fragmentName(draft4, "#a", false)
	assert.False(t, ok, "an identifier not written declares nothing")
}
