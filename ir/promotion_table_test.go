// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package ir_test

import (
	"fmt"
	"go/types"
	"os"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// promotionDoc is the document whose table says what a promotion can fill.
const promotionDoc = "../docs/ir/unmodeled.md"

// promotionTable opens the table's section in promotionDoc.
const promotionTable = "### What a promotion can fill"

// backticked matches one code span of a Markdown table cell.
var backticked = regexp.MustCompile("`([^`]+)`")

// promotionRow is one row of the table: the fields a promotion fills, the
// nodes keeping the extension, and the field those nodes record it in.
type promotionRow struct {
	fields, kept []string
	record       string
}

// promotionRows returns the rows of the table in doc, its header and rule left
// out. Each cell's code spans are what the row says, and its prose is ignored.
func promotionRows(doc string) []promotionRow {
	_, section, _ := strings.Cut(doc, promotionTable)
	var rows []promotionRow
	for line := range strings.Lines(section) {
		if strings.HasPrefix(line, "#") {
			break
		}
		cells := strings.Split(line, "|")
		if len(cells) != 5 || !strings.Contains(cells[1], "`") {
			continue
		}
		rows = append(rows, promotionRow{
			fields: spans(cells[1]),
			kept:   spans(cells[2]),
			record: strings.Join(spans(cells[3]), " "),
		})
	}
	return rows
}

// spans returns the code spans of a table cell.
func spans(cell string) []string {
	matches := backticked.FindAllStringSubmatch(cell, -1)
	out := make([]string, 0, len(matches))
	for _, m := range matches {
		out = append(out, m[1])
	}
	return out
}

// declared reports whether the struct named in ir declares or embeds a field,
// answering false for a name that is no struct.
type declared func(structName, field string) bool

// promotionFindings returns what keeps rows from being promotions rule 3 can
// record (docs/ir/unmodeled.md#promotion): a field no struct declares, a node
// that keeps no extension, and a node without the marker its row names. The
// last is what a list of targets drifting from the structs looks like.
func promotionFindings(rows []promotionRow, has declared) []string {
	var out []string
	for _, row := range rows {
		for _, field := range row.fields {
			structName, name, _ := strings.Cut(field, ".")
			if !has(structName, name) {
				out = append(out, fmt.Sprintf("%s names a field ir does not declare", field))
			}
		}
		for _, node := range row.kept {
			if !has(node, "Unmodeled") {
				out = append(out, fmt.Sprintf("%s keeps no extension: it has no Unmodeled", node))
			}
			if finding := recordFinding(node, row.record, has); finding != "" {
				out = append(out, finding)
			}
		}
	}
	return out
}

// recordFinding returns what is wrong with node recording in record, or "".
func recordFinding(node, record string, has declared) string {
	switch record {
	case "Provenance.Inferred":
		if !has(node, "Provenance") {
			return node + " is said to record in its Provenance, which it does not have"
		}
	case "Inferred":
		if has(node, "Provenance") {
			return node + " has a Provenance, which is where it records"
		}
		if !has(node, "Inferred") {
			return node + " is said to record in its Inferred, which it does not have"
		}
	default:
		return fmt.Sprintf("%s records in %q, which is neither marker", node, record)
	}
	return ""
}

// irDeclares answers declared from the ir package's own declarations, so an
// embedded field counts as it does for a reader of the struct.
func irDeclares(t *testing.T) declared {
	t.Helper()
	pkg := irDecls(t)
	return func(structName, field string) bool {
		named, err := pkg.Named(structName)
		if err != nil {
			return false
		}
		obj, _, _ := types.LookupFieldOrMethod(named, false, named.Obj().Pkg(), field)
		_, isField := obj.(*types.Var)
		return isField
	}
}

// TestPromotionTable_EveryNodeCanRecord holds the table to the structs, which
// is how the list it replaced came to name targets its own rule 4 forbade: a
// row promising a marker its node lacks fails here. Variant and EnumMember are
// the nodes rule 4 names, so one gaining a marker fails here until it stops.
func TestPromotionTable_EveryNodeCanRecord(t *testing.T) {
	t.Parallel()
	doc, err := os.ReadFile(promotionDoc)
	require.NoError(t, err)
	rows := promotionRows(string(doc))
	require.NotEmpty(t, rows, "%s has no rows under %q, so this proves nothing", promotionDoc, promotionTable)

	has := irDeclares(t)
	assert.Empty(t, promotionFindings(rows, has))
	for _, node := range []string{"Variant", "EnumMember"} {
		assert.False(t, has(node, "Provenance") || has(node, "Inferred"), "%s can record: rule 4 no longer names it", node)
	}
}

// TestPromotionFindings plants each row that fails, one defect at a time, and
// requires the one finding naming it, the row #674 reported among them.
func TestPromotionFindings(t *testing.T) {
	t.Parallel()
	structs := map[string][]string{
		"Marked":   {"Fact", "Unmodeled", "Provenance"},
		"Bare":     {"Fact", "Unmodeled", "Inferred"},
		"Unmarked": {"Fact", "Unmodeled"},
		"Sealed":   {"Fact", "Provenance"},
	}
	has := func(structName, field string) bool { return slices.Contains(structs[structName], field) }
	for _, tc := range []struct {
		name string
		row  promotionRow
		want string // "" means no finding
	}{
		{"a node with a Provenance", promotionRow{fields: []string{"Marked.Fact"}, kept: []string{"Marked"}, record: "Provenance.Inferred"}, ""},
		{"a node with its own marker", promotionRow{fields: []string{"Bare.Fact"}, kept: []string{"Bare"}, record: "Inferred"}, ""},
		{"a field on another node", promotionRow{fields: []string{"Unmarked.Fact"}, kept: []string{"Marked"}, record: "Provenance.Inferred"}, ""},
		{"a target no node can record", promotionRow{fields: []string{"Unmarked.Fact"}, kept: []string{"Unmarked"}, record: "Provenance.Inferred"},
			"Unmarked is said to record in its Provenance, which it does not have"},
		{"a marker the node lacks", promotionRow{fields: []string{"Unmarked.Fact"}, kept: []string{"Unmarked"}, record: "Inferred"},
			"Unmarked is said to record in its Inferred, which it does not have"},
		{"a bare marker beside a Provenance", promotionRow{fields: []string{"Marked.Fact"}, kept: []string{"Marked"}, record: "Inferred"},
			"Marked has a Provenance, which is where it records"},
		{"a field nothing declares", promotionRow{fields: []string{"Marked.Gone"}, kept: []string{"Marked"}, record: "Provenance.Inferred"},
			"Marked.Gone names a field ir does not declare"},
		{"a node keeping no extension", promotionRow{fields: []string{"Sealed.Fact"}, kept: []string{"Sealed"}, record: "Provenance.Inferred"},
			"Sealed keeps no extension: it has no Unmodeled"},
		{"a record that is no marker", promotionRow{fields: []string{"Marked.Fact"}, kept: []string{"Marked"}, record: "Docs"},
			`Marked records in "Docs", which is neither marker`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := promotionFindings([]promotionRow{tc.row}, has)
			if tc.want == "" {
				assert.Empty(t, got)
				return
			}
			assert.Equal(t, []string{tc.want}, got)
		})
	}
}

// TestPromotionRows_ReadsTheTableOnly pins the reader on the cases the real
// document does not vary: it stops at the next heading, skips the header and
// rule, and takes each cell's code spans and not its prose.
func TestPromotionRows_ReadsTheTableOnly(t *testing.T) {
	t.Parallel()
	doc := "| `Before.Table` | `A` | `Inferred` |\n" + promotionTable + "\n\n| Field | Kept on | Recorded in |\n|---|---|---|\n" +
		"| `A.B`, `A.C` | the `D`, or `E` | `Inferred` |\n\n### Next\n\n| `After.Table` | `A` | `Inferred` |\n"
	want := []promotionRow{{fields: []string{"A.B", "A.C"}, kept: []string{"D", "E"}, record: "Inferred"}}
	assert.Empty(t, cmp.Diff(want, promotionRows(doc), cmp.AllowUnexported(promotionRow{})))
}
