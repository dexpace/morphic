// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package e2e_test

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi"
	"github.com/dexpace/morphic/engine/reader"
	"github.com/dexpace/morphic/ir"
)

// silentServer is a server answering every path 404 and counting the requests
// it was asked, with the URL it lies at.
func silentServer(t *testing.T) (*atomic.Int64, string) {
	t.Helper()
	var hits atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		http.NotFound(w, nil)
	}))
	t.Cleanup(srv.Close)
	return &hits, srv.URL
}

// compileFilesAt compiles dir's spec.yaml through the disk reader, with
// external references allowed or not and at most maxDocs documents read (zero
// for the default).
func compileFilesAt(t *testing.T, dir string, allow bool, maxDocs int) (*ir.Document, []ir.Diagnostic) {
	t.Helper()
	path := filepath.Join(dir, "spec.yaml")
	data := readFileText(t, path)
	doc, diags, err := openapi.New().Compile(t.Context(), []compilers.Source{{Path: path, Data: []byte(data)}},
		compilers.Options{Reader: reader.Disk{}, FormatOptions: openapi.Options{AllowExternalRefs: allow, Limits: openapi.Limits{MaxExternalDocuments: maxDocs}}})
	require.NoError(t, err)
	require.NotNil(t, doc)
	return doc, diags
}

// readFileText returns the text of the file at path.
func readFileText(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	require.NoError(t, err)
	return string(b)
}

// TestCompile_AURLADocumentDeclaresIsNotRequestedBesideTheDocumentDeclaringIt
// pins what a compile must go on not requesting: five URLs the source names,
// each declared by a file it also reads, are answered by the declarations
// with no request, though the document budget is one document, whichever
// reference is written first. (Where the declaration is found only after the
// URL is held, a design reading every held document together would request
// all five and exhaust the budget.)
func TestCompile_AURLADocumentDeclaresIsNotRequestedBesideTheDocumentDeclaringIt(t *testing.T) {
	t.Parallel()
	for name, order := range map[string]bool{"L last": false, "L first": true} {
		hits, url := silentServer(t)
		dir := t.TempDir()
		var bundle, uses strings.Builder
		for _, k := range []string{"0", "1", "2", "3", "4"} {
			bundle.WriteString("s" + k + ": {$id: '" + url + "/s" + k + ".yaml', type: string, description: b" + k + "}\n")
			uses.WriteString("    U" + k + ": {$ref: '" + url + "/s" + k + ".yaml'}\n")
		}
		const l = "    L: {$ref: 'lib/bundle.yaml#/s0'}\n"
		schemas := uses.String() + l
		if order {
			schemas = l + uses.String()
		}
		layout(t, dir, map[string]string{"lib/bundle.yaml": bundle.String(), "spec.yaml": dynHead + schemas})

		doc, diags := compileFilesAt(t, dir, true, 1)
		assert.Empty(t, rootErrors(diags), "%s: %v", name, lines(diags))
		assert.Zero(t, hits.Load(), "%s: the server saw no request", name)
		for _, k := range []string{"0", "1", "2", "3", "4"} {
			sc, ok := doc.Types[componentID("U"+k)].(*ir.Scalar)
			require.True(t, ok, name)
			require.NotNil(t, sc.Base, name)
			assert.Equal(t, ir.TypeID("t/anon@lib~1bundle.yaml/s"+k), sc.Base.Target, "%s: U%s", name, k)
		}
	}
}

// TestCompile_AFilesDeclarationAnswersTheURLItNamesBeforeAnyRequest pins the
// same at the other end: a file's identifiers are counted before any URL its
// drain holds is requested, so a URL a file declares, and one it declares
// whose server would not have it, are the file's, with references allowed and
// with them refused, and no request either way.
func TestCompile_AFilesDeclarationAnswersTheURLItNamesBeforeAnyRequest(t *testing.T) {
	t.Parallel()
	for name, allow := range map[string]bool{"allowed": true, "refused": false} {
		hits, url := silentServer(t)
		dir := t.TempDir()
		layout(t, dir, map[string]string{
			"local.yaml": "Z: {$id: '" + url + "/pet.yaml', type: object, description: local}\n" +
				"Y: {$id: '" + url + "/gone.yaml', type: integer, description: local-gone}\n",
			"spec.yaml": dynHead + "    A: {$ref: 'local.yaml#/Z'}\n    B: {$ref: '" + url + "/pet.yaml'}\n" +
				"    C: {$ref: '" + url + "/gone.yaml'}\n",
		})
		doc, diags := compileFilesAt(t, dir, allow, 0)
		assert.Empty(t, rootErrors(diags), "%s: %v", name, lines(diags))
		assert.Zero(t, hits.Load(), "%s: no request", name)
		for ref, want := range map[string]ir.TypeID{"A": "t/anon@local.yaml/Z", "B": "t/anon@local.yaml/Z", "C": "t/anon@local.yaml/Y"} {
			sc, ok := doc.Types[componentID(ref)].(*ir.Scalar)
			require.True(t, ok, "%s: %s", name, ref)
			require.NotNil(t, sc.Base, "%s: %s", name, ref)
			assert.Equal(t, want, sc.Base.Target, "%s: %s", name, ref)
		}
	}
}
