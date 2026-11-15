// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package load

import (
	"context"
	"strings"
	"testing"

	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi/internal/budget"
	"github.com/dexpace/morphic/compilers/openapi/internal/doctree"
	refscope "github.com/dexpace/morphic/compilers/openapi/internal/resolve"
	"github.com/dexpace/morphic/engine/reader"
)

// jsonHalf is a source that writes a description holding the escape of half a
// surrogate pair, at line 1, column 70.
func jsonHalf() string {
	return `{"openapi":"3.0.3","info":{"title":"t","version":"1","description":"x` + `\u` + `d83d` + `y"},"paths":{}}`
}

// jsonPair is a source that writes its description as a surrogate pair of
// escapes.
func jsonPair() string {
	return `{"openapi":"3.0.3","info":{"title":"t","version":"1","description":"x` +
		`\u` + `d83d` + `\u` + `de00` + `y"},"paths":{}}`
}

// description returns the value the document tree writes for info.description.
func description(t *testing.T, doc *yaml.Node) string {
	t.Helper()
	root := doc.Content[0]
	for i := 0; i+1 < len(root.Content); i += 2 {
		if root.Content[i].Value != "info" {
			continue
		}
		info := root.Content[i+1]
		for j := 0; j+1 < len(info.Content); j += 2 {
			if info.Content[j].Value == "description" {
				return info.Content[j+1].Value
			}
		}
	}
	require.Fail(t, "the source writes no info.description")
	return ""
}

// TestDecode_ReadsAJSONSourceAsJSON pins the route a source takes through
// Decode, which detection and the compile share: a source that is one JSON
// text is read as JSON, so a surrogate pair is the character it names, and one
// that is YAML is read as YAML (docs/parser-library.md#json-as-yaml).
func TestDecode_ReadsAJSONSourceAsJSON(t *testing.T) {
	t.Parallel()
	parsed, err := Decode(t.Context(), []byte(jsonPair()), budget.Unbounded())

	require.NoError(t, err)
	assert.Equal(t, "x😀y", description(t, parsed.Root()))

	yamlParsed, err := Decode(t.Context(), []byte("openapi: 3.0.3\ninfo: {title: t, version: '1', description: \"x\\U0001F600y\"}\npaths: {}\n"), budget.Unbounded())
	require.NoError(t, err)
	assert.Equal(t, "x😀y", description(t, yamlParsed.Root()))
}

// TestDecode_ReturnsALoneSurrogateAsItIs pins the error a source writing half
// a surrogate pair gets: the *doctree.LoneSurrogateError itself, which
// detection quotes, and Load wraps as ErrParse, where the YAML parser's error
// for the same bytes is not the reader's to give.
func TestDecode_ReturnsALoneSurrogateAsItIs(t *testing.T) {
	t.Parallel()
	parsed, err := Decode(t.Context(), []byte(jsonHalf()), budget.Unbounded())

	assert.Nil(t, parsed)
	var lone *doctree.LoneSurrogateError
	require.ErrorAs(t, err, &lone)
	assert.Same(t, lone, err, "not wrapped")
	assert.Equal(t, `line 1, column 70: the escape \ud83d is half of a UTF-16 surrogate pair, `+
		`written without its other half, so it names no character`, err.Error())

	doc, _, loadErr := Load(t.Context(), 0, compilers.Source{Path: "spec.json", Data: []byte(jsonHalf())}, Options{})
	assert.Nil(t, doc)
	require.ErrorIs(t, loadErr, ErrParse)
	assert.ErrorAs(t, loadErr, &lone, "Load quotes the reader's error")
}

// TestDecode_AJSONTextBesideAStreamIsReadAsYAML pins that a source is read as
// JSON only when it is one JSON text and nothing else: a JSON document
// followed by a stream's separator and another document, or by a comment, is
// YAML's to read, so the documents after the first are still reported as
// dropped, and a comment still ends a document. A solidus escape, which only
// the JSON reader accepts, tells the two reads apart: the source that writes
// one is read when it stands alone and refused as YAML when a comment follows.
func TestDecode_AJSONTextBesideAStreamIsReadAsYAML(t *testing.T) {
	t.Parallel()
	const first = `{"openapi":"3.1.0","info":{"title":"T","version":"1"},"paths":{}}`

	doc, diags, err := Load(t.Context(), 0, compilers.Source{Path: "stream.json", Data: []byte(first + "\n---\n" + first + "\n")}, Options{})
	require.NoError(t, err)
	require.NotNil(t, doc)
	got := streamDiags(diags)
	require.Len(t, got, 1, "the second document is dropped and reported")
	assert.Contains(t, got[0].Message, "the one after it holds")

	doc, diags, err = Load(t.Context(), 0, compilers.Source{Path: "commented.json", Data: []byte(first + "\n# end\n")}, Options{})
	require.NoError(t, err)
	require.NotNil(t, doc)
	assert.Empty(t, streamDiags(diags))

	solidus := strings.Replace(first, `"T"`, `"a\/b"`, 1)
	doc, _, err = Load(t.Context(), 0, compilers.Source{Path: "solidus.json", Data: []byte(solidus + "\n")}, Options{})
	require.NoError(t, err, "a solidus escape alone is read as JSON")
	require.NotNil(t, doc)

	doc, _, err = Load(t.Context(), 0, compilers.Source{Path: "solidus.json", Data: []byte(solidus + "\n# end\n")}, Options{})
	require.NoError(t, err, "a solidus escape beside a comment is YAML's to read, which reads it as YAML 1.2 does")
	require.NotNil(t, doc)
	assert.Equal(t, "a/b", doc.Doc.Info.Title)
}

// TestParseTree_ReadsJSONAsJSON pins the parse of an external document and of
// an overlay before it is applied: JSON is read as JSON, so a surrogate pair
// in it parses, half of one is the reader's error as it is, a done context is
// ctx's error and no fall back to YAML, and anything else is YAML.
func TestParseTree_ReadsJSONAsJSON(t *testing.T) {
	t.Parallel()
	tree, err := parseTree(t.Context(), []byte(jsonPair()))
	require.NoError(t, err)
	assert.Equal(t, "x😀y", description(t, tree))

	var lone *doctree.LoneSurrogateError
	tree, err = parseTree(t.Context(), []byte(jsonHalf()))
	assert.Nil(t, tree)
	require.ErrorAs(t, err, &lone)
	assert.Same(t, lone, err, "not wrapped")

	cancelled, cancel := context.WithCancel(t.Context())
	cancel()
	tree, err = parseTree(cancelled, []byte(jsonPair()))
	assert.Nil(t, tree)
	assert.ErrorIs(t, err, context.Canceled)

	tree, err = parseTree(t.Context(), []byte("a: [1, 2]\n"))
	require.NoError(t, err)
	assert.Equal(t, yaml.DocumentNode, tree.Kind)
	assert.Equal(t, "a", tree.Content[0].Content[0].Value)
}

// TestReadJSON_ADoneContextEndsTheReadWithoutAYAMLParse pins that a context
// done as the JSON reader asks it is ctx's error from readJSON itself: the
// reader's one ask and the check of its error are the only two asks, where a
// read handed on to the YAML parser would ask a third time, through its reader.
func TestReadJSON_ADoneContextEndsTheReadWithoutAYAMLParse(t *testing.T) {
	t.Parallel()
	for name, read := range map[string]func(context.Context) error{
		"a source": func(ctx context.Context) error {
			_, err := Decode(ctx, []byte(jsonPair()), budget.Unbounded())
			return err
		},
		"an external document": func(ctx context.Context) error {
			_, err := parseTree(ctx, []byte(jsonPair()))
			return err
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			ctx := liveFor(t, 0)

			err := read(ctx)

			require.ErrorIs(t, err, context.Canceled)
			assert.Equal(t, 2, ctx.Calls, "the reader's first ask, and the check of its error")
		})
	}
}

// TestReadJSON_AContextDoneByTheEndOfTheReadIsCtxsError pins that the error a
// done context leaves is ctx's, as every other step of the load reports it,
// when the context is done after the reader's last ask and the reader ends the
// read with its own error, half of a surrogate pair.
func TestReadJSON_AContextDoneByTheEndOfTheReadIsCtxsError(t *testing.T) {
	t.Parallel()
	ctx := liveFor(t, 1)

	_, err := Decode(ctx, []byte(jsonHalf()), budget.Unbounded())

	assert.ErrorIs(t, err, context.Canceled)
	assert.Equal(t, 2, ctx.Calls, "the reader's one ask, which passed, and the check of its error")
}

// TestLoad_AJSONDocumentBehindARespelledURLIsReadAsJSON holds every spelling
// of an external JSON document's URL to the canonical one's outcome: a URL
// net/url respells, an upper-case or mixed-case scheme or escaped userinfo,
// reads the tree the compiler prepared for the document, as JSON, since the
// compiler binds every reference itself and nothing parses the document again
// as YAML.
func TestLoad_AJSONDocumentBehindARespelledURLIsReadAsJSON(t *testing.T) {
	t.Parallel()
	ext := `{"openapi":"3.1.0","info":{"title":"O","version":"1"},"paths":{"/x":{"get":` +
		`{"operationId":"x` + `\u` + `d83d` + `\u` + `de00` + `y","responses":{"200":{"description":"ok"}}}}}}`
	scheme := func(spelled string) func(string) string {
		return func(c string) string { return spelled + "://" + strings.TrimPrefix(c, "http://") }
	}

	for name, spell := range map[string]func(canonical string) string{
		"the canonical spelling": scheme("http"),
		"an upper-case scheme":   scheme("HTTP"),
		"a mixed-case scheme":    scheme("Http"),
		"escaped userinfo":       func(c string) string { return strings.Replace(c, "http://", "http://us%65r@", 1) },
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			srv, _ := countingServer(t, ext)
			src := compilers.Source{Path: "root.yaml", Data: []byte(rootReferencing(spell(srv.URL + "/ext.json")))}

			loaded, diags, err := Load(t.Context(), 0, src, Options{Reader: reader.Disk{}})

			require.NoError(t, err)
			assert.Empty(t, diags)
			require.NotNil(t, loaded)
			ref, ok := loaded.Doc.Paths.Get("/x")
			require.True(t, ok)
			item := refscope.Object[soa.PathItem](loaded.Bindings, ref)
			require.NotNil(t, item, "the reference resolved to a path item")
			assert.Equal(t, "x\U0001F600y", item.Get().GetOperationID())
		})
	}
}
