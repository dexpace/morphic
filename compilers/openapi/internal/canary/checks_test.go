package canary_test

import (
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/internal/decls"
)

// goodRecord is a record in the shape parseRecord reads, with what it must not
// read as a field: a list and a heading inside fenced code, a paragraph
// opening with an issue number, and a Code item running onto a second line.
const goodRecord = "# Title\n\n## Section\n\n" +
	"### `first-entry`\n\nProse.\n\n```yaml\n- not: a field\n### not a heading\n```\n\n" +
	"- Verified at: v1.2.3\n" +
	"- Pinned by: `TestLibraryBehaviour/first-entry`\n" +
	"- Code:\n" +
	"  - `load.first` does a thing,\n    over two lines.\n" +
	"  - `scan.Type.Method` does another.\n\n" +
	"#558 is an issue, not a heading.\n\n" +
	"### `second-entry`\n\n" +
	"- Verified at: v1.2.3\n" +
	"- Pinned by: `TestLibraryBehaviour/second-entry`\n" +
	"- Code:\n" +
	"  - `openapi.Limits.Field` is a field.\n"

// TestParseRecord_ReadsEachEntry reads the good record whole: both entries,
// each field, and nothing from the fence or the paragraph between them.
func TestParseRecord_ReadsEachEntry(t *testing.T) {
	t.Parallel()
	entries, problems := parseRecord(goodRecord)
	require.Empty(t, problems)
	assert.Equal(t, []entry{
		{key: "first-entry", line: 5, verified: "v1.2.3", pinnedBy: "TestLibraryBehaviour/first-entry",
			code: []string{"load.first", "scan.Type.Method"}},
		{key: "second-entry", line: 23, verified: "v1.2.3", pinnedBy: "TestLibraryBehaviour/second-entry",
			code: []string{"openapi.Limits.Field"}},
	}, entries)
}

// TestParseRecord_ReportsEachShapeProblem plants one defect at a time in the
// good record and requires exactly the problem it makes.
func TestParseRecord_ReportsEachShapeProblem(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, from, to, want string
	}{
		{"a heading that is no key", "### `second-entry`", "### Second entry", "is not a key"},
		{"a key that is no slug", "### `second-entry`", "### `Second_entry`", "is not a key"},
		{"no version", "- Verified at: v1.2.3\n- Pinned by: `TestLibraryBehaviour/second-entry`",
			"- Pinned by: `TestLibraryBehaviour/second-entry`", `second-entry does not write the field "Verified at"`},
		{"an empty version", "- Verified at: v1.2.3\n- Pinned by: `TestLibraryBehaviour/second-entry`",
			"- Verified at:\n- Pinned by: `TestLibraryBehaviour/second-entry`", `writes the field "Verified at" naming nothing`},
		{"a pin naming no test", "- Pinned by: `TestLibraryBehaviour/second-entry`\n",
			"- Pinned by: second-entry\n", `writes the field "Pinned by" naming nothing`},
		{"a repeated pin", "- Pinned by: `TestLibraryBehaviour/second-entry`\n",
			"- Pinned by: `TestLibraryBehaviour/second-entry`\n- Pinned by: `x`\n", `writes the field "Pinned by" twice`},
		{"no Code item", "- Code:\n  - `openapi.Limits.Field` is a field.\n", "- Code:\n",
			"second-entry names no declaration under"},
		{"a Code item naming nothing", "  - `scan.Type.Method` does another.", "  - another.", "names no declaration"},
		{"a field no reader knows", "- Code:\n  - `openapi", "- Owner: me\n- Code:\n  - `openapi", "a field no reader knows"},
		{"a Code item on the bullet's line", "- Code:\n  - `openapi", "- Code: `scan.X`\n  - `openapi",
			"on the bullet's own line"},
		{"a Code line that is no item", "  - `scan.Type.Method` does another.",
			"  - `scan.Type.Method` does another.\n  stray", "a Code line that is no item"},
		{"a continuation before any item", "- Code:\n  - `openapi", "- Code:\n    orphan\n  - `openapi",
			"a Code line that is no item"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, 1, strings.Count(goodRecord, tc.from), "the plant must have one site")
			_, problems := parseRecord(strings.Replace(goodRecord, tc.from, tc.to, 1))
			require.Len(t, problems, 1, "%v", problems)
			assert.Contains(t, problems[0], tc.want)
		})
	}
}

// TestCodeProblems_ReportsWhatACitationLacks requires a problem for a
// declaration that cannot be found, one whose doc comment cites nothing, and
// one citing another entry, and none for one citing its own.
func TestCodeProblems_ReportsWhatACitationLacks(t *testing.T) {
	t.Parallel()
	docs := map[string]string{
		"p.Cites":   "Cites does it (docs/parser-library.md#this-entry).",
		"p.Silent":  "Silent does it.",
		"p.Another": "Another does it (docs/parser-library.md#another-entry).",
	}
	problems := codeProblems([]entry{{key: "this-entry", code: []string{"p.Cites", "p.Silent", "p.Another", "p.Gone"}}},
		func(symbol string) (string, error) {
			doc, ok := docs[symbol]
			if !ok {
				return "", errors.New("declared nowhere")
			}
			return doc, nil
		})
	require.Len(t, problems, 3, "%v", problems)
	assert.Contains(t, problems[0], "p.Silent")
	assert.Contains(t, problems[1], "p.Another")
	assert.Contains(t, problems[2], "p.Gone: declared nowhere")
}

// TestDocAt_FindsEachKindOfDeclaration reads the doc comment of a function, a
// method, a type, a constant and a variable of a group, one declared alone,
// and a field.
func TestDocAt_FindsEachKindOfDeclaration(t *testing.T) {
	t.Parallel()
	const src = `package p

// F is a function.
func F() {}

// T is a type.
type T struct {
	// Field is a field.
	Field int
}

// M is a method.
func (T) M() {}

// The group's comment.
const (
	// A is a constant.
	A = 1
	B = 2
)

// V is a variable.
var V = 3
`
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "p.go", src, parser.ParseComments)
	require.NoError(t, err)
	want := map[string]string{
		"F": "F is a function.\n", "T": "T is a type.\n", "Field": "Field is a field.\n",
		"M": "M is a method.\n", "A": "A is a constant.\n", "B": "The group's comment.\n",
		"V": "V is a variable.\n",
	}
	found := map[string]string{}
	ast.Inspect(f, func(n ast.Node) bool {
		if id, ok := n.(*ast.Ident); ok && want[id.Name] != "" && found[id.Name] == "" {
			doc, ok := docAt([]*ast.File{f}, id.Pos())
			require.True(t, ok, id.Name)
			found[id.Name] = doc
		}
		return true
	})
	assert.Equal(t, want, found)
	_, ok := docAt([]*ast.File{f}, token.NoPos)
	assert.False(t, ok, "no declaration is named at no position")
}

// TestDocOf_ReportsWhatItCannotFind holds docOf to an error, not a guess, for
// each way a name can miss: no package, no declaration, no member, and a name
// of neither shape.
func TestDocOf_ReportsWhatItCannotFind(t *testing.T) {
	t.Parallel()
	pkg, err := decls.Load(importPath("load"))
	require.NoError(t, err)
	pkgs := map[string]*decls.Package{importPath("load"): pkg}

	doc, err := docOf(pkgs, "load.releaseAnchors")
	require.NoError(t, err)
	assert.True(t, citesEntry(doc, "anchored-entry-dropped"), "the control finds a real declaration")
	for _, symbol := range []string{"scan.Cycles", "load.noSuchName", "load.reach.noSuchMember", "load", "a.b.c.d"} {
		_, err := docOf(pkgs, symbol)
		assert.Error(t, err, symbol)
	}
}

// writeTree writes each file under root, by its slash-separated path.
func writeTree(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for path, text := range files {
		full := filepath.Join(root, filepath.FromSlash(path))
		require.NoError(t, os.MkdirAll(filepath.Dir(full), 0o755))
		require.NoError(t, os.WriteFile(full, []byte(text), 0o600))
	}
}

// TestCitationProblems_ReportsAKeyNoEntryHas plants citations of a key the
// record holds and of one it does not, in each kind of file read, in a file
// and a directory that are not, and as a link inside the record itself.
func TestCitationProblems_ReportsAKeyNoEntryHas(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeTree(t, root, map[string]string{
		"a/a.go":                 "package a // docs/parser-library.md#known and docs/parser-library.md#gone-go\n",
		"a/a.yaml":               "# parser-library.md#gone-yaml\n",
		"a/a.md":                 "See [it](docs/parser-library.md#known).\n",
		"a/a.txt":                "parser-library.md#unread-text\n",
		".hidden/h.go":           "package h // parser-library.md#unread-hidden\n",
		"docs/parser-library.md": "### `known`\n\nAs [`known`](#known) and [`gone`](#gone-link) say.\n",
		"b/b.md":                 "First line.\nSee parser-library.md#gone-md.\n",
	})
	problems, cited, err := citationProblems(root, map[string]bool{"known": true})
	require.NoError(t, err)
	assert.Equal(t, 7, cited, "each citation read is counted once")
	require.Len(t, problems, 4, "%v", problems)
	joined := strings.Join(problems, "\n")
	for _, gone := range []string{`a/a.go:1 cites "gone-go"`, `a/a.yaml:1 cites "gone-yaml"`,
		`b/b.md:2 cites "gone-md"`, `docs/parser-library.md:3 cites "gone-link"`} {
		assert.Contains(t, joined, gone)
	}
}

// TestVersionProblems_ReportsAVersionInAComment plants a version in a Go
// comment and a YAML comment, which are reported, and in a Go string and a
// quoted YAML value, which are not.
func TestVersionProblems_ReportsAVersionInAComment(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeTree(t, root, map[string]string{
		"tree/a.go":    "package a\n\n// It held at v1.2.3.\nconst s = \"v4.5.6\" // and at v1.2\n",
		"tree/a.yaml":  "x: 'a #v7.8.9'\ny: 1 # since v1.0.0\n$ref: '#/v2.0.0'\n",
		"other/b.go":   "package b // v9.9.9\n",
		"tree/.d/c.go": "package c // v8.8.8\n",
	})
	problems, read, err := versionProblems(root, "tree")
	require.NoError(t, err)
	assert.Equal(t, 3, read, "each comment in the files read")
	assert.Equal(t, []string{"tree/a.go:3 names v1.2.3", "tree/a.go:4 names v1.2", "tree/a.yaml:2 names v1.0.0"},
		problems)
}

// TestClassify_ReadsEachEnding holds classify to each way a child can end: the
// five outcomes, and the endings none of them names.
func TestClassify_ReadsEachEnding(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		out      string
		code     int
		timedOut bool
		want     result
		ok       bool
	}{
		{"a report", "noise\n" + reportMark + "\"line one\\nline two\"\n", 0, false,
			result{outcome: returned, report: "line one\nline two"}, true},
		{"a clean exit with no report", "noise\n", 0, false, result{}, false},
		{"a report that is not quoted", reportMark + "bare\n", 0, false, result{}, false},
		{"memory", memoryMark + "1234\n", memoryExit, false, result{outcome: exhausted, report: "1234"}, true},
		{"memory with no count", "", memoryExit, false, result{}, false},
		{"an overflow", "runtime: goroutine stack exceeds\nfatal error: stack overflow\n", 2, false,
			result{outcome: overflowed}, true},
		{"a panic", "panic: runtime error: nil map\n\ngoroutine 9 [running]:\n", 2, false,
			result{outcome: panicked, report: "runtime error: nil map"}, true},
		{"another fatal error", "fatal error: concurrent map writes\n", 2, false, result{}, false},
		{"another status", "", 1, false, result{}, false},
		{"a timeout", "partial", -1, true, result{outcome: hung}, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, ok := classify(tc.out, tc.code, tc.timedOut)
			require.Equal(t, tc.ok, ok)
			if ok {
				assert.Equal(t, tc.want.outcome, got.outcome)
				assert.Equal(t, tc.want.report, got.report)
				assert.Equal(t, tc.out, got.output)
			}
		})
	}
}

// TestProbe_TellsARecoverablePanicFromOneNothingReaches runs a child that
// panics on its own goroutine, which reports the panic and returns, and one
// that panics on another goroutine, which ends the process: the difference
// tagged-mapping-panic records.
func TestProbe_TellsARecoverablePanicFromOneNothingReaches(t *testing.T) {
	t.Parallel()
	here := probe(t, "panic here", "")
	require.Equal(t, returned, here.outcome, here.output)
	assert.Equal(t, recoveredMark+"here", here.report)

	elsewhere := probe(t, "panic elsewhere", "")
	require.Equal(t, panicked, elsewhere.outcome, elsewhere.output)
	assert.Equal(t, "elsewhere", elsewhere.report)
}
