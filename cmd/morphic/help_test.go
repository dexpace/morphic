package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/internal/testspec"
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
		{"root -help", []string{"-help"}},
		{"root help word", []string{"help"}},
		{"help compile", []string{"help", "compile"}},
		{"help -h", []string{"help", "-h"}},
		{"help --help", []string{"help", "--help"}},
		{"help compile --help", []string{"help", "compile", "--help"}},
		{"help compile -h", []string{"help", "compile", "-h"}},
		{"root -h compile", []string{"-h", "compile"}},
		{"root --help compile", []string{"--help", "compile"}},
		{"compile -h", []string{"compile", "-h"}},
		{"compile --help", []string{"compile", "--help"}},
		{"compile spec --help", []string{"compile", spec, "--help"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var stdout, stderr bytes.Buffer

			assert.Equal(t, 0, run(tt.args, &stdout, &stderr))
			assert.Contains(t, stdout.String(), "usage:")
			assert.Empty(t, stderr.String(), "help must never write to stderr")
		})
	}
}

// TestRun_EveryCommandAnswersItsHelpFlag holds the property no per-command test
// can: help works for whatever the command table holds, so a subcommand added
// later cannot answer -h with an error line and exit 2 by forgetting a block
// its neighbour carries. The flag spellings are the ones the shared dispatch
// must recognise; `morphic help <name>` reaches the table directly and stays
// correct either way, so it would hide the breakage rather than catch it.
func TestRun_EveryCommandAnswersItsHelpFlag(t *testing.T) {
	t.Parallel()

	table := commands()
	require.NotEmpty(t, table, "the command table must not be empty")
	for _, c := range table {
		for _, spelling := range []string{"-h", "--help", "-help"} {
			t.Run(c.name+" "+spelling, func(t *testing.T) {
				t.Parallel()
				var stdout, stderr bytes.Buffer

				code := run([]string{c.name, spelling}, &stdout, &stderr)

				assert.Equal(t, 0, code, "stderr: %s", stderr.String())
				assert.Contains(t, stdout.String(), c.usage)
				assert.Empty(t, stderr.String(), "help must never write to stderr")
			})
		}
	}
}

// TestRun_HelpFlagAsFlagValue pins the property the help design relies on:
// help is detected via errors.Is(err, flag.ErrHelp) from flag.Parse, never by
// pre-scanning argv for "--help". So "-o --help" must consume "--help" as the
// value of -o and compile normally, not print help. This guards against a
// future refactor of bindSpec or dispatch that pre-scans args and would keep
// full statement coverage while silently breaking that distinction. Not run in
// parallel: it changes the process working directory so -o's relative value
// resolves to a file literally named "--help".
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
	assert.NotContains(t, stdout.String(), "usage:",
		"--help must be consumed as -o's value, not treated as a help request")
	raw, err := os.ReadFile(filepath.Join(dir, "--help"))
	require.NoError(t, err)
	assert.Contains(t, string(raw), `"name": "Tiny"`)
}

func TestRun_HelpFormsAgree(t *testing.T) {
	t.Parallel()

	for _, c := range commands() {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			forms := [][]string{
				{c.name, "--help"},
				{c.name, "-h"},
				{"help", c.name},
				{"help", c.name, "--help"},
				{"help", c.name, "-h"},
				{"-h", c.name},
				{"--help", c.name},
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
			// whether it contains each name: a one-letter name like "o" is a
			// substring of almost any text, so a containment check passes for help
			// that documents nothing.
			assert.ElementsMatch(t, tt.flags, flagNamesIn(stdout.String()),
				"%s help must document exactly the flags %s accepts", tt.name, tt.name)
		})
	}
}

// summaryColumns returns, for each line of a rendered command list, the column
// its summary starts at. Each line is "  <name><padding> <summary>", so the
// summary begins after the run of spaces that follows the name.
func summaryColumns(t *testing.T, rendered string) []int {
	t.Helper()

	lines := strings.Split(strings.TrimSuffix(rendered, "\n"), "\n")
	cols := make([]int, 0, len(lines))
	for _, line := range lines {
		_, after, ok := strings.Cut(strings.TrimPrefix(line, "  "), " ")
		require.True(t, ok, "every command line carries a name and a summary: %q", line)
		summary := strings.TrimLeft(after, " ")
		require.NotEmpty(t, summary, "every command line carries a summary: %q", line)
		cols = append(cols, len(line)-len(summary))
	}
	return cols
}

// TestWriteCommandList_SummariesShareOneColumn is the assertion a golden cannot
// make: it renders names the shipped table does not hold, so it fails on a
// padding width that merely happens to fit today's names rather than recording
// the misalignment as the new expected output.
//
// The column must also be snug against the longest name. A width that is
// derived but slack still lines the summaries up, so equality alone would pass
// for a table padded to any constant wider than every name.
func TestWriteCommandList_SummariesShareOneColumn(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		table []command
	}{
		{"names shorter than the shipped ones", []command{
			{name: "a", summary: "first"},
			{name: "bc", summary: "second"},
		}},
		{"a name longer than the shipped ones", []command{
			{name: "compile", summary: "first"},
			{name: "generate-sdk", summary: "second"},
			{name: "x", summary: "third"},
		}},
		{"the shipped table", commands()},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.NotEmpty(t, tt.table)

			var buf bytes.Buffer
			writeCommandList(&buf, tt.table)

			longest := 0
			for _, c := range tt.table {
				longest = max(longest, len(c.name))
			}
			want := len("  ") + longest + len(" ")
			for i, got := range summaryColumns(t, buf.String()) {
				assert.Equal(t, want, got,
					"summary %d must start one space past the longest name, in:\n%s", i, buf.String())
			}
		})
	}
}

func TestRootHelp_ListsEveryCommand(t *testing.T) {
	t.Parallel()

	var stdout, stderr bytes.Buffer
	require.Equal(t, 0, run(nil, &stdout, &stderr))

	got := stdout.String()
	table := commands()
	require.NotEmpty(t, table, "the command table must not be empty")
	for _, c := range table {
		assert.Contains(t, got, c.name)
		assert.Contains(t, got, c.summary)
	}
}

// compareHelpGolden compares got against the golden file at path, or rewrites
// it when -update is passed to go test.
//
// The -update flag comes from irtest rather than being declared here. A second
// flag.Bool("update", ...) in this package would not merely shadow the first:
// both register on the same command-line FlagSet, so the binary panics at init
// the moment anything pulls irtest into these tests.
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

// TestHelp_MatchesGolden pins every text the CLI renders. The two help texts
// are the ones a user asks for; the two misuse texts are the ones a user gets
// by accident, and nothing else holds their wording — writeCommandUsage and the
// root help that misuse prints to stderr could otherwise drift in silence.
//
// The wantStderr column doubles as the stream-discipline assertion: help must
// never reach stderr, misuse must never reach stdout, and whichever stream is
// not under test must be empty.
func TestHelp_MatchesGolden(t *testing.T) {
	// Not parallel: the -update path writes files.
	tests := []struct {
		name       string
		args       []string
		golden     string
		wantCode   int
		wantStderr bool
	}{
		{"root", nil, "root-help.txt", 0, false},
		{"compile", []string{"help", "compile"}, "compile-help.txt", 0, false},
		{"validate", []string{"help", "validate"}, "validate-help.txt", 0, false},
		{"compile misuse", []string{"compile"}, "compile-usage.txt", 2, true},
		{"validate misuse", []string{"validate"}, "validate-usage.txt", 2, true},
		{"unknown command", []string{"bogus"}, "unknown-command.txt", 2, true},
	}

	for _, tt := range tests {
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
