// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package archtest_test

import (
	"errors"
	"fmt"
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

var (
	// markdownLink is an inline link, its target holding no space.
	markdownLink = regexp.MustCompile(`\]\(([^)\s]+)\)`)
	// uriScheme opens a target outside the repository, such as https: or
	// mailto:.
	uriScheme = regexp.MustCompile(`^[a-z][a-z0-9+.-]*:`)
	// docPath is a document named from the repository's root, as a Go comment
	// cites one.
	docPath = regexp.MustCompile(`docs/[A-Za-z0-9_./-]*[A-Za-z0-9_-]\.md`)
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
// on a Go comment naming a document that does not exist. The design documents
// cite each other by link across a tree, so a heading renamed or a document
// moved without them sends the reader nowhere.
func TestDocuments_LinksResolve(t *testing.T) {
	t.Parallel()
	problems, links, err := linkProblems(irtest.RepoRoot(t))
	require.NoError(t, err)
	require.NotZero(t, links, "found no link in a document, so this proves nothing")
	for _, p := range problems {
		t.Error(p)
	}
}

// linkProblems reports each broken link and document citation under root,
// leaving out testdata, and counts the links it read.
func linkProblems(root string) ([]string, int, error) {
	var problems []string
	links := 0
	err := keydoc.Walk(root, ".", func(path string) error {
		rel := keydoc.Rel(root, path)
		if slices.Contains(strings.Split(rel, "/"), "testdata") {
			return nil
		}
		var found []string
		var err error
		switch filepath.Ext(path) {
		case ".md":
			var n int
			found, n, err = documentLinkProblems(path, rel)
			links += n
		case ".go":
			found, err = commentCitationProblems(root, path, rel)
		}
		problems = append(problems, found...)
		return err
	})
	return problems, links, err
}

// documentLinkProblems reports each relative link of the Markdown file at path
// that resolves to nothing, and counts the relative links it read.
func documentLinkProblems(path, rel string) ([]string, int, error) {
	text, err := os.ReadFile(path)
	if err != nil {
		return nil, 0, err
	}
	var problems []string
	links := 0
	for i, line := range proseLines(string(text)) {
		for _, m := range markdownLink.FindAllStringSubmatch(codeSpan.ReplaceAllString(line, ""), -1) {
			if uriScheme.MatchString(m[1]) {
				continue
			}
			links++
			why, err := brokenLink(path, m[1])
			if err != nil {
				return nil, 0, err
			}
			if why != "" {
				problems = append(problems, fmt.Sprintf("%s:%d links %q, %s", rel, i+1, m[1], why))
			}
		}
	}
	return problems, links, nil
}

// brokenLink says what is wrong with target, a relative link from the document
// at doc, or "" when it resolves. A fragment is held to the headings of a
// Markdown target only, since another file has none.
func brokenLink(doc, target string) (string, error) {
	file, fragment, _ := strings.Cut(target, "#")
	resolved := doc
	if file != "" {
		resolved = filepath.Join(filepath.Dir(doc), file)
	}
	if _, err := os.Stat(resolved); errors.Is(err, fs.ErrNotExist) {
		return "which names no file", nil
	} else if err != nil {
		return "", err
	}
	if fragment == "" || filepath.Ext(resolved) != ".md" {
		return "", nil
	}
	text, err := os.ReadFile(resolved)
	if err != nil {
		return "", err
	}
	if !headingSlugs(string(text))[fragment] {
		return "whose file has no such heading", nil
	}
	return "", nil
}

// commentCitationProblems reports each document a comment of the Go file at
// path names that does not exist.
func commentCitationProblems(root, path, rel string) ([]string, error) {
	comments, err := keydoc.Comments(path)
	if err != nil {
		return nil, err
	}
	var problems []string
	for _, c := range comments {
		for _, cited := range docPath.FindAllString(c.Text, -1) {
			if _, err := os.Stat(filepath.Join(root, cited)); errors.Is(err, fs.ErrNotExist) {
				problems = append(problems, fmt.Sprintf("%s:%d cites %s, which does not exist", rel, c.Line, cited))
			} else if err != nil {
				return nil, err
			}
		}
	}
	return problems, nil
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
		if n := seen[slug]; n > 0 {
			slugs[fmt.Sprintf("%s-%d", slug, n)] = true
		} else {
			slugs[slug] = true
		}
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
// beside links that resolve, in a document, a fenced block, a span of code and
// under testdata, and citations of a document in Go comments.
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
		"package a\n\n/* A block cites docs/also/gone.md. */\nconst s = \"docs/string.md\"\n")
	writeDocFile(t, filepath.Join(root, "x", "testdata", "a.go"), "// Unread: docs/gone.md.\npackage a\n")

	problems, links, err := linkProblems(root)
	require.NoError(t, err)
	assert.Equal(t, 11, links, "each relative link read is counted once")
	assert.Equal(t, []string{
		`a/a.go:1 cites docs/gone.md, which does not exist`,
		`a/a.go:4 cites docs/also/gone.md, which does not exist`,
		`docs/guide.md:2 links "absent.md", which names no file`,
		`docs/guide.md:2 links "target.md#absent", whose file has no such heading`,
		`docs/guide.md:2 links "target.md#fenced", whose file has no such heading`,
		`docs/guide.md:2 links "#absent", whose file has no such heading`,
		`docs/guide.md:4 links "absent.md#x", which names no file`,
	}, problems)

	_, _, err = linkProblems(filepath.Join(root, "absent"))
	require.ErrorIs(t, err, fs.ErrNotExist)
}
