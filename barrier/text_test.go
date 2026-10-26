package barrier_test

import (
	"errors"
	"strings"
	"testing"
	"unicode"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/barrier"
)

// lineBreaks are the runes that end a line in Unicode's mandatory breaks, which
// no rendered text may hold.
const lineBreaks = "\n\r\v\f\u0085\u2028\u2029"

func TestLine_JoinsTheLinesALibraryWrote(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, in, want string
	}{
		{"already one line", "plain failure", "plain failure"},
		{"flat list", "first problem\nsecond problem", "first problem; second problem"},
		{"header and items", "yaml: unmarshal errors:\n  line 1: bad\n  line 2: worse",
			"yaml: unmarshal errors: line 1: bad; line 2: worse"},
		{"blank lines dropped", "one\n\n \t\ntwo", "one; two"},
		{"white space collapsed", "one\n\t  two   three", "one; two three"},
		{"trailing newline", "only\n", "only"},
		{"carriage returns alone", "one\rtwo", "one; two"},
		{"every mandatory break", "a\nb\r\nc\vd\fe\u0085f\u2028g\u2029h", "a; b; c; d; e; f; g; h"},
		{"a colon heads one line only", "head:\nitem\nnext", "head: item; next"},
		{"empty", "", barrier.NoText},
		{"white space alone", " \n\t\r\n ", barrier.NoText},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, barrier.Line(errors.New(tt.in)))
			assert.Equal(t, tt.want, barrier.Line(tt.in), "a string renders as an error carrying it does")
		})
	}
}

func TestLine_RendersAnyValueWithoutTrustingIt(t *testing.T) {
	t.Parallel()
	loop := map[string]any{}
	loop["self"] = loop
	var nilPtr *nilReceiverError
	tests := []struct {
		name  string
		value any
		want  string
	}{
		{"error", errors.New("failed"), "failed"},
		{"stringer", stringer{"as a stringer"}, "as a stringer"},
		{"named int", kilo(1000), "1000"},
		{"map holding itself", loop, "map[string]interface {}"},
		{"error whose Error panics", brokenTextError{}, "barrier_test.brokenTextError whose text panicked"},
		{"typed nil error", nilPtr, "*barrier_test.nilReceiverError whose text panicked"},
		{"nil", nil, "<nil>"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, barrier.Line(tt.value))
		})
	}
}

func TestLine_KeepsControlCharactersAnErrorReplaces(t *testing.T) {
	t.Parallel()
	text := "red \x1b[31mred\x00 bell\x07 del\x7f c1\u0090 end"

	perr := caught(t, func() { panic(text) })

	assert.Equal(t, text, barrier.Line(text), "a message's renderer escapes what Line keeps")
	assert.Equal(t, text, perr.Text(), "Text is Line's")
	assert.Contains(t, perr.Error(), "red �[31mred� bell� del� c1� end",
		"an error's text is printed as it is, so Error replaces them")
}

func TestPanicError_TextIsTheValuesLine(t *testing.T) {
	t.Parallel()
	for name, value := range map[string]any{
		"a string":             "boom",
		"an error":             errors.New("first\nsecond"),
		"lines past a bound":   strings.Repeat("a line of panic\n", 1<<12),
		"a value with no text": "",
		"an unprintable value": struct{ N int }{1},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			perr := caught(t, func() { panic(value) })
			assert.Equal(t, barrier.Line(value), perr.Text())
			assert.True(t, strings.HasPrefix(perr.Error(), "panic: "+perr.Text()+" ("),
				"Error carries the same text: %q", perr.Error())
		})
	}
}

// countingError counts the reads of its text, each of which says how many
// there have been.
type countingError struct{ reads *int }

func (e countingError) Error() string {
	*e.reads++
	return strings.Repeat("read ", *e.reads)
}

// TestPanicError_ReadsTheValuesTextOnce pins that Text and Error come from one
// read of the value. A library's error text can cost seconds to build, and a
// value read twice can say two different things.
func TestPanicError_ReadsTheValuesTextOnce(t *testing.T) {
	t.Parallel()
	reads := 0

	perr := caught(t, func() { panic(countingError{&reads}) })

	assert.Equal(t, 1, reads)
	assert.Equal(t, "read", perr.Text())
	assert.True(t, strings.HasPrefix(perr.Error(), "panic: read ("), perr.Error())
}

func TestLine_BoundsTheText(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		text   string
		elided bool
	}{
		{"exactly the bound", strings.Repeat("a", barrier.MaxText), false},
		{"one past the bound", strings.Repeat("a", barrier.MaxText+1), true},
		{"words that fill the bound", strings.Repeat("a ", (barrier.MaxText-2)/2) + "aa", false},
		{"a word past words that fill it", strings.Repeat("a ", (barrier.MaxText-2)/2) + "aa b", true},
		{"joined past the bound", strings.Repeat("ab\n", barrier.MaxText/3+1), true},
		{"a megabyte on one line", strings.Repeat("x", 1<<20), true},
		{"a megabyte of lines", strings.Repeat("a line\n", 1<<17), true},
		{"a megabyte of white space before the text", strings.Repeat(" ", 1<<20) + "tail", true},
		{"sixty-four megabytes", strings.Repeat("y", 64<<20), true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := barrier.Line(tt.text)
			assert.LessOrEqual(t, len(got), barrier.MaxText+len(barrier.ElidedMarker))
			assert.Equal(t, tt.elided, strings.HasSuffix(got, barrier.ElidedMarker), "elided marker on %.40q", got)
			assert.False(t, strings.ContainsAny(got, lineBreaks), "%.80q spans lines", got)
		})
	}
}

func TestLine_CutsOnARuneBoundary(t *testing.T) {
	t.Parallel()
	require.NotZero(t, barrier.MaxText%3, "a euro sign is three bytes, so a cut at the bound lands inside one")

	got := barrier.Line(strings.Repeat("€", 10_000))

	kept := strings.TrimSuffix(got, barrier.ElidedMarker)
	assert.NotEqual(t, got, kept, "the text was cut")
	assert.Equal(t, strings.Repeat("€", len(kept)/3), kept, "only whole runes are kept")
	assert.Greater(t, len(kept), barrier.MaxText-3, "and the cut backs up no further than it must")
}

// TestLine_IsBoundedInWorkNotOnlyOutput holds the bound to one on work: past
// it, the answer and the allocations no longer depend on how much more there
// was. Joining a whole text and cutting the result walks all of it, which is
// what costs time on a library's 1.2 GB error. Lines are joined only until the
// text is past the bound, so the smaller input of lines is under the read
// limit; a word is read whole, so one line is bounded by that limit alone. It
// is not run in parallel: AllocsPerRun measures the process.
func TestLine_IsBoundedInWorkNotOnlyOutput(t *testing.T) {
	tests := []struct {
		name         string
		unit         string
		smallReadsTo int
	}{
		{"many lines", "line\n", 2 * barrier.MaxText},
		{"one line", "x", 2 * barrier.ScanLimit},
	}
	for _, tt := range tests {
		name := tt.name
		small := strings.Repeat(tt.unit, tt.smallReadsTo/len(tt.unit))
		large := strings.Repeat(tt.unit, 1024*barrier.ScanLimit/len(tt.unit))

		assert.Equal(t, barrier.Line(small), barrier.Line(large), "%s: the answer", name)
		assert.Equal(t,
			testing.AllocsPerRun(4, func() { _ = barrier.Line(small) }),
			testing.AllocsPerRun(4, func() { _ = barrier.Line(large) }),
			"%s: the work", name)
	}
}

// FuzzCatch_MessageIsOneBoundedLine holds the message of a panic of any string
// to what its readers rely on: valid UTF-8, on one line, free of control
// characters, opening as a panic's does, and no longer than the bound and its
// marker allow beside the site. Text, which keeps control characters, is held
// to the rest, and is the value's Line. The value itself comes back whole. The
// seeds are the inputs a terminal or a log would take wrongly.
func FuzzCatch_MessageIsOneBoundedLine(f *testing.F) {
	for _, seed := range []string{
		"", "plain", "two\nlines", "carriage\r\nreturn", "tab\there", "\x1b[31mred\x1b[0m", "nul\x00byte",
		"bad \xff\xfe bytes", "half a rune \xe2\x82", "   ", strings.Repeat("€", 6000), strings.Repeat("x", 20000),
		string(rune(0x85)), string(rune(0x2028)) + string(rune(0x2029)), string(rune(0xfeff)) + "mark",
		string(rune(0x9f)) + string(rune(0xa0)) + string(rune(0x1d11e)), "head:\nitem", "a\v\fb",
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, in string) {
		perr := caught(t, func() { panic(in) })

		msg := perr.Error()
		assert.True(t, utf8.ValidString(msg), "message %q is not valid UTF-8", msg)
		assert.False(t, strings.ContainsFunc(msg, unicode.IsControl), "message %q holds a control character", msg)
		assert.False(t, strings.ContainsAny(msg, lineBreaks), "message %q spans lines", msg)
		assert.True(t, strings.HasPrefix(msg, "panic: "), "message %q does not open as a panic's does", msg)
		assert.LessOrEqual(t, len(msg), len("panic: ")+barrier.MaxText+len(barrier.ElidedMarker)+400,
			"the message is bounded whatever the value was")
		assert.Equal(t, in, perr.Value())

		text := perr.Text()
		assert.Equal(t, barrier.Line(in), text)
		assert.True(t, utf8.ValidString(text), "text %q is not valid UTF-8", text)
		assert.False(t, strings.ContainsAny(text, lineBreaks), "text %q spans lines", text)
		assert.NotEmpty(t, text, "a panic always has something to say")
		assert.LessOrEqual(t, len(text), barrier.MaxText+len(barrier.ElidedMarker))
	})
}
