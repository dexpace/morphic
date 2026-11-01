// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package ircases

import (
	"slices"
	"strings"
)

// CanonicalCase is one row of [Canonical]: the rule it pins, a source spelling
// and the canonical words that spelling renders to.
type CanonicalCase struct {
	Rule string
	In   string
	Want string
}

// HasCanonicalCase is one row of [HasCanonical]: the rule it pins, a name, words
// to compare it against and whether they are its canonical words.
type HasCanonicalCase struct {
	Rule  string
	Name  string
	Words string
	Want  bool
}

// Canonical is the conformance suite for the one canonical-naming grammar.
// Canonical is ABI: an emitter cannot tell which compiler produced a name, so
// every compiler owes the same answer for the same spelling, and the three
// copies this replaced gave different ones (#163).
//
// Each row names the rule it pins; the rows from formats other than OpenAPI are
// the point. irverify rejects a canonical that is not a word sequence and
// recomputes one from the source, but only a table of expected answers says the
// grammar is right: a check that recomputes moves with the grammar.
var Canonical = []CanonicalCase{
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
	// GitHub #336. A tail needs case on one side of the split, because a split
	// between two runes lowercasing leaves alone is one the next pass makes
	// again. The lowercase letter the rule looks for is not always in the
	// source: "\u2124\u2124A" has none, and lowercasing the A supplies one, so the pair
	// below used to segment differently from the spelling one case apart.
	{"a run of caseless capitals is no tail", "\u2124\u2124A", "\u2124\u2124a"},
	{"so the lowercase spelling segments the same way", "\u2124\u2124a", "\u2124\u2124a"},
	{"a caseless run still ends at a cased capital", "\u2124\u2124Ab", "\u2124\u2124_ab"},
	{"a name with no word rune has no words", "***", ""},
	{"the empty name is empty", "", ""},
}

// HasCanonical pins HasCanonicalWords where a comparison can go wrong:
// spellings one edit from a canonical form, mismatched inside a word that a
// separator, a case boundary or a letter/digit boundary ends, at a separator,
// and past the last word. The rows that hold pin what it accepts: the
// grammar's own rendering, a rune whose lowercase is longer included.
var HasCanonical = []HasCanonicalCase{
	{"a canonical name is its own words", "user_id", "user_id", true},
	{"a camel-case name is its words", "userID", "user_id", true},
	{"a name with no word rune has no words", "***", "", true},
	{"a name with no word rune has not one", "***", "x", false},
	{"the empty name has no words", "", "", true},
	{"a rune lowercasing lengthens compares lowercased", "Ⱥb", "ⱥb", true},
	{"a rune lowercasing lengthens is not its own lowercase", "Ⱥb", "Ⱥb", false},
	{"a wrong rune in a word a separator ends", "list-users", "lisx_users", false},
	{"a wrong rune in a word a case boundary ends", "firstName", "firsx_name", false},
	{"a wrong rune in a word a digit boundary ends", "v2Beta", "x_2_beta", false},
	{"a wrong rune after a separator", "firstName", "first_nane", false},
	{"a missing separator", "firstName", "firstname", false},
	{"a separator where the grammar has none", "username", "user_name", false},
	{"a doubled separator", "first_name", "first__name", false},
	{"a separator spelled otherwise", "first_name", "first-name", false},
	{"a cased word", "userID", "user_ID", false},
	{"a missing last word", "userID", "user", false},
	{"a missing last rune", "userID", "user_i", false},
	{"a trailing separator", "user", "user_", false},
	{"a leading separator", "user", "_user", false},
	{"an extra word", "user", "user_id", false},
	{"an undecodable byte for a rune", "user", "use\xff", false},
	{"an undecodable byte past the end", "user", "user\xff", false},
	{"a replacement character for a rune", "user", "use�", false},
}

// NearMisses returns spellings one edit from the canonical form words, none of
// them words itself.
func NearMisses(words string) []string {
	misses := []string{words + "_", "_" + words, words + "a", words + "\xff", strings.ToUpper(words)}
	if words != "" {
		misses = append(misses, words[:len(words)-1], "a"+words[1:])
	}
	if strings.Contains(words, "_") {
		misses = append(misses,
			strings.Replace(words, "_", "", 1),
			strings.Replace(words, "_", "__", 1),
			strings.Replace(words, "_", "-", 1))
	}
	return slices.DeleteFunc(misses, func(miss string) bool { return miss == words })
}

// DecimalForms lists spellings ir.NewBigVal accepts and stores unchanged,
// because each is already in canonical JSON form. The fuzz targets seed from
// this slice rather than restating it, so a form added here cannot go missing
// there.
var DecimalForms = []string{"0", "-1", "42", "3.14", "-0.5", "1e10", "2.5E-3", "9007199254740993"}

// DecimalSpellings are spellings of a handful of values, several of each, and
// values a rounding reading would merge. The fuzz targets seed from this slice
// rather than restating it.
var DecimalSpellings = []string{
	"0", "-0.0", "0e9", "1", "1.0", "1e0", "10e-1", "10", "1e1", "10.0", "1500", "1500.0", "15e2", "1.5e3",
	"0.001", "1e-3", "5", "-5", "-5.00", "-0.5e1", "1e1000001", "10e1000000", "1e-1000001",
	"9007199254740993", "9007199254740992", "1.0000000000000001", "1.0000000000000002",
}
