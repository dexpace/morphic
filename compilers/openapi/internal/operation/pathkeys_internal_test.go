package operation

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestPathKey_Defects pins the classifier behind diag.InvalidPathKey branch for
// branch. Every class a key may use and every class it may not is a row, because
// the classifier is the whole of the rule: a case the table omits is a spelling
// nothing decides about, and the exact-100% coverage gate would otherwise turn a
// row dropped from here into a build failure rather than a missing check.
func TestPathKey_Defects(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		key  string
		want []string
	}{
		// Allowed, each silent.
		{name: "unreserved and sub-delims", key: "/a-Z.0_9~!$&'()*+,;="},
		{name: "colon, at and slash", key: "/a:@/b"},
		{name: "template braces", key: "/a/{id}"},
		{name: "root", key: "/"},
		{name: "empty segment", key: "//a"},
		{name: "pct-encoded space", key: "/a/%20"},
		{name: "pct-encoded uppercase hex", key: "/a/%2F"},
		{name: "non-ASCII rune", key: "/a/é"},
		{name: "fragment deferred to #602", key: "/a#frag"},

		// Rejected: missing leading slash.
		{name: "empty key", key: "", want: []string{pathKeyNoSlashDefect}},
		{name: "no leading slash", key: "widgets", want: []string{pathKeyNoSlashDefect}},
		// Rejected: a query string.
		{name: "query", key: "/a?x=1", want: []string{pathKeyQueryDefect}},
		// Rejected: a character no URI path admits.
		{name: "space", key: "/a b", want: []string{fmt.Sprintf(pathKeyCharDefectFormat, ' ')}},
		{name: "tab", key: "/a\tb", want: []string{fmt.Sprintf(pathKeyCharDefectFormat, '\t')}},
		{name: "delete", key: "/a\x7fb", want: []string{fmt.Sprintf(pathKeyCharDefectFormat, '\x7f')}},
		{name: "quote", key: `/a"b`, want: []string{fmt.Sprintf(pathKeyCharDefectFormat, '"')}},
		{name: "less-than", key: "/a<b", want: []string{fmt.Sprintf(pathKeyCharDefectFormat, '<')}},
		{name: "greater-than", key: "/a>b", want: []string{fmt.Sprintf(pathKeyCharDefectFormat, '>')}},
		{name: "backslash", key: `/a\b`, want: []string{fmt.Sprintf(pathKeyCharDefectFormat, '\\')}},
		{name: "caret", key: "/a^b", want: []string{fmt.Sprintf(pathKeyCharDefectFormat, '^')}},
		{name: "backtick", key: "/a`b", want: []string{fmt.Sprintf(pathKeyCharDefectFormat, '`')}},
		{name: "pipe", key: "/a|b", want: []string{fmt.Sprintf(pathKeyCharDefectFormat, '|')}},
		{name: "open bracket", key: "/a[b", want: []string{fmt.Sprintf(pathKeyCharDefectFormat, '[')}},
		{name: "close bracket", key: "/a]b", want: []string{fmt.Sprintf(pathKeyCharDefectFormat, ']')}},
		// Rejected: a percent that begins no triplet.
		{name: "percent before non-hex", key: "/a/%zz", want: []string{pathKeyPercentDefect}},
		{name: "bare percent", key: "/a/%", want: []string{pathKeyPercentDefect}},
		{name: "one digit after percent", key: "/a/%2", want: []string{pathKeyPercentDefect}},

		// More than one defect, reported together and in a fixed order.
		{
			name: "missing slash, query and space",
			key:  "a/b c?x",
			want: []string{
				pathKeyNoSlashDefect,
				pathKeyQueryDefect,
				fmt.Sprintf(pathKeyCharDefectFormat, ' '),
			},
		},
		{
			name: "the first bad character is the one named",
			key:  "/a[ b",
			want: []string{fmt.Sprintf(pathKeyCharDefectFormat, '[')},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, pathKeyDefects(tc.key), "key %q", tc.key)
		})
	}
}
