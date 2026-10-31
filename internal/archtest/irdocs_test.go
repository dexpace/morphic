// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package archtest_test

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/internal/keydoc"
	"github.com/dexpace/morphic/ir/irtest"
)

// irDocs is the tree of the IR's design documents, relative to the repository's
// root.
const irDocs = "docs/ir"

// retiredTest is this file, whose strings plant the form it refuses.
const retiredTest = "internal/archtest/irdocs_test.go"

// retiredCitation is a design tree's name from when it was a single page.
var retiredCitation = regexp.MustCompile(`(?:ir|emitter)-design`)

// TestIRDocs_RootMapsEveryDocument fails on a document under the tree that its
// README does not link, so the map stays the way in to every document.
func TestIRDocs_RootMapsEveryDocument(t *testing.T) {
	t.Parallel()
	unmapped, mapped, err := unmappedDocuments(filepath.Join(irtest.RepoRoot(t), filepath.FromSlash(irDocs)))
	require.NoError(t, err)
	require.NotZero(t, mapped, "the README links no document, so this proves nothing")
	for _, doc := range unmapped {
		t.Errorf("%s/README.md links no %s", irDocs, doc)
	}
}

// TestDocuments_CiteNoRetiredPage refuses a design tree's name from when it was
// one page. That name and its section numbers name nothing now, so no check can
// hold them to the tree, and they return by habit if nothing stops them.
func TestDocuments_CiteNoRetiredPage(t *testing.T) {
	t.Parallel()
	problems, err := retiredCitations(irtest.RepoRoot(t))
	require.NoError(t, err)
	for _, p := range problems {
		t.Error(p)
	}
}

// retiredCitations reports each use under root of a design tree's former name,
// in a Go, Markdown or YAML file. This test's own file is left unread.
func retiredCitations(root string) ([]string, error) {
	var problems []string
	err := keydoc.Walk(root, ".", func(path string) error {
		rel := keydoc.Rel(root, path)
		if rel == retiredTest || !slices.Contains([]string{".go", ".md", ".yaml", ".yml"}, filepath.Ext(path)) {
			return nil
		}
		text, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, m := range retiredCitation.FindAllIndex(text, -1) {
			line := 1 + strings.Count(string(text[:m[0]]), "\n")
			problems = append(problems, fmt.Sprintf("%s:%d names a design tree as one page", rel, line))
		}
		return nil
	})
	return problems, err
}

// TestRetiredCitations_ReportsEachForm plants each spelling the page was cited
// by, and its name alone, beside citations of the tree.
func TestRetiredCitations_ReportsEachForm(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeDocFile(t, filepath.Join(root, "a", "a.go"), "package a\n\n"+
		"// Cites ir-design §4.3, and ir-design\n// §4.4, and `ir-design.md` §5.\n"+
		"// Also docs/emitter-design.md, docs/ir/models.md and docs/emitters/README.md.\n")
	writeDocFile(t, filepath.Join(root, "doc.md"), "See `ir-design.md §4.7`, or the ir-design tree.\n"+
		"Or ir-design/models.md.\n")
	writeDocFile(t, filepath.Join(root, "spec.yaml"), "# ir-design §4.8\n")
	writeDocFile(t, filepath.Join(root, "spec.yml"), "# ir-design §4.9\n")
	writeDocFile(t, filepath.Join(root, "notes.txt"), "ir-design §1\n")
	writeDocFile(t, filepath.Join(root, ".hidden", "h.md"), "ir-design §1\n")

	problems, err := retiredCitations(root)
	require.NoError(t, err)
	assert.Equal(t, []string{
		"a/a.go:3 names a design tree as one page",
		"a/a.go:3 names a design tree as one page",
		"a/a.go:4 names a design tree as one page",
		"a/a.go:5 names a design tree as one page",
		"doc.md:1 names a design tree as one page",
		"doc.md:1 names a design tree as one page",
		"doc.md:2 names a design tree as one page",
		"spec.yaml:1 names a design tree as one page",
		"spec.yml:1 names a design tree as one page",
	}, problems)
}
