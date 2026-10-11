package main

import (
	"errors"
	"fmt"
	"io"
	"strings"
	"unicode"
	"unicode/utf8"

	"charm.land/fang/v2"
	"github.com/charmbracelet/colorprofile"
	"github.com/spf13/cobra"
)

// Exit codes. They are a contract with CI wrappers: 1 says the spec has
// findings, 2 says morphic was invoked wrong or could not do its I/O.
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
	var ee *exitError
	if errors.As(err, &ee) {
		return ee.code
	}
	return exitFailure
}

// errorHandler reports err to w. Off a terminal it writes plain lines a log
// parser can read; on one it uses fang's styled block. fang's own handler
// cannot tell the two apart, because the writer it is handed has no file
// descriptor to ask.
func errorHandler(w io.Writer, styles fang.Styles, err error) {
	var ee *exitError
	if errors.As(err, &ee) && ee.err == nil {
		return
	}

	msg := strings.TrimRight(err.Error(), "\n")
	hint := usageHint(err)
	if cw, ok := w.(*colorprofile.Writer); ok && cw.Profile > colorprofile.NoTTY {
		writeStyledError(w, styles, msg, hint)
		return
	}

	emitf(w, "morphic: %s\n", msg)
	if hint != "" {
		emitf(w, "%s\n", hint)
	}
}

// usageHint returns the line that points a misuse at the right help page, or ""
// when err is not a misuse. A refusal cobra made before any command was chosen
// carries no path, and points at morphic itself.
func usageHint(err error) string {
	var ee *exitError
	if errors.As(err, &ee) {
		return ""
	}
	path := "morphic"
	var ue *usageError
	if errors.As(err, &ue) {
		path = ue.path
	}
	return fmt.Sprintf("Try %q for usage.", path+" --help")
}

// writeStyledError renders msg as fang's error block: a header, the message,
// and the hint when there is one.
//
// fang's text style also title-cases the message's whole first word, which turns
// "--explain" into "--Explain" and "OpenAPI.yaml" into "Openapi.yaml". It is
// dropped here, and the first letter raised on its own.
func writeStyledError(w io.Writer, styles fang.Styles, msg, hint string) {
	text := styles.ErrorText.UnsetTransform()
	emitf(w, "%s\n%s\n\n", styles.ErrorHeader.String(), text.Render(upperFirst(msg)))
	if hint != "" {
		emitf(w, "%s\n\n", text.UnsetWidth().Render(hint))
	}
}

// upperFirst raises s's first letter and changes nothing else. A string that
// does not start with a letter, an empty one and invalid UTF-8 included, is
// returned as it is: decoding any of those yields U+FFFD, which is not one.
func upperFirst(s string) string {
	r, size := utf8.DecodeRuneInString(s)
	if !unicode.IsLetter(r) {
		return s
	}
	return string(unicode.ToUpper(r)) + s[size:]
}
