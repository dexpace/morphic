package main

import (
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/dexpace/morphic/engine"
)

// Exit codes. They are a contract with CI wrappers: 1 says the spec has
// findings, 2 says morphic was invoked wrong, could not do its I/O, or was
// stopped by a signal.
const (
	exitFindings = 1
	exitFailure  = 2
)

// exitError ends the run with a chosen exit code. A nil err means the reason
// was already reported, as diagnostics are, so nothing more is printed.
type exitError struct {
	code int
	err  error
}

func (e *exitError) Error() string {
	if e.err == nil {
		return ""
	}
	return e.err.Error()
}

func (e *exitError) Unwrap() error { return e.err }

// exitWith returns the error that ends a run with code, or nil for 0.
func exitWith(code int) error {
	if code == 0 {
		return nil
	}
	return &exitError{code: code}
}

// failure wraps err as an I/O or internal failure: exit 2, message reported.
func failure(err error) error {
	return &exitError{code: exitFailure, err: err}
}

// usageError is a misuse of the command at path: exit 2, message reported with
// a pointer to that command's help.
type usageError struct {
	path string
	err  error
}

func (e *usageError) Error() string { return e.err.Error() }

func (e *usageError) Unwrap() error { return e.err }

// asMisuse classifies err as a misuse of cmd. It has the shape cobra's
// SetFlagErrorFunc wants, so a flag it refuses is classified the same way.
func asMisuse(cmd *cobra.Command, err error) error {
	return &usageError{path: cmd.CommandPath(), err: err}
}

// misuse reports a misuse of cmd. msg is a finished string, not a format, so a
// value echoed back from the user cannot be read as a verb.
func misuse(cmd *cobra.Command, msg string) error {
	return asMisuse(cmd, errors.New(msg))
}

// exitCode maps what Execute returned to the process exit code. Anything that
// is not an exitError — cobra's own refusals of the arguments — is misuse.
func exitCode(err error) int {
	if err == nil {
		return 0
	}
	if ee, ok := errors.AsType[*exitError](err); ok {
		return ee.code
	}
	return exitFailure
}

// reportError writes what ended the run to w, as plain lines a log parser can
// read: the reason, then for a misuse the help page to read. A run that ended
// well, or with findings the diagnostics already reported, writes nothing.
func reportError(w io.Writer, err error) {
	var ee *exitError
	if err == nil || errors.As(err, &ee) && ee.err == nil {
		return
	}

	emitf(w, "morphic: %s\n", strings.TrimRight(err.Error(), "\n"))
	reportViolations(w, err)
	if hint := usageHint(err); hint != "" {
		emitf(w, "%s\n", hint)
	}
}

// Bounds on what reportViolations writes. A verified document can break an
// invariant at every node, and both the path and the message carry the
// document's own text.
const (
	maxViolationLines = 50
	maxViolationPath  = 200
	maxViolationText  = 400
)

// reportViolations writes, a line apiece, the invariants a verified run found
// broken, when that is what ended it: the error names the first few, and
// someone developing a stage needs the rest to know what to fix.
func reportViolations(w io.Writer, err error) {
	verr, ok := errors.AsType[*engine.VerifyError](err)
	if !ok {
		return
	}
	shown := min(len(verr.Violations), maxViolationLines)
	for _, v := range verr.Violations[:shown] {
		emitf(w, "  %s at %.*q: %.*s\n", v.Code, maxViolationPath, v.Path, maxViolationText, v.Message)
	}
	if more := len(verr.Violations) - shown; more > 0 {
		emitf(w, "  and %d more\n", more)
	}
}

// usageHint returns the line that points a misuse at the right help page, or ""
// when err is not a misuse. A refusal cobra made before any command was chosen
// carries no path, and points at morphic itself.
func usageHint(err error) string {
	if _, ok := errors.AsType[*exitError](err); ok {
		return ""
	}
	path := "morphic"
	if ue, ok := errors.AsType[*usageError](err); ok {
		path = ue.path
	}
	return fmt.Sprintf("Try %q for usage.", path+" --help")
}
