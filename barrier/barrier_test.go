package barrier_test

import (
	"errors"
	"fmt"
	"runtime"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/barrier"
)

// caught runs f under Catch and returns the *PanicError it reports, failing the
// test when f does not panic.
func caught(t *testing.T, f func()) *barrier.PanicError {
	t.Helper()
	err := barrier.Catch(f)
	require.Error(t, err)
	perr, ok := errors.AsType[*barrier.PanicError](err)
	require.True(t, ok, "Catch returned %T, not a *PanicError", err)
	return perr
}

// raise panics with v from a function of its own, so a panic's site has a name
// to be checked against.
func raise(v any) { panic(v) }

// stringer is a panic value that renders itself.
type stringer struct{ text string }

func (s stringer) String() string { return s.text }

// brokenTextError is a panic value whose error text panics when read.
type brokenTextError struct{}

func (brokenTextError) Error() string { panic("rendering the panic value") }

// nilReceiverError implements error on a pointer receiver that reads through itself.
type nilReceiverError struct{ msg string }

func (e *nilReceiverError) Error() string { return e.msg }

// store writes to m, which a test hands nil.
func store(m map[string]int) { m["a"] = 1 }

// kilo is a named integer, a scalar with no String method.
type kilo int

// label is a named string with no String method.
type label string

func TestCatch_ReturnsNilWhenTheFunctionReturns(t *testing.T) {
	t.Parallel()
	calls := 0

	err := barrier.Catch(func() { calls++ })

	require.NoError(t, err)
	assert.Equal(t, 1, calls, "Catch calls the function exactly once")
}

func TestCatch_RefusesNoFunction(t *testing.T) {
	t.Parallel()

	err := barrier.Catch(nil)

	require.Error(t, err)
	assert.NotErrorAs(t, err, new(*barrier.PanicError),
		"a missing function is the caller's mistake, not a panic of the function's")
}

func TestCatch_ReportsAPanicWithItsValue(t *testing.T) {
	t.Parallel()
	sentinel := errors.New("sentinel")
	tests := []struct {
		name  string
		value any
		text  string
	}{
		{"string", "boom", "boom"},
		{"error", sentinel, "sentinel"},
		{"wrapped error", fmt.Errorf("outer: %w", sentinel), "outer: sentinel"},
		{"stringer", stringer{"as a stringer"}, "as a stringer"},
		{"int", 42, "42"},
		{"named int", kilo(1000), "1000"},
		{"named string", label("tagged"), "tagged"},
		{"unsigned", uint8(7), "7"},
		{"float", 1.5, "1.5"},
		{"complex", complex(1, 2), "(1+2i)"},
		{"bool", true, "true"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			perr := caught(t, func() { panic(tt.value) })

			assert.Equal(t, tt.value, perr.Value())
			assert.True(t, strings.HasPrefix(perr.Error(), "panic: "+tt.text),
				"message %q should open with the value's text %q", perr.Error(), tt.text)
		})
	}
}

func TestCatch_NamesAnUnrenderableValueByItsType(t *testing.T) {
	t.Parallel()
	loop := map[string]any{}
	loop["self"] = loop
	ring := make([]any, 1)
	ring[0] = ring
	tests := []struct {
		name  string
		value any
		text  string
	}{
		{"struct", struct{ N int }{1}, "struct { N int }"},
		{"pointer", &struct{ N int }{1}, "*struct { N int }"},
		{"map holding itself", loop, "map[string]interface {}"},
		{"slice holding itself", ring, "[]interface {}"},
		{"func", func() {}, "func()"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			perr := caught(t, func() { panic(tt.value) })

			assert.True(t, strings.HasPrefix(perr.Error(), "panic: "+tt.text+" ("),
				"message %q should name the type %q and nothing of the value", perr.Error(), tt.text)
		})
	}
}

func TestCatch_SurvivesAValueWhoseTextPanics(t *testing.T) {
	t.Parallel()
	var nilPtr *nilReceiverError
	tests := []struct {
		name  string
		value any
		text  string
	}{
		{"error whose Error panics", brokenTextError{}, "barrier_test.brokenTextError whose text panicked"},
		{"typed nil error", nilPtr, "*barrier_test.nilReceiverError whose text panicked"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			perr := caught(t, func() { panic(tt.value) })

			assert.True(t, strings.HasPrefix(perr.Error(), "panic: "+tt.text), perr.Error())
			assert.Equal(t, tt.value, perr.Value(), "the value is kept whatever it renders as")
		})
	}
}

func TestCatch_ReportsARuntimeError(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		f    func()
		text string
	}{
		{"nil dereference", func() { var p *int; _ = *p }, "runtime error: invalid memory address"},
		{"index out of range", func() { s := []int{}; i := 3; _ = s[i] }, "runtime error: index out of range [3] with length 0"},
		{"write to a nil map", func() { store(nil) }, "assignment to entry in nil map"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			perr := caught(t, tt.f)

			assert.Contains(t, perr.Error(), tt.text)
			var rerr runtime.Error
			assert.ErrorAs(t, perr, &rerr, "the runtime's error stays reachable through the PanicError")
		})
	}
}

func TestCatch_ReportsAPanicOfNil(t *testing.T) {
	t.Parallel()

	perr := caught(t, func() { panic(nil) })

	var nilErr *runtime.PanicNilError
	assert.ErrorAs(t, perr, &nilErr, "Go 1.21 turns panic(nil) into an error of its own")
}

func TestPanicError_UnwrapReachesWhatWasRaised(t *testing.T) {
	t.Parallel()
	sentinel := errors.New("sentinel")

	fromError := caught(t, func() { panic(fmt.Errorf("outer: %w", sentinel)) })
	fromString := caught(t, func() { panic("boom") })

	assert.ErrorIs(t, fromError, sentinel)
	assert.NoError(t, fromString.Unwrap(), "a panic of a string wraps no error")
}

func TestCatch_MessageIsOneLine(t *testing.T) {
	t.Parallel()
	// Each line break Unicode makes mandatory ends a line, and the white space
	// within a line is a run of spaces and a tab.
	text := "first\nsecond\r\nthird\tfourth\vfifth\fsixth\u0085seventh\u2028eighth\u2029ninth   tenth"

	perr := caught(t, func() { panic(text) })

	assert.Equal(t,
		"panic: first; second; third fourth; fifth; sixth; seventh; eighth; ninth tenth (at "+siteOf(perr)+")",
		perr.Error())
}

// siteOf returns the "(at ...)" part of a message, without its parentheses.
func siteOf(perr *barrier.PanicError) string {
	_, site, _ := strings.Cut(perr.Error(), " (at ")
	return strings.TrimSuffix(site, ")")
}

func TestCatch_NeutralizesControlCharacters(t *testing.T) {
	t.Parallel()
	// ESC opens a terminal escape, NUL ends a C string, BEL beeps, DEL and the
	// C1 range are controls too.
	text := "red \x1b[31mred\x00 bell\x07 del\x7f c1\u0090 end"

	perr := caught(t, func() { panic(text) })

	for _, r := range perr.Error() {
		assert.False(t, r < ' ' || r == 0x7f || (r >= 0x80 && r < 0xa0), "control %U survived in %q", r, perr.Error())
	}
	assert.Contains(t, perr.Error(), "red \uFFFD[31mred\uFFFD bell\uFFFD del\uFFFD c1\uFFFD end")
}

func TestCatch_SaysSomethingPanickedWhateverTheValueSays(t *testing.T) {
	t.Parallel()
	for name, value := range map[string]any{
		"an empty string": "",
		"white space":     " \t\n ",
		"an empty error":  errors.New(""),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			perr := caught(t, func() { panic(value) })

			assert.True(t, strings.HasPrefix(perr.Error(), "panic: "+barrier.NoText+" ("), perr.Error())
		})
	}
}

func TestCatch_RepairsInvalidUTF8(t *testing.T) {
	t.Parallel()

	perr := caught(t, func() { panic("bad \xff\xfe bytes, half a rune \xe2\x82") })

	assert.True(t, utf8.ValidString(perr.Error()), "%q is not valid UTF-8", perr.Error())
	assert.Contains(t, perr.Error(), "bad \uFFFD bytes, half a rune \uFFFD")
}

func TestCatch_BoundsTheMessage(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		text   string
		elided bool
	}{
		{"exactly the bound", strings.Repeat("a", barrier.MaxText), false},
		{"one past the bound", strings.Repeat("a", barrier.MaxText+1), true},
		{"a megabyte on one line", strings.Repeat("x", 1<<20), true},
		{"a megabyte of lines", strings.Repeat("a line\n", 1<<17), true},
		{"a megabyte of white space before the text", strings.Repeat(" ", 1<<20) + "tail", true},
		{"sixty-four megabytes", strings.Repeat("y", 64<<20), true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			perr := caught(t, func() { panic(tt.text) })

			text, _, _ := strings.Cut(strings.TrimPrefix(perr.Error(), "panic: "), " (at ")
			assert.LessOrEqual(t, len(text), barrier.MaxText+len(barrier.ElidedMarker))
			assert.Equal(t, tt.elided, strings.HasSuffix(text, barrier.ElidedMarker), "elided marker on %.40q", text)
			assert.NotContains(t, perr.Error(), "\n")
		})
	}
}

func TestCatch_CutsOnARuneBoundary(t *testing.T) {
	t.Parallel()
	// A euro sign is three bytes and neither bound is a multiple of three, so
	// each cut lands inside a rune unless it backs up to where one starts.
	require.NotZero(t, barrier.MaxText%3)

	perr := caught(t, func() { panic(strings.Repeat("€", 10_000)) })

	text, _, _ := strings.Cut(strings.TrimPrefix(perr.Error(), "panic: "), " (at ")
	kept := strings.TrimSuffix(text, barrier.ElidedMarker)
	assert.NotEqual(t, text, kept, "the text was cut")
	assert.Equal(t, strings.Repeat("€", len(kept)/3), kept, "only whole runes are kept")
	assert.LessOrEqual(t, len(kept), barrier.MaxText)
	assert.Greater(t, len(kept), barrier.MaxText-3, "and the cut backs up no further than it must")
}

func TestCatch_ReadCutLeavesNoStrayRune(t *testing.T) {
	t.Parallel()
	// White space fills the read up to the middle of a euro sign. Cut there, its
	// first byte would be repaired into a replacement character that the text
	// never held, ahead of the marker.
	text := strings.Repeat(" ", barrier.ScanLimit-1) + "€"

	perr := caught(t, func() { panic(text) })

	assert.True(t, strings.HasPrefix(perr.Error(), "panic: "+barrier.ElidedMarker+" ("), perr.Error())
}

func TestCatch_SiteNamesWhereThePanicWasRaised(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		f    func()
		fn   string
	}{
		{"an explicit panic", func() { raise("boom") }, "barrier_test.raise"},
		{"a runtime panic", func() { var p *int; _ = *p }, "barrier_test.TestCatch_SiteNamesWhereThePanicWasRaised.func"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			site := siteOf(caught(t, tt.f))

			assert.Contains(t, site, tt.fn)
			assert.Regexp(t, `^\S+ barrier_test\.go:\d+$`, site, "the site reads function, then the file's base name and line")
			assert.NotContains(t, site, "runtime.", "the runtime's frames are not where the panic was raised")
			assert.NotContains(t, site, "barrier.Catch", "nor are the barrier's own")
		})
	}
}

func TestCatch_NamesNoSiteWhereNoFrameIsTheCallers(t *testing.T) {
	t.Parallel()

	err := barrier.PanicWithNoCallerFrame()

	require.Error(t, err)
	assert.Equal(t, "panic: bare", err.Error())
}

func TestPanicError_StackHoldsThePanicsFrames(t *testing.T) {
	t.Parallel()

	perr := caught(t, func() { raise("boom") })
	stack := perr.Stack()

	assert.Contains(t, string(stack), "barrier_test.raise")
	assert.LessOrEqual(t, len(stack), barrier.MaxStack)

	clear(stack)
	assert.Contains(t, string(perr.Stack()), "barrier_test.raise", "the caller owns the copy it was given")
}

func TestPanicError_StackIsBounded(t *testing.T) {
	t.Parallel()
	var deep func(n int)
	deep = func(n int) {
		if n == 0 {
			panic("bottom")
		}
		deep(n - 1)
	}

	perr := caught(t, func() { deep(5_000) })

	assert.LessOrEqual(t, len(perr.Stack()), barrier.MaxStack, "5,000 frames deep, and still within the bound")
	assert.Contains(t, string(perr.Stack()), "TestPanicError_StackIsBounded", "the stack still names the code that panicked")
	assert.Contains(t, perr.Error(), "bottom")
}

func TestCatch_PassesGoexitThrough(t *testing.T) {
	t.Parallel()
	t.Run("goexit", func(t *testing.T) {
		t.Parallel()
		// SkipNow ends the goroutine with runtime.Goexit. Swallowed by Catch, the
		// subtest would run on and fail on the next line instead of ending skipped.
		_ = barrier.Catch(t.SkipNow)
		t.Error("Catch stopped the goroutine's exit")
	})
}

func TestCatch_NestsAndStaysUsable(t *testing.T) {
	t.Parallel()
	var inner error

	outer := barrier.Catch(func() {
		inner = barrier.Catch(func() { panic("inner") })
	})

	require.NoError(t, outer, "the inner barrier took the panic")
	require.Error(t, inner)
	assert.Contains(t, inner.Error(), "inner")
	assert.NoError(t, barrier.Catch(func() {}), "and Catch works after a panic")
}
