// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package bind

import (
	"encoding/json/jsontext"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/budget"
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

// TestHeldRole_ReadsToTheFirstFieldNoModelHolds pins heldRole for a schema:
// raw YAML beneath an Encoding Object another nests, read from the root as far
// as that field, a schema, raw YAML or the pointer's end, whichever comes
// first, and charged a step a token read, so a budget short of the tokens it
// reads stops it unanswered. The document holds nothing: the grammar alone
// answers.
func TestHeldRole_ReadsToTheFirstFieldNoModelHolds(t *testing.T) {
	t.Parallel()
	const encoding = "/paths/~1a/post/requestBody/content/m~1x/encoding/f"
	tests := []struct {
		pointer  string
		role     Role
		readsFor int
	}{
		{encoding + "/encoding/g/headers/H/schema/items", Raw, 9},
		{encoding + "/prefixEncoding/0/headers/H/schema", Raw, 9},
		{encoding + "/itemEncoding/encoding/g/headers/H/schema", Raw, 9},
		{encoding + "/headers/H/schema/encoding/g", Schema, 11},
		{encoding + "/x-ext/encoding/g", Schema, 9},
		{encoding + "/contentType/encoding", Schema, 9},
		{"/components/schemas/A/properties/encoding/encoding", Schema, 3},
		{encoding, Schema, 8},
	}
	var root yaml.Node
	require.NoError(t, yaml.Unmarshal([]byte("openapi: 3.2.0\n"), &root))
	for _, tc := range tests {
		r := reading{at: Position{Pointer: jsontext.Pointer(tc.pointer)}, state: sSchema}
		w := &work{ctx: t.Context(), steps: budget.New(budget.Unbounded())}
		role, reason := heldRole(newDocs(&root, Config{Self: "spec.yaml"}, w), r)
		assert.Equal(t, NoReason, reason, tc.pointer)
		assert.Equal(t, tc.role, role, tc.pointer)
		assert.Equal(t, tc.readsFor, w.steps.Spent(), tc.pointer)

		short := &work{ctx: t.Context(), steps: budget.New(budget.AtMost(tc.readsFor - 1))}
		role, reason = heldRole(newDocs(&root, Config{Self: "spec.yaml"}, short), r)
		assert.Equal(t, NoTarget, reason, "%s, a step short", tc.pointer)
		assert.Equal(t, NoRole, role, "%s, a step short", tc.pointer)
	}
	role, reason := heldRole(nil, reading{state: sRaw})
	assert.Equal(t, Raw, role, "a reading at raw YAML reads nothing more")
	assert.Equal(t, NoReason, reason)
}
