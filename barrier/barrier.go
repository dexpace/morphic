package barrier

import (
	"bytes"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
)

// maxStack bounds the stack text a PanicError keeps, so a panic raised deep in
// a recursion holds a page of it rather than the whole.
const maxStack = 64 << 10

// maxFrames bounds the frames searched for the panic's site. The site is the
// first frame past the runtime's own, within a handful of the top.
const maxFrames = 32

// errNoFunction reports a Catch handed nothing to call.
var errNoFunction = errors.New("barrier: no function to call")

// PanicError is the error a recovered panic becomes. Its message is one line of
// bounded length whatever the panic value was; the value whole, its text, and
// the stack cut at 64 KiB stay beside it, for the caller that wants them.
type PanicError struct {
	value any
	text  string
	msg   string
	stack []byte
}

// Catch calls f and returns nil when it returns, or the *PanicError describing
// the panic that ended it. A nil f is refused with an error of its own, since
// calling it would blame f for the caller's mistake.
//
// runtime.Goexit is not a panic and passes through, ending the goroutine as it
// would without Catch.
func Catch(f func()) (err error) {
	if f == nil {
		return errNoFunction
	}

	completed := false
	defer func() {
		if completed {
			return
		}
		// A recover that returns nil is a Goexit, which the deferred call does
		// not stop, or a panic(nil) under panicnil=1, which it does: the second
		// is a panic like any other, and the first never reads the result.
		err = newPanicError(recover())
	}()
	f()
	completed = true
	return nil
}

// newPanicError describes the panic value r, reading its text once. It runs in
// the deferred call that recovered r, so the stack it reads is the panic's own.
func newPanicError(r any) *PanicError {
	text := describe(r)
	msg := "panic: " + render(text, visible)
	if site := siteOf(); site != "" {
		msg += " (at " + site + ")"
	}
	return &PanicError{value: r, text: render(text, asIs), msg: msg, stack: stackOf()}
}

// Error returns the panic as one line: its value, then where it was raised.
// Control characters are replaced with U+FFFD, since an error's text is
// printed as it is.
func (e *PanicError) Error() string { return e.msg }

// Text returns the panic value's text as Line renders it, for a caller that
// words a message of its own around it: the text Error carries, with control
// characters kept for the message's renderer to escape.
func (e *PanicError) Text() string { return e.text }

// Unwrap returns the panic value when it is an error, so errors.Is and
// errors.As see through the panic to what was raised.
func (e *PanicError) Unwrap() error {
	err, _ := e.value.(error)
	return err
}

// Value returns what the code panicked with.
func (e *PanicError) Value() any { return e.value }

// Stack returns the goroutine's stack as the panic left it, cut at 64 KiB. The
// caller owns the copy.
func (e *PanicError) Stack() []byte { return bytes.Clone(e.stack) }

// siteOf names where the panic was raised: the first frame of the stack that
// is neither the runtime's nor this package's, as function, file and line. It
// is empty when the top frames hold none.
func siteOf() string {
	var pcs [maxFrames]uintptr
	n := runtime.Callers(1, pcs[:])
	frames := runtime.CallersFrames(pcs[:n])
	self := reflect.TypeFor[PanicError]().PkgPath() + "."

	for {
		frame, more := frames.Next()
		own := strings.HasPrefix(frame.Function, "runtime.") || strings.HasPrefix(frame.Function, self)
		if frame.Function != "" && !own {
			return fmt.Sprintf("%s %s:%d", frame.Function, filepath.Base(frame.File), frame.Line)
		}
		if !more {
			return ""
		}
	}
}

// stackOf returns the calling goroutine's stack, cut at maxStack, in a slice
// that holds no more than it uses.
func stackOf() []byte {
	buf := make([]byte, maxStack)
	return bytes.Clone(buf[:runtime.Stack(buf, false)])
}
