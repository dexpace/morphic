package canary_test

import (
	"cmp"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"runtime/debug"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/internal/decls"
)

// repoRoot is the repository root, from this package's directory.
const repoRoot = "../../../.."

// recordPath is the page this package pins, relative to repoRoot.
const recordPath = "docs/parser-library.md"

// libraryModule is the parser library's module path.
const libraryModule = "github.com/speakeasy-api/openapi"

// module is this repository's module path.
const module = "github.com/dexpace/morphic"

// pinPrefix opens the name of every entry's pin: the subtest of
// TestLibraryBehaviour named for its key.
const pinPrefix = "TestLibraryBehaviour/"

// The fields an entry writes, each a bullet of its own.
const (
	verifiedField = "- Verified at: "
	pinnedField   = "- Pinned by: "
	codeField     = "- Code:"
	codeItem      = "  - "
)

var (
	// keyHeading is an entry's heading: its key, a lowercase slug, in code.
	keyHeading = regexp.MustCompile("^### `([a-z0-9]+(?:-[a-z0-9]+)*)`$")
	// heading is any Markdown heading.
	heading = regexp.MustCompile(`^#{1,6} `)
	// codeSpan is the first span of code on a line.
	codeSpan = regexp.MustCompile("`([^`]+)`")
	// citation is a reference to an entry of the record, anywhere it is written.
	citation = regexp.MustCompile(`parser-library\.md#([A-Za-z0-9_-]+)`)
	// anchorLink is a link from the record to one of its own entries.
	anchorLink = regexp.MustCompile(`\]\(#([A-Za-z0-9_-]+)\)`)
	// version is a release version, written as a v and two or three numbers.
	version = regexp.MustCompile(`\bv\d+\.\d+(?:\.\d+)?\b`)
)

// entry is one entry of the record: its key, the line of its heading, and
// what its fields say.
type entry struct {
	key      string
	line     int
	verified string
	pinnedBy string
	code     []string
}

// readRecord returns the record's entries, failing the test on any problem
// with its shape.
func readRecord(t *testing.T) []entry {
	t.Helper()
	text, err := os.ReadFile(filepath.Join(repoRoot, recordPath))
	require.NoError(t, err)
	entries, problems := parseRecord(string(text))
	require.Empty(t, problems, "%s is not in the shape its readers expect", recordPath)
	require.NotEmpty(t, entries, "the record holds no entry, so nothing below checks anything")
	return entries
}

// parseRecord reads the entries of a record, and reports each problem with
// its shape: a third-level heading that is not a key, or an entry whose
// fields are missing, repeated or empty. Fenced code is not read.
func parseRecord(text string) ([]entry, []string) {
	var entries []entry
	var problems []string
	var cur *entry
	fenced, inCode := false, false
	for i, line := range strings.Split(text, "\n") {
		n := i + 1
		if strings.HasPrefix(line, "```") {
			fenced = !fenced
		}
		switch {
		case fenced || strings.HasPrefix(line, "```"):
		case strings.HasPrefix(line, "### "):
			m := keyHeading.FindStringSubmatch(line)
			if m == nil {
				problems = append(problems, fmt.Sprintf("line %d: a third-level heading is not a key: %q", n, line))
				cur = nil
				continue
			}
			entries = append(entries, entry{key: m[1], line: n})
			cur, inCode = &entries[len(entries)-1], false
		case heading.MatchString(line):
			cur = nil
		case cur != nil:
			inCode, problems = readField(cur, line, n, inCode, problems)
		}
	}
	for _, e := range entries {
		problems = append(problems, fieldProblems(e)...)
	}
	return entries, problems
}

// readField reads one line of an entry into e, returning whether the line
// leaves the reader in the Code list, and problems with what it found.
func readField(e *entry, line string, n int, inCode bool, problems []string) (bool, []string) {
	repeated := func(field string) []string {
		return append(problems, fmt.Sprintf("line %d: %s writes %q twice", n, e.key, field))
	}
	switch {
	case strings.HasPrefix(line, verifiedField):
		if e.verified != "" {
			return false, repeated(verifiedField)
		}
		e.verified = strings.TrimSpace(strings.TrimPrefix(line, verifiedField))
		return false, problems
	case strings.HasPrefix(line, pinnedField):
		if e.pinnedBy != "" {
			return false, repeated(pinnedField)
		}
		e.pinnedBy = firstSpan(line)
		return false, problems
	case line == codeField:
		if e.code != nil {
			return true, repeated(codeField)
		}
		e.code = []string{}
		return true, problems
	case inCode && strings.HasPrefix(line, codeItem):
		symbol := firstSpan(line)
		if symbol == "" {
			return true, append(problems, fmt.Sprintf("line %d: a Code item of %s names no declaration", n, e.key))
		}
		e.code = append(e.code, symbol)
		return true, problems
	case strings.HasPrefix(line, "- "):
		return false, append(problems, fmt.Sprintf("line %d: %s writes a field no reader knows: %q", n, e.key, line))
	}
	return inCode && (line == "" || strings.HasPrefix(line, "    ")), problems
}

// firstSpan returns the text of the first span of code on line, or "".
func firstSpan(line string) string {
	if m := codeSpan.FindStringSubmatch(line); m != nil {
		return m[1]
	}
	return ""
}

// fieldProblems reports each field e leaves unwritten, and a Code field that
// names no declaration.
func fieldProblems(e entry) []string {
	var problems []string
	for field, written := range map[string]bool{
		verifiedField: e.verified != "", pinnedField: e.pinnedBy != "", codeField: e.code != nil,
	} {
		if !written {
			problems = append(problems, fmt.Sprintf("line %d: %s does not write %q", e.line, e.key, field))
		}
	}
	if e.code != nil && len(e.code) == 0 {
		problems = append(problems, fmt.Sprintf("line %d: %s names no declaration under %q", e.line, e.key, codeField))
	}
	slices.Sort(problems)
	return problems
}

// TestRecord_EveryEntryHasACanaryAndNoMore holds the record's keys to the
// canaries': an entry with no pin records a behaviour nothing checks, and a
// pin with no entry checks a behaviour nothing records.
func TestRecord_EveryEntryHasACanaryAndNoMore(t *testing.T) {
	t.Parallel()
	entries := readRecord(t)
	keys := make([]string, 0, len(entries))
	for _, e := range entries {
		keys = append(keys, e.key)
	}
	slices.Sort(keys)
	assert.Equal(t, slices.Sorted(maps.Keys(canaries())), keys)
}

// TestRecord_EachEntryNamesItsCanary requires each entry's pin to be the
// subtest named for its key, the one TestLibraryBehaviour runs.
func TestRecord_EachEntryNamesItsCanary(t *testing.T) {
	t.Parallel()
	for _, e := range readRecord(t) {
		assert.Equal(t, pinPrefix+e.key, e.pinnedBy, "line %d", e.line)
	}
}

// TestRecord_EntriesAreVerifiedAtTheLinkedVersion requires every entry to
// have been verified at the library version this binary links, so a bump
// fails here until every entry has been read again.
func TestRecord_EntriesAreVerifiedAtTheLinkedVersion(t *testing.T) {
	t.Parallel()
	linked, err := linkedVersion()
	require.NoError(t, err)
	for _, e := range readRecord(t) {
		assert.Equal(t, linked, e.verified,
			"line %d: %s was verified at another version; re-read it against %s, then restamp it",
			e.line, e.key, linked)
	}
}

// linkedVersion returns the version of the parser library this binary was
// built with: the one go.mod requires, a replace notwithstanding.
func linkedVersion() (string, error) {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "", errors.New("the binary carries no build information")
	}
	for _, dep := range info.Deps {
		if dep.Path == libraryModule {
			return dep.Version, nil
		}
	}
	return "", fmt.Errorf("the binary does not link %s", libraryModule)
}

// TestRecord_CodeCitesItsEntry requires each declaration an entry names to
// exist in the package it names, and its doc comment to cite the entry, so the
// reader of either finds the other.
func TestRecord_CodeCitesItsEntry(t *testing.T) {
	t.Parallel()
	entries := readRecord(t)
	paths := map[string]bool{}
	for _, e := range entries {
		for _, symbol := range e.code {
			paths[importPath(strings.Split(symbol, ".")[0])] = true
		}
	}
	pkgs, err := decls.LoadEach(slices.Sorted(maps.Keys(paths))...)
	require.NoError(t, err)
	byPath := map[string]*decls.Package{}
	for _, p := range pkgs {
		byPath[p.Types.Path()] = p
	}
	for _, problem := range codeProblems(entries, func(symbol string) (string, error) {
		return docOf(byPath, symbol)
	}) {
		t.Error(problem)
	}
}

// codeProblems reports each declaration an entry names that docOf cannot
// find, or whose doc comment does not cite the entry.
func codeProblems(entries []entry, docOf func(symbol string) (string, error)) []string {
	var problems []string
	for _, e := range entries {
		for _, symbol := range e.code {
			doc, err := docOf(symbol)
			switch {
			case err != nil:
				problems = append(problems, fmt.Sprintf("%s names %s: %v", e.key, symbol, err))
			case !citesEntry(doc, e.key):
				problems = append(problems, fmt.Sprintf("%s names %s, whose doc comment does not cite %s#%s",
					e.key, symbol, recordPath, e.key))
			}
		}
	}
	return problems
}

// citesEntry reports whether text cites the record's entry key.
func citesEntry(text, key string) bool {
	for _, m := range citation.FindAllStringSubmatch(text, -1) {
		if m[1] == key {
			return true
		}
	}
	return false
}

// importPath returns the import path a symbol's package name stands for: the
// compiler itself, or one of its internal packages.
func importPath(pkg string) string {
	if pkg == "openapi" {
		return module + "/compilers/openapi"
	}
	return module + "/compilers/openapi/internal/" + pkg
}

// docOf returns the doc comment of the declaration symbol names: pkg.Name, or
// pkg.Type.Member for a method or field.
func docOf(pkgs map[string]*decls.Package, symbol string) (string, error) {
	parts := strings.Split(symbol, ".")
	if len(parts) < 2 || len(parts) > 3 {
		return "", fmt.Errorf("%q is neither package.Name nor package.Type.Member", symbol)
	}
	p, ok := pkgs[importPath(parts[0])]
	if !ok {
		return "", fmt.Errorf("no package %s was loaded", importPath(parts[0]))
	}
	obj := p.Types.Scope().Lookup(parts[1])
	if obj == nil {
		return "", fmt.Errorf("%s declares no %s", p.Types.Path(), parts[1])
	}
	if len(parts) == 3 {
		member, _, _ := types.LookupFieldOrMethod(types.NewPointer(obj.Type()), true, p.Types, parts[2])
		if member == nil {
			return "", fmt.Errorf("%s.%s has no field or method %s", p.Types.Path(), parts[1], parts[2])
		}
		obj = member
	}
	doc, ok := docAt(p.Syntax, obj.Pos())
	if !ok {
		return "", fmt.Errorf("no declaration of %s was found in its package's files", symbol)
	}
	return doc, nil
}

// docAt returns the doc comment of the declaration whose name is at pos: a
// function or method, a type, a constant or variable, or a struct field. A
// type, constant or variable with no comment of its own takes its
// declaration's, the one over a whole group.
func docAt(files []*ast.File, pos token.Pos) (string, bool) {
	var doc *ast.CommentGroup
	found := false
	at := func(id *ast.Ident, comments ...*ast.CommentGroup) {
		if id.Pos() == pos && !found {
			found = true
			doc = cmp.Or(comments...)
		}
	}
	for _, f := range files {
		ast.Inspect(f, func(n ast.Node) bool {
			switch d := n.(type) {
			case *ast.FuncDecl:
				at(d.Name, d.Doc)
			case *ast.GenDecl:
				for _, spec := range d.Specs {
					specDoc(spec, d, at)
				}
			case *ast.Field:
				for _, id := range d.Names {
					at(id, d.Doc)
				}
			}
			return !found
		})
	}
	return doc.Text(), found
}

// specDoc offers at each name spec declares, with the comments that may
// document it, its own first.
func specDoc(spec ast.Spec, decl *ast.GenDecl, at func(*ast.Ident, ...*ast.CommentGroup)) {
	switch s := spec.(type) {
	case *ast.TypeSpec:
		at(s.Name, s.Doc, decl.Doc)
	case *ast.ValueSpec:
		for _, id := range s.Names {
			at(id, s.Doc, decl.Doc)
		}
	}
}

// TestRecord_CitationsNameAnEntry requires every citation of the record, in a
// comment of a Go or YAML file or anywhere in a Markdown one, and every link
// the record makes to itself, to name one of its entries.
func TestRecord_CitationsNameAnEntry(t *testing.T) {
	t.Parallel()
	keys := map[string]bool{}
	for _, e := range readRecord(t) {
		keys[e.key] = true
	}
	problems, cited, err := citationProblems(repoRoot, keys)
	require.NoError(t, err)
	require.NotZero(t, cited, "found no citation of the record, so this proves nothing")
	for _, problem := range problems {
		t.Error(problem)
	}
}

// citationProblems reports each citation under root of a key keys lacks, and
// counts the citations it read.
func citationProblems(root string, keys map[string]bool) ([]string, int, error) {
	var problems []string
	count := 0
	record := filepath.Join(root, recordPath)
	err := walkFiles(root, ".", func(path string) error {
		found, err := citations(path, path == record)
		if err != nil {
			return err
		}
		for _, c := range found {
			count++
			if !keys[c.text] {
				problems = append(problems, fmt.Sprintf("%s:%d cites %q, which is no entry of %s",
					rel(root, path), c.line, c.text, recordPath))
			}
		}
		return nil
	})
	return problems, count, err
}

// citations returns the key each citation in the file at path names, and its
// line: in a comment of a Go or YAML file, anywhere in a Markdown one, and for
// the record itself in each link it makes to its own entries too.
func citations(path string, record bool) ([]comment, error) {
	texts, err := commentsOf(path)
	if filepath.Ext(path) == ".md" {
		texts, err = lines(path)
	}
	patterns := []*regexp.Regexp{citation}
	if record {
		patterns = append(patterns, anchorLink)
	}
	var out []comment
	for _, c := range texts {
		for _, p := range patterns {
			for _, m := range p.FindAllStringSubmatch(c.text, -1) {
				out = append(out, comment{text: m[1], line: c.line})
			}
		}
	}
	return out, err
}

// walkFiles calls visit with each file under root/tree, leaving out every
// directory below it whose name starts with a dot.
func walkFiles(root, tree string, visit func(path string) error) error {
	start := filepath.Join(root, tree)
	return filepath.WalkDir(start, func(path string, d fs.DirEntry, err error) error {
		switch {
		case err != nil:
			return err
		case d.IsDir() && path != start && strings.HasPrefix(d.Name(), "."):
			return fs.SkipDir
		case d.IsDir():
			return nil
		}
		return visit(path)
	})
}

// rel returns path relative to root, for a message.
func rel(root, path string) string {
	if r, err := filepath.Rel(root, path); err == nil {
		return filepath.ToSlash(r)
	}
	return path
}

// versionedTrees are the trees whose comments may not name a version of the
// parser library: the compiler, the harness that drives it, and their
// fixtures.
var versionedTrees = []string{"compilers", "internal/harness", "testdata"}

// TestRecord_NoCommentNamesALibraryVersion requires no comment in the
// compiler's Go or YAML to name a version: a behaviour of the library is
// recorded once, in an entry that states the version it was verified at, and a
// comment that names one instead goes stale unseen when the library moves.
func TestRecord_NoCommentNamesALibraryVersion(t *testing.T) {
	t.Parallel()
	var read int
	for _, tree := range versionedTrees {
		problems, n, err := versionProblems(repoRoot, tree)
		require.NoError(t, err)
		read += n
		for _, problem := range problems {
			t.Errorf("%s: a comment here cites a behaviour of the parser library by its entry in %s, "+
				"never by a version", problem, recordPath)
		}
	}
	require.NotZero(t, read, "read no comment, so this proves nothing")
}

// versionProblems reports each comment naming a version in the Go and YAML
// files under root/tree, and counts the comments it read.
func versionProblems(root, tree string) ([]string, int, error) {
	var problems []string
	read := 0
	err := walkFiles(root, tree, func(path string) error {
		comments, err := commentsOf(path)
		if err != nil {
			return err
		}
		read += len(comments)
		for _, c := range comments {
			if v := version.FindString(c.text); v != "" {
				problems = append(problems, fmt.Sprintf("%s:%d names %s", rel(root, path), c.line, v))
			}
		}
		return nil
	})
	return problems, read, err
}

// commentsOf returns the comments of the Go or YAML file at path, and none for
// any other file.
func commentsOf(path string) ([]comment, error) {
	switch filepath.Ext(path) {
	case ".go":
		return goComments(path)
	case ".yaml", ".yml":
		return yamlComments(path)
	}
	return nil, nil
}

// comment is a comment's text and the line it starts on.
type comment struct {
	text string
	line int
}

// goComments returns every comment in the Go file at path.
func goComments(path string) ([]comment, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, nil, parser.ParseComments|parser.SkipObjectResolution)
	if err != nil {
		return nil, err
	}
	out := make([]comment, 0, len(f.Comments))
	for _, g := range f.Comments {
		out = append(out, comment{text: g.Text(), line: fset.Position(g.Pos()).Line})
	}
	return out, nil
}

// lines returns each line of the file at path, as a comment of its own.
func lines(path string) ([]comment, error) {
	text, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	split := strings.Split(string(text), "\n")
	out := make([]comment, 0, len(split))
	for i, line := range split {
		out = append(out, comment{text: line, line: i + 1})
	}
	return out, nil
}

// yamlComments returns every comment in the YAML file at path, a line each.
// It reads them by line rather than by parsing, since a fixture may be YAML
// the parser refuses.
func yamlComments(path string) ([]comment, error) {
	text, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var out []comment
	for i, line := range strings.Split(string(text), "\n") {
		if c, ok := yamlComment(line); ok {
			out = append(out, comment{text: c, line: i + 1})
		}
	}
	return out, nil
}

// yamlComment returns the comment line ends with: from a '#' that opens the
// line or follows a space, outside quotes.
func yamlComment(line string) (string, bool) {
	var quote rune
	for i, r := range line {
		switch {
		case quote != 0:
			if r == quote {
				quote = 0
			}
		case r == '\'' || r == '"':
			quote = r
		case r == '#' && (i == 0 || line[i-1] == ' ' || line[i-1] == '\t'):
			return line[i+1:], true
		}
	}
	return "", false
}
