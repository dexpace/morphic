package load

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
)

// fileURI returns the file URI naming the absolute path path, spelled with
// scheme.
func fileURI(scheme, path string) string {
	return scheme + "://" + filepath.ToSlash(path)
}

// TestLoad_AFileURIReadsTheFileItNames pins GitHub #927: a $ref written as a
// file URI reads the file, where the resolver's own HTTP client refused the
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
		"c.yaml": "x: {description: ok}\n",
	})
	require.NoError(t, os.Mkdir(filepath.Join(dir, "sub"), 0o750))
	writeFile(t, dir, filepath.Join("sub", "b.yaml"), "x: {$ref: '../c.yaml#/x'}\n")

	root := responseRoot(fileURI("file", filepath.Join(dir, "a.yaml")) + "#/x")

	_, diags := loadExternal(t, dir, root, Options{MaxExternalDocuments: 3})
	_, short := loadExternal(t, dir, root, Options{MaxExternalDocuments: 2})

	assert.Empty(t, diags, "three documents, read within a budget of three")
	_, unresolved := budgetFindings(short)
	require.Len(t, unresolved, 1, "%+v", short)
	assert.Contains(t, unresolved[0], "external document "+fileURI("file", filepath.Join(dir, "c.yaml"))+" refused",
		"the third document is the file URI the second's relative $ref names")
}

// TestLoad_AFileURIIsRefusedWithItsReason pins that a file URI naming no local
// file fails the reference with the reader's reason, as it gave it.
func TestLoad_AFileURIIsRefusedWithItsReason(t *testing.T) {
	t.Parallel()

	_, diags := loadExternal(t, t.TempDir(), schemaRoot("file://server/share/a.yaml#/x"), Options{})

	_, unresolved := budgetFindings(diags)
	require.Len(t, unresolved, 1, "%+v", diags)
	assert.Contains(t, unresolved[0], `a file URI names a file on host "server", not a local one`)
	assert.NotContains(t, unresolved[0], "external document", "the reader's reason, not wrapped as a refusal")
}

// TestLoad_AFileURIAndAPathAreTwoDocuments pins the identity a file URI has: a
// URL, named only as spelled, so the file its path names is another document,
// read and charged on its own. Under a budget of one, the second is refused.
func TestLoad_AFileURIAndAPathAreTwoDocuments(t *testing.T) {
	t.Parallel()
	dir := externalDir(t, map[string]string{"a.yaml": "x: {description: ok}\n"})
	root := responseRoot("./a.yaml#/x") +
		"    B: {$ref: '" + fileURI("file", filepath.Join(dir, "a.yaml")) + "#/x'}\n"

	_, within := loadExternal(t, dir, root, Options{MaxExternalDocuments: 2})
	_, past := loadExternal(t, dir, root, Options{MaxExternalDocuments: 1})

	assert.Empty(t, within, "both read, one document each")
	budget, unresolved := budgetFindings(past)
	assert.Len(t, budget, 1)
	require.Len(t, unresolved, 1, "%+v", past)
	assert.Contains(t, unresolved[0], "past the 1-document budget")
	assert.Equal(t, diag.BudgetExceeded, budget[0].Code)
}
