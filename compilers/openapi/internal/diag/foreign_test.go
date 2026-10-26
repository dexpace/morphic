package diag_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dexpace/morphic/barrier"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
)

// nilReceiverError implements error on a pointer receiver that reads through
// itself, so a typed nil of it panics when its text is read.
type nilReceiverError struct{ msg string }

func (e *nilReceiverError) Error() string { return e.msg }

// TestOneLine_QuotesAsBarrierLineDoes holds OneLine to barrier.Line, whose
// tests pin the joins, the bound, the cut on a rune boundary and the work: a
// diagnostic quotes a library's error as any message quotes what code the
// caller does not own says.
func TestOneLine_QuotesAsBarrierLineDoes(t *testing.T) {
	t.Parallel()
	var nilPtr *nilReceiverError
	for name, err := range map[string]error{
		"a header and its items": errors.New("yaml: unmarshal errors:\n  line 1: bad\n  line 2: worse"),
		"a megabyte of lines":    errors.New(strings.Repeat("a line of complaint\n", 1<<16)),
		"runes past the cut":     errors.New(strings.Repeat("é", 1<<12)),
		"a control character":    errors.New("key M\x1b[31mY not found"),
		"a typed nil":            nilPtr,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got := diag.OneLine(err)
			assert.Equal(t, barrier.Line(err), got)
			assert.NotContains(t, got, "\n")
		})
	}
}

func TestCatch_ReportsNothingWhenTheCallReturns(t *testing.T) {
	t.Parallel()
	calls := 0

	text, panicked := diag.Catch(func() { calls++ })

	assert.False(t, panicked)
	assert.Empty(t, text)
	assert.Equal(t, 1, calls, "Catch calls the function exactly once")
}

// TestCatch_QuotesAPanicAsOneBoundedLine pins what every barrier of the
// compiler's puts in its message: the value as OneLine quotes an error carrying
// the same text, one line and a small part of the whole, with nothing of where
// the panic was raised, which a diagnostic about the source does not carry.
func TestCatch_QuotesAPanicAsOneBoundedLine(t *testing.T) {
	t.Parallel()
	huge := strings.Repeat("the dependency faulted\n", 1<<10)

	text, panicked := diag.Catch(func() { panic(huge) })

	assert.True(t, panicked)
	assert.Equal(t, diag.OneLine(errors.New(huge)), text)
	assert.NotContains(t, text, "\n")
	assert.Less(t, len(text), len(huge)/4, "the bound cut the value")
	assert.True(t, strings.HasPrefix(text, "the dependency faulted; the dependency faulted"), text)
	assert.NotContains(t, text, " (at ", "the panic's site stays out")
}

func TestCatch_QuotesWhatAPanicOfAnyValueSays(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		f    func()
		want string
	}{
		{"an error", func() { panic(errors.New("first\nsecond")) }, "first; second"},
		{"a runtime error", func() { s := []int{}; i := 3; _ = s[i] }, "runtime error: index out of range [3] with length 0"},
		{"a value of no text", func() { panic(struct{ N int }{1}) }, "struct { N int }"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			text, panicked := diag.Catch(tt.f)
			assert.True(t, panicked)
			assert.Equal(t, tt.want, text)
		})
	}
}

func TestCatch_ReportsNoFunctionAsBarrierRefusesIt(t *testing.T) {
	t.Parallel()
	text, panicked := diag.Catch(nil)

	assert.True(t, panicked, "a call that cannot run did not return either")
	assert.Equal(t, diag.OneLine(barrier.Catch(nil)), text)
}
