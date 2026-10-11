package archtest_test

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"unicode"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// maxDocCommentWords caps the words in one doc comment, as CLAUDE.md's Go code
// style requires. Words are counted by docWords.
const maxDocCommentWords = 100

// longDocComments excuses the doc comments allowed past maxDocCommentWords,
// keyed "<file>:<name>", the file and name a violation prints. Each entry
// records the most words its comment may reach and why the excess is necessary
// and valuable. An entry that excuses nothing is itself a violation, so the
// list cannot outlive the comments it names.
var longDocComments = map[string]longDoc{}

// longDoc is one longDocComments entry.
type longDoc struct {
	words int    // the most words the comment may reach
	why   string // why the excess is necessary and valuable
}

// TestDocComments_StayUnderTheCap measures every doc comment in the module,
// tests included, and reports each one past maxDocCommentWords that
// longDocComments does not excuse.
func TestDocComments_StayUnderTheCap(t *testing.T) {
	t.Parallel()
	docs, err := measureDocComments(repoRoot(t))
	require.NoError(t, err)
	require.NotEmpty(t, docs, "the sweep measured no doc comments, so an empty result proves nothing")
	for _, v := range docCommentViolations(docs, longDocComments) {
		t.Error(v)
	}
}

// TestMeasureDocComments_ReachesEveryDocPosition plants a doc comment at every
// position Go attaches one to, beside comments that document nothing, and
// checks the walk measures exactly the former. The live tree proves only that
// the positions it happens to use are reached.
func TestMeasureDocComments_ReachesEveryDocPosition(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	long := docText(maxDocCommentWords + 1)
	src := strings.NewReplacer(
		"// LONG", long,
		"// CAP", docText(maxDocCommentWords)+"\n// 140 #74 0.4.0 — -> *",
		"// TRAILING", "// "+strings.Repeat("w ", maxDocCommentWords+1),
	).Replace(plantedDocs)
	writeDocFile(t, filepath.Join(root, "a", "a.go"), src)
	writeDocFile(t, filepath.Join(root, "a", "a_test.go"), long+"\npackage a\n\n"+long+"\nfunc TestX() {}\n")
	for _, skipped := range []string{"testdata", ".hidden", "_skipped"} {
		writeDocFile(t, filepath.Join(root, "a", skipped, "s.go"), long+"\npackage s\n")
	}

	docs, err := measureDocComments(root)
	require.NoError(t, err)

	got := map[string]int{}
	for _, d := range docs {
		got[d.key()] = d.words
	}
	over := maxDocCommentWords + 1
	want := map[string]int{
		"a/a.go:package a":      over,
		"a/a.go:F":              over,
		"a/a.go:T.M":            over,
		"a/a.go:T":              over,
		"a/a.go:T.Field":        over,
		"a/a.go:T.Embedded":     over,
		"a/a.go:I":              over,
		"a/a.go:I.Method":       over,
		"a/a.go:C":              over,
		"a/a.go:var (V1, ...)":  over,
		"a/a.go:V1":             over,
		"a/a.go:table.name":     over,
		"a/a.go:G":              over,
		"a/a.go:AtCap":          maxDocCommentWords,
		"a/a_test.go:package a": over,
		"a/a_test.go:TestX":     over,
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("measured doc comments (-want +got):\n%s", diff)
	}
}

// TestDocCommentViolations_ExceptionsAreExact checks the cap's boundary and
// every way an exception can be wrong: a comment past its entry's ceiling, an
// entry without a reason, and entries that excuse nothing because their comment
// shrank or is gone.
func TestDocCommentViolations_ExceptionsAreExact(t *testing.T) {
	t.Parallel()
	limit := maxDocCommentWords
	docs := []measuredDoc{
		{file: "x.go", name: "AtCap", line: 1, words: limit},
		{file: "x.go", name: "Over", line: 2, words: limit + 1},
		{file: "x.go", name: "Excused", line: 3, words: limit + 9},
		{file: "x.go", name: "PastCeiling", line: 4, words: limit + 10},
		{file: "x.go", name: "Unreasoned", line: 5, words: limit + 1},
		{file: "x.go", name: "Shrunk", line: 6, words: limit},
	}
	allowed := map[string]longDoc{
		"x.go:Excused":     {words: limit + 9, why: "needed"},
		"x.go:PastCeiling": {words: limit + 9, why: "needed"},
		"x.go:Unreasoned":  {words: limit + 1},
		"x.go:Shrunk":      {words: limit + 9, why: "needed"},
		"x.go:Gone":        {words: limit + 9, why: "needed"},
	}

	want := []string{
		fmt.Sprintf("x.go:2: Over: doc comment is %d words; the cap is %d", limit+1, limit),
		fmt.Sprintf("x.go:4: PastCeiling: doc comment is %d words; its longDocComments entry allows %d",
			limit+10, limit+9),
		`longDocComments["x.go:Gone"]: excuses nothing; no doc comment by that name is over the cap`,
		`longDocComments["x.go:Shrunk"]: excuses nothing; no doc comment by that name is over the cap`,
		`longDocComments["x.go:Unreasoned"]: an exception needs its reason`,
	}
	if diff := cmp.Diff(want, docCommentViolations(docs, allowed)); diff != "" {
		t.Errorf("violations (-want +got):\n%s", diff)
	}
}

// plantedDocs puts a doc comment at every position Go attaches one to, and
// comments where they document nothing: after a field, on a declaration inside
// a function body, and inside a function literal at top level.
const plantedDocs = `// LONG
package a

// LONG
func F() {}

// LONG
func (*T) M() {}

// LONG
type T struct {
	// LONG
	Field int
	// LONG
	Embedded
	Trailing int // TRAILING
}

// LONG
type I interface {
	// LONG
	Method()
}

// LONG
const C = 1

// LONG
var (
	// LONG
	V1 = 1
	V2 = 2
)

var table = []struct {
	// LONG
	name string
}{}

var hook = func() {
	type local struct {
		// LONG
		f int
	}
	_ = local{}
}

type (
	// LONG
	G int
)

func body() {
	// LONG
	type local struct{}
	_ = local{}
}

// CAP
//
//go:noinline
func AtCap() {}
`

// docText returns a doc comment of n words split over two lines, so a count
// that reads only one line comes up short.
func docText(n int) string {
	half := n / 2
	return "// " + strings.TrimSpace(strings.Repeat("w ", half)) +
		"\n// " + strings.TrimSpace(strings.Repeat("w ", n-half))
}

// writeDocFile plants src at file.
func writeDocFile(t *testing.T, file, src string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(file), 0o755))
	require.NoError(t, os.WriteFile(file, []byte(src), 0o600))
}

// measuredDoc is one doc comment's length and position.
type measuredDoc struct {
	file  string // slash-separated, relative to the walked root
	name  string // what the comment documents
	line  int
	words int
}

// key names the comment the way longDocComments does: by what it documents
// rather than by line, so an entry survives edits above it.
func (d measuredDoc) key() string { return d.file + ":" + d.name }

// TestDocWords_CountsOnlyTokensWithALetter pins what a word is: an identifier,
// a path or a hyphenated keyword is one, while a number, an issue reference, a
// dash or a list marker is not.
func TestDocWords_CountsOnlyTokensWithALetter(t *testing.T) {
	t.Parallel()
	tests := []struct {
		text string
		want int
	}{
		{text: "", want: 0},
		{text: "Detect reports the dialect.", want: 4},
		{text: "json/v2 x-sunset Document.IRVersion", want: 3},
		{text: "140 #74 0.4.0 — -> * (1) 3.1.0", want: 0},
		{text: "  - see #74 — twice\n\n\tcode(x)", want: 3},
	}
	for _, tc := range tests {
		assert.Equal(t, tc.want, docWords(tc.text), "%q", tc.text)
	}
}

// docWords counts the words in a doc comment's rendered text: tokens with a
// letter in them. Rendering has already dropped the comment markers and
// directives; numbers, punctuation and list markers are not words either.
func docWords(text string) int {
	n := 0
	for _, tok := range strings.Fields(text) {
		if strings.IndexFunc(tok, unicode.IsLetter) >= 0 {
			n++
		}
	}
	return n
}

// measureDocComments walks root for every Go file the go tool would build,
// tests included, and measures each doc comment in it.
func measureDocComments(root string) ([]measuredDoc, error) {
	var docs []measuredDoc
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if p != root && ignoredByGoTool(d.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if filepath.Ext(p) != ".go" {
			return nil
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, p, nil, parser.ParseComments|parser.SkipObjectResolution)
		if err != nil {
			return err
		}
		for _, c := range docComments(f) {
			docs = append(docs, measuredDoc{
				file:  filepath.ToSlash(rel),
				name:  c.name,
				line:  fset.Position(c.doc.Pos()).Line,
				words: docWords(c.doc.Text()),
			})
		}
		return nil
	})
	return docs, err
}

// docCommentViolations reports each doc comment past maxDocCommentWords that
// allowed does not excuse, then each allowed entry that gives no reason or that
// no comment over the cap still needs.
func docCommentViolations(docs []measuredDoc, allowed map[string]longDoc) []string {
	var out []string
	needed := map[string]bool{}
	for _, d := range docs {
		if d.words <= maxDocCommentWords {
			continue
		}
		entry, excused := allowed[d.key()]
		needed[d.key()] = excused
		switch {
		case !excused:
			out = append(out, fmt.Sprintf("%s:%d: %s: doc comment is %d words; the cap is %d",
				d.file, d.line, d.name, d.words, maxDocCommentWords))
		case d.words > entry.words:
			out = append(out, fmt.Sprintf("%s:%d: %s: doc comment is %d words; its longDocComments entry allows %d",
				d.file, d.line, d.name, d.words, entry.words))
		}
	}
	for _, key := range slices.Sorted(maps.Keys(allowed)) {
		if allowed[key].why == "" {
			out = append(out, fmt.Sprintf("longDocComments[%q]: an exception needs its reason", key))
		}
		if !needed[key] {
			out = append(out, fmt.Sprintf(
				"longDocComments[%q]: excuses nothing; no doc comment by that name is over the cap", key))
		}
	}
	return out
}

// docComment is a doc comment and the name of what it documents.
type docComment struct {
	name string
	doc  *ast.CommentGroup
}

// docComments returns every doc comment in f: the package clause's, each
// top-level declaration's and spec's, and each field's or method's inside them.
// It never enters a function body, where a comment documents no API.
func docComments(f *ast.File) []docComment {
	out := []docComment{{name: "package " + f.Name.Name, doc: f.Doc}}
	for _, decl := range f.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			out = append(out, docComment{name: funcName(d), doc: d.Doc})
		case *ast.GenDecl:
			out = append(out, genDeclComments(d)...)
		default: // *ast.BadDecl: the parse already failed
		}
	}
	return slices.DeleteFunc(out, func(c docComment) bool { return c.doc == nil })
}

// funcName names a function, or a method by its receiver's type.
func funcName(fn *ast.FuncDecl) string {
	if recv, ok := receiverType(fn); ok {
		return recv + "." + fn.Name.Name
	}
	return fn.Name.Name
}

// genDeclComments returns a type, const, var or import declaration's doc
// comments. A grouped declaration's own comment is named for the group, since
// it documents the block rather than its first spec.
func genDeclComments(d *ast.GenDecl) []docComment {
	if len(d.Specs) == 0 {
		return []docComment{{name: d.Tok.String() + " ()", doc: d.Doc}}
	}
	name := specName(d.Specs[0])
	if d.Lparen.IsValid() {
		name = d.Tok.String() + " (" + name + ", ...)"
	}
	out := []docComment{{name: name, doc: d.Doc}}
	for _, spec := range d.Specs {
		out = append(out, specComments(spec)...)
	}
	return out
}

// specName names what a spec declares.
func specName(spec ast.Spec) string {
	switch s := spec.(type) {
	case *ast.TypeSpec:
		return s.Name.Name
	case *ast.ValueSpec:
		return s.Names[0].Name
	case *ast.ImportSpec:
		return "import " + s.Path.Value
	default:
		return fmt.Sprintf("%T", spec)
	}
}

// specComments returns a spec's own doc comment and those of the fields and
// methods its types declare, each named after the spec. A function literal is
// a body, so its comments are skipped like any other function's.
func specComments(spec ast.Spec) []docComment {
	name := specName(spec)
	var own *ast.CommentGroup
	var nodes []ast.Node
	switch s := spec.(type) {
	case *ast.TypeSpec:
		own, nodes = s.Doc, []ast.Node{s.Type}
	case *ast.ValueSpec:
		own = s.Doc
		if s.Type != nil {
			nodes = append(nodes, s.Type)
		}
		for _, v := range s.Values {
			nodes = append(nodes, v)
		}
	case *ast.ImportSpec:
		own = s.Doc
	default:
	}
	out := []docComment{{name: name, doc: own}}
	for _, n := range nodes {
		ast.Inspect(n, func(n ast.Node) bool {
			switch x := n.(type) {
			case *ast.FuncLit:
				return false
			case *ast.Field:
				out = append(out, docComment{name: name + "." + fieldName(x), doc: x.Doc})
			default:
			}
			return true
		})
	}
	return out
}

// fieldName names a field or method, or an embedded field by its type.
func fieldName(f *ast.Field) string {
	if len(f.Names) > 0 {
		return f.Names[0].Name
	}
	return types.ExprString(f.Type)
}
