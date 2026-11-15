// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package e2e_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/ir"
)

// pairDoc is a document whose title holds the escapes of one surrogate pair
// between two letters, their hex digits in the case hexCase gives, and whose
// version is the YAML text version.
func pairDoc(hexCase func(string) string, version string) string {
	return "openapi: 3.1.0\ninfo: {title: \"a" + srBS + "u" + hexCase("D83D") + srBS + "u" + hexCase("DE00") + "b\", version: " +
		version + "}\npaths: {}\n"
}

// TestCompile_ASurrogatePairOfEscapesInYAMLIsTheCharacterItNames holds a
// double-quoted YAML scalar spelling U+1F600 as the two escapes JSON spells it
// with to that character: the document is clean, its name holds the character,
// the escapes in lower case read the same, and a finding after the scalar is
// placed where the text writes it.
func TestCompile_ASurrogatePairOfEscapesInYAMLIsTheCharacterItNames(t *testing.T) {
	t.Parallel()
	upper := strings.ToUpper
	lower := strings.ToLower

	docs := make([]*ir.Document, 0, 2)
	for name, hexCase := range map[string]func(string) string{"upper": upper, "lower": lower} {
		doc, diags := compileText(t, "spec.yaml", pairDoc(hexCase, `"1"`), openapi.Options{})

		require.NotNil(t, doc, name)
		assert.Empty(t, diags, name)
		assert.Equal(t, "a\U0001F600b", doc.Name, name)
		docs = append(docs, doc)
	}
	require.Len(t, docs, 2)
	assert.Equal(t, jsonOf(t, docs[0]), jsonOf(t, docs[1]), "the case of the hex digits changes nothing")

	src := pairDoc(upper, "1")
	_, diags := compileText(t, "spec.yaml", src, openapi.Options{})
	mismatch := byCode(diags, typeMismatch)
	require.Len(t, mismatch, 1, "%+v", diags)
	line := strings.Split(src, "\n")[1]
	assert.Equal(t, ir.Position{Line: 2, Column: strings.Index(line, "1}") + 1}, mismatch[0].Provenance.Position,
		"the version is placed where the text writes it, after twelve characters of escape")
}

// TestCompile_ALoneHalfOfASurrogatePairIsStillRefused holds what a pair is
// not: a high half alone, a low half before a high one, and a high half
// separated from its low half each draw today's refusal of the source.
func TestCompile_ALoneHalfOfASurrogatePairIsStillRefused(t *testing.T) {
	t.Parallel()
	for name, title := range map[string]string{
		"high alone":         srBS + "uD83D",
		"low then high":      srBS + "uDE00" + srBS + "uD83D",
		"high, text, low":    srBS + "uD83Dx" + srBS + "uDE00",
		"high then an other": srBS + "uD83D" + srBS + "u0041",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			doc, diags := compileText(t, "spec.yaml", srMin+"x-t: \""+title+"\"\n", openapi.Options{})

			assert.Nil(t, doc)
			require.Len(t, diags, 1, "%+v", diags)
			assert.Equal(t, diag.UndecodableSource, diags[0].Code)
			assert.Equal(t, ir.SeverityError, diags[0].Severity)
			assert.Contains(t, diags[0].Message, "found invalid Unicode character escape code")
		})
	}
}

// TestCompile_ASurrogatePairInAKeyAndBesideOtherRespellingsReadsEachAsItsOwn
// holds the pair in an explicit mapping key to the character, and the pair,
// a raw U+E000 and an escaped slash in one document to the three characters
// each names, the slash and a NEL written before the pair as well as after.
func TestCompile_ASurrogatePairInAKeyAndBesideOtherRespellingsReadsEachAsItsOwn(t *testing.T) {
	t.Parallel()
	key := srMin + "x-m:\n  ? \"k" + srBS + "uD83D" + srBS + "uDE00\"\n  : 1\n"

	doc, diags := compileText(t, "spec.yaml", key, openapi.Options{})

	require.NotNil(t, doc)
	assert.Empty(t, diags)
	raw, ok := doc.Unmodeled["openapi:x-m"]
	require.True(t, ok, "%v", doc.Unmodeled)
	assert.JSONEq(t, "{\"k\U0001F600\": 1}", string(raw.Value))

	mixed := "openapi: 3.1.0\ninfo: {title: \"a" + srBS + "uD83D" + srBS + "uDE00b\", version: \"1\"," +
		" description: \"" + srE0 + srBS + "/\"}\npaths: {}\n"

	doc, diags = compileText(t, "spec.yaml", mixed, openapi.Options{})

	require.NotNil(t, doc)
	assert.Empty(t, diags)
	assert.Equal(t, "a\U0001F600b", doc.Name)
	assert.Equal(t, srE0+"/", doc.Docs.Description, "a raw U+E000 and an escaped slash read as they do beside no pair")

	before := "openapi: 3.1.0\ninfo: {description: \"" + srBS + "/" + srNEL + "\", title: \"a" + srBS + "uD83D" + srBS +
		"uDE00b\", version: \"1\"}\npaths: {}\n"

	doc, diags = compileText(t, "spec.yaml", before, openapi.Options{})

	require.NotNil(t, doc, "%+v", diags)
	assert.Empty(t, diags)
	assert.Equal(t, "a\U0001F600b", doc.Name, "a pair after triggers whose placeholders are wider than they are")
	assert.Equal(t, "/"+srNEL, doc.Docs.Description)
}
