package load

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"runtime/debug"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// childMaxStack bounds a child's goroutine stacks, so a runaway recursion
// overflows at once rather than outlasting childTimeout, under -race too.
const childMaxStack = 64 << 20

// childTimeout bounds one child. None is meant to come near it: a child still
// running then has hung, which no test here asks about.
const childTimeout = 2 * time.Minute

// childEnd is how a child stopped.
type childEnd string

const (
	childPassed     childEnd = "passed"     // its test passed
	childOverflowed childEnd = "overflowed" // the runtime's fatal stack overflow
	childPanicked   childEnd = "panicked"   // a panic nothing recovered
)

// asChild returns what the parent handed this process under env, and bounds
// its stacks, or reports false when this process is no such child.
func asChild(env string) (string, bool) {
	value := os.Getenv(env)
	if value == "" {
		return "", false
	}
	debug.SetMaxStack(childMaxStack)
	return value, true
}

// inChild runs test alone in a child: a re-run of this test binary, told by env,
// which asChild reads, that it is the child and what to work on. A stack
// overflow, or a panic the library does not recover, ends the process, so a
// test that asks whether one happens asks it in a child. inChild returns what
// the child printed and how it stopped; a child that stops any other way,
// failing its test, hanging or never starting, fails t.
func inChild(t *testing.T, test, env, value string) (string, childEnd) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), childTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^"+test+"$")
	cmd.Env = append(os.Environ(), env+"="+value)

	out, err := cmd.CombinedOutput()

	end, ok := childEndOf(err, string(out))
	require.True(t, ok, "the child stopped in a way no test here asks about (%v):\n%.1500s", err, out)
	return string(out), end
}

// childEndOf reads how a child that returned err, having printed out, stopped.
// The runtime ends a process with status 2 on both a fatal error and a panic,
// so its own text tells them apart.
func childEndOf(err error, out string) (childEnd, bool) {
	if err == nil {
		return childPassed, true
	}
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 2 {
		return "", false
	}
	switch {
	case strings.Contains(out, "fatal error: stack overflow"):
		return childOverflowed, true
	case strings.HasPrefix(out, "panic: ") || strings.Contains(out, "\npanic: "):
		return childPanicked, true
	}
	return "", false
}
