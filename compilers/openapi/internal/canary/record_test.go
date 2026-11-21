// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package canary_test

import (
	"cmp"
	"errors"
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
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
	"github.com/dexpace/morphic/internal/keydoc"
)

// repoRoot is the repository root, from this package's directory.
const repoRoot = "../../../.."

// recordPath is the page this package pins, relative to repoRoot.
const recordPath = "docs/parser-library.md"

// citedAs is how a citation spells the record, before the "#" and the key.
const citedAs = "parser-library.md"

// libraryModule is the parser library's module path.
const libraryModule = "github.com/speakeasy-api/openapi"

// module is this repository's module path.
const module = "github.com/dexpace/morphic"

// pinPrefix opens the name of every entry's pin: the subtest of
// TestLibraryBehaviour named for its key.
const pinPrefix = "TestLibraryBehaviour/"

// The fields an entry writes, each a bullet of its own, and the bullet that
// opens each item of the Code list below its field.
const (
	verifiedField = "Verified at"
	pinnedField   = "Pinned by"
	codeField     = "Code"
	codeItem      = "  - "
)

var (
	// citation is a reference to an entry of the record, anywhere it is written.
	citation = regexp.MustCompile(regexp.QuoteMeta(citedAs) + `#([A-Za-z0-9_-]+)`)
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
// with their shape.
func readRecord(t *testing.T) []entry {
	t.Helper()
	text, err := os.ReadFile(filepath.Join(repoRoot, recordPath))
	require.NoError(t, err)
	entries, problems := parseRecord(string(text))
	require.Empty(t, problems, "%s is not in the shape its readers expect", recordPath)
	require.NotEmpty(t, entries, "the record holds no entry, so nothing below checks anything")
	return entries
}

// parseRecord reads the entries of a record through keydoc, and reports each
// problem with their shape: keydoc's, and an entry whose fields are missing,
// or whose Code field lists something that is no item naming a declaration.
func parseRecord(text string) ([]entry, []string) {
	read, problems := keydoc.Parse(text, verifiedField, pinnedField, codeField)
	entries := make([]entry, 0, len(read))
	for _, keydocEntry := range read {
		got := entry{key: keydocEntry.Key, line: keydocEntry.Line}
		if field, ok := keydocEntry.Field(verifiedField); ok {
			got.verified = field.Text()
		}
		if field, ok := keydocEntry.Field(pinnedField); ok {
			got.pinnedBy = firstSpan(field.Text())
		}
		if field, ok := keydocEntry.Field(codeField); ok {
			var bad []string
			got.code, bad = codeItems(keydocEntry.Key, field)
			problems = append(problems, bad...)
		}
		problems = append(problems, fieldProblems(keydocEntry, got)...)
		entries = append(entries, got)
	}
	return entries, problems
}

// codeItems returns the declaration each item of a Code field names, and a
// problem for each line of the field that neither opens an item naming one
// nor continues one.
func codeItems(key string, field keydoc.Field) ([]string, []string) {
	code := []string{}
	var problems []string
	if field.Value != "" {
		problems = append(problems, fmt.Sprintf("line %d: %s writes its Code items on the bullet's own line", field.Line, key))
	}
	for i, line := range field.Lines {
		switch {
		case strings.HasPrefix(line, codeItem) && firstSpan(line) != "":
			code = append(code, firstSpan(line))
		case strings.HasPrefix(line, codeItem):
			problems = append(problems, fmt.Sprintf("line %d: a Code item of %s names no declaration", field.Line+i+1, key))
		case !strings.HasPrefix(line, "    ") || i == 0:
			problems = append(problems, fmt.Sprintf("line %d: %s writes a Code line that is no item: %q", field.Line+i+1, key, line))
		}
	}
	return code, problems
}

// firstSpan returns the text of the first span of code s writes, or "".
func firstSpan(s string) string {
	if spans := keydoc.Spans(s); len(spans) > 0 {
		return spans[0]
	}
	return ""
}

// fieldProblems reports each field keydocEntry leaves unwritten, a version or
// pin that is written naming nothing, and a Code field that names no
// declaration.
func fieldProblems(keydocEntry keydoc.Entry, got entry) []string {
	var problems []string
	for _, field := range []string{verifiedField, pinnedField, codeField} {
		if _, ok := keydocEntry.Field(field); !ok {
			problems = append(problems, fmt.Sprintf("line %d: %s does not write the field %q", keydocEntry.Line, keydocEntry.Key, field))
		}
	}
	for _, w := range []struct{ field, value string }{{verifiedField, got.verified}, {pinnedField, got.pinnedBy}} {
		if field, ok := keydocEntry.Field(w.field); ok && w.value == "" {
			problems = append(problems, fmt.Sprintf("line %d: %s writes the field %q naming nothing", field.Line, keydocEntry.Key, w.field))
		}
	}
	if _, ok := keydocEntry.Field(codeField); ok && len(got.code) == 0 {
		problems = append(problems, fmt.Sprintf("line %d: %s names no declaration under %q", keydocEntry.Line, keydocEntry.Key, codeField))
	}
	return problems
}

// TestRecord_EveryEntryHasACanaryAndNoMore holds the record's keys to the
// canaries': an entry with no pin records a behaviour nothing checks, and a
// pin with no entry checks a behaviour nothing records.
func TestRecord_EveryEntryHasACanaryAndNoMore(t *testing.T) {
	t.Parallel()
	entries := readRecord(t)
	keys := make([]string, 0, len(entries))
	for _, entry := range entries {
		keys = append(keys, entry.key)
	}
	slices.Sort(keys)
	assert.Equal(t, slices.Sorted(maps.Keys(canaries())), keys)
}

// TestRecord_EachEntryNamesItsCanary requires each entry's pin to be the
// subtest named for its key, the one TestLibraryBehaviour runs.
func TestRecord_EachEntryNamesItsCanary(t *testing.T) {
	t.Parallel()
	for _, entry := range readRecord(t) {
		assert.Equal(t, pinPrefix+entry.key, entry.pinnedBy, "line %d", entry.line)
	}
}

// TestRecord_EntriesAreVerifiedAtTheLinkedVersion requires every entry to
// have been verified at the library version this binary links, so a bump
// fails here until every entry has been read again.
func TestRecord_EntriesAreVerifiedAtTheLinkedVersion(t *testing.T) {
	t.Parallel()
	linked, err := linkedVersion()
	require.NoError(t, err)
	for _, entry := range readRecord(t) {
		assert.Equal(t, linked, entry.verified,
			"line %d: %s was verified at another version; re-read it against %s, then restamp it",
			entry.line, entry.key, linked)
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
	for _, entry := range entries {
		for _, symbol := range entry.code {
			paths[importPath(strings.Split(symbol, ".")[0])] = true
		}
	}
	pkgs, err := decls.LoadEach(slices.Sorted(maps.Keys(paths))...)
	require.NoError(t, err)
	byPath := map[string]*decls.Package{}
	for _, loadedPackage := range pkgs {
		byPath[loadedPackage.Types.Path()] = loadedPackage
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
	for _, entry := range entries {
		for _, symbol := range entry.code {
			doc, err := docOf(symbol)
			switch {
			case err != nil:
				problems = append(problems, fmt.Sprintf("%s names %s: %v", entry.key, symbol, err))
			case !citesEntry(doc, entry.key):
				problems = append(problems, fmt.Sprintf("%s names %s, whose doc comment does not cite %s#%s",
					entry.key, symbol, recordPath, entry.key))
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
	loadedPackage, ok := pkgs[importPath(parts[0])]
	if !ok {
		return "", fmt.Errorf("no package %s was loaded", importPath(parts[0]))
	}
	obj := loadedPackage.Types.Scope().Lookup(parts[1])
	if obj == nil {
		return "", fmt.Errorf("%s declares no %s", loadedPackage.Types.Path(), parts[1])
	}
	if len(parts) == 3 {
		member, _, _ := types.LookupFieldOrMethod(types.NewPointer(obj.Type()), true, loadedPackage.Types, parts[2])
		if member == nil {
			return "", fmt.Errorf("%s.%s has no field or method %s", loadedPackage.Types.Path(), parts[1], parts[2])
		}
		obj = member
	}
	doc, ok := docAt(loadedPackage.Syntax, obj.Pos())
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
	for _, entry := range readRecord(t) {
		keys[entry.key] = true
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
	found, err := keydoc.Citations(root, recordPath, citedAs)
	var problems []string
	for _, citation := range found {
		if !keys[citation.Key] {
			problems = append(problems, fmt.Sprintf("%s:%d cites %q, which is no entry of %s",
				citation.Path, citation.Line, citation.Key, recordPath))
		}
	}
	return problems, len(found), err
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
	err := keydoc.Walk(root, tree, func(path string) error {
		comments, err := keydoc.Comments(path)
		if err != nil {
			return err
		}
		read += len(comments)
		for _, comment := range comments {
			if v := version.FindString(comment.Text); v != "" {
				problems = append(problems, fmt.Sprintf("%s:%d names %s", keydoc.Rel(root, path), comment.Line, v))
			}
		}
		return nil
	})
	return problems, read, err
}
