package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/internal/testspec"
)

// TestRun_TerminatorEndsFlagParsing pins what "--" means everywhere morphic
// reads an argument list: it ends flag parsing for the rest of the invocation,
// not for the next argument. Each case names the layer it exercises, because
// the three read argv independently — the subcommand's own flags, the root
// command word, and help's command word — and a terminator honoured in one is
// no evidence about the others.
func TestRun_TerminatorEndsFlagParsing(t *testing.T) {
	t.Parallel()

	spec := writeFile(t, "spec.yaml", testspec.Tiny)
	outDir := t.TempDir()
	shielded := filepath.Join(outDir, "shielded.json")
	trailing := filepath.Join(outDir, "trailing.json")
	written := filepath.Join(outDir, "written.json")

	tests := []struct {
		name     string
		args     []string
		wantCode int
		wantErr  string
		// wantFile, when set, must exist afterwards; wantNoFile must not.
		wantFile   string
		wantNoFile string
	}{
		{
			name:       "flags after a shielded spec are operands",
			args:       []string{"compile", "--", spec, "-o", shielded},
			wantCode:   2,
			wantErr:    "compile requires exactly one spec file",
			wantNoFile: shielded,
		},
		{
			name:     "every shielded operand is an operand",
			args:     []string{"compile", "--", "-nope.yaml", "-second.yaml"},
			wantCode: 2,
			wantErr:  "compile requires exactly one spec file",
		},
		{
			name:     "a shielded dash-named spec reaches the engine",
			args:     []string{"compile", "--", "-nope.yaml"},
			wantCode: 2,
			wantErr:  `read spec "-nope.yaml"`,
		},
		{
			name:       "a terminator after the spec still shields",
			args:       []string{"compile", spec, "--", "-o", trailing},
			wantCode:   2,
			wantErr:    "compile requires exactly one spec file",
			wantNoFile: trailing,
		},
		{
			name:     "flags before the terminator still parse",
			args:     []string{"compile", "-o", written, "--", spec},
			wantCode: 0,
			wantFile: written,
		},
		{
			name:     "a root terminator shields the command word",
			args:     []string{"--", "compile", spec},
			wantCode: 0,
		},
		{
			name:     "a root terminator ends help flags",
			args:     []string{"--", "-h"},
			wantCode: 2,
			wantErr:  `unknown command "-h"`,
		},
		{
			name:     "a help terminator ends help flags",
			args:     []string{"help", "--", "--help"},
			wantCode: 2,
			wantErr:  `unknown command "--help"`,
		},
		{
			name:     "a help terminator shields the command word",
			args:     []string{"help", "--", "compile"},
			wantCode: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var stdout, stderr bytes.Buffer

			code := run(tt.args, &stdout, &stderr)

			require.Equal(t, tt.wantCode, code, "stderr: %s", stderr.String())
			if tt.wantErr != "" {
				assert.Contains(t, stderr.String(), tt.wantErr)
			}
			if tt.wantFile != "" {
				assert.FileExists(t, tt.wantFile)
			}
			if tt.wantNoFile != "" {
				assert.NoFileExists(t, tt.wantNoFile,
					"an operand the user shielded must never be honoured as -o")
			}
		})
	}
}

// TestRun_TerminatorAsFlagValue pins the half of the rule that is easy to lose
// while fixing the other half: the "--" a flag asked for is that flag's value,
// not a terminator, so scanning argv for the token without tracking which flags
// consume the argument after them would break this. It passes both before and
// after the terminator fix, and reddens on a fix that pre-scans instead.
//
// Not run in parallel: it changes the process working directory so -o's value
// resolves to a file literally named "--".
func TestRun_TerminatorAsFlagValue(t *testing.T) {
	dir := t.TempDir()
	prevWD, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(dir))
	t.Cleanup(func() { require.NoError(t, os.Chdir(prevWD)) })

	spec := writeFile(t, "spec.yaml", testspec.Tiny)
	var stdout, stderr bytes.Buffer

	code := run([]string{"compile", "-o", "--", spec, "--skip-validate"}, &stdout, &stderr)

	require.Equal(t, 0, code, "stderr: %s", stderr.String())
	raw, err := os.ReadFile(filepath.Join(dir, "--"))
	require.NoError(t, err)
	assert.Contains(t, string(raw), `"name": "Tiny"`,
		`"--" must be consumed as -o's value, leaving the flags after the spec to parse`)
}

func TestTakesNextValue_FlagSpellings(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		arg  string
		want bool
	}{
		{"a value flag", "-o", true},
		{"a value flag spelled with two dashes", "--o", true},
		{"a value flag carrying its value inline", "-o=x", false},
		{"a boolean flag", "-skip-validate", false},
		{"a flag this command does not define", "-bogus", false},
		{"an operand", "spec.yaml", false},
		{"a bare dash", "-", false},
		{"the terminator", "--", false},
		{"more dashes than a flag can have", "---o", false},
		{"a flag with no name", "-=x", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			fs, _ := newCompileFlags()
			assert.Equal(t, tt.want, takesNextValue(fs, tt.arg))
		})
	}
}

func TestSplitAtTerminator_Cases(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		args         []string
		wantBefore   []string
		wantOperands []string
	}{
		{"no terminator", []string{"-o", "x", "spec.yaml"}, []string{"-o", "x", "spec.yaml"}, nil},
		{"leading terminator", []string{"--", "-a", "-b"}, []string{}, []string{"-a", "-b"}},
		{"terminator with nothing after it", []string{"spec.yaml", "--"}, []string{"spec.yaml"}, []string{}},
		{"only the first terminator splits", []string{"--", "a", "--", "b"}, []string{}, []string{"a", "--", "b"}},
		{"a terminator a flag asked for is its value", []string{"-o", "--", "spec.yaml"},
			[]string{"-o", "--", "spec.yaml"}, nil},
		{"a boolean flag does not swallow the terminator", []string{"-skip-validate", "--", "-a"},
			[]string{"-skip-validate"}, []string{"-a"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			fs, _ := newCompileFlags()

			before, operands := splitAtTerminator(fs, tt.args)

			assert.Equal(t, tt.wantBefore, before)
			assert.Equal(t, tt.wantOperands, operands)
		})
	}
}
