package operation

import (
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

// Phrases pathKeyDefects returns for the rules that name nothing of the key.
const (
	wantNoSlash      = `does not begin with "/"`
	wantBadPercent   = `has a "%" that begins no percent-encoded octet`
	wantStrayBrace   = `has a brace that is not part of a "{name}" template expression`
	wantEmptySegment = `has an empty segment ("//")`
)

// TestPathKeyDefects pins the classifier behind diag.InvalidPathKey rule by
// rule: each row is a class of key the grammar admits or refuses, with the
// phrases it must draw, so a rule widened or narrowed by one class reddens here.
func TestPathKeyDefects(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		key  string
		want []string
	}{
		{name: "root", key: "/"},
		{name: "segments", key: "/a/b"},
		{name: "trailing slash", key: "/a/"},
		{name: "each class of literal pchar", key: "/aZ09-._~!$&'()*+,;=:@"},
		{name: "pct-encoded either case", key: "/a/%2F%2f%20%41"},
		{name: "expression", key: "/a/{id}"},
		{name: "adjacent expressions", key: "/{a}{b}"},
		{name: "expression inside a segment", key: "/a{b}c"},
		{name: "name with space and quotes", key: "/a/{test id}/{`x\"}"},
		{name: "name with ? # / and %", key: "/a/{x?y}/{x#y}/{x/y}/{%zz}"},
		{name: "name with non-ASCII and control", key: "/a/{é}/{\t}"},
		{name: "names differing in case", key: "/a/{id}/b/{Id}"},

		{name: "empty key", key: "", want: []string{wantNoSlash}},
		{name: "no leading slash", key: "widgets", want: []string{wantNoSlash}},
		{name: "expression first", key: "{id}", want: []string{wantNoSlash}},

		{name: "space", key: "/a b", want: []string{`contains " ", which a path admits only percent-encoded`}},
		{name: "control", key: "/a\tb\x00", want: []string{`contains "\t" and "\x00", which a path admits only percent-encoded`}},
		{name: "non-ASCII", key: "/a/é", want: []string{`contains "é", which a path admits only percent-encoded`}},
		{name: "invalid UTF-8", key: "/a\xffb", want: []string{`contains "\xff", which a path admits only percent-encoded`}},
		{
			name: "each character once, first-seen order",
			key:  "/a b<c d<",
			want: []string{`contains " " and "<", which a path admits only percent-encoded`},
		},
		{
			name: "three characters",
			key:  "/a b<c>",
			want: []string{`contains " ", "<" and ">", which a path admits only percent-encoded`},
		},

		{name: "percent before non-hex", key: "/a/%zz", want: []string{wantBadPercent}},
		{name: "bare percent", key: "/a/%", want: []string{wantBadPercent}},
		{name: "one digit after percent", key: "/a/%2", want: []string{wantBadPercent}},
		{name: "second digit not hex", key: "/a/%2g", want: []string{wantBadPercent}},
		{name: "triplet ending the key", key: "/a/%41"},
		{name: "bad percent then a triplet", key: "/a/%%41", want: []string{wantBadPercent}},
		{
			name: "what follows a bad percent is read",
			key:  "/a%<b",
			want: []string{`contains "<", which a path admits only percent-encoded`, wantBadPercent},
		},

		{name: "unclosed brace", key: "/foo/{param", want: []string{wantStrayBrace}},
		{
			name: "what follows a stray brace is read",
			key:  "/{a b",
			want: []string{`contains " ", which a path admits only percent-encoded`, wantStrayBrace},
		},
		{name: "closing brace alone", key: "/a}b", want: []string{wantStrayBrace}},
		{name: "empty expression", key: "/a/{}", want: []string{wantStrayBrace}},
		{name: "nested brace", key: "/a/{b{c}}", want: []string{wantStrayBrace}},
		{name: "stray brace before a used name", key: "/a/{b{c}/{c}", want: []string{wantStrayBrace, `repeats "{c}"`}},

		{name: "repeated expression", key: "/users/{id}/issues/{id}", want: []string{`repeats "{id}"`}},
		{name: "thrice is named once", key: "/{a}/{a}/{a}", want: []string{`repeats "{a}"`}},
		{name: "repeats in second-use order", key: "/{a}/{b}/{b}/{a}", want: []string{`repeats "{b}" and "{a}"`}},

		{name: "leading empty segment", key: "//a", want: []string{wantEmptySegment}},
		{name: "inner empty segment", key: "/a//b", want: []string{wantEmptySegment}},
		{name: "empty segment then a segment", key: "//a/b", want: []string{wantEmptySegment}},

		{name: "query", key: "/a?x=1", want: []string{`carries the query "?x=1"`}},
		{name: "empty query", key: "/a?", want: []string{`carries the query "?"`}},
		{name: "query ends at the fragment", key: "/a?x#y", want: []string{`carries the query "?x"`}},
		{name: "nothing after ? is path", key: "/a?b c//{", want: []string{`carries the query "?b c//{"`}},

		{name: "fragment", key: "/a#frag"},
		{name: "nothing after # is read", key: "/a#b c?x//{id}{id}{"},

		{
			name: "every rule, in order",
			key:  "a b%zz}{x}{x}//?q",
			want: []string{
				wantNoSlash,
				`contains " ", which a path admits only percent-encoded`,
				wantBadPercent,
				wantStrayBrace,
				`repeats "{x}"`,
				wantEmptySegment,
				`carries the query "?q"`,
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if diff := cmp.Diff(tc.want, pathKeyDefects(tc.key)); diff != "" {
				t.Errorf("pathKeyDefects(%q) mismatch (-want +got):\n%s", tc.key, diff)
			}
		})
	}
}

// TestPathKeyDefects_EveryByte reads each byte between two letters and holds
// the verdict to RFC 3986's pchar, so no byte is admitted or refused by a slip
// in pathLiteralBytes. The bytes with a reading of their own are left to the
// table above.
func TestPathKeyDefects_EveryByte(t *testing.T) {
	t.Parallel()
	const pchar = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789" +
		"-._~" + "!$&'()*+,;=" + ":@"
	for b := range 256 {
		c := byte(b)
		if strings.IndexByte("/%{}?#", c) >= 0 {
			continue
		}
		key := "/a" + string([]byte{c}) + "b"
		var want []string
		if strings.IndexByte(pchar, c) < 0 {
			want = []string{"contains " + strconv.Quote(string([]byte{c})) +
				", which a path admits only percent-encoded"}
		}
		if diff := cmp.Diff(want, pathKeyDefects(key)); diff != "" {
			t.Errorf("byte %#x (-want +got):\n%s", c, diff)
		}
	}
}

// TestIsHexDigit holds isHexDigit to strconv's reading of one base-16 digit
// over every byte.
func TestIsHexDigit(t *testing.T) {
	t.Parallel()
	for b := range 256 {
		c := byte(b)
		_, err := strconv.ParseUint(string([]byte{c}), 16, 8)
		if got, want := isHexDigit(c), err == nil; got != want {
			t.Errorf("isHexDigit(%#x) = %v, want %v", c, got, want)
		}
	}
}

// FuzzPathKeyDefects_AgreesWithTheABNF holds the scanner to the 3.2.0 §4.8.2
// ABNF transcribed as a regular expression, so a reader can check the grammar
// against the specification line by line rather than through the scanner. The
// path is the key up to its first "?" or "#" outside an expression; a key is
// clean exactly when that path matches, uses no expression twice, and no query
// follows it.
func FuzzPathKeyDefects_AgreesWithTheABNF(f *testing.F) {
	const (
		pchar   = `(?:[A-Za-z0-9\-._~!$&'()*+,;=:@]|%[0-9A-Fa-f]{2})`
		expr    = `\{[^{}]+\}`
		segment = `(?:` + pchar + `|` + expr + `)+`
	)
	template := regexp.MustCompile(`^/(?:` + segment + `/)*(?:` + segment + `)?$`)
	exprs := regexp.MustCompile(expr)
	pathPart := regexp.MustCompile(`^(?:` + expr + `|[^?#])*`)
	for _, seed := range []string{"", "/", "a", "/a/{id}", "/{a}{b}", "/a/{x y}", "/{a/b}", "//a", "/a/%2",
		"/a{b", "/a}", "/{}", "/{a{b}}", "/u/{id}/i/{id}", "/é", "/a?x", "/a#f", "/a/{x?y}", "/a/{x?"} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, key string) {
		p := pathPart.FindString(key)
		query := strings.HasPrefix(key[len(p):], "?")
		names := exprs.FindAllString(p, -1)
		used := len(names)
		slices.Sort(names)
		clean := template.MatchString(p) && len(slices.Compact(names)) == used && !query

		got := pathKeyDefects(key)
		if clean != (len(got) == 0) {
			t.Fatalf("key %q: ABNF clean=%v, defects=%q", key, clean, got)
		}
		if reported := slices.ContainsFunc(got, func(d string) bool {
			return strings.HasPrefix(d, "carries the query ")
		}); reported != query {
			t.Fatalf("key %q: query after the path=%v, reported=%v", key, query, reported)
		}
	})
}
