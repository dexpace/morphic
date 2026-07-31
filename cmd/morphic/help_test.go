package main

import (
	"bytes"
	"flag"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRun_HelpForms(t *testing.T) {
	t.Parallel()
	spec := writeFile(t, "spec.yaml", tinySpec)

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

// TestRun_HelpFlagAsFlagValue pins the property the help design relies on:
// help is detected via errors.Is(err, flag.ErrHelp) from flag.Parse, never by
// pre-scanning argv for "--help". So "-o --help" must consume "--help" as the
// value of -o and compile normally, not print help. This guards against a
// future refactor of runCompile that pre-scans args and would keep full
// statement coverage while silently breaking that distinction. Not run in
// parallel: it changes the process working directory so -o's relative value
// resolves to a file literally named "--help".
func TestRun_HelpFlagAsFlagValue(t *testing.T) {
	dir := t.TempDir()
	prevWD, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(dir))
	t.Cleanup(func() { require.NoError(t, os.Chdir(prevWD)) })

	spec := writeFile(t, "spec.yaml", tinySpec)
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

	forms := [][]string{
		{"compile", "--help"},
		{"compile", "-h"},
		{"help", "compile"},
		{"help", "compile", "--help"},
		{"help", "compile", "-h"},
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
}

func TestRun_CompileHelpListsEveryFlag(t *testing.T) {
	t.Parallel()

	var stdout, stderr bytes.Buffer
	require.Equal(t, 0, run([]string{"help", "compile"}, &stdout, &stderr))

	got := stdout.String()
	fs, _ := newCompileFlags()
	fs.VisitAll(func(f *flag.Flag) {
		assert.Contains(t, got, f.Name, "compile help must document -%s", f.Name)
	})
}

func TestRootHelp_ListsEveryCommand(t *testing.T) {
	t.Parallel()

	var stdout, stderr bytes.Buffer
	require.Equal(t, 0, run(nil, &stdout, &stderr))

	got := stdout.String()
	require.NotEmpty(t, commands, "the command table must not be empty")
	for _, c := range commands {
		assert.Contains(t, got, c.name)
		assert.Contains(t, got, c.summary)
	}
}
