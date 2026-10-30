// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package keydoc_test

import (
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/internal/keydoc"
)

// goodDoc is a keyed document in the shape Parse reads, with what it must not
// read as an entry or a field: a list and a heading inside fenced code, a
// paragraph opening with an issue number, and a bullet after a heading that
// closes the last entry.
const goodDoc = "# Title\n\nIntro.\n\n## Section\n\n" +
	"### `first-entry`\n\nProse,\nover two lines.\n\n```yaml\n- not: a field\n### not a heading\n```\n\n" +
	"- One: `a`, `b`\n" +
	"- Two:\n  - an item,\n    run on.\n" +
	"\n#558 is prose, not a heading.\n\n" +
	"### `second-entry`\n\n" +
	"- One: x\n" +
	"\n## Another section\n\n- not a field of any entry\n"

// TestParse_ReadsEachEntry reads the good document whole: both entries, each
// field with the lines it runs on over, and the prose around them, and nothing
// from the fence or after the closing heading.
func TestParse_ReadsEachEntry(t *testing.T) {
	t.Parallel()
	entries, problems := keydoc.Parse(goodDoc, "One", "Two")
	require.Empty(t, problems)
	want := []keydoc.Entry{
		{Key: "first-entry", Line: 7, Text: "Prose,\nover two lines.\n\n\n\n#558 is prose, not a heading.",
			Fields: []keydoc.Field{
				{Name: "One", Line: 17, Value: "`a`, `b`"},
				{Name: "Two", Line: 18, Lines: []string{"  - an item,", "    run on."}},
			}},
		{Key: "second-entry", Line: 24, Fields: []keydoc.Field{{Name: "One", Line: 26, Value: "x"}}},
	}
	if diff := cmp.Diff(want, entries); diff != "" {
		t.Errorf("entries (-want +got):\n%s", diff)
	}
}

// TestParse_ReportsEachShapeProblem plants one defect at a time in the good
// document and requires exactly the problem it makes.
func TestParse_ReportsEachShapeProblem(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, from, to, want string
	}{
		{"a heading that is no key", "### `second-entry`", "### Second entry", "line 24: a third-level heading is not a key"},
		{"a key that is no slug", "### `second-entry`", "### `Second_entry`", "is not a key"},
		{"a bullet naming no field", "- One: x\n", "- One: x\n- a list item\n", "line 27: second-entry writes a bullet that names no field"},
		{"a field no reader knows", "- One: x\n", "- One: x\n- Three: y\n", "second-entry writes a field no reader knows"},
		{"a field written twice", "- One: x\n", "- One: x\n- One: y\n", `second-entry writes the field "One" twice`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, 1, strings.Count(goodDoc, tc.from), "the plant must have one site")
			_, problems := keydoc.Parse(strings.Replace(goodDoc, tc.from, tc.to, 1), "One", "Two")
			require.Len(t, problems, 1, "%v", problems)
			assert.Contains(t, problems[0], tc.want)
		})
	}
}

// TestParse_ARefusedBulletKeepsItsLinesOutOfTheProse holds the lines below a
// refused bullet to the bullet: read as prose, they would hand a test reading
// an entry's text what its author wrote as a field.
func TestParse_ARefusedBulletKeepsItsLinesOutOfTheProse(t *testing.T) {
	t.Parallel()
	entries, problems := keydoc.Parse("### `k`\n\nProse.\n\n- One: a\n- One: b\n  `c`\n", "One")
	require.Len(t, problems, 1)
	require.Len(t, entries, 1)
	assert.Equal(t, "Prose.", entries[0].Text)
	assert.Equal(t, []keydoc.Field{{Name: "One", Line: 5, Value: "a"}}, entries[0].Fields)
}

// TestParse_AHeadingThatIsNoKeyOpensNoEntry requires the fields below a
// refused heading to be read as no entry's, rather than the entry above's.
func TestParse_AHeadingThatIsNoKeyOpensNoEntry(t *testing.T) {
	t.Parallel()
	entries, problems := keydoc.Parse("### `k`\n\n- One: a\n\n### Not a key\n\n- One: b\n", "One")
	require.Len(t, problems, 1)
	require.Len(t, entries, 1)
	assert.Equal(t, []keydoc.Field{{Name: "One", Line: 3, Value: "a"}}, entries[0].Fields)
}

// TestEntry_Field finds the field an entry writes under a name, and reports
// one it does not write.
func TestEntry_Field(t *testing.T) {
	t.Parallel()
	e := keydoc.Entry{Fields: []keydoc.Field{{Name: "One", Value: "a"}, {Name: "Two", Value: "b"}}}
	f, ok := e.Field("Two")
	require.True(t, ok)
	assert.Equal(t, "b", f.Value)
	_, ok = e.Field("Three")
	assert.False(t, ok)
}

// TestField_Text joins a field's value and the lines it runs on over into one
// line, whether or not the bullet writes a value of its own.
func TestField_Text(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "a b c", keydoc.Field{Value: "a", Lines: []string{"  b", "    c "}}.Text())
	assert.Equal(t, "b", keydoc.Field{Lines: []string{"  b"}}.Text())
	assert.Empty(t, keydoc.Field{}.Text())
}

// TestSpans returns each span of code in order, and none from text with none.
func TestSpans(t *testing.T) {
	t.Parallel()
	assert.Equal(t, []string{"a", "b c"}, keydoc.Spans("x `a` and `b c`, and a lone ` tick"))
	assert.Empty(t, keydoc.Spans("no code"))
}
