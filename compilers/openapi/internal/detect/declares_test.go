package detect_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dexpace/morphic/compilers/openapi/internal/detect"
)

// TestDeclaresProbeKey_ScopesTheNameToItsOwnDocument pins the guard that
// decides whose bytes these are. A name followed by a colon is a key wherever
// it sits, so the guard says whose: block style answers with column 0, flow
// style with the root mapping's own depth. A key there declares the format only
// with a version beside it (GitHub #497). The cases answering no are documents
// naming the word where none declares its format, or declaring it with prose, a
// collection or nothing beside it; saying yes would report this compiler's
// parse error over a file that was never its own.
func TestDeclaresProbeKey_ScopesTheNameToItsOwnDocument(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name, src string
		want      bool
	}{
		{"flow mapping declares it", `{"openapi":"3.1.0"}`, true},
		{"flow mapping declares swagger", `{"swagger":"2.0"}`, true},
		{"space around the mapping and the colon", "  \n\t{\"openapi\" : \"3.1.0\"}", true},
		{"an escape hides no key from the scan", `{"a\"b":1,"openapi":"3.1.0"}`, true},
		{"block style at column 0", "openapi: 3.1.0\n", true},
		{"block style past a byte-order mark", "\xef\xbb\xbfopenapi: 3.1.0\ninfo: [\n", true},
		// The version beside the key is read as YAML reads it, on its own line.
		{"block style, a quoted version", "openapi: \"3.1.0\"\ninfo: [\n", true},
		{"block style, a version and a comment", "openapi: 3.1.0 # v\ninfo: [\n", true},
		{"block style, CRLF", "openapi: 3.1.0\r\ninfo: [\r\n", true},
		{"block style, swagger", "swagger: \"2.0\"\ninfo: [\n", true},
		{"flow style, a bare version", `{"openapi":3.1,`, true},
		// And a key with no version beside it declares nothing, in either style.
		{"block style, prose beside the key", "openapi: is a format\n[\n", false},
		{"block style, a mapping under the key", "openapi:\n  enabled: true\n[\n", false},
		{"block style, a collection that does not close", "openapi: [unterminated\n", false},
		{"block style, no space after the colon", "openapi:3.1.0\n[\n", false},
		{"block style, a longer key the name begins", "openapi:x: 3.1.0\n[\n", false},
		{"block style, the version on the next line", "openapi:\n  3.1.0\n[\n", false},
		{"flow style, a collection for the version", `{"openapi":{"a":"3.1.0"},`, false},
		{"flow style, prose for the version", `{"openapi":"is a format",`, false},
		{"flow style, nothing after the colon", `{"openapi":`, false},
		{"flow style past a byte-order mark", "\xef\xbb\xbf{\"openapi\":\"3.1.0\",", true},
		// What may stand in front of a root flow mapping: comment lines, and up
		// to two node properties — an anchor and a tag, in either order.
		{"flow style behind a comment line", "# c\n  # d\n{\"openapi\":\"3.1.0\",", true},
		{"flow style behind an anchor", "&x {\"openapi\":\"3.1.0\",", true},
		{"flow style behind a tag and an anchor", "!!map &x {\"openapi\":\"3.1.0\",", true},
		{"flow style behind a property ended by a flow indicator", "&x{\"openapi\":\"3.1.0\",", true},
		{"a comment with nothing after it", "# openapi: 3.1.0", false},
		{"a property with nothing after it", "&x", false},
		{"three properties are more than a node carries", "&a &b &c {\"openapi\":\"3.1.0\",", false},

		{"nested one level down", `{"a":{"openapi":"3.1.0"}}`, false},
		{"nested inside a sequence", `{"a":[{"openapi":"3.1.0"}]}`, false},
		{"a document that opens a sequence", `[{"openapi":"3.1.0"}]`, false},
		{"block style indented under another key", "a:\n  openapi: 3.1.0\n", false},
		{"the name is a value", `{"note":"openapi"}`, false},
		{"the name in a comment", "x: 1\n# openapi is a format\n", false},
		{"the name has no colon after it", `{"openapi",1}`, false},
		{"the name ends the bytes", `{"openapi"`, false},
		{"a string runs off the end", `{"a":"unterminated`, false},
		{"nothing but whitespace", "  \n\t ", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, detect.DeclaresProbeKey([]byte(tc.src)))
		})
	}
}

// TestDeclaresBlockKey_ReadsABoundedNumberOfLines pins maxVersionLines. The
// guard parses each line that begins with the key, so a version behind as many
// lines without one as the bound allows is still found, and one behind a line
// more is not: the bound is what stops a file of nothing but such lines costing
// a parse per line.
func TestDeclaresBlockKey_ReadsABoundedNumberOfLines(t *testing.T) {
	t.Parallel()
	behind := func(n int) []byte {
		return []byte(strings.Repeat("openapi: is prose\n", n) + "openapi: 3.1.0\n[\n")
	}
	assert.True(t, detect.DeclaresBlockKey(behind(detect.MaxVersionLines-1), "openapi"), "the last line the bound reads")
	assert.False(t, detect.DeclaresBlockKey(behind(detect.MaxVersionLines), "openapi"), "one line past the bound")
}
