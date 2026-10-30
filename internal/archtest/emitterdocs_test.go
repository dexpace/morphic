package archtest_test

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir/irtest"
)

// emitterDocs is the tree of the emitter's design documents, relative to the
// repository's root, and each path below is relative to it.
const (
	emitterDocs = "docs/emitters"
	emitterRoot = "README.md"
	sharedTable = "refine/lowering-table.md"
	targetsDir  = "targets"
	targetTable = "lowering.md"
	// tableHeadRows are a table's header and its rule, which hold no construct.
	tableHeadRows = 2
)

// TestEmitterDocs_EveryTargetAnswersEveryConstruct holds each target's lowering
// table to the shared one. One table with a column per target kept every row
// answered by every target at a glance; divided by target, a construct added
// for one would leave the others silent, which is the gap the rule "a missing
// row is an IR bug" exists to surface.
func TestEmitterDocs_EveryTargetAnswersEveryConstruct(t *testing.T) {
	t.Parallel()
	docs := filepath.Join(irtest.RepoRoot(t), filepath.FromSlash(emitterDocs))
	shared, err := tableConstructs(filepath.Join(docs, filepath.FromSlash(sharedTable)))
	require.NoError(t, err)
	require.NotEmpty(t, shared, "found no construct, so this proves nothing")
	targets, err := os.ReadDir(filepath.Join(docs, targetsDir))
	require.NoError(t, err)
	require.NotEmpty(t, targets, "found no target, so this proves nothing")
	for _, target := range targets {
		answered, err := tableConstructs(filepath.Join(docs, targetsDir, target.Name(), targetTable))
		require.NoError(t, err)
		for _, p := range constructProblems(target.Name(), shared, answered) {
			t.Error(p)
		}
	}
}

// TestEmitterDocs_RootMapsEveryDocument fails on a document under the tree that
// its root does not link, so the map stays the way in to every document.
func TestEmitterDocs_RootMapsEveryDocument(t *testing.T) {
	t.Parallel()
	docs := filepath.Join(irtest.RepoRoot(t), filepath.FromSlash(emitterDocs))
	unmapped, mapped, err := unmappedDocuments(docs)
	require.NoError(t, err)
	require.NotZero(t, mapped, "the root links no document, so this proves nothing")
	for _, doc := range unmapped {
		t.Errorf("%s links no %s", emitterRoot, doc)
	}
}

// tableConstructs returns the first column of the first table of the Markdown
// file at path, its header and rule left out and each link read as its text: a
// citation's path differs with the depth of the file it is written in.
func tableConstructs(path string) ([]string, error) {
	text, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var rows []string
	for _, line := range proseLines(string(text)) {
		if !strings.HasPrefix(line, "|") {
			if len(rows) > 0 {
				break
			}
			continue
		}
		rows = append(rows, line)
	}
	if len(rows) <= tableHeadRows {
		return nil, nil
	}
	constructs := make([]string, 0, len(rows)-tableHeadRows)
	for _, row := range rows[tableHeadRows:] {
		constructs = append(constructs, inlineLink.ReplaceAllString(firstCell(row), "$1"))
	}
	return constructs, nil
}

// firstCell returns the first cell of a table row, a pipe inside a span of code
// being no separator.
func firstCell(row string) string {
	inCode := false
	for i, r := range row[1:] {
		switch {
		case r == '`':
			inCode = !inCode
		case r == '|' && !inCode:
			return strings.TrimSpace(row[1 : i+1])
		}
	}
	return strings.TrimSpace(row[1:])
}

// constructProblems reports each construct of the shared table target's table
// does not answer, and each it lists that the shared one does not.
func constructProblems(target string, shared, answered []string) []string {
	var problems []string
	for _, construct := range shared {
		if !slices.Contains(answered, construct) {
			problems = append(problems, fmt.Sprintf("%s answers no row for %s", target, construct))
		}
	}
	for _, construct := range answered {
		if !slices.Contains(shared, construct) {
			problems = append(problems, fmt.Sprintf("%s lowers %s, which %s lists nowhere", target, construct, sharedTable))
		}
	}
	return problems
}

// unmappedDocuments returns, relative to docs, each Markdown document under it
// that its root does not link, and counts the documents the root does.
func unmappedDocuments(docs string) ([]string, int, error) {
	text, err := os.ReadFile(filepath.Join(docs, emitterRoot))
	if err != nil {
		return nil, 0, err
	}
	linked := map[string]bool{}
	for _, line := range proseLines(string(text)) {
		for _, m := range markdownLink.FindAllStringSubmatch(codeSpan.ReplaceAllString(line, ""), -1) {
			file, _, _ := strings.Cut(m[1], "#")
			linked[file] = true
		}
	}
	var unmapped []string
	mapped := 0
	err = filepath.WalkDir(docs, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || filepath.Ext(path) != ".md" {
			return err
		}
		rel, err := filepath.Rel(docs, path)
		if err != nil {
			return err
		}
		switch rel = filepath.ToSlash(rel); {
		case rel == emitterRoot:
		case linked[rel]:
			mapped++
		default:
			unmapped = append(unmapped, rel)
		}
		return nil
	})
	return unmapped, mapped, err
}

// TestTableConstructs_ReadsTheFirstTable plants a target's table the way the
// real ones are written: a prose lead, a code span holding a pipe, a citation in
// a cell, and a second table that is no part of the answer.
func TestTableConstructs_ReadsTheFirstTable(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "lowering.md")
	writeDocFile(t, path, "# Table\n\nLead.\n\n| IR construct | Go |\n|---|---|\n"+
		"| `A \\| B` union | a |\n| plain ([see](../a.md#x)) | b \\| c |\n\n## Steps\n\n| Step | Lowers |\n|---|---|\n| `S` | x |\n")
	got, err := tableConstructs(path)
	require.NoError(t, err)
	assert.Equal(t, []string{"`A \\| B` union", "plain (see)"}, got)

	empty := filepath.Join(t.TempDir(), "empty.md")
	writeDocFile(t, empty, "# No table\n\n| only | a header |\n|---|---|\n")
	got, err = tableConstructs(empty)
	require.NoError(t, err)
	assert.Empty(t, got)

	_, err = tableConstructs(filepath.Join(t.TempDir(), "absent.md"))
	require.ErrorIs(t, err, fs.ErrNotExist)
}

// TestConstructProblems_ReportsADriftEachWay plants a construct one table has
// and the other lacks, in each direction.
func TestConstructProblems_ReportsADriftEachWay(t *testing.T) {
	t.Parallel()
	assert.Empty(t, constructProblems("go", []string{"a", "b"}, []string{"b", "a"}), "order is no drift")
	assert.Equal(t, []string{
		"go answers no row for b",
		"go lowers c, which refine/lowering-table.md lists nowhere",
	}, constructProblems("go", []string{"a", "b"}, []string{"a", "c"}))
}

// TestUnmappedDocuments_ReportsADocumentTheRootOmits plants a document the root
// links with a fragment, one it links plainly, and one it does not.
func TestUnmappedDocuments_ReportsADocumentTheRootOmits(t *testing.T) {
	t.Parallel()
	docs := t.TempDir()
	writeDocFile(t, filepath.Join(docs, "README.md"), "[a](a.md#x) and [b](sub/b.md); `[c](c.md)`\n")
	writeDocFile(t, filepath.Join(docs, "a.md"), "# A\n")
	writeDocFile(t, filepath.Join(docs, "sub", "b.md"), "# B\n")
	writeDocFile(t, filepath.Join(docs, "c.md"), "# C\n")
	writeDocFile(t, filepath.Join(docs, "sub", "data.txt"), "not a document\n")

	unmapped, mapped, err := unmappedDocuments(docs)
	require.NoError(t, err)
	assert.Equal(t, []string{"c.md"}, unmapped)
	assert.Equal(t, 2, mapped)

	_, _, err = unmappedDocuments(filepath.Join(docs, "absent"))
	require.ErrorIs(t, err, fs.ErrNotExist)
}
