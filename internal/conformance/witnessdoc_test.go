// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

// This file is a package-level suite, not a per-source-file test: it reads the
// witness document each format keeps beside its corpus,
// testdata/conformance/<format>/matrix.md, and holds it to the matrix column
// that format is keyed by.
//
// The checks are functions of text, not of the tree, so the defects they exist
// to catch can be planted in a string and shown to be reported
// (witnessdoc_defects_test.go). A check that only ever meets the committed
// document cannot be seen to fail.
package conformance_test // external test package — exercises only the public API

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/internal/keydoc"
)

const (
	// corpusRoot holds one directory of corpus specs per format, addressed
	// relative to this test file.
	corpusRoot = "../../testdata/conformance"
	// witnessDocName is the witness document's name inside a format's directory.
	witnessDocName = "matrix.md"
	// witnessesField and uncoveredField are the two fields an entry chooses
	// between: the specs that witness a row, or why none does yet.
	witnessesField = "Witnesses"
	uncoveredField = "Uncovered"
)

// cellMark is what one matrix cell says about a row, by its legend marker.
type cellMark struct {
	expressible bool
	known       bool
}

// witnessDocPath spells format's witness document as messages name it.
func witnessDocPath(format string) string {
	return path.Join("testdata/conformance", format, witnessDocName)
}

// readCorpus returns a format's witness document and the names of its specs,
// failing with what to add when the directory or the document is missing.
func readCorpus(t *testing.T, format string) (string, []string) {
	t.Helper()
	dir := filepath.Join(corpusRoot, format)
	entries, err := os.ReadDir(dir)
	require.NoError(t, err, "engine.New registers a compiler for %q, so testdata/conformance/%s/ must hold "+
		"its corpus and a %s naming the specs that witness each row its column marks expressible",
		format, format, witnessDocName)
	files := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() {
			files = append(files, e.Name())
		}
	}

	text, err := os.ReadFile(filepath.Join(dir, witnessDocName))
	require.NoError(t, err, "engine.New registers a compiler for %q, so add %s with one entry per row its "+
		"column marks expressible (testdata/conformance/openapi/matrix.md shows the shape)",
		format, witnessDocPath(format))
	return string(text), specNames(files)
}

// specNames returns the name of each spec among files, the file names of one
// format's corpus directory: the base name without its extension. The witness
// document and the golden beside each spec are not specs.
//
// Any extension counts, because the formats do not share one.
func specNames(files []string) []string {
	var names []string
	for _, f := range files {
		if f == witnessDocName || strings.HasSuffix(f, ".golden.json") {
			continue
		}
		names = append(names, strings.TrimSuffix(f, filepath.Ext(f)))
	}
	return names
}

// witnessProblems holds text, a format's witness document, to the matrix column
// at index column and to specs, the names of the specs in the format's corpus.
// Each problem it returns says what to add or change, and where.
//
// The document needs an entry for every row the column marks expressible and
// none for a row it marks absent or for one the matrix does not declare. An
// entry holds exactly one of Witnesses, naming specs that exist, and
// Uncovered, saying why none does.
func witnessProblems(m matrix, column int, format, text string, specs []string) []string {
	doc := witnessDocPath(format)
	entries, shape := keydoc.Parse(text, witnessesField, uncoveredField)
	problems := make([]string, 0, len(shape))
	for _, p := range shape {
		problems = append(problems, doc+": "+p)
	}

	marks := rowMarks(m, column)
	byKey := make(map[string]keydoc.Entry, len(entries))
	for _, e := range entries {
		if first, dup := byKey[e.Key]; dup {
			problems = append(problems, fmt.Sprintf("%s line %d: entry %q repeats the one at line %d: keep one",
				doc, e.Line, e.Key, first.Line))
			continue
		}
		byKey[e.Key] = e
		problems = append(problems, judgeEntry(doc, format, e, marks, specs)...)
	}
	for _, row := range m.rows {
		if _, has := byKey[row.key]; !has && marks[row.key].expressible {
			problems = append(problems, fmt.Sprintf("%s: matrix row %q (%s) is marked expressible for %s but has "+
				"no entry: add \"### `%s`\" with a \"- Witnesses:\" line naming the specs that witness it, or an "+
				"\"- Uncovered:\" line saying what blocks a spec and what retires the reason",
				doc, row.key, row.capability, format, row.key))
		}
	}
	return problems
}

// rowMarks reads the cell each row holds in the column at index column.
func rowMarks(m matrix, column int) map[string]cellMark {
	marks := make(map[string]cellMark, len(m.rows))
	for _, row := range m.rows {
		expressible, known := legendMarker(row.cells[column])
		marks[row.key] = cellMark{expressible: expressible, known: known}
	}
	return marks
}

// judgeEntry reports what is wrong with one entry. An entry for a row whose cell
// carries no marker is judged like any other: TestMatrix_RowsCarryUniqueSlugKeys
// reports the cell, and nothing says the entry is wrong.
func judgeEntry(doc, format string, e keydoc.Entry, marks map[string]cellMark, specs []string) []string {
	mark, declared := marks[e.Key]
	switch {
	case !declared:
		return []string{fmt.Sprintf("%s line %d: entry %q names no row of ir-spec-matrix.md: "+
			"correct the key, or delete the entry", doc, e.Line, e.Key)}
	case mark.known && !mark.expressible:
		return []string{fmt.Sprintf("%s line %d: entry %q is for a row ir-spec-matrix.md marks absent (—) for %s: "+
			"delete the entry, or correct the cell if the format can state the capability",
			doc, e.Line, e.Key, format)}
	}

	witnesses, witnessed := e.Field(witnessesField)
	uncovered, excused := e.Field(uncoveredField)
	switch {
	case witnessed && excused:
		return []string{fmt.Sprintf("%s line %d: entry %q needs exactly one of %s and %s, and has both: "+
			"a row with a witnessing spec has no reason to be uncovered", doc, e.Line, e.Key,
			witnessesField, uncoveredField)}
	case !witnessed && !excused:
		return []string{fmt.Sprintf("%s line %d: entry %q needs exactly one of %s and %s, and has neither",
			doc, e.Line, e.Key, witnessesField, uncoveredField)}
	case excused && uncovered.Text() == "":
		return []string{fmt.Sprintf("%s line %d: entry %q has an empty %s field: say what blocks a spec and "+
			"what retires the reason", doc, uncovered.Line, e.Key, uncoveredField)}
	case excused:
		return nil
	}
	return witnessNameProblems(doc, format, e.Key, witnesses, specs)
}

// witnessNameProblems reports what is wrong with the specs a Witnesses field
// names: none at all, text outside a code span that would otherwise pass
// unread, a spec named twice, and a name no file in the corpus answers to.
func witnessNameProblems(doc, format, key string, f keydoc.Field, specs []string) []string {
	text := f.Text()
	names := keydoc.Spans(text)
	if len(names) == 0 {
		return []string{fmt.Sprintf("%s line %d: entry %q lists no spec in %s: name each as a code span, its file "+
			"name without the extension", doc, f.Line, key, witnessesField)}
	}

	var problems []string
	rest := text
	for _, name := range names {
		rest = strings.Replace(rest, "`"+name+"`", "", 1)
	}
	if extra := strings.Trim(rest, ", "); extra != "" {
		problems = append(problems, fmt.Sprintf("%s line %d: entry %q writes %q in %s outside a code span, "+
			"where it is not read as a spec: put each spec name in code", doc, f.Line, key, extra, witnessesField))
	}
	seen := make(map[string]bool, len(names))
	for _, name := range names {
		switch {
		case seen[name]:
			problems = append(problems, fmt.Sprintf("%s line %d: entry %q names %q twice: name each spec once",
				doc, f.Line, key, name))
		case !slices.Contains(specs, name):
			problems = append(problems, fmt.Sprintf("%s line %d: entry %q names %q, which is no spec in "+
				"testdata/conformance/%s/: add the spec there, or correct the name", doc, f.Line, key, name, format))
		}
		seen[name] = true
	}
	return problems
}
