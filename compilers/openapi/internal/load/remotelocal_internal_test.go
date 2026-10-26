package load

import (
	"net/http"
	"path/filepath"
	"testing"

	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

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

			_, err = newExternal(t.Context(), doc, Options{Reader: reader.Disk{}}, newExternalReads(sourceDocument{})).Do(req)

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
// relative $ref included, and another key's value spelling a path is not read
// as a reference.
func TestReachesLocal_PassesALocalDocumentAndANetworkOne(t *testing.T) {
	t.Parallel()
	local := openapitest.YAMLDocument(t, remoteDoc("/etc/passwd"))
	_, refuse := reachesLocal(t.Context(), local, "/dir/a.yaml")
	assert.False(t, refuse, "a local document's own $refs are not judged against the network rule")

	onHost := "components:\n  schemas:\n" +
		"    A: {$ref: './b.yaml#/x'}\n" +
		"    B: {$ref: 'https://other/c.yaml#/x'}\n" +
		"    C: {$id: './v2/', $ref: '#/components/schemas/A'}\n" +
		"    D: {$id: 'urn:example:d', $ref: './d.yaml#/x'}\n" +
		"    E: {description: '/etc/passwd', default: 'file:///etc/passwd'}\n"
	_, refuse = reachesLocal(t.Context(), openapitest.YAMLDocument(t, onHost), "http://host/dir/a.yaml")
	assert.False(t, refuse, "a network document staying on the host reaches no local file")
}

// TestExternal_DoReadsANetworkDocumentStayingOnTheHost pins the other side:
// a network document whose $refs stay on the host is prepared and stored, not
// refused.
func TestExternal_DoReadsANetworkDocumentStayingOnTheHost(t *testing.T) {
	t.Parallel()
	srv := newTestServer(t, func(w http.ResponseWriter, _ *http.Request) {
		_, err := w.Write([]byte(remoteDoc("./b.yaml#/x")))
		assert.NoError(t, err)
	})
	key := srv.URL + "/a.yaml"
	req, err := http.NewRequest(http.MethodGet, key, nil)
	require.NoError(t, err)
	doc := &soa.OpenAPI{}

	resp, err := newExternal(t.Context(), doc, Options{Reader: reader.Disk{}}, newExternalReads(sourceDocument{})).Do(req)

	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, resp.Body.Close()) })
	_, ok := doc.GetCachedExternalDocument(key)
	assert.True(t, ok, "a document that stays on the host is prepared")
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
