package keydoc

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestYAMLComment_StartsAtAHashOutsideQuotes reads where a YAML line's comment
// starts, if it has one.
func TestYAMLComment_StartsAtAHashOutsideQuotes(t *testing.T) {
	t.Parallel()
	tests := []struct {
		line, want string
		has        bool
	}{
		{"# a whole line", " a whole line", true},
		{"  # indented", " indented", true},
		{"key: value # after", " after", true},
		{"key: value\t# after a tab", " after a tab", true},
		{"$ref: '#/components'", "", false},
		{`key: "a # b"`, "", false},
		{"key: a#b", "", false},
		{"key: 'it''s' # after a doubled quote", " after a doubled quote", true},
		{"key: it's # after an apostrophe", " after an apostrophe", true},
		{`key: "a \" # b"`, "", false},
		{`key: "a \\" # after an escaped backslash`, " after an escaped backslash", true},
		{"[a, 'b # c'] # after a flow sequence", " after a flow sequence", true},
	}
	for _, tc := range tests {
		got, has := yamlComment(tc.line)
		assert.Equal(t, tc.has, has, tc.line)
		assert.Equal(t, tc.want, got, tc.line)
	}
}
