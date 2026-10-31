// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package archtest_test

import (
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"unicode"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/internal/keydoc"
	"github.com/dexpace/morphic/ir/irtest"
)

// citationTest is the test of the citations, whose strings plant broken ones.
const citationTest = "internal/archtest/doclinks_test.go"

var (
	// markdownLink is an inline link, its target holding no space. It is the one
	// form the documents write but for the root README's raw HTML, which stays
	// unread.
	markdownLink = regexp.MustCompile(`\]\(([^)\s]+)\)`)
	// uriScheme opens a target outside the repository, such as https: or
	// mailto:.
	uriScheme = regexp.MustCompile(`^[a-z][a-z0-9+.-]*:`)
	// docPath is a document named from the repository's root, as a comment or a
	// Go sketch cites one, and the heading it names if any.
	docPath = regexp.MustCompile(`docs/[A-Za-z0-9_./-]*[A-Za-z0-9_-]\.md(?:#[A-Za-z0-9_-]+)?`)
	// designPath is docPath for a Go string, read under the design trees alone:
	// a test elsewhere plants documents that exist only in it.
	designPath = regexp.MustCompile(`docs/(?:ir|emitters)/[A-Za-z0-9_./-]*[A-Za-z0-9_-]\.md(?:#[A-Za-z0-9_-]+)?`)
	// codeSpan is an inline span of code, whose text is no Markdown.
	codeSpan = regexp.MustCompile("`[^`]*`")
	// fenceRun is the run of backticks or tildes opening or closing a fenced
	// block, which may be indented under a list item.
	fenceRun = regexp.MustCompile("^\\s*(`{3,}|~{3,})")
	// atxHeading is a heading, with its text.
	atxHeading = regexp.MustCompile(`^#{1,6}\s+(.+?)\s*$`)
	// inlineLink is a link inside a heading, which renders as its text.
	inlineLink = regexp.MustCompile(`\[([^\]]*)\]\([^)]*\)`)
)

// TestDocuments_LinksResolve reads every Markdown document of the repository
// and fails on a relative link to no file, or to a heading its file lacks, and
// holds a citation to the same in a Go sketch, a Go or YAML comment, and a Go
// string naming a design document. The design documents are cited by path and
// heading across a tree, so a heading renamed or a document moved without what
// cites it sends the reader nowhere. Whether a heading still states what its
// citer claims is left to a reader: a rule moved to another heading passes.
func TestDocuments_LinksResolve(t *testing.T) {
	t.Parallel()
	problems, links, err := linkProblems(irtest.RepoRoot(t))
	require.NoError(t, err)
	require.NotZero(t, links, "found no link in a document, so this proves nothing")
	for _, p := range problems {
		t.Error(p)
	}
}

// linkProblems reports each broken link and document citation under root, and
// counts the links it read. Under testdata it reads a YAML comment alone, since
// a conformance spec cites the rule it witnesses.
func linkProblems(root string) ([]string, int, error) {
	var problems []string
	links := 0
	err := keydoc.Walk(root, ".", func(path string) error {
		rel := keydoc.Rel(root, path)
		var found []string
		var err error
		switch ext := filepath.Ext(path); {
		case ext == ".yaml", ext == ".yml":
			found, err = commentCitationProblems(root, path, rel)
		case slices.Contains(strings.Split(rel, "/"), "testdata"):
		case ext == ".md":
			var n int
			found, n, err = documentLinkProblems(root, path, rel)
			links += n
		case ext == ".go":
			found, err = goCitationProblems(root, path, rel)
		}
		problems = append(problems, found...)
		return err
	})
	return problems, links, err
}

// documentLinkProblems reports each relative link of the Markdown file at path
// that resolves to nothing, and each document a fenced line cites, where a link
// does not render, and counts the relative links it read.
func documentLinkProblems(root, path, rel string) ([]string, int, error) {
	text, err := os.ReadFile(path)
	if err != nil {
		return nil, 0, err
	}
	var problems []string
	links := 0
	prose := proseLines(string(text))
	raw := strings.Split(string(text), "\n")
	for i, line := range linkLines(string(text)) {
		if prose[i] == "" {
			found, err := citationProblems(root, rel, i+1, raw[i], docPath)
			if err != nil {
				return nil, 0, err
			}
			problems = append(problems, found...)
		}
		for _, m := range markdownLink.FindAllStringSubmatch(line, -1) {
			if uriScheme.MatchString(m[1]) {
				continue
			}
			links++
			why, broken, err := brokenLink(path, m[1])
			if err != nil {
				return nil, 0, err
			}
			if broken {
				problems = append(problems, fmt.Sprintf("%s:%d links %q, %s", rel, i+1, m[1], why))
			}
		}
	}
	return problems, links, nil
}

// brokenLink says whether target, a relative link from the document at doc,
// resolves to nothing, and why. A fragment is held to the headings of a Markdown
// target only, since another file has none.
func brokenLink(doc, target string) (why string, broken bool, err error) {
	file, fragment, _ := strings.Cut(target, "#")
	resolved := doc
	if file != "" {
		resolved = filepath.Join(filepath.Dir(doc), file)
	}
	_, err = os.Stat(resolved)
	if errors.Is(err, fs.ErrNotExist) {
		return "which names no file", true, nil
	}
	if err != nil {
		return "", false, err
	}
	if fragment == "" || filepath.Ext(resolved) != ".md" {
		return "", false, nil
	}
	text, err := os.ReadFile(resolved)
	if err != nil {
		return "", false, err
	}
	if !headingSlugs(string(text))[fragment] {
		return "whose file has no such heading", true, nil
	}
	return "", false, nil
}

// goCitationProblems reports each broken citation in a comment of the Go file
// at path, and in a string naming a design document. A test of the citations
// plants broken ones in its strings, so its own are left unread.
func goCitationProblems(root, path, rel string) ([]string, error) {
	problems, err := commentCitationProblems(root, path, rel)
	if err != nil || rel == citationTest {
		return problems, err
	}
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
	if err != nil {
		return nil, err
	}
	var walkErr error
	ast.Inspect(file, func(n ast.Node) bool {
		lit, ok := n.(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING || walkErr != nil {
			return walkErr == nil
		}
		found, err := citationProblems(root, rel, fset.Position(lit.Pos()).Line, lit.Value, designPath)
		problems, walkErr = append(problems, found...), err
		return false
	})
	return problems, walkErr
}

// commentCitationProblems reports each broken citation in a comment of the Go
// or YAML file at path.
func commentCitationProblems(root, path, rel string) ([]string, error) {
	comments, err := keydoc.Comments(path)
	if err != nil {
		return nil, err
	}
	var problems []string
	for _, c := range comments {
		found, err := citationProblems(root, rel, c.Line, c.Text, docPath)
		if err != nil {
			return nil, err
		}
		problems = append(problems, found...)
	}
	return problems, nil
}

// citationProblems reports each citation cite finds in text, written at line of
// the file rel, that names no document under root or no heading of it.
func citationProblems(root, rel string, line int, text string, cite *regexp.Regexp) ([]string, error) {
	var problems []string
	for _, cited := range cite.FindAllString(text, -1) {
		why, broken, err := brokenLink(filepath.Join(root, rel), filepath.Join(citedFrom(rel), cited))
		if err != nil {
			return nil, err
		}
		if broken {
			problems = append(problems, fmt.Sprintf("%s:%d cites %s, %s", rel, line, cited, why))
		}
	}
	return problems, nil
}

// citedFrom returns the path from the directory of the file rel back to the
// root, which a citation is written from.
func citedFrom(rel string) string {
	return strings.Repeat("../", strings.Count(rel, "/"))
}

// proseLines returns the lines of a Markdown document with each line of a
// fenced block left empty, so an index still numbers its line.
func proseLines(text string) []string {
	lines := strings.Split(text, "\n")
	open := ""
	for i, line := range lines {
		run := fenceRun.FindStringSubmatch(line)
		switch {
		case open == "" && run == nil:
			continue
		case open == "":
			open = run[1]
		case run != nil && strings.TrimSpace(line) == run[1] && run[1][0] == open[0] && len(run[1]) >= len(open):
			open = ""
		}
		lines[i] = ""
	}
	return lines
}

// linkLines returns the lines of a Markdown document with each fenced block and
// span of code left empty, so an index still numbers its line and what remains
// is what can hold a link. A span may cross lines but not a blank one, so ticks
// pair within a block of text and never within a line alone: a span closing on
// a line would pair its tick with the next span's and hide what lies between.
func linkLines(text string) []string {
	blocks := strings.Split(strings.Join(proseLines(text), "\n"), "\n\n")
	for i, block := range blocks {
		blocks[i] = codeSpan.ReplaceAllStringFunc(block, func(span string) string {
			return strings.Repeat("\n", strings.Count(span, "\n"))
		})
	}
	return strings.Split(strings.Join(blocks, "\n\n"), "\n")
}

// headingSlugs returns the fragment of each heading of a Markdown document. A
// heading repeating an earlier one's slug takes a numbered suffix, as it
// renders.
func headingSlugs(text string) map[string]bool {
	seen := map[string]int{}
	slugs := map[string]bool{}
	for _, line := range proseLines(text) {
		m := atxHeading.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		slug := headingSlug(m[1])
		fragment := slug
		if n := seen[slug]; n > 0 {
			fragment = fmt.Sprintf("%s-%d", slug, n)
		}
		slugs[fragment] = true
		seen[slug]++
	}
	return slugs
}

// headingSlug is the fragment a heading's text renders to on GitHub: lowercase,
// each space a hyphen, and any character but a letter, a number, a hyphen or an
// underscore dropped, a link reading as its text.
func headingSlug(title string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(inlineLink.ReplaceAllString(title, "$1")) {
		switch {
		case r == ' ':
			b.WriteRune('-')
		case r == '-', r == '_', unicode.IsLetter(r), unicode.IsNumber(r):
			b.WriteRune(r)
		}
	}
	return b.String()
}

// TestHeadingSlug_FollowsTheRendering pins the fragment of each form of heading
// the documents write, against what GitHub renders for it.
func TestHeadingSlug_FollowsTheRendering(t *testing.T) {
	t.Parallel()
	tests := []struct{ title, want string }{
		{"The size check", "the-size-check"},
		{"`openapi-versions`", "openapi-versions"},
		{"`User` in Go", "user-in-go"},
		{"`snake_case` names", "snake_case-names"},
		{"A failure's place", "a-failures-place"},
		{"ModelShape — visibility projection", "modelshape--visibility-projection"},
		{"Optionality × Nullability × Presence (INV8)", "optionality--nullability--presence-inv8"},
		{"Why a domain AST → text → `go/format`, not `go/ast` directly", "why-a-domain-ast--text--goformat-not-goast-directly"},
		{"see [the plan](plan.md) first", "see-the-plan-first"},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.want, headingSlug(tt.title), tt.title)
	}
}

// TestLinkProblems_ReportsALinkThatResolvesToNothing plants a link of each form
// beside links that resolve, in a document, a fenced block, a span of code, after
// a span crossing a line, past a stray tick a blank line ends, and under testdata,
// and citations of a document in Go comments.
func TestLinkProblems_ReportsALinkThatResolvesToNothing(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeDocFile(t, filepath.Join(root, "docs", "target.md"),
		"# Title\n\n## The rule\n\n## Twice\n\n## Twice\n\n```text\n## Fenced\n```\n")
	writeDocFile(t, filepath.Join(root, "docs", "data.txt"), "data\n")
	writeDocFile(t, filepath.Join(root, "docs", "guide.md"), strings.Join([]string{
		"[file](target.md) [heading](target.md#the-rule) [again](target.md#twice-1) [self](#guide)",
		"[gone](absent.md) [no heading](target.md#absent) [fenced heading](target.md#fenced) [no self](#absent)",
		"[web](https://example.com/absent.md) [mail](mailto:a@example.com) [data](data.txt#line)",
		"`[span](absent.md)`, [one](target.md) `x` [other](absent.md#x) `y`",
		"`a span crossing",
		"two lines` [after it](absent.md) `x` [beside it](target.md)",
		"",
		"a stray ` tick",
		"",
		"[past a blank line](absent.md) and a stray ` tick",
		"  ```",
		"[fence](absent.md)",
		"  ```",
		"~~~~",
		"```",
		"[longer fence](absent.md)",
		"~~~~",
		"```",
		"~~~",
		"[other character](absent.md)",
		"```",
		"```",
		"```go",
		"[opener inside](absent.md)",
		"```",
		"````",
		"```",
		"[shorter fence](absent.md)",
		"````",
		"# Guide",
		"",
	}, "\n"))
	writeDocFile(t, filepath.Join(root, "x", "testdata", "fixture.md"), "[unread](absent.md)\n")
	writeDocFile(t, filepath.Join(root, ".hidden", "h.md"), "[unread](absent.md)\n")
	writeDocFile(t, filepath.Join(root, "a", "a.go"), "// Package a follows docs/target.md and docs/gone.md.\n"+
		"package a\n\n/* A block cites docs/also/gone.md. */\nconst s = \"docs/string.md\"\n\n"+
		"// T follows docs/target.md#the-rule, not docs/target.md#absent.\nconst T = \"docs/ir/gone.md\"\n")
	writeDocFile(t, filepath.Join(root, "x", "testdata", "a.go"), "// Unread: docs/gone.md.\npackage a\n")
	writeDocFile(t, filepath.Join(root, "x", "testdata", "spec.yaml"),
		"# Follows docs/target.md and docs/target.md#absent.\nnote: docs/gone.md\n")
	writeDocFile(t, filepath.Join(root, "x", "testdata", "spec.yml"), "# Follows docs/target.md#absent.\n")
	writeDocFile(t, filepath.Join(root, "docs", "sketch.md"),
		"```go\ntype T struct{} // docs/target.md#the-rule, not docs/gone.md\n```\n")

	problems, links, err := linkProblems(root)
	require.NoError(t, err)
	assert.Equal(t, 14, links, "each relative link read is counted once")
	assert.Equal(t, []string{
		`a/a.go:1 cites docs/gone.md, which names no file`,
		`a/a.go:4 cites docs/also/gone.md, which names no file`,
		`a/a.go:7 cites docs/target.md#absent, whose file has no such heading`,
		`a/a.go:8 cites docs/ir/gone.md, which names no file`,
		`docs/guide.md:2 links "absent.md", which names no file`,
		`docs/guide.md:2 links "target.md#absent", whose file has no such heading`,
		`docs/guide.md:2 links "target.md#fenced", whose file has no such heading`,
		`docs/guide.md:2 links "#absent", whose file has no such heading`,
		`docs/guide.md:4 links "absent.md#x", which names no file`,
		`docs/guide.md:6 links "absent.md", which names no file`,
		`docs/guide.md:10 links "absent.md", which names no file`,
		`docs/sketch.md:2 cites docs/gone.md, which names no file`,
		`x/testdata/spec.yaml:1 cites docs/target.md#absent, whose file has no such heading`,
		`x/testdata/spec.yml:1 cites docs/target.md#absent, whose file has no such heading`,
	}, problems)

	_, _, err = linkProblems(filepath.Join(root, "absent"))
	require.ErrorIs(t, err, fs.ErrNotExist)
}
