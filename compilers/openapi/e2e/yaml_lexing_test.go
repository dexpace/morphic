// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package e2e_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/engine/reader"
	"github.com/dexpace/morphic/ir"
)

// titled is a document whose info.title is the YAML text title and whose
// description is the single-quoted scalar a<NEL>b, before a trailing paths.
func titled(title string) string {
	return "openapi: 3.1.0\ninfo:\n  title: " + title + "\n  version: \"1\"\n  description: 'a" + srNEL + "b'\npaths: {}\n"
}

// TestCompile_AnEscapedSlashReadsAsASlash holds a YAML 1.2 escape the library
// refuses to the reading YAML 1.2 gives it, with the same IR and the same
// positions as the document written with a plain slash.
func TestCompile_AnEscapedSlashReadsAsASlash(t *testing.T) {
	t.Parallel()
	escaped := "openapi: 3.1.0\ninfo: {title: \"a" + srBS + "/b\", version: \"1\"}\npaths: {}\n"
	plain := "openapi: 3.1.0\ninfo: {title: \"a/b\",  version: \"1\"}\npaths: {}\n"

	doc, diags := compileText(t, "spec.yaml", escaped, openapi.Options{})
	twin, twinDiags := compileText(t, "spec.yaml", plain, openapi.Options{})

	require.NotNil(t, doc)
	assert.Empty(t, diags)
	assert.Equal(t, "a/b", doc.Name)
	assert.Equal(t, jsonOf(t, twin), jsonOf(t, doc), "the IR is the twin's")
	assert.Equal(t, lines(twinDiags), lines(diags))

	// An escaped backslash, then the escape of a, is three characters of text
	// and not a slash or a bell.
	doc, diags = compileText(t, "spec.yaml",
		"openapi: 3.1.0\ninfo: {title: \"x"+srBS+srBS+srBS+"x61y\", version: \"1\"}\npaths: {}\n", openapi.Options{})
	require.NotNil(t, doc)
	assert.Empty(t, diags)
	assert.Equal(t, "x"+srBS+"ay", doc.Name)
}

// TestCompile_ALineBreakInACommentEndsNoLine holds the characters YAML 1.1 ends
// a line at, which YAML 1.2 does not, to what YAML 1.2 says: one in a comment is
// part of the comment, so what follows it is not a second `paths`.
func TestCompile_ALineBreakInACommentEndsNoLine(t *testing.T) {
	t.Parallel()
	src := "openapi: 3.1.0\ninfo: {title: t, version: \"1\"} # c" + srNEL + "paths: {}\n" +
		"paths: {/a: {get: {operationId: getA, responses: {\"200\": {description: ok}}}}}\n"

	doc, diags := compileText(t, "spec.yaml", src, openapi.Options{})

	require.NotNil(t, doc)
	assert.Empty(t, diags, "no duplicate key is written")
	assert.Len(t, allOperations(doc), 1)
}

// TestCompile_APlaceholderNeverShowsInTheIR holds the respelling invisible in
// the IR, for the characters a text can spell that a placeholder might have been.
func TestCompile_APlaceholderNeverShowsInTheIR(t *testing.T) {
	t.Parallel()
	titles := map[string]string{
		"an escape":               "\"x" + srBS + "uE000y\"",
		"an escape in lower case": "\"x" + srBS + "ue000y\"",
		"a long escape":           "\"x" + srBS + "U0000E000y\"",
		"the character itself":    "\"x" + srE0 + "y\"",
	}
	for name, title := range titles {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			doc, diags := compileText(t, "spec.yaml", titled(title), openapi.Options{})

			require.NotNil(t, doc)
			assert.Empty(t, diags)
			assert.Equal(t, "x"+srE0+"y", doc.Name)
			assert.Equal(t, "a"+srNEL+"b", doc.Docs.Description)
		})
	}

	doc, diags := compileText(t, "spec.yaml",
		strings.Replace(titled("\"x"+srBS+"uE000y\""), "paths: {}", "x-t: !e%EE%80%80 v\npaths: {}", 1), openapi.Options{})
	require.NotNil(t, doc)
	assert.Empty(t, diags, "a tag spelled with a percent run beside a placeholder-sized character")
	assert.Equal(t, "x"+srE0+"y", doc.Name)
}

// TestCompile_ADirectiveLookalikeInAScalarIsText holds a line inside a scalar
// that starts with %YAML to being the scalar's text, not a directive.
func TestCompile_ADirectiveLookalikeInAScalarIsText(t *testing.T) {
	t.Parallel()
	src := "openapi: 3.1.0\ninfo:\n  title: t\n  version: \"1\"\n  description: \"a\n%YAML 1.3\n  b\"\npaths: {}\n"

	doc, diags := compileText(t, "spec.yaml", src, openapi.Options{})

	require.NotNil(t, doc)
	assert.Empty(t, diags)
	assert.Equal(t, "a %YAML 1.3 b", doc.Docs.Description)
}

// TestCompile_ATextSpellingEveryPlaceholderIsTheParsersOwn states the limit: a
// text that leaves no placeholder free is read as the parser reads it.
func TestCompile_ATextSpellingEveryPlaceholderIsTheParsersOwn(t *testing.T) {
	t.Parallel()
	var every []string
	for r := rune(0xE000); r < 0xE040; r++ {
		every = append(every, "\""+string(r)+"\"")
	}
	src := "openapi: 3.1.0\ninfo: {title: \"a" + srNEL + "b\", version: \"1\"}\npaths: {}\nx-e: [" + strings.Join(every, ", ") + "]\n"

	doc, diags := compileText(t, "spec.yaml", src, openapi.Options{})

	require.NotNil(t, doc)
	assert.Empty(t, diags)
	assert.Equal(t, "a b", doc.Name, "the raw U+0085 in a double-quoted scalar reads as the parser reads it")
}

// TestCompile_ASurrogatePairEscapeInYAMLIsRefused pins a stated limit: the
// lexer refuses a pair of surrogate escapes in YAML text, which YAML 1.2 does
// not join.
func TestCompile_ASurrogatePairEscapeInYAMLIsRefused(t *testing.T) {
	t.Parallel()
	src := "openapi: 3.1.0\ninfo: {title: \"a" + srBS + "uD83D" + srBS + "uDE00b\", version: \"1\"}\npaths: {}\n"

	doc, diags := compileText(t, "spec.yaml", src, openapi.Options{})

	assert.Nil(t, doc)
	require.Len(t, diags, 1, "%+v", diags)
	assert.Equal(t, diag.UndecodableSource, diags[0].Code)
	assert.Equal(t, ir.SeverityError, diags[0].Severity)
	assert.Contains(t, diags[0].Message, "found invalid Unicode character escape code")
}

// TestCompile_AJSONDocumentReadsAlikeBehindEverySpelling holds a document a
// reference reads to one reading whatever way the reference spells its URL: a
// JSON document with a surrogate pair escaped and a raw U+0085, reached behind
// each spelling of the scheme, gives the same IR and no diagnostic.
func TestCompile_AJSONDocumentReadsAlikeBehindEverySpelling(t *testing.T) {
	t.Parallel()
	body := `{"a":{"type":"string","description":"x` + srNEL + `y","title":"` + srBS + `ud83d` + srBS + `ude00"}}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, err := w.Write([]byte(body))
		assert.NoError(t, err)
	}))
	t.Cleanup(srv.Close)
	host := strings.TrimPrefix(srv.URL, "http://")

	schemes := []string{"http", "HTTP"}
	docs := make([]*ir.Document, 0, len(schemes))
	for _, scheme := range schemes {
		src := srMin + "components: {schemas: {S: {$ref: \"" + scheme + "://" + host + "/doc.json#/a\"}}}\n"

		doc, diags, err := openapi.New().Compile(t.Context(),
			[]compilers.Source{{Path: "root.yaml", Data: []byte(src)}},
			compilers.Options{Reader: reader.Disk{}, FormatOptions: openapi.Options{AllowExternalRefs: true}})

		require.NoError(t, err, scheme)
		require.NotNil(t, doc, scheme)
		assert.Empty(t, diags, scheme)
		docs = append(docs, doc)
	}

	for i, doc := range docs {
		var found bool
		for id, td := range doc.Types {
			if !strings.HasPrefix(string(id), "t/anon@") {
				continue
			}
			found = true
			assert.Equal(t, "x"+srNEL+"y", td.Common().Docs.Description, "spelling %d", i)
			assert.Equal(t, "\U0001F600", td.Common().Docs.Summary, "spelling %d: the pair joined", i)
		}
		assert.True(t, found, "the document's type is lowered, spelling %d", i)
	}
}
