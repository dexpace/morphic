// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

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

			assert.Equal(t, 0, run(t.Context(), tt.args, &stdout, &stderr))
			assert.Contains(t, stdout.String(), "Usage:")
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

				code := run(t.Context(), []string{c.Name(), spelling}, &stdout, &stderr)

				assert.Equal(t, 0, code, "stderr: %s", stderr.String())
				assert.Contains(t, stdout.String(), c.Use)
				assert.Empty(t, stderr.String(), "help must never write to stderr")
			})
		}
	}
}

// TestRun_NilArgsAreNoArguments pins that run given nil args is a run with no
// arguments and not a run of the process's own. cobra falls back to os.Args[1:]
// for a nil slice, which a test binary cannot show on its own: pflag skips its
// -test.* flags. Not run in parallel, as it replaces os.Args.
func TestRun_NilArgsAreNoArguments(t *testing.T) {
	prev := os.Args
	t.Cleanup(func() { os.Args = prev })
	os.Args = []string{"morphic", "bogus"}
	var stdout, stderr bytes.Buffer

	code := run(t.Context(), nil, &stdout, &stderr)

	assert.Equal(t, 0, code, "stderr: %s", stderr.String())
	assert.Contains(t, stdout.String(), "Usage:")
}

// TestRun_HelpFlagAsFlagValue pins that a help flag typed where a flag wants a
// value is that flag's value: "-o --help" must write a file named "--help" and
// compile normally, not print help. Not run in parallel: it changes the process
// working directory so -o's relative value resolves to that file.
func TestRun_HelpFlagAsFlagValue(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	spec := writeFile(t, "spec.yaml", testspec.Tiny)
	var stdout, stderr bytes.Buffer

	code := run(t.Context(), []string{"compile", "-o", "--help", spec}, &stdout, &stderr)

	require.Equal(t, 0, code, "stderr: %s", stderr.String())
	assert.NotContains(t, stdout.String(), "Usage:",
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
// also pins that the single-dash spelling "-help" is refused, with the two-dash
// spelling to use.
func TestRun_HelpFlagDoesNotMaskAMistypedCommand(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		args   []string
		reason string
	}{
		{"-h bogus", []string{"-h", "bogus"}, `unknown command "bogus"`},
		{"--help bogus", []string{"--help", "bogus"}, `unknown command "bogus"`},
		{"-help", []string{"-help"}, `write "--help", not "-help"`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var stdout, stderr bytes.Buffer

			assert.Equal(t, 2, run(t.Context(), tt.args, &stdout, &stderr))
			assert.Empty(t, stdout.String())
			assert.Contains(t, stderr.String(), tt.reason)
		})
	}
}

// TestRun_OperandsAfterTerminatorAreRefused pins that "--" at the top level does
// not turn a command into help. It once ran the command it was followed by;
// cobra reads the operand as nothing and prints help with exit 0, which for
// "morphic -- compile spec" is a compile that silently produced nothing.
func TestRun_OperandsAfterTerminatorAreRefused(t *testing.T) {
	t.Parallel()

	spec := writeFile(t, "spec.yaml", testspec.Tiny)
	tests := []struct {
		name string
		args []string
		word string
	}{
		{"a command", []string{"--", "compile", spec}, "compile"},
		{"the help word", []string{"--", "help"}, "help"},
		{"a help flag", []string{"--", "-h"}, "-h"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var stdout, stderr bytes.Buffer

			assert.Equal(t, 2, run(t.Context(), tt.args, &stdout, &stderr))
			assert.Empty(t, stdout.String(), "nothing may be written for a refused invocation")
			assert.Contains(t, stderr.String(), `unexpected argument "`+tt.word+`" after "--"`)
			assert.Contains(t, stderr.String(), `Try "morphic --help" for usage.`)
		})
	}

	// A "--" with nothing after it has nothing to refuse: it is a bare morphic.
	var stdout, stderr bytes.Buffer
	assert.Equal(t, 0, run(t.Context(), []string{"--"}, &stdout, &stderr), "stderr: %s", stderr.String())
	assert.Contains(t, stdout.String(), "Usage:")
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
				require.Equal(t, 0, run(t.Context(), args, &stdout, &stderr), "stderr: %s", stderr.String())
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
			require.Equal(t, 0, run(t.Context(), []string{"help", tt.name}, &stdout, &stderr))

			// Read the flag names back out of the help text rather than asking
			// whether it contains each name: a short name like "o" is a substring
			// of almost any text, so a containment check passes for help that
			// documents nothing. help is cobra's, defined when the command runs.
			assert.ElementsMatch(t, append([]string{"help"}, tt.flags...), flagNamesIn(stdout.String()),
				"%s help must document exactly the flags %s accepts", tt.name, tt.name)
		})
	}
}

// flagLine matches a flag row of cobra's help: an optional "-x, " short form,
// then "--name", then the value's name or the description's padding.
var flagLine = regexp.MustCompile(`^\s+(?:-\w, )?--([a-z-]+)\s`)

// flagNamesIn returns the flag names documented by rendered help: the long
// names on the rows under its "Flags:" title. Every line cobra does not indent,
// whether a section title, the description or the closing pointer to --help,
// ends the section before it.
func flagNamesIn(rendered string) []string {
	var names []string
	inFlags := false
	for line := range strings.SplitSeq(rendered, "\n") {
		if line != "" && !strings.HasPrefix(line, " ") {
			inFlags = line == "Flags:"
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

	rendered := "Usage:\n  morphic compile --flags\n\nFlags:\n" +
		"      --fail-on string   exit 1\n  -o, --output string    write\n\nExamples:\n  morphic --x a\n"

	assert.Equal(t, []string{"fail-on", "output"}, flagNamesIn(rendered))
}

func TestRootHelp_ListsEveryCommand(t *testing.T) {
	t.Parallel()

	var stdout, stderr bytes.Buffer
	require.Equal(t, 0, run(t.Context(), nil, &stdout, &stderr))

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
func compareHelpGolden(t *testing.T, path, got string) {
	t.Helper()

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
			require.Equal(t, tt.wantCode, run(t.Context(), tt.args, &stdout, &stderr))

			got, quiet := stdout.String(), stderr.String()
			if tt.wantStderr {
				got, quiet = quiet, got
			}
			require.Empty(t, quiet, "nothing may reach the stream this text does not use")

			compareHelpGolden(t, filepath.Join("testdata", tt.golden), got)
		})
	}
}
