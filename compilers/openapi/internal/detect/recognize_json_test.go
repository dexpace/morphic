// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package detect_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
)

// jsonHead opens a JSON source that declares OpenAPI 3.0 and writes a
// description whose value the tests below complete.
const jsonHead = `{"openapi":"3.0.3","info":{"title":"t","version":"1","description":"`

// escape spells a \uXXXX escape from its four hex digits, so that the halves of
// a surrogate pair are never adjacent where this file is written.
func escape(hex string) string { return `\u` + hex }

// TestRecognize_AJSONSourceWritingASurrogatePairIsRecognized pins that a JSON
// source that writes a character outside the Basic Multilingual Plane as the
// pair of escapes RFC 8259 gives it is recognized by what it declares and
// carries its parse, as the same source written with the character is
// (docs/parser-library.md#json-as-yaml).
func TestRecognize_AJSONSourceWritingASurrogatePairIsRecognized(t *testing.T) {
	t.Parallel()
	for name, src := range map[string]string{
		"a pair":                    jsonHead + `x` + escape("d83d") + escape("de00") + `y"},"paths":{}}`,
		"a pair in upper case":      jsonHead + escape("D83D") + escape("DE00") + `"},"paths":{}}`,
		"a solidus escape":          jsonHead + `a\/b"},"paths":{}}`,
		"a key past the YAML limit": jsonHead + `"},"paths":{"/` + longKey() + `":{}}}`,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got, diags, ok := recognize(t, src)

			assert.True(t, ok)
			assert.Equal(t, compilers.SourceFormat{Name: "openapi", Version: "3.0"}, got.Format)
			assert.Empty(t, diags)
			assert.NotNil(t, got.Parsed, "the parse the compile goes on to lower")
		})
	}
}

// longKey is a path longer than the YAML parser allows a key (its colon within
// 1,024 characters of the key's start), which JSON does not limit.
func longKey() string {
	key := make([]byte, 1100)
	for i := range key {
		key[i] = 'x'
	}
	return string(key)
}

// TestRecognize_ALoneSurrogateIsReportedWhereItIsWritten pins what a JSON
// source declaring a version and writing half of a surrogate pair is told: an
// undecodable source whose message names the escape as written and the line and
// column it begins at, where the YAML parser's names neither.
func TestRecognize_ALoneSurrogateIsReportedWhereItIsWritten(t *testing.T) {
	t.Parallel()
	cases := map[string]struct{ src, want string }{
		"a high half": {
			jsonHead + `x` + escape("d83d") + `y"},"paths":{}}`,
			"source declares an OpenAPI or Swagger version and cannot be read: line 1, column 70: " +
				`the escape \ud83d is half of a UTF-16 surrogate pair, written without its other half, so it names no character`,
		},
		"a low half on a later line": {
			"{\n \"openapi\":\"3.0.3\",\n \"info\":{\"title\":\"" + escape("DE00") + `","version":"1"},"paths":{}}`,
			"source declares an OpenAPI or Swagger version and cannot be read: line 3, column 19: " +
				`the escape \uDE00 is half of a UTF-16 surrogate pair, written without its other half, so it names no character`,
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got, diags, ok := recognize(t, tc.src)

			assert.False(t, ok)
			assert.Equal(t, compilers.SourceFormat{}, got.Format)
			require.Len(t, diags, 1)
			assert.Equal(t, diag.UndecodableSource, diags[0].Code)
			assert.Equal(t, tc.want, diags[0].Message)
		})
	}
}
