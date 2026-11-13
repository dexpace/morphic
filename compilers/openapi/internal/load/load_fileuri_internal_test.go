// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package load

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi/internal/budget"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
)

// fileURI returns the file URI naming the absolute path path, spelled with
// scheme.
func fileURI(scheme, path string) string {
	return scheme + "://" + filepath.ToSlash(path)
}

// TestLoad_AFileURIReadsTheFileItNames pins GitHub #927: a $ref written as a
// file URI reads the file, under each spelling of the scheme and authority,
// its anchored operations held as the read releases their anchors.
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
			assertRecoveredAt(t, got, "/x")
		})
	}
}

// TestLoad_AFileURIDocumentResolvesItsOwnRelativeRefs pins the chain through a
// document a file URI named: its relative $ref is read against that URI, so it
// names a file URI too and is read, through two documents to the third, a
// refusal naming it by the file it names.
func TestLoad_AFileURIDocumentResolvesItsOwnRelativeRefs(t *testing.T) {
	t.Parallel()
	dir := externalDir(t, map[string]string{
		"a.yaml": "x: {$ref: './sub/b.yaml#/x'}\n",
		"c.yaml": "x: {description: ok}\n",
	})
	require.NoError(t, os.Mkdir(filepath.Join(dir, "sub"), 0o750))
	writeFile(t, dir, filepath.Join("sub", "b.yaml"), "x: {$ref: '../c.yaml#/x'}\n")

	root := responseRoot(fileURI("file", filepath.Join(dir, "a.yaml")) + "#/x")

	_, diags := loadExternal(t, dir, root, Options{MaxExternalDocuments: budget.AtMost(3)})
	_, short := loadExternal(t, dir, root, Options{MaxExternalDocuments: budget.AtMost(2)})

	assert.Empty(t, diags, "three documents, read within a budget of three")
	_, unresolved := budgetFindings(short)
	require.Len(t, unresolved, 1, "%+v", short)
	assert.Contains(t, unresolved[0], "external document "+filepath.Join(dir, "c.yaml")+" refused",
		"the third document is the file the second's relative $ref names")
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

// TestLoad_AFileURIAndAPathAreOneDocument pins the identity a file URI has: the
// file its path names (resolve.FileOf), as the reader reads it, so a file a
// $ref names by a file URI, another by its path and a third through a dot
// segment is one document, read and charged once, in either order. Under a
// budget of one, all three resolve; under none, none is read.
func TestLoad_AFileURIAndAPathAreOneDocument(t *testing.T) {
	t.Parallel()
	dir := externalDir(t, map[string]string{"a.yaml": "x: {description: ok}\n"})
	byURI := "    B: {$ref: '" + fileURI("file", filepath.Join(dir, "a.yaml")) + "#/x'}\n"
	byDot := "    C: {$ref: '" + filepath.Join(dir, "x", "..", "a.yaml") + "#/x'}\n"
	for _, root := range []string{responseRoot("./a.yaml#/x") + byURI + byDot, responseRoot("./a.yaml#/x") + byDot + byURI} {
		fake := &fakeReader{data: []byte("x: {description: ok}\n")}
		src := compilers.Source{Path: filepath.Join(dir, "root.yaml"), Data: []byte(root)}

		_, within, err := Load(t.Context(), 0, src, Options{Reader: fake, MaxExternalDocuments: budget.AtMost(1)})
		require.NoError(t, err)
		_, none := loadExternal(t, dir, root, Options{MaxExternalDocuments: budget.AtMost(0)})

		assert.Empty(t, within, "one document, read within a budget of one")
		assert.Len(t, fake.reqs, 1, "and read once")
		over, unresolved := budgetFindings(none)
		assert.Len(t, over, 1)
		assert.Len(t, unresolved, 3, "%+v", none)
		assert.Equal(t, diag.BudgetExceeded, over[0].Code)
	}
}
