package keydoc

import (
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Citation is one citation of an entry of a keyed document.
type Citation struct {
	// Path is the citing file, relative to the root the citations were read
	// under, with forward slashes.
	Path string
	// Line is the line the citation is written on.
	Line int
	// Key is the key the citation names, which may be no entry's.
	Key string
}

// selfLink is a link from a keyed document to one of its own entries.
var selfLink = regexp.MustCompile(`\]\(#([A-Za-z0-9_-]+)\)`)

// Citations returns every citation, under root, of the keyed document at doc,
// a slash-separated path relative to root: in a comment of a Go or YAML file,
// on any line of a Markdown one, and in each link doc makes to its own
// entries. citedAs is how a citation spells the document, such as
// "parser-library.md"; a citation is citedAs, a "#" and the key. A directory
// below root whose name starts with a dot is not read.
func Citations(root, doc, citedAs string) ([]Citation, error) {
	cite := regexp.MustCompile(regexp.QuoteMeta(citedAs) + `#([A-Za-z0-9_-]+)`)
	var out []Citation
	err := Walk(root, ".", func(path string) error {
		rel := Rel(root, path)
		texts, err := Comments(path)
		if filepath.Ext(path) == ".md" {
			texts, err = lines(path)
		}
		patterns := []*regexp.Regexp{cite}
		if rel == doc {
			patterns = append(patterns, selfLink)
		}
		for _, c := range texts {
			for _, p := range patterns {
				for _, m := range p.FindAllStringSubmatch(c.Text, -1) {
					out = append(out, Citation{Path: rel, Line: c.Line, Key: m[1]})
				}
			}
		}
		return err
	})
	return out, err
}

// Walk calls visit with each file under root/tree, leaving out every
// directory below it whose name starts with a dot.
func Walk(root, tree string, visit func(path string) error) error {
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

// Rel returns path relative to root with forward slashes, or path itself
// where it is not below root.
func Rel(root, path string) string {
	if r, err := filepath.Rel(root, path); err == nil {
		return filepath.ToSlash(r)
	}
	return path
}

// Comment is one comment of a file, or one line of a Markdown one.
type Comment struct {
	// Text is the comment as written, its markers included.
	Text string
	// Line is the line it starts on.
	Line int
}

// Comments returns the comments of the Go or YAML file at path, and none for
// any other file. A Go comment is each // line or /* */ block; a YAML one runs
// from a # outside quotes to the end of its line.
func Comments(path string) ([]Comment, error) {
	switch filepath.Ext(path) {
	case ".go":
		return goComments(path)
	case ".yaml", ".yml":
		return yamlComments(path)
	}
	return nil, nil
}

// goComments returns every comment in the Go file at path.
func goComments(path string) ([]Comment, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, nil, parser.ParseComments|parser.SkipObjectResolution)
	if err != nil {
		return nil, err
	}
	var out []Comment
	for _, g := range f.Comments {
		for _, c := range g.List {
			out = append(out, Comment{Text: c.Text, Line: fset.Position(c.Pos()).Line})
		}
	}
	return out, nil
}

// lines returns each line of the file at path, as a comment of its own.
func lines(path string) ([]Comment, error) {
	text, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	split := strings.Split(string(text), "\n")
	out := make([]Comment, 0, len(split))
	for i, line := range split {
		out = append(out, Comment{Text: line, Line: i + 1})
	}
	return out, nil
}

// yamlComments returns every comment in the YAML file at path, a line each.
// It reads them by line rather than by parsing, since a fixture may be YAML
// the parser refuses.
func yamlComments(path string) ([]Comment, error) {
	text, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var out []Comment
	for i, line := range strings.Split(string(text), "\n") {
		if c, ok := yamlComment(line); ok {
			out = append(out, Comment{Text: c, Line: i + 1})
		}
	}
	return out, nil
}

// yamlComment returns the comment line ends with: from a '#' that opens the
// line or follows a space, outside quotes. A quote opens a quoted scalar only
// where a scalar starts, so an apostrophe inside a plain one opens nothing,
// and a backslash in a double-quoted one escapes the byte after it.
func yamlComment(line string) (string, bool) {
	var quote byte
	for i := 0; i < len(line); i++ {
		c := line[i]
		switch {
		case quote == '"' && c == '\\':
			i++
		case quote != 0:
			if c == quote {
				quote = 0
			}
		case (c == '\'' || c == '"') && (i == 0 || strings.IndexByte(" \t[{,", line[i-1]) >= 0):
			quote = c
		case c == '#' && (i == 0 || line[i-1] == ' ' || line[i-1] == '\t'):
			return line[i+1:], true
		}
	}
	return "", false
}
