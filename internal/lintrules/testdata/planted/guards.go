package planted

import (
	"context"
	"errors"
)

// BadSentinel is an exported sentinel not spelled ErrXxx.
var BadSentinel = errors.New("bad") // want "the sentinel error name `BadSentinel` should conform to the `ErrXxx` format (errname)"

// badThing is an error type not spelled xxxError.
type badThing struct{} // want "the error type name `badThing` should conform to the `xxxError` format (errname)"

// Error makes badThing an error.
func (badThing) Error() string { return "badThing" }

// holdsContext stores a context in a struct.
type holdsContext struct {
	ctx context.Context // want "found a struct that contains a context.Context field (containedctx)"
}

// noPeriod has a doc comment that is no sentence // want "Comment should end in a period (godot)"
var noPeriod = 1

// plantGuards draws the guards' findings.
func plantGuards(v any, len int) (int, error) { // want "param len has same name as predeclared identifier (predeclared)"
	n := v.(int) // want "type assertion must be checked (forcetypeassert)"
	m, ok := v.(int)
	if !ok {
		return n + len, badThing{}
	}
	_ = holdsContext{}
	_ = noPeriod
	return m, nil
}
