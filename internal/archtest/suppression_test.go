package archtest_test

import (
	"fmt"
	"go/ast"
	"go/token"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// suppressionDirectives are the comment forms that silence a linter from inside
// the source. None may stand: a suppression is a rule in .golangci.yml's
// exclusions, where every one is found in a single place with its reason beside
// it. A directive naming a linter the config leaves off is read by nothing yet
// reads as a live constraint, and refusing the whole class closes that without
// asking which linters run. Each pattern is at least as wide as its tool's.
var suppressionDirectives = []struct {
	name    string
	pattern *regexp.Regexp
}{
	// golangci-lint trims leading slashes and spaces, then wants "nolint" and a
	// space, a colon or the end. The case, tab and block spellings it ignores
	// (v2.14.0) are refused too, so a release that learns them opens no hole.
	{"nolint", regexp.MustCompile(`(?i)^(?://|/\*)[/*\s]*nolint(?:[\s:*]|$)`)},
	// revive's own directive, which golangci-lint passes through and nolintlint
	// never saw.
	{"revive", regexp.MustCompile(`^//\s*revive:(?:enable|disable)`)},
	// staticcheck's own directive. Its unused analyzer honours it for U1000, and
	// golangci-lint did not for SA1012 (v2.14.0), so it may suppress or stand inert.
	{"lint:ignore", regexp.MustCompile(`^//\s*lint:(?:file-)?ignore`)},
	// exhaustive's own: an ignore on a switch, a map literal or an enum's
	// declaration takes it out of the check. It wants "//exhaustive:" with no
	// space (v0.13.0); the spellings it ignores are refused too. Its enforce
	// directive only adds a check, so it stands.
	{"exhaustive:ignore", regexp.MustCompile(`(?i)^(?://|/\*)\s*exhaustive:ignore`)},
}

// suppressionOf names the directive a comment's text spells, or false when it
// spells none.
func suppressionOf(text string) (string, bool) {
	for _, d := range suppressionDirectives {
		if d.pattern.MatchString(text) {
			return d.name, true
		}
	}
	return "", false
}

// TestSuppressions_NoDirectiveStandsInTheSource reads every comment of every Go
// file the go tool would build, tests included, and fails on one that spells a
// suppression directive. It reads comments rather than text, so a string
// literal, or a sentence that mentions a directive without opening a comment
// with it, is not one.
func TestSuppressions_NoDirectiveStandsInTheSource(t *testing.T) {
	t.Parallel()
	violations, files, err := suppressionViolations(repoRoot(t))
	require.NoError(t, err)
	require.Positive(t, files, "the walk read no Go file, so it checked nothing")
	for _, v := range violations {
		t.Error(v)
	}
}

// TestSuppressionViolations_PlantedSpellingsAreCaught plants each spelling the
// patterns must refuse: the ones golangci-lint v2.14.0 honours, and the ones it
// ignores that a later release could start to read.
func TestSuppressionViolations_PlantedSpellingsAreCaught(t *testing.T) {
	t.Parallel()
	tests := []struct{ name, comment, directive string }{
		{"nolint with no space", "//nolint:errcheck // reason", "nolint"},
		{"nolint after a space", "// nolint:errcheck // reason", "nolint"},
		{"nolint after two spaces", "//  nolint:errcheck", "nolint"},
		{"nolint after a third slash", "/// nolint:errcheck", "nolint"},
		{"bare nolint", "//nolint", "nolint"},
		{"bare nolint after a space", "// nolint", "nolint"},
		{"nolint all", "//nolint:all", "nolint"},
		{"nolint after a tab", "//\tnolint:errcheck", "nolint"},
		{"nolint in capitals", "//NOLINT:errcheck", "nolint"},
		{"nolint in mixed case", "//Nolint:errcheck", "nolint"},
		{"nolint in a block comment", "/*nolint:errcheck*/", "nolint"},
		{"bare nolint in a block comment", "/*nolint*/", "nolint"},
		{"nolint then a tab", "//nolint\t// reason", "nolint"},
		{"prose that begins with the word", "// nolint is a word that begins this line.", "nolint"},
		{"revive disable-next-line", "//revive:disable-next-line:exported", "revive"},
		{"revive disable-line", "//revive:disable-line:exported", "revive"},
		{"revive disable after a space", "// revive:disable:exported", "revive"},
		{"revive enable", "//revive:enable:exported", "revive"},
		{"staticcheck ignore", "//lint:ignore SA1012 reason", "lint:ignore"},
		{"staticcheck ignore of unused", "//lint:ignore U1000 reason", "lint:ignore"},
		{"staticcheck file-ignore", "//lint:file-ignore SA1012 reason", "lint:ignore"},
		{"exhaustive ignore", "//exhaustive:ignore reason", "exhaustive:ignore"},
		{"exhaustive ignore of a required default", "//exhaustive:ignore-default-case-required", "exhaustive:ignore"},
		{"exhaustive ignore after a space", "// exhaustive:ignore", "exhaustive:ignore"},
		{"exhaustive ignore in a block comment", "/*exhaustive:ignore*/", "exhaustive:ignore"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			writeDocFile(t, filepath.Join(root, "a", "planted.go"), "package planted\n\nvar V int "+tc.comment+"\n")

			violations, files, err := suppressionViolations(root)
			require.NoError(t, err)
			require.Equal(t, 1, files)
			require.Len(t, violations, 1, "%v", violations)
			assert.Contains(t, violations[0], "a/planted.go:3:")
			assert.Contains(t, violations[0], tc.directive+" directive")
		})
	}
}

// TestSuppressionViolations_PlantedPlacementsAreCaught plants one directive
// everywhere a file can hold a comment, since the walk is over comments and a
// placement it did not reach would be a place to hide one.
func TestSuppressionViolations_PlantedPlacementsAreCaught(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, file, src string
		line            int
	}{
		{"trailing a declaration", "a/p.go", "package p\n\nvar V int //nolint\n", 3},
		{"above a declaration", "a/p.go", "package p\n\n//nolint\nvar V int\n", 3},
		{"inside a body", "a/p.go", "package p\n\nfunc F() {\n\t//nolint:errcheck\n}\n", 4},
		{"on a later line of a doc comment", "a/p.go", "package p\n\n// V counts.\n//nolint:unused\nvar V int\n", 4},
		{"before the package clause", "a/p.go", "//nolint\npackage p\n", 1},
		{"in a test file", "a/p_test.go", "package p\n\nvar V int //nolint\n", 3},
		{"in a nested directory", "a/b/c/p.go", "package p\n\nvar V int //nolint\n", 3},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			writeDocFile(t, filepath.Join(root, tc.file), tc.src)

			violations, _, err := suppressionViolations(root)
			require.NoError(t, err)
			require.Len(t, violations, 1, "%v", violations)
			assert.Contains(t, violations[0], fmt.Sprintf("%s:%d:", tc.file, tc.line))
		})
	}
}

// TestSuppressionViolations_ProseAndStringsAreNotDirectives plants what must
// pass: sentences that mention a directive mid-comment, words that only begin
// like one, string literals that spell one, the directives the go tool owns,
// and exhaustive's enforce, which adds a check rather than removing one.
func TestSuppressionViolations_ProseAndStringsAreNotDirectives(t *testing.T) {
	t.Parallel()
	const src = "package p\n\n" +
		"// A //nolint directive is refused here, so this sentence is prose.\n" +
		"// nolinter begins like a directive and is a word.\n" +
		"//nolintx is another.\n" +
		"// revive: is a linter, named here with its colon.\n" +
		"// The lint:ignore form is named mid-comment, as //revive:disable is.\n" +
		"const quoted = \"//nolint:errcheck\"\n" +
		"const raw = `// revive:disable`\n" +
		"//go:noinline\n" +
		"//exhaustive:enforce\n" +
		"func F() {}\n"
	root := t.TempDir()
	writeDocFile(t, filepath.Join(root, "p.go"), src)

	violations, files, err := suppressionViolations(root)
	require.NoError(t, err)
	assert.Equal(t, 1, files)
	assert.Empty(t, violations)
}

// suppressionViolations walks root for every Go file the go tool would build and
// returns a message for each comment that spells a suppression directive, with
// the number of files it read.
func suppressionViolations(root string) ([]string, int, error) {
	var violations []string
	files := 0
	err := walkGoFiles(root, func(rel string, fset *token.FileSet, f *ast.File) {
		files++
		for _, group := range f.Comments {
			for _, c := range group.List {
				if name, ok := suppressionOf(c.Text); ok {
					violations = append(violations, fmt.Sprintf(
						"%s:%d: %s directive: move it to an exclusion in .golangci.yml, or reword the comment if it is prose",
						rel, fset.Position(c.Pos()).Line, name))
				}
			}
		}
	})
	return violations, files, err
}
