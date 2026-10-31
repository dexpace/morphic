// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package archtest_test

import (
	"fmt"
	"io/fs"
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

// emitterDocs is the tree of the emitter's design documents, relative to the
// repository's root, and each path below is relative to it.
const (
	emitterDocs = "docs/emitters"
	emitterRoot = "README.md"
	sharedTable = "refine/lowering-table.md"
	sharedSteps = "refine/steps.md"
	targetsDir  = "targets"
	targetTable = "lowering.md"
	// constructHead and stepHead each open a kind of table, as its first cell.
	constructHead = "IR construct"
	stepHead      = "Step"
	// tableHeadRows are a table's header and its rule, which hold no construct.
	tableHeadRows = 2
)

// commonDocumentList is the root's list of the documents every target has, each
// a span of code.
var commonDocumentList = regexp.MustCompile(`documents every target has \(([^)]*)\)`)

// TestEmitterDocs_EveryTargetAnswersEveryConstruct holds each target's lowering
// table to the shared one. One table with a column per target kept every row
// answered by every target at a glance; divided by target, a construct added
// for one would leave the others silent, which is the gap the rule "a missing
// row is an IR bug" exists to surface. A row with an empty answer answers
// nothing.
func TestEmitterDocs_EveryTargetAnswersEveryConstruct(t *testing.T) {
	t.Parallel()
	docs := filepath.Join(irtest.RepoRoot(t), filepath.FromSlash(emitterDocs))
	shared, err := tableConstructs(filepath.Join(docs, filepath.FromSlash(sharedTable)), constructHead)
	require.NoError(t, err)
	require.NotEmpty(t, shared, "found no construct, so this proves nothing")
	for _, target := range emitterTargets(t, docs) {
		answered, err := tableConstructs(filepath.Join(docs, targetsDir, target, targetTable), constructHead)
		require.NoError(t, err)
		for _, p := range constructProblems(target, shared, answered) {
			t.Error(p)
		}
	}
}

// TestEmitterDocs_EveryTargetHoldsTheCommonDocuments holds each target to the
// documents the root says every target has, so that list stays what a new
// target has to answer.
func TestEmitterDocs_EveryTargetHoldsTheCommonDocuments(t *testing.T) {
	t.Parallel()
	docs := filepath.Join(irtest.RepoRoot(t), filepath.FromSlash(emitterDocs))
	common, err := commonDocuments(filepath.Join(docs, emitterRoot))
	require.NoError(t, err)
	require.NotEmpty(t, common, "the root lists no document, so this proves nothing")
	for _, target := range emitterTargets(t, docs) {
		for _, doc := range common {
			if _, err := os.Stat(filepath.Join(docs, targetsDir, target, doc+".md")); err != nil {
				t.Errorf("%s lacks a document every target has: %v", target, err)
			}
		}
	}
}

// TestEmitterDocs_TargetStepsAreCanonical fails on a step a target's step table
// names and the canonical list does not, so a step renamed or removed there
// leaves no row behind. A target's table holds only the steps it spells out.
func TestEmitterDocs_TargetStepsAreCanonical(t *testing.T) {
	t.Parallel()
	docs := filepath.Join(irtest.RepoRoot(t), filepath.FromSlash(emitterDocs))
	canonical, err := tableConstructs(filepath.Join(docs, filepath.FromSlash(sharedSteps)), stepHead)
	require.NoError(t, err)
	require.NotEmpty(t, canonical, "found no step, so this proves nothing")
	spelled := 0
	for _, target := range emitterTargets(t, docs) {
		steps, err := tableConstructs(filepath.Join(docs, targetsDir, target, targetTable), stepHead)
		require.NoError(t, err)
		spelled += len(steps)
		for _, step := range steps {
			if !slices.Contains(canonical, step) {
				t.Errorf("%s spells out %s, which %s lists nowhere", target, step, sharedSteps)
			}
		}
	}
	require.NotZero(t, spelled, "no target spells out a step, so this proves nothing")
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

// emitterTargets returns the name of each target under docs.
func emitterTargets(t *testing.T, docs string) []string {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(docs, targetsDir))
	require.NoError(t, err)
	require.NotEmpty(t, entries, "found no target, so this proves nothing")
	targets := make([]string, 0, len(entries))
	for _, entry := range entries {
		targets = append(targets, entry.Name())
	}
	return targets
}

// commonDocuments returns the documents the root at path says every target has,
// each without its extension.
func commonDocuments(path string) ([]string, error) {
	text, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	list := commonDocumentList.FindStringSubmatch(strings.Join(proseLines(string(text)), " "))
	if list == nil {
		return nil, nil
	}
	return keydoc.Spans(list[1]), nil
}

// tableConstructs returns the first column of the table opening on head in the
// Markdown file at path, its header and rule left out and each link read as its
// text: a citation's path differs with the depth of the file it is written in.
// A row with nothing after its first cell is left out too, as it answers
// nothing.
func tableConstructs(path, head string) ([]string, error) {
	text, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	rows := tableRows(proseLines(string(text)), head)
	if len(rows) <= tableHeadRows {
		return nil, nil
	}
	constructs := make([]string, 0, len(rows)-tableHeadRows)
	for _, row := range rows[tableHeadRows:] {
		cell, rest := cutCell(row)
		if strings.Trim(rest, " |") != "" {
			constructs = append(constructs, inlineLink.ReplaceAllString(cell, "$1"))
		}
	}
	return constructs, nil
}

// tableRows returns the rows of the first table of lines opening on head, or
// nil when none does.
func tableRows(lines []string, head string) []string {
	var rows []string
	for _, line := range slices.Concat(lines, []string{""}) {
		switch {
		case strings.HasPrefix(line, "|"):
			rows = append(rows, line)
		case len(rows) > 0 && strings.HasPrefix(rows[0], "| "+head+" |"):
			return rows
		default:
			rows = nil
		}
	}
	return nil
}

// cutCell returns the first cell of a table row and what follows it. A pipe
// separates cells unless a backslash escapes it, in a span of code or not, as
// GitHub renders a table.
func cutCell(row string) (cell, rest string) {
	for i := 1; i < len(row); i++ {
		if row[i] == '|' && row[i-1] != '\\' {
			return strings.TrimSpace(row[1:i]), row[i+1:]
		}
	}
	return strings.TrimSpace(row[1:]), ""
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
			linked[strings.TrimPrefix(file, "./")] = true
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

// TestTableConstructs_ReadsTheNamedTable plants a target's table the way the
// real ones are written: a prose lead, an escaped pipe in a code span and out of
// one, a citation in a cell, a row with no answer, and a second table that ends
// the file with no newline after it. Each head reads its own table alone.
func TestTableConstructs_ReadsTheNamedTable(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	tables := filepath.Join(dir, "lowering.md")
	writeDocFile(t, tables, "# Table\n\nLead.\n\n| IR construct | Go |\n|---|---|\n"+
		"| `A \\| B` union | a |\n| plain ([see](../a.md#x)) | b \\| c |\n| C \\| D | d |\n|tight|e|\n| silent |  |\n"+
		"\n## Steps\n\n| Step | Lowers |\n|---|---|\n| `S` | x |")
	headerOnly := filepath.Join(dir, "empty.md")
	writeDocFile(t, headerOnly, "# No table\n\n| IR construct | a header |\n|---|---|\n")

	tests := []struct {
		name, path, head string
		want             []string
	}{
		{"the first table", tables, constructHead, []string{"`A \\| B` union", "plain (see)", "C \\| D", "tight"}},
		{"the table after it", tables, stepHead, []string{"`S`"}},
		{"a head no table opens on", tables, "absent", nil},
		{"a table of no rows", headerOnly, constructHead, nil},
	}
	for _, tt := range tests {
		got, err := tableConstructs(tt.path, tt.head)
		require.NoError(t, err, tt.name)
		assert.Equal(t, tt.want, got, tt.name)
	}

	_, err := tableConstructs(filepath.Join(dir, "absent.md"), constructHead)
	require.ErrorIs(t, err, fs.ErrNotExist)
}

// TestCommonDocuments_ReadsTheRootsList plants the root's list wrapped across
// lines beside a copy of its words in a fenced block, and a root that has none.
func TestCommonDocuments_ReadsTheRootsList(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, text string
		want       []string
	}{
		{
			"a wrapped list",
			"```\nthe documents every target has (`fenced`)\n```\n\n" +
				"Each holds the documents every target has (`overview`,\n`lowering` and `errors`) and others (`not`).\n",
			[]string{"overview", "lowering", "errors"},
		},
		{"no list", "No list.\n", nil},
	}
	for _, tt := range tests {
		path := filepath.Join(t.TempDir(), "README.md")
		writeDocFile(t, path, tt.text)
		got, err := commonDocuments(path)
		require.NoError(t, err, tt.name)
		assert.Equal(t, tt.want, got, tt.name)
	}

	_, err := commonDocuments(filepath.Join(t.TempDir(), "absent.md"))
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
