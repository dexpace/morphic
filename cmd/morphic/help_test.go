package main

import (
	"bytes"
	"encoding/json/v2"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/internal/testspec"
	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/ir/irtest"
)

func TestRun_HelpForms(t *testing.T) {
	t.Parallel()
	spec := writeFile(t, "spec.yaml", testspec.Tiny)

	tests := []struct {
		name string
		args []string
	}{
		{"no arguments", nil},
		{"root -h", []string{"-h"}},
		{"root --help", []string{"--help"}},
		{"root help word", []string{"help"}},
		{"help compile", []string{"help", "compile"}},
		{"help -h", []string{"help", "-h"}},
		{"help --help", []string{"help", "--help"}},
		{"root -h compile", []string{"-h", "compile"}},
		{"root --help compile", []string{"--help", "compile"}},
		{"compile -h", []string{"compile", "-h"}},
		{"compile --help", []string{"compile", "--help"}},
		{"compile spec --help", []string{"compile", spec, "--help"}},
		{"help flag after extra operands", []string{"--help", "compile", "extra"}},
		// The flag belongs to the help command here, so this asks for help of help
		// and not of compile. A help flag is no longer a token help strips.
		{"help compile --help", []string{"help", "compile", "--help"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var stdout, stderr bytes.Buffer

			assert.Equal(t, 0, run(tt.args, &stdout, &stderr))
			assert.Contains(t, stdout.String(), "USAGE")
			assert.Empty(t, stderr.String(), "help must never write to stderr")
		})
	}
}

// TestRun_EveryCommandAnswersItsHelpFlag holds the property no per-command test
// can: help works for whatever the command tree holds, so a subcommand added
// later cannot answer -h with an error line and exit 2 by forgetting something
// its neighbour carries. `morphic help <name>` is a different path and stays
// correct either way, so it would hide the breakage rather than catch it.
func TestRun_EveryCommandAnswersItsHelpFlag(t *testing.T) {
	t.Parallel()

	cmds := newRootCmd().Commands()
	require.NotEmpty(t, cmds, "the command tree must not be empty")
	for _, c := range cmds {
		for _, spelling := range []string{"-h", "--help"} {
			t.Run(c.Name()+" "+spelling, func(t *testing.T) {
				t.Parallel()
				var stdout, stderr bytes.Buffer

				code := run([]string{c.Name(), spelling}, &stdout, &stderr)

				assert.Equal(t, 0, code, "stderr: %s", stderr.String())
				assert.Contains(t, stdout.String(), c.Use)
				assert.Empty(t, stderr.String(), "help must never write to stderr")
			})
		}
	}
}

// TestRun_NilArgsAreNoArguments pins that run(nil) is a run with no arguments
// and not a run of the process's own. cobra falls back to os.Args[1:] for a nil
// slice, which a test binary cannot show on its own: pflag skips its -test.*
// flags. Not run in parallel, as it replaces os.Args.
func TestRun_NilArgsAreNoArguments(t *testing.T) {
	prev := os.Args
	t.Cleanup(func() { os.Args = prev })
	os.Args = []string{"morphic", "bogus"}
	var stdout, stderr bytes.Buffer

	code := run(nil, &stdout, &stderr)

	assert.Equal(t, 0, code, "stderr: %s", stderr.String())
	assert.Contains(t, stdout.String(), "USAGE")
}

// TestRun_HelpFlagAsFlagValue pins that a help flag typed where a flag wants a
// value is that flag's value: "-o --help" must write a file named "--help" and
// compile normally, not print help. Not run in parallel: it changes the process
// working directory so -o's relative value resolves to that file.
func TestRun_HelpFlagAsFlagValue(t *testing.T) {
	dir := t.TempDir()
	prevWD, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(dir))
	t.Cleanup(func() { require.NoError(t, os.Chdir(prevWD)) })

	spec := writeFile(t, "spec.yaml", testspec.Tiny)
	var stdout, stderr bytes.Buffer

	code := run([]string{"compile", "-o", "--help", spec}, &stdout, &stderr)

	require.Equal(t, 0, code, "stderr: %s", stderr.String())
	assert.NotContains(t, stdout.String(), "USAGE",
		"--help must be consumed as -o's value, not treated as a help request")
	raw, err := os.ReadFile(filepath.Join(dir, "--help"))
	require.NoError(t, err)
	// Read the name back out of the document rather than matching the artifact's
	// text: what this test is about is which file was written, not how it is
	// formatted, and -o's formatting is a flag away from changing.
	var doc ir.Document
	require.NoError(t, json.Unmarshal(raw, &doc))
	assert.Equal(t, "Tiny", doc.Name)
}

// TestRun_HelpFlagDoesNotMaskAMistypedCommand pins that asking for help does not
// excuse a command that does not exist: "-h bogus" is the unknown command and
// exit 2, as "bogus" alone is, rather than root help that hides the typo. It
// also pins that the single-dash spelling "-help" is gone: pflag reads it as a
// cluster of shorthands.
func TestRun_HelpFlagDoesNotMaskAMistypedCommand(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		args   []string
		reason string
	}{
		{"-h bogus", []string{"-h", "bogus"}, `unknown command "bogus"`},
		{"--help bogus", []string{"--help", "bogus"}, `unknown command "bogus"`},
		{"-help", []string{"-help"}, "unknown shorthand flag"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var stdout, stderr bytes.Buffer

			assert.Equal(t, 2, run(tt.args, &stdout, &stderr))
			assert.Empty(t, stdout.String())
			assert.Contains(t, stderr.String(), tt.reason)
		})
	}
}

func TestRun_HelpFormsAgree(t *testing.T) {
	t.Parallel()

	for _, c := range newRootCmd().Commands() {
		t.Run(c.Name(), func(t *testing.T) {
			t.Parallel()
			forms := [][]string{
				{c.Name(), "--help"},
				{c.Name(), "-h"},
				{"help", c.Name()},
				{"-h", c.Name()},
				{"--help", c.Name()},
			}

			var want string
			for i, args := range forms {
				var stdout, stderr bytes.Buffer
				require.Equal(t, 0, run(args, &stdout, &stderr), "stderr: %s", stderr.String())
				if i == 0 {
					want = stdout.String()
					require.NotEmpty(t, want)
					continue
				}
				assert.Empty(t, cmp.Diff(want, stdout.String()), "help text differs for %v", args)
			}
		})
	}
}

func TestRun_CommandHelpListsEveryFlag(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		flags []string
	}{
		{"compile", compileFlagNames},
		{"validate", validateFlagNames},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var stdout, stderr bytes.Buffer
			require.Equal(t, 0, run([]string{"help", tt.name}, &stdout, &stderr))

			// Read the flag names back out of the help text rather than asking
			// whether it contains each name: a short name like "o" is a substring
			// of almost any text, so a containment check passes for help that
			// documents nothing. help is cobra's, defined when the command runs.
			assert.ElementsMatch(t, append([]string{"help"}, tt.flags...), flagNamesIn(stdout.String()),
				"%s help must document exactly the flags %s accepts", tt.name, tt.name)
		})
	}
}

// flagLine matches a flag row of fang's help: an optional "-x " short form, then
// "--name", then the description's padding.
var flagLine = regexp.MustCompile(`^\s+(?:-\w )?--([a-z-]+)\s`)

// flagNamesIn returns the flag names documented by rendered help: the long
// names on the rows under its FLAGS title.
func flagNamesIn(rendered string) []string {
	var names []string
	inFlags := false
	for line := range strings.SplitSeq(rendered, "\n") {
		if title := strings.TrimSpace(line); title != "" && strings.ToUpper(title) == title {
			inFlags = title == "FLAGS"
			continue
		}
		if m := flagLine.FindStringSubmatch(line); inFlags && m != nil {
			names = append(names, m[1])
		}
	}
	return names
}

func TestFlagNamesIn_ReadsOnlyTheFlagsSection(t *testing.T) {
	t.Parallel()

	rendered := "  USAGE\n\n    morphic compile --flags\n\n  FLAGS\n\n" +
		"    --fail-on  Exit 1\n    -o --output  Write\n\n  EXAMPLES\n\n    morphic --x a\n"

	assert.Equal(t, []string{"fail-on", "output"}, flagNamesIn(rendered))
}

func TestRootHelp_ListsEveryCommand(t *testing.T) {
	t.Parallel()

	var stdout, stderr bytes.Buffer
	require.Equal(t, 0, run(nil, &stdout, &stderr))

	got := strings.ToLower(stdout.String())
	cmds := newRootCmd().Commands()
	require.NotEmpty(t, cmds, "the command tree must not be empty")
	for _, c := range cmds {
		assert.Contains(t, got, c.Name())
		assert.Contains(t, got, strings.ToLower(c.Short))
	}
}

// compareHelpGolden compares got against the golden file at path, or rewrites
// it when -update is passed to go test.
//
// The -update flag comes from irtest rather than being declared here: a second
// flag.Bool("update", ...) on the same command-line FlagSet panics at init.
//
// Trailing spaces are dropped from got first. fang pads its layout to a column
// width; the padding means nothing, and a golden that kept it would redden when
// an editor stripped it on save.
func compareHelpGolden(t *testing.T, path, got string) {
	t.Helper()

	lines := strings.Split(got, "\n")
	for i, line := range lines {
		lines[i] = strings.TrimRight(line, " ")
	}
	got = strings.Join(lines, "\n")

	if irtest.Update() {
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(t, os.WriteFile(path, []byte(got), 0o644))
		return
	}

	want, err := os.ReadFile(path)
	require.NoError(t, err, "read golden %s (run with -update to create)", path)
	assert.Empty(t, cmp.Diff(string(want), got), "golden mismatch for %s", path)
}

// helpGoldenCase is one rendering with a committed golden: the args that produce
// it, the file it is compared against, and which stream it must arrive on.
type helpGoldenCase struct {
	name       string
	args       []string
	golden     string
	wantCode   int
	wantStderr bool
}

// helpGoldenCases lists every rendering with a committed golden: the two the
// root itself produces, then each command's own help and misuse text.
//
// It is a function rather than a literal inside the test so
// TestHelp_GoldenTableCoversEveryCommand can hold it to the command tree. Adding
// a command otherwise forces only a root-help.txt regeneration, which says
// nothing about that command's own two pages — and a golden nobody wrote is a
// golden nobody notices missing.
func helpGoldenCases() []helpGoldenCase {
	return []helpGoldenCase{
		{"root", nil, "root-help.txt", 0, false},
		{"unknown command", []string{"bogus"}, "unknown-command.txt", 2, true},
		{"compile", []string{"help", "compile"}, "compile-help.txt", 0, false},
		{"compile misuse", []string{"compile"}, "compile-usage.txt", 2, true},
		{"validate", []string{"help", "validate"}, "validate-help.txt", 0, false},
		{"validate misuse", []string{"validate"}, "validate-usage.txt", 2, true},
	}
}

// TestHelp_GoldenTableCoversEveryCommand holds the hand-written golden table to
// the command tree, the way ir's hand-written kind lists are held to the kinds
// its sources declare: the list stays explicit, so a command needing a different
// misuse invocation can have one, and forgetting a command reddens here naming
// it rather than passing quietly.
//
// Without this, a third command ships with its help and usage text rendered by
// no test at all. The only failure adding one causes is the root-help and
// unknown-command goldens gaining a row, and regenerating those — which the new
// row obliges anyway — leaves the suite green.
func TestHelp_GoldenTableCoversEveryCommand(t *testing.T) {
	t.Parallel()
	covered := make(map[string]bool, len(helpGoldenCases()))
	for _, tc := range helpGoldenCases() {
		covered[tc.golden] = true
	}

	for _, c := range newRootCmd().Commands() {
		assert.Truef(t, covered[c.Name()+"-help.txt"],
			"%s has no help golden; add it to helpGoldenCases", c.Name())
		assert.Truef(t, covered[c.Name()+"-usage.txt"],
			"%s has no misuse golden; add it to helpGoldenCases", c.Name())
	}
}

// TestHelp_MatchesGolden pins every text the CLI renders. The two help texts
// are the ones a user asks for; the two misuse texts are the ones a user gets
// by accident, and nothing else holds their wording — the error line and the
// hint that misuse prints to stderr could otherwise drift in silence.
//
// The wantStderr column doubles as the stream-discipline assertion: help must
// never reach stderr, misuse must never reach stdout, and whichever stream is
// not under test must be empty.
func TestHelp_MatchesGolden(t *testing.T) {
	// Not parallel: the -update path writes files.
	for _, tt := range helpGoldenCases() {
		t.Run(tt.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			require.Equal(t, tt.wantCode, run(tt.args, &stdout, &stderr))

			got, quiet := stdout.String(), stderr.String()
			if tt.wantStderr {
				got, quiet = quiet, got
			}
			require.Empty(t, quiet, "nothing may reach the stream this text does not use")

			compareHelpGolden(t, filepath.Join("testdata", tt.golden), got)
		})
	}
}
