// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package load

import (
	"net/http"
	"path/filepath"
	"testing"

	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/budget"
	"github.com/dexpace/morphic/compilers/openapi/internal/oas"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/engine/reader"
)

// TestOverNetwork pins which keys are a network fetch: http and https, in any
// case, and nothing else — a file: URI and a path are local.
func TestOverNetwork(t *testing.T) {
	t.Parallel()
	for key, want := range map[string]bool{
		"http://h/a.yaml":  true,
		"https://h/a.yaml": true,
		"HTTP://h/a.yaml":  true,
		"file:///a.yaml":   false,
		"/a/b.yaml":        false,
		"./a.yaml":         false,
		"":                 false,
	} {
		assert.Equal(t, want, overNetwork(key), key)
	}
}

// TestLocalTarget pins which $ref and $id values, read against a network base,
// name a local location: an absolute path, a protocol-relative //host path the
// resolver reads as a file, and a file: URI; and which do not: a relative path
// and an absolute URL, which stay on the network, an internal pointer, and a
// URI on another scheme, under which a relative $ref names no file either.
func TestLocalTarget(t *testing.T) {
	t.Parallel()
	const base = "http://host/dir/a.yaml"
	tests := []struct {
		ref   string
		local bool
	}{
		{"/etc/passwd", true},
		{"//evil/etc/passwd", true},
		{"file:///etc/passwd", true},
		{"FILE:///etc/passwd", true},
		{"./b.yaml", false},
		{"../c.yaml", false},
		{"https://other/d.yaml", false},
		{"#/components/schemas/S", false},
		{"/x/", true},
		{"./v2/", false},
		{"urn:example:pets", false},
		{"tag:example.com,2026:pets", false},
		{"://bad", false}, // the resolver cannot place it, and reports it itself
	}
	for _, tc := range tests {
		t.Run(tc.ref, func(t *testing.T) {
			t.Parallel()
			_, local := localTarget(tc.ref, base)
			assert.Equal(t, tc.local, local)
		})
	}
}

// remoteDoc is a document whose one schema is a $ref to ref, as a network
// document that reaches out of itself would be.
func remoteDoc(ref string) string {
	return "components:\n  schemas:\n    S: {$ref: '" + ref + "'}\n"
}

// readsDraft4ID is what external.readsID answers for a 3.1 source: draft 4,
// alone, names its identifier `id`.
func readsDraft4ID(dialect string) bool {
	return dialect == "http://json-schema.org/draft-04/schema#"
}

// remoteReader is the reader of a 3.1 source's external references, judging
// what it fetches against the rule table, as a load hands it one.
func remoteReader(t *testing.T, doc *soa.OpenAPI) external {
	t.Helper()
	doc.OpenAPI = "3.1.0"
	self := sourceDocument{judged: judgment{checker: oas.NewChecker()}}
	return newExternal(t.Context(), doc, Options{Reader: reader.Disk{}}, newExternalReads(self, budget.Unbounded()))
}

// TestExternal_DoRefusesANetworkDocumentReachingALocalFile pins GitHub #929: a
// document fetched over the network that references a local file is refused
// when it is prepared, before the file is opened, however the $ref spells the
// file — an absolute path, a //host path, or a file: URI. The refusal names
// the $ref and the file, and the document is not stored.
func TestExternal_DoRefusesANetworkDocumentReachingALocalFile(t *testing.T) {
	t.Parallel()
	tests := map[string]struct{ body, reason string }{
		"an absolute path $ref":   {remoteDoc("/etc/passwd"), `its $ref "/etc/passwd" reaches the local file /etc/passwd`},
		"a //host $ref":           {remoteDoc("//evil/etc/passwd"), `its $ref "//evil/etc/passwd" reaches the local file //evil/etc/passwd`},
		"a file: $ref":            {remoteDoc("file:///etc/passwd"), `its $ref "file:///etc/passwd" reaches the local file file:///etc/passwd`},
		"a file: $id above a ref": {"components:\n  schemas:\n    S:\n      $id: 'file:///x/'\n      $ref: './secret.yaml#/x'\n", `its $id "file:///x/" rebases references onto the local file:///x/`},
		"a draft 4 file: id above a ref": {"components:\n  schemas:\n    S:\n      $schema: 'http://json-schema.org/draft-04/schema#'\n" +
			"      id: 'file:///x/'\n      properties: {p: {$ref: 'secret.yaml'}}\n", `its id "file:///x/" rebases references onto the local file:///x/`},
		"a draft 4 file: id among others": {"components:\n  schemas:\n    R: {id: 'https://host/r/'}\n    S:\n" +
			"      $schema: 'http://json-schema.org/draft-04/schema#'\n      id: 'file:///x/'\n" +
			"      properties: {p: {$ref: 'secret.yaml'}}\n    T: {id: 'https://host/t/'}\n",
			`its id "file:///x/" rebases references onto the local file:///x/`},
		"a draft 4 id the binder trims": {"components:\n  schemas:\n    S:\n      $schema: 'http://json-schema.org/draft-04/schema#'\n" +
			"      id: ' FILE:///x/#'\n      properties: {p: {$ref: 'secret.yaml'}}\n", `its id " FILE:///x/#" rebases references onto the local file:///x/`},
		"a draft 4 id naming a drive": {"jsonSchemaDialect: 'http://json-schema.org/draft-04/schema#'\nopenapi: 3.1.0\n" +
			"components:\n  schemas:\n    S:\n      id: 'c:/x/'\n      properties: {p: {$ref: 'secret.yaml'}}\n",
			`its id "c:/x/" rebases references onto the local c:///x/`},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			srv := newTestServer(t, func(w http.ResponseWriter, _ *http.Request) {
				_, err := w.Write([]byte(tc.body))
				assert.NoError(t, err)
			})
			key := srv.URL + "/remote.yaml"
			req, err := http.NewRequest(http.MethodGet, key, nil)
			require.NoError(t, err)
			doc := &soa.OpenAPI{}

			_, err = remoteReader(t, doc).Do(req)

			require.Error(t, err)
			assert.Contains(t, err.Error(), "external document "+key+" refused: it is fetched over the network and "+tc.reason)
			_, ok := doc.GetCachedExternalDocument(key)
			assert.False(t, ok, "nothing is stored for a refused document")
		})
	}
}

// TestReachesLocal_PassesALocalDocumentAndANetworkOne pins what reachesLocal
// leaves alone: a local document (its base is not a network one) is never
// scanned, however its $refs spell a file, and a network document whose $refs
// and $ids name no local location reaches no local file, a urn: $id above a
// relative $ref included, another key's value spelling a path is not read as a
// reference, and an `id` naming no file: URI, an example's among them, stays on
// the host. Where no dialect the document names reads an identifier in `id`,
// no `id` reaches anything.
func TestReachesLocal_PassesALocalDocumentAndANetworkOne(t *testing.T) {
	t.Parallel()
	local := openapitest.YAMLDocument(t, remoteDoc("/etc/passwd"))
	_, refuse := reachesLocal(t.Context(), local, "/dir/a.yaml", readsDraft4ID)
	assert.False(t, refuse, "a local document's own $refs are not judged against the network rule")

	onHost := "components:\n  schemas:\n" +
		"    A: {$ref: './b.yaml#/x'}\n" +
		"    B: {$ref: 'https://other/c.yaml#/x'}\n" +
		"    C: {$id: './v2/', $ref: '#/components/schemas/A'}\n" +
		"    D: {$id: 'urn:example:d', $ref: './d.yaml#/x'}\n" +
		"    E: {description: '/etc/passwd', default: 'file:///etc/passwd'}\n" +
		"    F: {example: {id: '/etc/passwd'}, id: 'urn:example:f'}\n" +
		"    G: {$schema: 'http://json-schema.org/draft-04/schema#', id: './v3/', properties: {p: {$ref: 'q.yaml'}}}\n"
	_, refuse = reachesLocal(t.Context(), openapitest.YAMLDocument(t, onHost), "http://host/dir/a.yaml", readsDraft4ID)
	assert.False(t, refuse, "a network document staying on the host reaches no local file")

	noID := "components:\n  schemas:\n" +
		"    A: {example: {id: 'u:42'}, $schema: 'http://json-schema.org/draft-07/schema#'}\n" +
		"    B: {id: 'file:///x/', properties: {p: {$ref: 'secret.yaml'}}}\n"
	_, refuse = reachesLocal(t.Context(), openapitest.YAMLDocument(t, noID), "http://host/dir/a.yaml", readsDraft4ID)
	assert.False(t, refuse, "no dialect the document names reads an id")
	_, refuse = reachesLocal(t.Context(), openapitest.YAMLDocument(t, noID+
		"    C: {$schema: 'http://json-schema.org/draft-04/schema#'}\n"), "http://host/dir/a.yaml", readsDraft4ID)
	assert.True(t, refuse, "once it names draft 4, an id naming a file rebases its schema's references")
}

// TestExternal_DoReadsANetworkDocumentStayingOnTheHost pins the other side:
// a network document whose $refs stay on the host is prepared and stored, not
// refused, and so is one whose example writes an `id` a drive letter would
// spell, as no dialect it names reads one.
func TestExternal_DoReadsANetworkDocumentStayingOnTheHost(t *testing.T) {
	t.Parallel()
	for name, body := range map[string]string{
		"a relative $ref":       remoteDoc("./b.yaml#/x"),
		"an example's id":       "openapi: 3.1.0\ncomponents:\n  schemas:\n    S: {type: object, example: {id: 'u:42'}}\n",
		"a 2020-12 schema's id": "components:\n  schemas:\n    S: {id: 'file:///x/', properties: {p: {$ref: 'b.yaml'}}}\n",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			srv := newTestServer(t, func(w http.ResponseWriter, _ *http.Request) {
				_, err := w.Write([]byte(body))
				assert.NoError(t, err)
			})
			key := srv.URL + "/a.yaml"
			req, err := http.NewRequest(http.MethodGet, key, nil)
			require.NoError(t, err)
			doc := &soa.OpenAPI{}

			resp, err := remoteReader(t, doc).Do(req)

			require.NoError(t, err)
			t.Cleanup(func() { assert.NoError(t, resp.Body.Close()) })
			_, ok := doc.GetCachedExternalDocument(key)
			assert.True(t, ok, "a document that stays on the host is prepared")
		})
	}
}

// TestLoad_ANetworkDocumentReachingALocalFileIsRefusedAndReadsNothing pins the
// whole path through Load: the referencing $ref is reported unresolved with
// the reason, and the local file — here one that would itself fail to load, so
// a read would show — is never opened. A two-order check: the local file is
// also referenced straight from the source, which resolves on its own, so the
// network crossing is the only thing refused, in either declaration order.
func TestLoad_ANetworkDocumentReachingALocalFileIsRefusedAndReadsNothing(t *testing.T) {
	t.Parallel()
	dir := externalDir(t, map[string]string{
		"local.yaml": "components:\n  schemas:\n    S: {type: string}\n",
	})
	srv := newTestServer(t, func(w http.ResponseWriter, _ *http.Request) {
		_, err := w.Write([]byte(remoteDoc(filepath.Join(dir, "local.yaml") + "#/components/schemas/S")))
		assert.NoError(t, err)
	})
	remote := "    R: {$ref: '" + srv.URL + "/remote.yaml#/components/schemas/S'}\n"
	fromSource := "    L: {$ref: './local.yaml#/components/schemas/S'}\n"
	for _, order := range []string{remote + fromSource, fromSource + remote} {
		spec := "openapi: 3.1.0\ninfo: {title: T, version: \"1\"}\npaths: {}\ncomponents:\n  schemas:\n" + order

		_, diags := loadExternal(t, dir, spec, Options{})

		_, unresolved := budgetFindings(diags)
		require.Len(t, unresolved, 1, "only the network crossing is unresolved: %+v", diags)
		assert.Contains(t, unresolved[0], "reaches the local file")
		assert.NotContains(t, unresolved[0], "./local.yaml", "the source's own reference to the file resolves")
	}
}
