package main

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/internal/testspec"
)

// TestRun_SingleDashLongFlagsAreRefused pins that a long flag typed with one
// dash, as morphic's earlier flag package accepted, is refused with the
// spelling to use. pflag would read "-opt=a=1" on compile as "-o pt=a=1",
// dropping the option and writing a file named "pt=a=1" with exit 0. Not run
// in parallel: it moves into an empty directory, which a misread must not
// write to.
func TestRun_SingleDashLongFlagsAreRefused(t *testing.T) {
	spec := writeFile(t, "spec.yaml", testspec.Tiny)
	dir := t.TempDir()
	t.Chdir(dir)

	tests := []struct {
		name   string
		args   []string
		reason string
		help   string // the command the hint points at
	}{
		{"an option and its value joined", []string{"compile", spec, "-opt=a=1"},
			`write "--opt=a=1", not "-opt=a=1"`, "morphic compile"},
		{"an option and its value apart", []string{"compile", "-opt", "a=1", spec},
			`write "--opt", not "-opt"`, "morphic compile"},
		{"before the command it belongs to", []string{"-opt=a=1", "compile", spec},
			`write "--opt=a=1", not "-opt=a=1"`, "morphic compile"},
		{"the output file", []string{"compile", spec, "-output=ir.json"},
			`write "--output=ir.json", not "-output=ir.json"`, "morphic compile"},
		{"a command's help flag, which cobra defines late", []string{"compile", spec, "-help"},
			`write "--help", not "-help"`, "morphic compile"},
		{"the version flag", []string{"-version"}, `write "--version", not "-version"`, "morphic"},
		{"validate's flags", []string{"validate", spec, "-skip-validate"},
			`write "--skip-validate", not "-skip-validate"`, "morphic validate"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer

			assert.Equal(t, 2, run(tt.args, &stdout, &stderr))
			assert.Empty(t, stdout.String())
			assert.Contains(t, stderr.String(), "long flags take two dashes: "+tt.reason)
			assert.Contains(t, stderr.String(), `Try "`+tt.help+` --help" for usage.`)
			assert.Equal(t, 1, strings.Count(stderr.String(), "Try "), "got:\n%s", stderr.String())

			entries, err := os.ReadDir(dir)
			require.NoError(t, err)
			assert.Empty(t, entries, "a refused run writes nothing")
		})
	}
}

// TestRun_SingleDashArgumentsThatAreNotFlags pins what the refusal leaves to
// pflag: an argument pflag reads as a flag's value or as an operand is not a
// flag, whatever it is spelled like, and a run pflag refuses earlier reports
// that first problem.
func TestRun_SingleDashArgumentsThatAreNotFlags(t *testing.T) {
	t.Parallel()

	spec := writeFile(t, "spec.yaml", testspec.Tiny)
	tests := []struct {
		name   string
		args   []string
		reason string
	}{
		// --explain refuses -o before anything is written, which shows -opt was
		// read as -o's value.
		{"the value of a short flag", []string{"compile", spec, "--explain", "", "-o", "-opt"},
			"--explain writes no document"},
		{"the value of a long flag", []string{"compile", spec, "--fail-on", "-pretty"},
			`invalid --fail-on "-pretty"`},
		{"an operand after --", []string{"compile", "--", "-opt"}, `read spec "-opt"`},
		{"an argument of an unknown command", []string{"bogus", "-opt"}, `unknown command "bogus"`},
		{"an argument after one pflag refuses", []string{"compile", spec, "--bogus", "-opt=a=1"},
			"unknown flag: --bogus"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var stdout, stderr bytes.Buffer

			assert.Equal(t, 2, run(tt.args, &stdout, &stderr))
			assert.Contains(t, stderr.String(), tt.reason)
			assert.NotContains(t, stderr.String(), "two dashes")
		})
	}
}

// TestRun_AJoinedValueIsReadOnce pins that looking at the arguments before
// cobra parses them sets nothing: an --opt value set twice is refused as a
// repeated key, so a look that set it would fail this run.
func TestRun_AJoinedValueIsReadOnce(t *testing.T) {
	t.Parallel()

	spec := writeFile(t, "spec.yaml", testspec.Tiny)
	var stdout, stderr bytes.Buffer

	require.Equal(t, 0, run([]string{"compile", spec, "--opt=grouping=path-prefix"}, &stdout, &stderr),
		"stderr: %s", stderr.String())
	assert.NotEmpty(t, stdout.String())
}
