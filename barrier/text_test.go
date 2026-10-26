package barrier_test

import (
	"strings"
	"testing"
	"unicode"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"

	"github.com/dexpace/morphic/barrier"
)

// FuzzCatch_MessageIsOneBoundedLine holds the message of a panic of any string
// to what its readers rely on: valid UTF-8, on one line, free of control
// characters, opening as a panic's does, and no longer than the bound and its
// marker allow beside the site. The value itself comes back whole. The seeds
// are the inputs a terminal or a log would take wrongly.
func FuzzCatch_MessageIsOneBoundedLine(f *testing.F) {
	for _, seed := range []string{
		"", "plain", "two\nlines", "carriage\r\nreturn", "tab\there", "\x1b[31mred\x1b[0m", "nul\x00byte",
		"bad \xff\xfe bytes", "half a rune \xe2\x82", "   ", strings.Repeat("€", 6000), strings.Repeat("x", 20000),
		string(rune(0x85)), string(rune(0x2028)) + string(rune(0x2029)), string(rune(0xfeff)) + "mark",
		string(rune(0x9f)) + string(rune(0xa0)) + string(rune(0x1d11e)),
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, in string) {
		perr := caught(t, func() { panic(in) })

		msg := perr.Error()
		assert.True(t, utf8.ValidString(msg), "message %q is not valid UTF-8", msg)
		assert.False(t, strings.ContainsFunc(msg, unicode.IsControl), "message %q holds a control character", msg)
		assert.False(t, strings.ContainsAny(msg, "\n\r"+string(rune(0x2028))+string(rune(0x2029))), "message %q spans lines", msg)
		assert.True(t, strings.HasPrefix(msg, "panic: "), "message %q does not open as a panic's does", msg)
		assert.LessOrEqual(t, len(msg), len("panic: ")+barrier.MaxText+len(barrier.ElidedMarker)+400,
			"the message is bounded whatever the value was")
		assert.Equal(t, in, perr.Value())
	})
}
