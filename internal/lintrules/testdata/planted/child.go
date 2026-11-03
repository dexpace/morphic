// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package planted

import (
	"context"
	"os"
	"os/exec"
	"syscall"
)

// plantChildCommands names each starter of a child the forbid entries refuse,
// beside the os/exec names a test may still use.
func plantChildCommands() {
	_ = exec.Command("x")                              // want "use of `exec.Command` forbidden because \"start a child through exectest.Command\" (forbidigo)"
	_ = exec.CommandContext(context.Background(), "x") // want "use of `exec.CommandContext` forbidden because \"start a child through exectest.Command\" (forbidigo)"
	_ = &exec.Cmd{Path: "x"}                           // want "use of `exec.Cmd` forbidden because \"start a child through exectest.Command\" (forbidigo)"
	_, _ = os.StartProcess("x", nil, nil)              // want "use of `os.StartProcess` forbidden because \"start a child through exectest.Command\" (forbidigo)"
	_, _ = syscall.ForkExec("x", nil, nil)             // want "use of `syscall.ForkExec` forbidden because \"start a child through exectest.Command\" (forbidigo)"
	_, _, _ = syscall.StartProcess("x", nil, nil)      // want "use of `syscall.StartProcess` forbidden because \"start a child through exectest.Command\" (forbidigo)"
	_ = syscall.Exec("x", nil, nil)                    // want "use of `syscall.Exec` forbidden because \"start a child through exectest.Command\" (forbidigo)"
	var _ *exec.ExitError
	_, _ = exec.LookPath("x")
}
