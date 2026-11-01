// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package ir_test

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/internal/jsonshape"
	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/ir/irtest"
)

// generation is one IR generation: an IRVersion and the shape of the JSON it
// was published with (docs/ir/ir-version.md).
type generation struct {
	// version is the IRVersion.
	version string
	// format is the jsonshape.Format the shape was described in.
	format int
	// shape is the hex SHA-256 of the description, shapeGolden.
	shape string
	// meaning says what changed, for a bump that keeps the shape of the
	// generation before it: a change of meaning, which no description sees. It
	// is empty for every other row.
	meaning string
}

// generations is every IR generation since the shape pin landed, oldest first.
// A bump appends a row for the new IRVersion, and a change to jsonshape.Format
// appends one describing the current generation anew. A row on main is never
// edited: two changes claiming one version each append a row in the same
// place, so whichever merges second meets the other's in a conflict and takes
// the next version.
var generations = []generation{
	{version: "0.14.0", format: 1, shape: "9dabe5d1adf8d55ac0b06a72a113424ff063034fa92f1f35f2e6955bcc5a14b9"},
	{version: "0.15.0", format: 1, shape: "9dabe5d1adf8d55ac0b06a72a113424ff063034fa92f1f35f2e6955bcc5a14b9",
		meaning: "a union a discriminator routes is WireTagged"},
	{version: "0.16.0", format: 1, shape: "95adc76be7af90d5f2565395147095a7c1f504338663ba22bcf7fd3b61c9a9f4"},
	{version: "0.17.0", format: 1, shape: "95adc76be7af90d5f2565395147095a7c1f504338663ba22bcf7fd3b61c9a9f4",
		meaning: "Sources lists every document a reference reads, and what each declares is lowered there"},
	{version: "0.18.0", format: 1, shape: "159c405ed1d0729015ae8b206060f6a9ca90a184b9d58030c53f56a0ce763b26"},
	{version: "0.19.0", format: 1, shape: "1052ad10f358985084b882007c6632092b99e6af0811a9234abfe608b1a19798"},
	{version: "0.20.0", format: 1, shape: "1052ad10f358985084b882007c6632092b99e6af0811a9234abfe608b1a19798",
		meaning: "an encoding key or a discriminator's Property names the declaration the model's flattening takes"},
	{version: "0.21.0", format: 1, shape: "93e13e68b0e2c6a8623109d342c08353a4209cbc57d8f3828705646f51d50bf0"},
	{version: "0.22.0", format: 1, shape: "28c69354fd5e99549277dc63910f3ac5e2968fc56c1f1397b87e53215a834622"},
	{version: "0.23.0", format: 1, shape: "9b887e665e395b70b8b74056a490d011b23c333c7eccc21369a15860b442a8ee"},
}

// shapeGolden holds the description the newest row's shape hashes, so a change
// to the shape reviews as a diff of it.
const shapeGolden = "testdata/shape.golden.txt"

// versionLog is the document whose history records each generation.
const versionLog = "../docs/ir/version-history.md"

// TestIRVersion_PinsTheEncodedShape holds IRVersion to the shape of the JSON a
// Document encodes: the description of every type it reaches must hash to the
// shape generations records for IRVersion. A shape change at an unmoved
// version fails here, and so does a bump with no row. -update rewrites
// shapeGolden but never the table, so it cannot settle either.
func TestIRVersion_PinsTheEncodedShape(t *testing.T) {
	t.Parallel()
	desc, err := jsonshape.Describe(irDecls(t), "Document")
	require.NoError(t, err)

	if irtest.Update() {
		require.NoError(t, os.WriteFile(shapeGolden, []byte(desc), 0o644))
	}
	want, err := os.ReadFile(shapeGolden)
	require.NoError(t, err, "go test ./ir -run TestIRVersion -update writes it")
	assert.Empty(t, cmp.Diff(string(want), desc),
		"%s is stale: go test ./ir -run TestIRVersion -update rewrites it, then review its diff", shapeGolden)

	sum := sha256.Sum256([]byte(desc))
	for _, finding := range pinFindings(generations, ir.IRVersion, jsonshape.Format, hex.EncodeToString(sum[:])) {
		t.Error(finding)
	}
}

// TestIRVersion_GenerationsAppendInOrder holds the table to its form: versions
// ascending, a bump described in its predecessor's format so the two compare,
// and a bump that keeps its predecessor's shape saying why.
func TestIRVersion_GenerationsAppendInOrder(t *testing.T) {
	t.Parallel()
	for _, finding := range rowFindings(generations, jsonshape.Format) {
		t.Error(finding)
	}
}

// TestIRVersion_HistoryRecordsEachGeneration requires
// docs/ir/version-history.md to end at IRVersion and to name every generation
// the table does, so a bump cannot land without saying what it changed.
func TestIRVersion_HistoryRecordsEachGeneration(t *testing.T) {
	t.Parallel()
	doc, err := os.ReadFile(versionLog)
	require.NoError(t, err)
	history, err := versionHistory(string(doc))
	require.NoError(t, err)
	for _, finding := range historyFindings(history, generations, ir.IRVersion) {
		t.Error(finding)
	}
}

// pinFindings returns what keeps rows from pinning a build at version whose
// shape, described in format, hashes to shape. Each finding says what to do.
func pinFindings(rows []generation, version string, format int, shape string) []string {
	if _, ok := parseVersion(version); !ok {
		return []string{fmt.Sprintf("IRVersion %q is not MAJOR.MINOR.PATCH (docs/ir/ir-version.md)", version)}
	}
	if len(rows) == 0 {
		return []string{"generations is empty: it holds a row for IRVersion " + version}
	}
	last := rows[len(rows)-1]
	row := func(v string) string {
		return fmt.Sprintf("{version: %q, format: %d, shape: %q}", v, format, shape)
	}
	switch {
	case last.version != version && compareVersions(version, last.version) > 0:
		finding := fmt.Sprintf("IRVersion is %s, but generations ends at %s: append %s", version, last.version, row(version))
		if format == last.format && shape == last.shape {
			finding += fmt.Sprintf(". The shape is still %s's, so set meaning to what changed, "+
				"or revert the bump (docs/ir/ir-version.md)", last.version)
		}
		return []string{finding}
	case last.version != version:
		return []string{fmt.Sprintf("generations ends at %s, past IRVersion %s: a row is appended "+
			"by the bump that moves IRVersion to it", last.version, version)}
	case format != last.format:
		return []string{fmt.Sprintf("jsonshape.Format is %d, but %s was described in format %d: append %s, "+
			"describing it anew. If the shape changed too, the bump is a row after that one",
			format, version, last.format, row(version))}
	case shape != last.shape:
		next := nextGeneration(version)
		return []string{fmt.Sprintf("the encoded shape changed, but IRVersion is still %s, "+
			"published with another shape (docs/ir/ir-version.md). Set IRVersion to %s, append %s, "+
			"and say what changed under %s in docs/ir/version-history.md. "+
			"If a merge brought in another change that claimed %s, that version is taken: the next is yours. "+
			"If only jsonshape changed how it spells a shape, move jsonshape.Format instead",
			version, next, row(next), next, version)}
	default:
		return nil
	}
}

// rowFindings returns what keeps rows from being a table of generations, the
// newest described in format at most.
func rowFindings(rows []generation, format int) []string {
	if len(rows) == 0 {
		return []string{"generations is empty"}
	}
	var out []string
	for i, r := range rows {
		if _, ok := parseVersion(r.version); !ok {
			out = append(out, fmt.Sprintf("row %d: version %q is not MAJOR.MINOR.PATCH", i, r.version))
		}
		if r.format < 1 || r.format > format {
			out = append(out, fmt.Sprintf("row %d: format %d is no jsonshape.Format up to %d", i, r.format, format))
		}
		if b, err := hex.DecodeString(r.shape); err != nil || len(b) != sha256.Size || strings.ToLower(r.shape) != r.shape {
			out = append(out, fmt.Sprintf("row %d: shape %q is not a lower-case hex SHA-256", i, r.shape))
		}
		if i == 0 {
			if r.meaning != "" {
				out = append(out, fmt.Sprintf("row 0: %s has no generation before it to share a shape with", r.version))
			}
			continue
		}
		if finding := stepFinding(rows[i-1], r); finding != "" {
			out = append(out, fmt.Sprintf("row %d: %s", i, finding))
		}
	}
	return out
}

// stepFinding returns what is wrong with r following prev, or "" if nothing.
func stepFinding(prev, r generation) string {
	order := compareVersions(prev.version, r.version)
	switch {
	case order > 0:
		return fmt.Sprintf("%s follows %s, but rows ascend by version", r.version, prev.version)
	case order == 0 && r.format <= prev.format:
		return fmt.Sprintf("%s is described again in format %d after format %d: "+
			"a generation is described anew only in a later format", r.version, r.format, prev.format)
	case order == 0 && r.meaning != "":
		return fmt.Sprintf("%s in format %d describes a generation anew, which is no bump, "+
			"so it states no meaning", r.version, r.format)
	case order == 0:
		return ""
	case r.format != prev.format:
		return fmt.Sprintf("%s is described in format %d and %s in format %d: describe %s anew "+
			"in format %d first, so the bump compares with it", r.version, r.format, prev.version,
			prev.format, prev.version, r.format)
	case r.shape == prev.shape && r.meaning == "":
		return fmt.Sprintf("%s has %s's shape: a bump that changes no shape states the meaning it "+
			"changes (docs/ir/ir-version.md); without one, revert the bump", r.version, prev.version)
	case r.shape != prev.shape && r.meaning != "":
		return fmt.Sprintf("%s changes %s's shape, so it states no meaning: "+
			"its version history entry says what changed", r.version, prev.version)
	default:
		return ""
	}
}

// historyEntry matches one entry of the version history: a list item opening
// with the version in bold.
var historyEntry = regexp.MustCompile(`^- \*\*([^*]+)\*\* — `)

// versionHistory returns the versions versionLog lists, in the order it lists
// them: the entries between its heading and the next heading or rule.
func versionHistory(doc string) ([]string, error) {
	_, section, found := strings.Cut(doc, "\n## Generations\n")
	if !found {
		return nil, fmt.Errorf("%s has no ## Generations heading", versionLog)
	}
	var out []string
	for line := range strings.Lines(section) {
		if strings.HasPrefix(line, "#") || strings.HasPrefix(line, "---") {
			break
		}
		if m := historyEntry.FindStringSubmatch(line); m != nil {
			out = append(out, m[1])
		}
	}
	return out, nil
}

// historyFindings returns what keeps history from recording each generation:
// it must ascend, end at version, and name every version rows does.
func historyFindings(history []string, rows []generation, version string) []string {
	if len(history) == 0 {
		return []string{"docs/ir/version-history.md lists no generation"}
	}
	var out []string
	for i := 1; i < len(history); i++ {
		if compareVersions(history[i-1], history[i]) >= 0 {
			out = append(out, fmt.Sprintf("the version history lists %s after %s, but its entries ascend",
				history[i], history[i-1]))
		}
	}
	if last := history[len(history)-1]; last != version {
		out = append(out, fmt.Sprintf("docs/ir/version-history.md ends at %s, but IRVersion is %s: "+
			"add an entry saying what %s changed", last, version, version))
	}
	for _, r := range rows {
		// IRVersion's entry is either the last one or the one asked for above.
		if r.version == version {
			continue
		}
		if !slices.Contains(history, r.version) {
			out = append(out, fmt.Sprintf("generation %s has no entry in docs/ir/version-history.md", r.version))
		}
	}
	return slices.Compact(out)
}

// shapeA and shapeB are two distinct well-formed shapes for the synthetic
// tables below.
const (
	shapeA = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	shapeB = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
)

// TestPinFindings plants each way a build can disagree with its table and
// requires the finding that says what to do; a build that agrees has none.
func TestPinFindings(t *testing.T) {
	t.Parallel()
	pinned := []generation{{version: "0.3.0", format: 1, shape: shapeA}}
	for _, tc := range []struct {
		name    string
		rows    []generation
		version string
		format  int
		shape   string
		want    string // "" means no finding
	}{
		{"the pinned shape", pinned, "0.3.0", 1, shapeA, ""},
		{"a shape change at an unmoved version", pinned, "0.3.0", 1, shapeB,
			`the encoded shape changed, but IRVersion is still 0.3.0, published with another shape (docs/ir/ir-version.md). ` +
				`Set IRVersion to 0.4.0, append {version: "0.4.0", format: 1, shape: "` + shapeB + `"}, ` +
				`and say what changed under 0.4.0 in docs/ir/version-history.md. ` +
				`If a merge brought in another change that claimed 0.3.0, that version is taken: the next is yours. ` +
				`If only jsonshape changed how it spells a shape, move jsonshape.Format instead`},
		{"a bump with no row", pinned, "0.4.0", 1, shapeB,
			`IRVersion is 0.4.0, but generations ends at 0.3.0: append {version: "0.4.0", format: 1, shape: "` + shapeB + `"}`},
		{"a bump with no row and no shape change", pinned, "0.4.0", 1, shapeA,
			`IRVersion is 0.4.0, but generations ends at 0.3.0: append {version: "0.4.0", format: 1, shape: "` + shapeA + `"}. ` +
				`The shape is still 0.3.0's, so set meaning to what changed, or revert the bump (docs/ir/ir-version.md)`},
		{"a row with no bump", pinned, "0.2.0", 1, shapeA,
			`generations ends at 0.3.0, past IRVersion 0.2.0: a row is appended by the bump that moves IRVersion to it`},
		{"a new description format", pinned, "0.3.0", 2, shapeB,
			`jsonshape.Format is 2, but 0.3.0 was described in format 1: append {version: "0.3.0", format: 2, shape: "` +
				shapeB + `"}, describing it anew. If the shape changed too, the bump is a row after that one`},
		{"an empty table", nil, "0.3.0", 1, shapeA, "generations is empty: it holds a row for IRVersion 0.3.0"},
		{"a version that is no version", pinned, "v0.3", 1, shapeA, `IRVersion "v0.3" is not MAJOR.MINOR.PATCH (docs/ir/ir-version.md)`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := pinFindings(tc.rows, tc.version, tc.format, tc.shape)
			if tc.want == "" {
				assert.Empty(t, got)
				return
			}
			assert.Equal(t, []string{tc.want}, got)
		})
	}
}

// TestRowFindings plants each malformed table, one defect at a time, and
// requires the one finding naming it; a well-formed table has none.
func TestRowFindings(t *testing.T) {
	t.Parallel()
	a := generation{version: "0.3.0", format: 1, shape: shapeA}
	b := generation{version: "0.4.0", format: 1, shape: shapeB}
	with := func(g generation, edit func(*generation)) generation {
		edit(&g)
		return g
	}
	for _, tc := range []struct {
		name string
		rows []generation
		want string // "" means no finding
	}{
		{"a bump that changes the shape", []generation{a, b}, ""},
		{"a bump for a changed meaning", []generation{a, with(b, func(g *generation) { g.shape, g.meaning = shapeA, "x" })}, ""},
		{"a generation described anew", []generation{a, with(a, func(g *generation) { g.format, g.shape = 2, shapeB })}, ""},
		{"an empty table", nil, "generations is empty"},
		{"a version that is no version", []generation{with(a, func(g *generation) { g.version = "0.3" })},
			`row 0: version "0.3" is not MAJOR.MINOR.PATCH`},
		{"no format", []generation{with(a, func(g *generation) { g.format = 0 })}, "row 0: format 0 is no jsonshape.Format up to 2"},
		{"a format jsonshape has not reached", []generation{with(a, func(g *generation) { g.format = 3 })},
			"row 0: format 3 is no jsonshape.Format up to 2"},
		{"a short shape", []generation{with(a, func(g *generation) { g.shape = "aa" })},
			`row 0: shape "aa" is not a lower-case hex SHA-256`},
		{"a shape that is not hex", []generation{with(a, func(g *generation) { g.shape = strings.Repeat("g", 64) })},
			`row 0: shape "` + strings.Repeat("g", 64) + `" is not a lower-case hex SHA-256`},
		{"an upper-case shape", []generation{with(a, func(g *generation) { g.shape = strings.ToUpper(shapeA) })},
			`row 0: shape "` + strings.ToUpper(shapeA) + `" is not a lower-case hex SHA-256`},
		{"a first row claiming a predecessor's shape", []generation{with(a, func(g *generation) { g.meaning = "x" })},
			"row 0: 0.3.0 has no generation before it to share a shape with"},
		{"rows out of order", []generation{b, a}, "row 1: 0.3.0 follows 0.4.0, but rows ascend by version"},
		{"a version listed twice", []generation{a, with(a, func(g *generation) { g.shape = shapeB })},
			"row 1: 0.3.0 is described again in format 1 after format 1: a generation is described anew only in a later format"},
		{"a description anew claiming a meaning", []generation{a, with(a, func(g *generation) { g.format, g.meaning = 2, "x" })},
			"row 1: 0.3.0 in format 2 describes a generation anew, which is no bump, so it states no meaning"},
		{"a bump in a new format", []generation{a, with(b, func(g *generation) { g.format = 2 })},
			"row 1: 0.4.0 is described in format 2 and 0.3.0 in format 1: describe 0.3.0 anew in format 2 first, so the bump compares with it"},
		{"a bump that changes no shape", []generation{a, with(b, func(g *generation) { g.shape = shapeA })},
			"row 1: 0.4.0 has 0.3.0's shape: a bump that changes no shape states the meaning it changes (docs/ir/ir-version.md); without one, revert the bump"},
		{"a meaning claimed by a shape change", []generation{a, with(b, func(g *generation) { g.meaning = "x" })},
			"row 1: 0.4.0 changes 0.3.0's shape, so it states no meaning: its version history entry says what changed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := rowFindings(tc.rows, 2)
			if tc.want == "" {
				assert.Empty(t, got)
				return
			}
			assert.Equal(t, []string{tc.want}, got)
		})
	}
}

// TestVersionHistory reads the entries of a history section and stops at the
// next heading or rule, so a later section's list items are not read as
// generations.
func TestVersionHistory(t *testing.T) {
	t.Parallel()
	doc := "# T\n\n## Generations\n\nIntro.\n\n" +
		"- **0.2.0** — first.\n  - **0.9.0** — a nested item is no entry.\n- **0.3.0** — second,\n  wrapped.\n" +
		"- **bold** without a dash is no entry\n\n---\n\n- **0.4.0** — past the rule.\n"
	got, err := versionHistory(doc)
	require.NoError(t, err)
	assert.Equal(t, []string{"0.2.0", "0.3.0"}, got)

	got, err = versionHistory("x\n## Generations\n- **0.2.0** — first.\n## Next\n- **0.3.0** — past the heading.\n")
	require.NoError(t, err)
	assert.Equal(t, []string{"0.2.0"}, got)

	got, err = versionHistory("# T\n\n- **0.2.0** — under no history heading.\n")
	require.ErrorContains(t, err, "has no ## Generations heading")
	assert.Nil(t, got)
}

// TestHistoryFindings plants each way a history can fail to record the table,
// and requires the finding naming it.
func TestHistoryFindings(t *testing.T) {
	t.Parallel()
	rows := []generation{{version: "0.3.0"}, {version: "0.3.0"}, {version: "0.4.0"}}
	for _, tc := range []struct {
		name    string
		history []string
		version string
		want    []string
	}{
		{"a history recording each generation", []string{"0.2.0", "0.3.0", "0.4.0"}, "0.4.0", nil},
		{"an empty history", nil, "0.4.0", []string{"docs/ir/version-history.md lists no generation"}},
		{"a history out of order", []string{"0.3.0", "0.2.0", "0.4.0"}, "0.4.0",
			[]string{"the version history lists 0.2.0 after 0.3.0, but its entries ascend"}},
		{"a history listing a version twice", []string{"0.3.0", "0.3.0", "0.4.0"}, "0.4.0",
			[]string{"the version history lists 0.3.0 after 0.3.0, but its entries ascend"}},
		{"a bump the history does not record", []string{"0.3.0", "0.4.0"}, "0.5.0",
			[]string{"docs/ir/version-history.md ends at 0.4.0, but IRVersion is 0.5.0: add an entry saying what 0.5.0 changed"}},
		{"a bump the table records and the history does not", []string{"0.2.0", "0.3.0"}, "0.4.0",
			[]string{"docs/ir/version-history.md ends at 0.3.0, but IRVersion is 0.4.0: add an entry saying what 0.4.0 changed"}},
		{"a generation the history skips", []string{"0.2.0", "0.4.0"}, "0.4.0",
			[]string{"generation 0.3.0 has no entry in docs/ir/version-history.md"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, historyFindings(tc.history, rows, tc.version))
		})
	}
}

// TestParseVersion holds the grammar IRVersion is read in, and the order and
// successor derived from it.
func TestParseVersion(t *testing.T) {
	t.Parallel()
	for s, want := range map[string]bool{
		"0.14.0": true, "1.0.0": true, "10.20.30": true,
		"": false, "0.14": false, "0.14.0.1": false, "v0.14.0": false, "0.14.0-rc.1": false,
		"00.1.0": false, "0.01.0": false, "0..0": false, "-1.0.0": false, "0.1.x": false,
		"1234567890.0.0": false,
	} {
		_, ok := parseVersion(s)
		assert.Equal(t, want, ok, "%q", s)
	}
	for _, tc := range []struct {
		a, b string
		want int
	}{
		{"0.9.0", "0.10.0", -1}, {"0.10.0", "0.9.0", 1}, {"1.0.0", "1.0.0", 0},
		{"x", "0.1.0", -1}, {"0.1.0", "x", 1}, {"x", "y", -1},
	} {
		assert.Equal(t, tc.want, compareVersions(tc.a, tc.b), "%s against %s", tc.a, tc.b)
	}
	assert.Equal(t, "0.15.0", nextGeneration("0.14.3"))
	assert.Equal(t, "x", nextGeneration("x"))
}
