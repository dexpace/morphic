// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package load

import (
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/bind"
	"github.com/dexpace/morphic/compilers/openapi/internal/budget"
	"github.com/dexpace/morphic/compilers/openapi/internal/oas"
	"github.com/dexpace/morphic/engine/reader"
)

// remoteDoc is a document whose one schema is a $ref to ref, as a network
// document that reaches out of itself would be.
func remoteDoc(ref string) string {
	return "components:\n  schemas:\n    S: {$ref: '" + ref + "'}\n"
}

// remoteReader is the reader of a 3.1 source's external references, judging
// what it fetches against the rule table, as a load hands it one.
func remoteReader(t *testing.T, doc *soa.OpenAPI) external {
	t.Helper()
	doc.OpenAPI = "3.1.0"
	self := sourceDocument{judged: judgment{checker: oas.NewChecker()}}
	return newExternal(t.Context(), doc, Options{Reader: reader.Disk{}}, newExternalReads(self, budget.Unbounded()))
}

// TestEscapes_RefusesALocalReferenceOfANetworkDocument pins how a reference
// object of a document read over the network, which the parser's resolver
// reads, is kept off the local file system: respelled under refusedScheme,
// its refusal saying why, where it names a document by no http or https URL,
// however it spells one. An absolute path and a network-path reference name
// the host, and are read there. Written in a local document, the same values
// are read as written.
func TestEscapes_RefusesALocalReferenceOfANetworkDocument(t *testing.T) {
	t.Parallel()
	const network, local = "http://host/v1/a.yaml", "/dir/a.yaml"
	refuses := refusedByName(Options{Reader: &fakeReader{}}, local, nil)
	for _, tc := range []struct {
		ref     string
		refused bool
	}{
		{"file:///etc/passwd#/X", true},
		{"FILE:///etc/passwd#/X", true},
		{" file:///etc/passwd#/X", true},
		{"file:passwd#/X", true},
		{`C:\x\secret.yaml#/X`, true},
		{"/etc/passwd#/X", false},
		{"//other/passwd#/X", false},
		{"b.yaml#/X", false},
		{"https://other/b.yaml#/X", false},
		{"#/components/schemas/S", false},
	} {
		t.Run(tc.ref, func(t *testing.T) {
			t.Parallel()
			esc := newEscapes(refuses)
			root := decodeRoot(t, "{$ref: '"+tc.ref+"'}")
			esc.escape(t.Context(), root, network)
			spelled := refValues(root)[0]
			key, _, _ := strings.Cut(spelled, "#")

			assert.Equal(t, tc.refused, strings.HasPrefix(spelled, refusedScheme+":"), spelled)
			if why := esc.refusalOf(key); tc.refused {
				assert.ErrorContains(t, why, bind.OffNetwork)
			} else {
				assert.NoError(t, why)
			}
			assert.False(t, esc.rewritable(t.Context(), decodeRoot(t, "{$ref: '"+tc.ref+"'}"), local),
				"a local document reads it as written")
		})
	}
}

// TestExternal_ASourceReadOverTheNetworkReadsNoFile pins the read every
// document of a compile whose source was read over the network goes through:
// a location on no http or https URL is refused unread, the one the resolver
// reaches by rebasing onto a `$self` included, which no respelling sees.
func TestExternal_ASourceReadOverTheNetworkReadsNoFile(t *testing.T) {
	t.Parallel()
	fake := &fakeReader{data: []byte("X: {type: integer}\n")}
	self := sourceDocument{path: "http://host/v1/root.yaml", judged: judgment{checker: oas.NewChecker()}}
	e := newExternal(t.Context(), &soa.OpenAPI{}, Options{Reader: fake}, newExternalReads(self, budget.Unbounded()))

	_, err := e.Open("/etc/secret.yaml")
	require.ErrorContains(t, err, bind.OffNetwork)
	req, err := http.NewRequest(http.MethodGet, "file:///etc/secret.yaml", nil)
	require.NoError(t, err)
	_, err = e.Do(req)
	require.ErrorContains(t, err, bind.OffNetwork)
	assert.Empty(t, fake.reqs, "nothing is read")
}

// TestExternal_DoReadsANetworkDocumentStayingOnTheHost pins that a network
// document is prepared and stored, not refused, whatever its references name:
// one whose $refs stay on the host, one whose example writes an `id` a drive
// letter would spell or a `$ref` naming a file, which are data, and one whose
// $ref or $id names a file, as each reference is judged on its own where it is
// read.
func TestExternal_DoReadsANetworkDocumentStayingOnTheHost(t *testing.T) {
	t.Parallel()
	for name, body := range map[string]string{
		"a relative $ref":       remoteDoc("./b.yaml#/x"),
		"an example's id":       "openapi: 3.1.0\ncomponents:\n  schemas:\n    S: {type: object, example: {id: 'u:42'}}\n",
		"a 2020-12 schema's id": "components:\n  schemas:\n    S: {id: 'file:///x/', properties: {p: {$ref: 'b.yaml'}}}\n",
		"an example's $ref":     "components:\n  schemas:\n    S: {type: object, example: {$ref: 'file:///etc/passwd'}}\n",
		"a $ref naming a file":  remoteDoc("file:///etc/passwd"),
		"an $id naming a file":  "components:\n  schemas:\n    S: {$id: 'file:///x/', $ref: './secret.yaml#/x'}\n",
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
// whole path through Load: the $ref in the network document naming the local
// file is reported unresolved with the reason. A two-order check: the local
// file is also referenced straight from the source, which resolves on its own,
// so the network crossing is the only thing refused, in either declaration
// order.
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
		assert.Contains(t, unresolved[0], bind.OffNetwork)
		assert.NotContains(t, unresolved[0], "./local.yaml", "the source's own reference to the file resolves")
	}
}
