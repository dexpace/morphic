// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package load

import (
	"net/http"
	"path/filepath"
	"testing"

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
// name a local location, and the location each names: a file: URI does, and a
// drive path, which a reader reads from disk, and nothing else. An absolute
// path and a network-path reference name a resource on the network, as RFC 3986
// §5.2 resolves them against the document's URL (GitHub #936), as do a relative
// path and an absolute URL; an internal pointer names the document itself, and
// a URI on another scheme names no file.
func TestLocalTarget(t *testing.T) {
	t.Parallel()
	const base = "http://host/dir/a.yaml"
	tests := []struct {
		ref, target string
		local       bool
	}{
		{"/etc/passwd", "http://host/etc/passwd", false},
		{"//evil/etc/passwd", "http://evil/etc/passwd", false},
		{"/x/", "http://host/x/", false},
		{"/x/b.yaml#/S", "http://host/x/b.yaml", false},
		{"file:///etc/passwd", "file:///etc/passwd", true},
		{"FILE:///etc/passwd", "FILE:///etc/passwd", true},
		{"./b.yaml", "http://host/dir/b.yaml", false},
		{"../c.yaml", "http://host/c.yaml", false},
		{"https://other/d.yaml", "https://other/d.yaml", false},
		{"#/components/schemas/S", base, false},
		{"./v2/", "http://host/dir/v2/", false},
		{"urn:example:pets", "urn:example:pets", false},
		{"tag:example.com,2026:pets", "tag:example.com,2026:pets", false},
	}
	for _, tc := range tests {
		t.Run(tc.ref, func(t *testing.T) {
			t.Parallel()
			target, local := localTarget(tc.ref, base)
			assert.Equal(t, tc.local, local)
			assert.Equal(t, tc.target, target)
		})
	}
	_, local := localTarget("://bad", base)
	assert.False(t, local, "the resolver cannot place it, and reports it itself")
	_, local = localTarget(`C:\x.yaml`, base)
	assert.True(t, local, "a drive path, spelled as the system spells it, a reader reads from disk")
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
func remoteReader(t *testing.T) *external {
	t.Helper()
	self := sourceDocument{judged: judgment{checker: oas.NewChecker()}}
	return newExternal("3.1.0", Options{Reader: reader.Disk{}}, newExternalReads(self, budget.Unbounded()))
}

// TestExternal_DocumentRefusesANetworkDocumentReachingALocalFile pins GitHub #929: a
// document fetched over the network that references a local file is refused
// when it is prepared, before the file is opened, a $ref naming a file: URI or
// an identifier rebasing one onto one. The refusal names the $ref and the
// file, and the document is not stored. An absolute path names a resource on
// the network (TestLocalTarget).
func TestExternal_DocumentRefusesANetworkDocumentReachingALocalFile(t *testing.T) {
	t.Parallel()
	tests := map[string]struct{ body, reason string }{
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
			ext := remoteReader(t)

			_, err := ext.document(t.Context(), key)

			require.Error(t, err)
			assert.Contains(t, err.Error(), "external document "+key+" refused: it is fetched over the network and "+tc.reason)
			_, ok := ext.read.preparedFor(key)
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

// TestReachesLocal_NamesTheLeastOffenderInEitherOrder pins whose value the
// refusal of a network document quotes when several reach the local file
// system: the one at the least pointer, whichever is declared first (GitHub
// #940). Of one schema's, an $id sorts before its $ref, and of a key written
// twice, the first is named; an `id` read as draft 4 reads it is named the same
// way, where no $ref or $id reaches the disk.
func TestReachesLocal_NamesTheLeastOffenderInEitherOrder(t *testing.T) {
	t.Parallel()
	const draft4 = "    D: {$schema: 'http://json-schema.org/draft-04/schema#'}\n"
	for _, tc := range []struct{ name, first, second, quoted string }{
		{"two $refs", "    a: {$ref: 'file:///etc/a.yaml'}\n", "    b: {$ref: 'file:///etc/b.yaml'}\n",
			`its $ref "file:///etc/a.yaml" reaches the local file file:///etc/a.yaml`},
		{"an $id beside a $ref", "    a: {$ref: 'file:///etc/a.yaml', $id: 'file:///etc/'}\n", "    b: {$ref: 'file:///etc/b.yaml'}\n",
			`its $id "file:///etc/" rebases references onto the local file:///etc/`},
		{"a key written twice", "    a: {$ref: 'file:///etc/a.yaml', $ref: 'file:///etc/c.yaml'}\n", "    b: {$ref: 'file:///etc/b.yaml'}\n",
			`its $ref "file:///etc/a.yaml" reaches the local file file:///etc/a.yaml`},
		{"two ids", draft4 + "    a: {id: 'file:///etc/a/'}\n", "    b: {id: 'file:///etc/b/'}\n",
			`its id "file:///etc/a/" rebases references onto the local file:///etc/a/`},
	} {
		for _, order := range []string{tc.first + tc.second, tc.second + tc.first} {
			root := openapitest.YAMLDocument(t, "components:\n  schemas:\n"+order)

			reason, refuse := reachesLocal(t.Context(), root, "http://host/x.yaml", readsDraft4ID)

			require.True(t, refuse, "%s: %s", tc.name, order)
			assert.Equal(t, tc.quoted, reason, "%s: %s", tc.name, order)
		}
	}
}

// TestExternal_DocumentReadsANetworkDocumentStayingOnTheHost pins the other side:
// a network document whose $refs stay on the host is prepared and stored, not
// refused, and so is one whose example writes an `id` a drive letter would
// spell, as no dialect it names reads one.
func TestExternal_DocumentReadsANetworkDocumentStayingOnTheHost(t *testing.T) {
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
			ext := remoteReader(t)

			_, err := ext.document(t.Context(), key)

			require.NoError(t, err)
			_, ok := ext.read.preparedFor(key)
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
		_, err := w.Write([]byte(remoteDoc("file://" + filepath.ToSlash(filepath.Join(dir, "local.yaml")) +
			"#/components/schemas/S")))
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
