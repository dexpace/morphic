// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package bind_test

import (
	"encoding/json/jsontext"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/bind"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
)

// TestBind_ASelfIsADocumentsBaseAndName holds a document's $self, resolved
// against its name, to naming the document and basing its relative
// references: the source named by its $self in full and by a relative path
// lands in itself, another document's $self answers as an $id does, and a
// $self holding a `#`, or one Locate places nowhere, names nothing.
func TestBind_ASelfIsADocumentsBaseAndName(t *testing.T) {
	t.Parallel()
	other := "openapi: 3.2.0\n$self: https://e.test/o/other.yaml\nX: {type: string}\n"
	src := "openapi: 3.2.0\n$self: https://e.test/api/a.yaml\ncomponents:\n  schemas:\n" +
		"    U: {$ref: 'https://e.test/api/a.yaml#/components/schemas/B'}\n" +
		"    V: {$ref: 'a.yaml#/components/schemas/B'}\n    B: {type: string}\n" +
		"    W: {$ref: 'local/other.yaml#/X'}\n    Y: {$ref: 'https://e.test/o/other.yaml#/X'}\n"
	table := bound(t, src, bind.Config{Self: "spec.yaml", Open: func(name string) (*yaml.Node, error) {
		if name != "https://e.test/api/local/other.yaml" {
			return nil, errors.New("no such document")
		}
		return openapitest.YAMLDocument(t, other), nil
	}})

	assert.Equal(t, "https://e.test/api/a.yaml", table.Document(bind.Source).Self)
	for _, site := range []string{"/components/schemas/U", "/components/schemas/V"} {
		e := entryAt(t, table, jsontext.Pointer(site))
		require.Equal(t, bind.Resolved, e.Chain.Status, site)
		assert.Equal(t, bind.Source, e.Chain.End.Doc, "%s names the source by its $self", site)
	}
	w := entryAt(t, table, "/components/schemas/W")
	require.Equal(t, bind.Resolved, w.Chain.Status, "a relative reference is read against the $self")
	assert.Equal(t, "https://e.test/api/local/other.yaml", table.Document(w.Chain.End.Doc).Name)
	assert.Equal(t, "https://e.test/o/other.yaml", table.Document(w.Chain.End.Doc).Self)
	y := entryAt(t, table, "/components/schemas/Y")
	require.Equal(t, bind.Resolved, y.Chain.Status, "another document's $self is declared at its root")
	assert.Equal(t, w.Chain.End, y.Chain.End)

	for name, written := range map[string]string{"a fragment": "https://e.test/a.yaml#f", "placed nowhere": "nowhere"} {
		nameless := bound(t, "openapi: 3.2.0\n$self: '"+written+"'\n", bind.Config{Self: "spec.yaml",
			Locate: func(uri, _ string) (string, bool) { return uri, uri != "nowhere" }})
		assert.Empty(t, nameless.Document(bind.Source).Self, name)
	}
}

// TestBind_AMappedLocationIsJudgedWhereAFetchedDocumentReadsIt holds a
// reference a document read over the network writes to a URL that Locate
// places on a file: the located name is judged as well as the URL, so the
// file is never read.
func TestBind_AMappedLocationIsJudgedWhereAFetchedDocumentReadsIt(t *testing.T) {
	t.Parallel()
	var opened []string
	table := bound(t, "openapi: 3.1.0\ncomponents:\n  schemas:\n    A: {$ref: 'https://n.test/net.yaml#/X'}\n",
		bind.Config{Self: "spec.yaml",
			Locate: func(uri, _ string) (string, bool) {
				if uri == "https://m.test/b.yaml" {
					return "/files/b.yaml", true
				}
				return uri, true
			},
			Open: func(name string) (*yaml.Node, error) {
				opened = append(opened, name)
				return openapitest.YAMLDocument(t, "X: {$ref: 'https://m.test/b.yaml#/B'}\n"), nil
			}})

	var why string
	for _, e := range table.Entries() {
		if e.Hop.Status == bind.Unresolved {
			why = e.Hop.Why()
		}
	}
	assert.Contains(t, why, bind.OffNetwork)
	assert.Equal(t, []string{"https://n.test/net.yaml"}, opened)
}

// TestBind_ADeclarationAnswersOnlyWhoCouldReadItsDocument holds an identifier
// to the documents that could read its declarer: a file's answers the source,
// a fetched document's answers itself but no other document for a URI of
// another host, which is then read as a location, and a fetched document is
// answered by no file.
func TestBind_ADeclarationAnswersOnlyWhoCouldReadItsDocument(t *testing.T) {
	t.Parallel()
	docs := map[string]string{
		"/local.yaml":                "Z: {$id: 'https://t.test/p.yaml', type: object}\n",
		"https://h.test/hijack.yaml": "Z: {$id: 'https://t.test/q.yaml', type: object}\nW: {$ref: 'https://t.test/q.yaml'}\n",
		"https://h.test/evil.yaml":   "X: {$ref: 'https://t.test/p.yaml'}\n",
	}
	var opened []string
	src := "openapi: 3.1.0\ncomponents:\n  schemas:\n    L: {$ref: 'local.yaml#/Z'}\n" +
		"    P: {$ref: 'https://t.test/p.yaml'}\n    Q: {$ref: 'https://t.test/q.yaml'}\n" +
		"    H: {$ref: 'https://h.test/hijack.yaml#/W'}\n    E: {$ref: 'https://h.test/evil.yaml#/X'}\n"
	cfg := resolving
	cfg.Open = func(name string) (*yaml.Node, error) {
		opened = append(opened, name)
		if doc, ok := docs[name]; ok {
			return openapitest.YAMLDocument(t, doc), nil
		}
		return nil, errors.New("not found")
	}
	table := bound(t, src, cfg)

	local, hijack := docNamed(t, table, "/local.yaml"), docNamed(t, table, "https://h.test/hijack.yaml")
	assert.Equal(t, bind.Position{Doc: local, Pointer: "/Z"}, entryAt(t, table, "/components/schemas/P").Chain.End,
		"a file's declaration answers the source")
	assert.Equal(t, bind.Unresolved, entryAt(t, table, "/components/schemas/Q").Chain.Status,
		"another host's declaration answers no other document: the URL is read")
	assert.Equal(t, bind.Position{Doc: hijack, Pointer: "/Z"}, entryAt(t, table, "/components/schemas/H").Chain.End,
		"a document answers itself")
	assert.Equal(t, bind.Unresolved, entryAt(t, table, "/components/schemas/E").Chain.Status,
		"a fetched document is answered by no file")
	assert.Contains(t, opened, "https://t.test/q.yaml")
	assert.Contains(t, opened, "https://t.test/p.yaml")
}
