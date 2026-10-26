package main

import (
	"bytes"
	"fmt"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// complete asks the shell-completion entry point what it would offer for args,
// and returns the suggestions and the directive that came with them. The entry
// point is cobra's hidden __complete command, the one every shell script
// morphic generates calls.
func complete(t *testing.T, args ...string) (suggestions []string, directive cobra.ShellCompDirective) {
	t.Helper()

	var stdout, stderr bytes.Buffer
	require.Equal(t, 0, run(t.Context(), append([]string{"__complete"}, args...), &stdout, &stderr),
		"stderr: %s", stderr.String())

	lines := strings.Split(strings.TrimSuffix(stdout.String(), "\n"), "\n")
	last := lines[len(lines)-1]
	_, err := fmt.Sscanf(last, ":%d", &directive)
	require.NoError(t, err, "the last line is the directive, got %q", last)
	return lines[:len(lines)-1], directive
}

func TestComplete_SpecOperandOffersSpecFiles(t *testing.T) {
	t.Parallel()

	for _, command := range []string{"compile", "validate"} {
		t.Run(command, func(t *testing.T) {
			t.Parallel()

			got, directive := complete(t, command, "")

			assert.Equal(t, []string{"yaml", "yml", "json"}, got)
			assert.Equal(t, cobra.ShellCompDirectiveFilterFileExt, directive,
				"the directive filters files by these extensions, not offers them as words")
		})
	}
}

func TestComplete_SecondOperandOffersNothing(t *testing.T) {
	t.Parallel()

	for _, command := range []string{"compile", "validate"} {
		t.Run(command, func(t *testing.T) {
			t.Parallel()

			got, directive := complete(t, command, "spec.yaml", "")

			assert.Empty(t, got)
			assert.Equal(t, cobra.ShellCompDirectiveNoFileComp, directive,
				"a spec takes one operand; offering files for a second would invite a misuse")
		})
	}
}

func TestComplete_OutputOffersJSONFiles(t *testing.T) {
	t.Parallel()

	got, directive := complete(t, "compile", "--output", "")

	assert.Equal(t, []string{"json"}, got)
	assert.Equal(t, cobra.ShellCompDirectiveFilterFileExt, directive)
}

// TestComplete_HelpOffersCommandsButNotItself holds both halves of the filter:
// the commands help can describe are offered with their summaries, while help
// and the completion entry point itself are not commands anyone asks about.
func TestComplete_HelpOffersCommandsButNotItself(t *testing.T) {
	t.Parallel()

	got, directive := complete(t, "help", "")

	names := make([]string, 0, len(got))
	for _, line := range got {
		name, summary, ok := strings.Cut(line, "\t")
		require.True(t, ok, "every offer carries its summary: %q", line)
		assert.NotEmpty(t, summary)
		names = append(names, name)
	}
	assert.ElementsMatch(t, []string{"compile", "validate", "completion"}, names)
	assert.Equal(t, cobra.ShellCompDirectiveNoFileComp, directive)

	again, directive := complete(t, "help", "compile", "")
	assert.Empty(t, again, "help takes one topic")
	assert.Equal(t, cobra.ShellCompDirectiveNoFileComp, directive)
}
