package compile_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dexpace/morphic/compilers/compile"
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
// nothing outside this table settles where the boundaries fall *inside* a word —
// "foo2bar" and "foo_2_bar" are both word sequences.
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
	{"a name with no word rune has no words", "***", ""},
	{"the empty name is empty", "", ""},
}

func TestCanonicalWords_Conformance(t *testing.T) {
	t.Parallel()
	for _, tc := range canonicalCases {
		t.Run(tc.rule, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, compile.CanonicalWords(tc.in), "input %q", tc.in)
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
			once := compile.CanonicalWords(tc.in)
			assert.Equal(t, once, compile.CanonicalWords(once), "re-canonicalizing %q", once)
		})
	}
}

// TestNamingFor_KeepsTheSpellingAndTheWords pins the pairing that makes a Naming
// neutral: the source spelling is preserved verbatim for anyone who needs the
// original, and the words beside it carry no casing an emitter should own.
func TestNamingFor_KeepsTheSpellingAndTheWords(t *testing.T) {
	t.Parallel()
	assert.Equal(t, ir.Naming{Source: "com.example.User", Canonical: "com_example_user"},
		compile.NamingFor("com.example.User"))
	assert.Equal(t, ir.Naming{Source: "***", Canonical: ""}, compile.NamingFor("***"),
		"a spelling with no words still keeps its spelling")
}
