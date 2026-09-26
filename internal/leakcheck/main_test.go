package leakcheck_test

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/internal/leakcheck"
)

// TestMain wires this package's own tests through the check it implements,
// which is what keeps internal/leakcheck itself inside the archtest guard
// that requires the call wherever a go statement appears.
func TestMain(m *testing.M) {
	leakcheck.Main(m)
}

// leakCheckChildEnv gates TestMain_ReportsALeak_Child: unset, it skips; set,
// it leaks on purpose. TestMain_ReportsALeak re-executes this test binary with
// and without it, which is the permanent reach test for this whole package —
// standing in for the plant-a-leak-and-delete-it cycle a one-off proof would
// otherwise leave no record of.
const leakCheckChildEnv = "LEAKCHECK_CHILD"

// TestMain_ReportsALeak_Child starts a goroutine blocked forever on a channel
// nothing else holds, once its parent asks it to, then returns without
// joining it — leaving exactly the leak this package exists to catch.
func TestMain_ReportsALeak_Child(t *testing.T) {
	if os.Getenv(leakCheckChildEnv) != "1" {
		t.Skipf("set %s=1 to run: this test intentionally leaks a goroutine", leakCheckChildEnv)
	}
	block := make(chan struct{})
	go func() {
		<-block
	}()
}

// TestMain_ReportsALeak re-executes this test binary, isolating the leak in a
// child process so the parent's own coverage profile and pass/fail stay this
// test's, not the leak's.
func TestMain_ReportsALeak(t *testing.T) {
	t.Run("leaking child fails and names the stack", func(t *testing.T) {
		t.Parallel()
		code, output := runChild(t, true)
		assert.NotZero(t, code, "a leaked goroutine must fail the run")
		assert.Contains(t, output, "TestMain_ReportsALeak_Child",
			"the leaked goroutine's stack must name the test that started it")
		assert.Contains(t, output, "leaked goroutine", "the one-line summary must be printed")
	})

	t.Run("non-leaking child passes", func(t *testing.T) {
		t.Parallel()
		// The sibling case: without this, "leaking child fails" could pass
		// because the harness fails every child for some unrelated reason.
		code, output := runChild(t, false)
		assert.Zero(t, code, "with nothing leaked, the run must pass; output:\n%s", output)
	})
}

// runChild re-executes this test binary in a child process restricted to
// TestMain_ReportsALeak_Child, and returns its exit code and combined output.
//
// No -test.* flag other than -test.run is passed, so the child never learns
// its parent's -test.coverprofile path (go test sets that internally) and
// cannot write to, and so clobber, it.
func runChild(t *testing.T, leak bool) (int, string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestMain_ReportsALeak_Child$")
	cmd.Env = os.Environ()
	if leak {
		cmd.Env = append(cmd.Env, leakCheckChildEnv+"=1")
	}
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out

	err := cmd.Run()
	require.NoError(t, ctx.Err(), "child did not finish within the bound; output so far:\n%s", out.String())
	return childExitCode(t, err), out.String()
}

// childExitCode reads the process exit code out of cmd.Run's error, which is
// nil on a clean exit and an *exec.ExitError otherwise.
func childExitCode(t *testing.T, err error) int {
	t.Helper()
	if err == nil {
		return 0
	}
	var exitErr *exec.ExitError
	require.ErrorAs(t, err, &exitErr, "runChild must fail only by the child's own exit code")
	return exitErr.ExitCode()
}
