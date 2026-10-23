package operation

import (
	"strconv"
	"testing"

	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
)

func TestStatusRange_NamesAStatus(t *testing.T) {
	t.Parallel()
	cases := []struct {
		code     string
		from, to int
	}{
		{"default", 0, 0},
		{"200", 200, 200},
		{"100", 100, 100}, // the low bound of what HTTP defines
		{"599", 599, 599}, // and the high one
		{"4XX", 400, 499},
		{"5xx", 500, 599}, // lowercase is read too
		{"1XX", 100, 199},
	}
	for _, tc := range cases {
		t.Run(tc.code, func(t *testing.T) {
			t.Parallel()
			r, ok := statusRange(tc.code)
			require.True(t, ok, "%q names a status", tc.code)
			assert.Equal(t, tc.from, r.From)
			assert.Equal(t, tc.to, r.To)
		})
	}
}

// TestStatusRange_NamesNoStatus pins that every key here is reported as naming
// no status, not read as {0,0} or a range OpenAPI cannot declare, which a
// consumer could not tell from a declared default (GitHub #262).
//
// The zero range beside the false is load-bearing: lowerResponses routes on
// isErrorRange without re-testing ok, so a false paired with a range from 400
// up would send a key naming no status to lowerErrorCase, which would classify
// a fault from a range nothing derived and assert a status the key never named.
func TestStatusRange_NamesNoStatus(t *testing.T) {
	t.Parallel()
	for _, code := range []string{
		"wat",  // not a number at all; used to reach {0,0}
		"20A",  // two digits and a letter
		"2X0",  // half a wildcard
		"1XY",  // the third character the wildcard test never read
		"9XX",  // a leading digit past the 1XX-5XX OpenAPI defines
		"6XX",  // the first one past it
		"0",    // Atoi read this as 0, which is default's own range
		"00",   // and this
		"000",  // and this, at the right width
		"600",  // three digits past what HTTP defines
		"099",  // and below it
		"+200", // Atoi accepts a sign; a status key has none
		"",     // no key at all
		"Default",
		"2XXX",
	} {
		// Quoted so the empty key names a subtest of its own rather than "#00".
		t.Run(strconv.Quote(code), func(t *testing.T) {
			t.Parallel()
			r, ok := statusRange(code)
			assert.False(t, ok, "%q names no status", code)
			assert.Equal(t, ir.StatusRange{}, r,
				"a false is paired with the zero range; lowerResponses routes on that")
		})
	}
}

// TestStatusConditions_RecordsNothingForAnUnreadableKey pins the disposition
// half: an unreadable key must not borrow default's {0,0} range, or the two
// become one shape in the IR.
func TestStatusConditions_RecordsNothingForAnUnreadableKey(t *testing.T) {
	t.Parallel()
	assert.Empty(t, statusConditions(ir.StatusRange{}, false).StatusCodes)
	assert.Equal(t,
		[]ir.StatusRange{{From: 0, To: 0}},
		statusConditions(ir.StatusRange{}, true).StatusCodes,
		"a declared default still records the catch-all it means")
}

func TestFaultFor(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "client", faultFor(ir.StatusRange{From: 404, To: 404}))
	assert.Equal(t, "server", faultFor(ir.StatusRange{From: 503, To: 503}))
	assert.Empty(t, faultFor(ir.StatusRange{}))
}

func TestLowerResponses_NoResponses(t *testing.T) {
	t.Parallel()
	l := newRawLowerer(&soa.OpenAPI{})
	op := &ir.Operation{}
	diags := lowerResponses(l.ctx, l.types, op, &soa.Operation{}, "/op")
	assert.Nil(t, op.Responses)
	assert.Nil(t, op.Errors)
	assert.Nil(t, op.Default)
	assert.Empty(t, diags)
}

// TestFaultFor_ClassifiesAtTheClassBoundaries pins where one HTTP class ends and
// the next begins, which no fixture reaches: the corpus uses 400, 404, 429 and
// the 4XX/5XX wildcards, so both upper bounds are stated by the code and held by
// nothing. Narrowing either one by a single code leaves the whole suite green
// while a 499 stops being a client fault — and Fault is what an SDK emitter
// reads to decide which exception class it raises.
func TestFaultFor_ClassifiesAtTheClassBoundaries(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		from int
		want string
	}{
		{name: "the first client code", from: 400, want: "client"},
		{name: "the last client code", from: 499, want: "client"},
		{name: "the first server code", from: 500, want: "server"},
		{name: "the last server code", from: 599, want: "server"},
		{name: "a success is neither", from: 200, want: ""},
		{name: "past the server class", from: 600, want: ""},
		{name: "the catch-all range, which no ErrorCase holds", from: 0, want: ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, faultFor(ir.StatusRange{From: tc.from, To: tc.from}))
		})
	}
}
