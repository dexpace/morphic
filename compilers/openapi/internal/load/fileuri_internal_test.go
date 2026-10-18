package load

import (
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
)

// TestFilePathOf pins how a file URI names a local file (RFC 8089): decoded,
// with localhost read as no host, a drive letter's leading slash dropped only
// where paths are read as Windows reads them, and every URI naming no local
// file, or naming one ambiguously, refused with the reason.
func TestFilePathOf(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, uri string
		windows   bool
		want      string
		wantErr   string
	}{
		{name: "an empty authority", uri: "file:///a/b.yaml", want: "/a/b.yaml"},
		{name: "no authority", uri: "file:/a/b.yaml", want: "/a/b.yaml"},
		{name: "localhost", uri: "file://localhost/a/b.yaml", want: "/a/b.yaml"},
		{name: "localhost in any case", uri: "file://LocalHost/a/b.yaml", want: "/a/b.yaml"},
		{name: "a percent-encoded name", uri: "file:///a/b%20c.yaml", want: "/a/b c.yaml"},
		{name: "dot segments, left for the read to clean", uri: "file:///a/./c/../b.yaml", want: "/a/./c/../b.yaml"},
		{name: "a drive on Windows", uri: "file:///C:/a/b.yaml", windows: true, want: "C:/a/b.yaml"},
		{name: "a lower-case drive on Windows", uri: "file://localhost/c:/a", windows: true, want: "c:/a"},
		{name: "a drive elsewhere is a directory", uri: "file:///C:/a/b.yaml", want: "/C:/a/b.yaml"},
		{name: "no drive on Windows", uri: "file:///a/b.yaml", windows: true, want: "/a/b.yaml"},
		{name: "a digit is no drive", uri: "file:///1:/a", windows: true, want: "/1:/a"},
		{name: "an escaped backslash elsewhere", uri: "file:///a%5Cb.yaml", want: `/a\b.yaml`},
		{name: "a relative path", uri: "file:a.yaml", wantErr: "names no absolute path"},
		{name: "no path", uri: "file://", wantErr: "names no absolute path"},
		{name: "another host", uri: "file://server/share/a.yaml", wantErr: `on host "server", not a local one`},
		{name: "localhost with a port", uri: "file://localhost:80/a.yaml", wantErr: `on host "localhost:80"`},
		{name: "a user", uri: "file://me@/a.yaml", wantErr: "user names no local file"},
		{name: "a user at localhost", uri: "file://me@localhost/a.yaml", wantErr: "user names no local file"},
		{name: "a query", uri: "file:///a.yaml?v=1", wantErr: "query names nothing"},
		{name: "an empty query", uri: "file:///a.yaml?", wantErr: "query names nothing"},
		{name: "an escaped slash", uri: "file:///a%2Fb.yaml", wantErr: "escaped slash"},
		{name: "an escaped slash in lower case", uri: "file:///a%2fb.yaml", wantErr: "escaped slash"},
		{name: "an escaped backslash on Windows", uri: "file:///a%5cb.yaml", windows: true, wantErr: "backslash is a separator"},
		{name: "a backslash on Windows", uri: `file:///a\\b.yaml`, windows: true, wantErr: "backslash is a separator"},
		{name: "a share", uri: "file:////server/share/a.yaml", wantErr: "two slashes"},
		{name: "a share on Windows", uri: "file:////server/share/a.yaml", windows: true, wantErr: "two slashes"},
		{name: "a share under localhost", uri: "file://localhost//server/share/a.yaml", wantErr: "two slashes"},
		{name: "a backslash share on Windows", uri: `file:///\\server\share\a.yaml`, windows: true,
			wantErr: "backslash is a separator"},
		{name: "a backslash after the root elsewhere", uri: `file:///\\server\a.yaml`, want: `/\\server\a.yaml`},
		{name: "two slashes after a drive on Windows", uri: "file:///C://a.yaml", windows: true, want: "C://a.yaml"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			u, err := url.Parse(tc.uri)
			require.NoError(t, err)

			got, err := filePathOf(u, tc.windows)

			if tc.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

// fileURI returns the file URI naming the absolute path path, spelled with
// scheme.
func fileURI(scheme, path string) string {
	return scheme + "://" + filepath.ToSlash(path)
}

// TestLoad_AFileURIReadsTheFileItNames pins GitHub #927: a $ref written as a
// file URI reads the file, where the HTTP client it was handed refused the
// scheme, under each spelling of the scheme and authority. Spelled in capitals,
// the resolver keys the document apart from the request, so its anchored
// operations hold only through GitHub #538's recovery, as a URL's do.
func TestLoad_AFileURIReadsTheFileItNames(t *testing.T) {
	t.Parallel()
	dir := externalDir(t, map[string]string{"ext.yaml": anchoredExternalDoc})
	ext := filepath.Join(dir, "ext.yaml")
	for _, uri := range []string{
		fileURI("file", ext),
		fileURI("FILE", ext),
		"file://localhost" + filepath.ToSlash(ext),
		"file:" + filepath.ToSlash(ext),
	} {
		t.Run(uri, func(t *testing.T) {
			t.Parallel()

			got, diags := loadExternal(t, dir, rootReferencing(uri), Options{})

			assert.Empty(t, diags)
			assertRecoveredAt(t, got.Doc, "/x")
		})
	}
}

// TestLoad_AFileURIDocumentResolvesItsOwnRelativeRefs pins the chain through a
// document a file URI named: its relative $ref is read against that URI, so it
// names a file URI too and is read, through two documents to the third.
func TestLoad_AFileURIDocumentResolvesItsOwnRelativeRefs(t *testing.T) {
	t.Parallel()
	dir := externalDir(t, map[string]string{
		"a.yaml": "x: {$ref: './sub/b.yaml#/x'}\n",
		"c.yaml": "x: {type: string}\n",
	})
	require.NoError(t, os.Mkdir(filepath.Join(dir, "sub"), 0o750))
	writeFile(t, dir, filepath.Join("sub", "b.yaml"), "x: {$ref: '../c.yaml#/x'}\n")

	root := schemaRoot(fileURI("file", filepath.Join(dir, "a.yaml")) + "#/x")

	_, diags := loadExternal(t, dir, root, Options{MaxExternalDocuments: 3})
	_, short := loadExternal(t, dir, root, Options{MaxExternalDocuments: 2})

	assert.Empty(t, diags, "three documents, read within a budget of three")
	_, unresolved := budgetFindings(short)
	require.Len(t, unresolved, 1, "%+v", short)
	assert.Contains(t, unresolved[0], "external document "+fileURI("file", filepath.Join(dir, "c.yaml"))+" refused",
		"the third document is the file URI the second's relative $ref names")
}

// TestLoad_AFileURIIsRefusedWithItsReason pins that a file URI naming no local
// file fails the reference with the reason, and opens nothing.
func TestLoad_AFileURIIsRefusedWithItsReason(t *testing.T) {
	t.Parallel()

	_, diags := loadExternal(t, t.TempDir(), schemaRoot("file://server/share/a.yaml#/x"), Options{})

	_, unresolved := budgetFindings(diags)
	require.Len(t, unresolved, 1, "%+v", diags)
	assert.Contains(t, unresolved[0], `external document file://server/share/a.yaml refused: `+
		`a file URI names a file on host "server", not a local one`)
}

// TestLoad_AFileURIAndAPathAreTwoDocuments pins the identity a file URI has: a
// URL, named only as spelled, so the file its path names is another document,
// read and charged on its own. Under a budget of one, the second is refused.
func TestLoad_AFileURIAndAPathAreTwoDocuments(t *testing.T) {
	t.Parallel()
	dir := externalDir(t, map[string]string{"a.yaml": "x: {type: string}\n"})
	root := schemaRoot("./a.yaml#/x") +
		"    B: {$ref: '" + fileURI("file", filepath.Join(dir, "a.yaml")) + "#/x'}\n"

	_, within := loadExternal(t, dir, root, Options{MaxExternalDocuments: 2})
	_, past := loadExternal(t, dir, root, Options{MaxExternalDocuments: 1})

	assert.Empty(t, within, "both read, one document each")
	budget, unresolved := budgetFindings(past)
	assert.Len(t, budget, 1)
	require.Len(t, unresolved, 1, "%+v", past)
	assert.True(t, strings.Contains(unresolved[0], "past the 1-document budget"), unresolved[0])
	assert.Equal(t, diag.BudgetExceeded, budget[0].Code)
}
