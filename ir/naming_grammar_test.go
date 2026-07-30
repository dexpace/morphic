package ir_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dexpace/morphic/ir"
)

// canonicalCases is the conformance suite for the one canonical-naming grammar.
// It is a suite rather than a handful of examples because Canonical is ABI: an
// emitter cannot tell which compiler produced a name, so every compiler owes the
// same answer for the same spelling, and the three copies this replaced gave
// different ones (GitHub #163).
//
// Each row names the rule it pins. The rows drawn from a format other than
// OpenAPI are the point of the suite: irverify rejects a canonical that is not a
// word sequence, so a compiler leaving "." in place is already caught, but
// irverify now recomputes a canonical from the source beside it, so a document it
// is handed is held to every row here — but a table of expected answers is
// still what says the grammar is *right*, since a check that recomputes moves
// with the grammar it recomputes through.
var canonicalCases = []struct {
	rule string
	in   string
	want string
}{
	{"a single lowercase word is itself", "user", "user"},
	{"casing is dropped", "User", "user"},
	{"camelCase splits", "firstName", "first_name"},
	{"a trailing acronym splits", "userID", "user_id"},
	{"a leading acronym keeps its tail", "HTTPServer", "http_server"},
	{"letter/digit boundaries split", "APIKey2", "api_key_2"},
	{"digit/letter boundaries split too", "v2Beta", "v_2_beta"},
	{"an inner digit run splits both sides", "foo2bar", "foo_2_bar"},
	{"an existing snake name is left alone", "user_name", "user_name"},
	{"a hyphen separates", "list-users", "list_users"},
	{"a space separates", "list users", "list_users"},
	{"a dot separates: a namespaced component name", "com.example.User", "com_example_user"},
	{"a dot separates: a proto package", "example.v1", "example_v_1"},
	{"a slash separates: a media type", "application/json", "application_json"},
	{"brackets separate: a deep-object parameter", "filter[name]", "filter_name"},
	{"braces and slashes separate: an operation hint", "get /pets/{petId}", "get_pets_pet_id"},
	{"a header mixing separators", "X-Trace.Id", "x_trace_id"},
	{"leading and trailing separators produce no empty words", "..padded.name..", "padded_name"},
	{"punctuation is not a word character", "$weird@name!", "weird_name"},
	// A decomposed é (e + combining acute) is one letter written as two runes:
	// the mark belongs to the letter before it, so reading it as a separator
	// would split a word in half.
	{"a combining mark belongs to its letter", "cafe\u0301_v2", "cafe\u0301_v_2"},
	// What "upper" means, settled in GitHub #187. The grammar splits where
	// lowercasing will change the rune, which is the test irverify.isCased applies
	// to a whole canonical; a rune the two disagreed about made the grammar split
	// its own output on a second pass.
	{"a letter with no lowercase form is not a case boundary", "A\u2124", "a\u2124"},
	{"so a name carrying one is already a fixed point", "a\u2124", "a\u2124"},
	{"nor does an acronym run end at one", "COUNT\u2124", "count\u2124"},
	{"a titlecase letter is a boundary, and lowercases", "x\u01C5y", "x_\u01C6y"},
	{"an uppercase whose lowercase is a different letter", "a\u1E9Eb", "a_\u00DFb"},
	{"an uppercase whose lowercase is two runes", "\u0130stanbul", "istanbul"},
	// The acronym-tail rule asks a different question from the one above: not
	// whether a rune is a case transition, but whether it belongs to a run of
	// capitals. A letter with no lowercase form is one; a titlecase letter is one
	// and is not IsUpper.
	{"a run of capitals ends at one with no lowercase form", "\u2124Server", "\u2124_server"},
	{"and such a letter can be the tail itself", "HTTP\u2124erver", "http_\u2124erver"},
	{"a titlecase letter opens a run", "\u01C5Bc", "\u01C6_bc"},
	{"a name with no word rune has no words", "***", ""},
	{"the empty name is empty", "", ""},
}

func TestCanonicalWords_Conformance(t *testing.T) {
	t.Parallel()
	for _, tc := range canonicalCases {
		t.Run(tc.rule, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, ir.CanonicalWords(tc.in), "input %q", tc.in)
		})
	}
}

// TestCanonicalWords_IsIdempotent asserts the grammar is a fixed point: feeding
// a canonical back in returns it unchanged. Without that, a name that crossed
// two stages would drift, and it is the property irverify's segmentation check
// relies on to be decidable from the value alone.
func TestCanonicalWords_IsIdempotent(t *testing.T) {
	t.Parallel()
	for _, tc := range canonicalCases {
		t.Run(tc.rule, func(t *testing.T) {
			t.Parallel()
			once := ir.CanonicalWords(tc.in)
			assert.Equal(t, once, ir.CanonicalWords(once), "re-canonicalizing %q", once)
		})
	}
}
