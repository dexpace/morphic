package oas

import (
	"encoding/json/jsontext"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	yaml "gopkg.in/yaml.v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/ir"
)

// parse reads doc into a node.
func parse(t *testing.T, doc string) *yaml.Node {
	t.Helper()
	var n yaml.Node
	require.NoError(t, yaml.Unmarshal([]byte(doc), &n))
	return &n
}

// TestCheck_AnUnknownCodeRuleIsATableError pins that a Code row the engine
// does not implement is reported, never skipped.
func TestCheck_AnUnknownCodeRuleIsATableError(t *testing.T) {
	spec := Table()
	obj := spec.must(Info)
	obj.Rules = append(obj.Rules, code(Always(), "no-such-predicate", "", Cite{Minor: V30, Section: "Info Object", Sentence: "x"}))
	spec.objects[spec.index[Info]] = obj
	diags := Checker{spec: spec}.Check(parse(t, `{title: t, version: v}`), "3.1.0", Options{Start: Info})
	require.Len(t, diags, 1)
	assert.Equal(t, diag.InternalInvariant, diags[0].Code)
	assert.Contains(t, diags[0].Message, "no-such-predicate")
}

// TestCheck_WorkIsBounded pins that the walk stops at its budget and its depth,
// and says so once, as an error.
func TestCheck_WorkIsBounded(t *testing.T) {
	deep := "{}"
	for range MaxDepth + 8 {
		deep = "{properties: {a: " + deep + "}}"
	}
	tests := []struct {
		name   string
		doc    string
		budget int
	}{
		{"budget", `{properties: {a: {}, b: {}, c: {}, d: {}}}`, 3},
		{"depth", deep, 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			diags := NewChecker().Check(parse(t, tc.doc), "3.1.0", Options{Start: Schema, Budget: tc.budget})
			require.Len(t, diags, 1)
			assert.Equal(t, diag.BudgetExceeded, diags[0].Code)
			assert.Equal(t, ir.SeverityError, diags[0].Severity)
		})
	}
}

// TestCheck_DefaultsFillTheUnsetOptions pins the options a caller may leave out.
func TestCheck_DefaultsFillTheUnsetOptions(t *testing.T) {
	c := NewChecker()
	doc := parse(t, "openapi: 3.1.0\ninfo: {title: t}\n")
	got := c.Check(doc, "3.1.0", Options{})
	require.NotEmpty(t, got, "the document is read from its root")
	for _, d := range got {
		assert.Equal(t, ir.Provenance{}, d.Provenance, "unset Locate places nothing")
	}
	var placed []jsontext.Pointer
	c.Check(doc, "3.1.0", Options{Locate: func(p jsontext.Pointer) ir.Provenance {
		placed = append(placed, p)
		return ir.Provenance{}
	}})
	assert.NotEmpty(t, placed)
	assert.Empty(t, c.Check(nil, "3.1.0", Options{}), "nothing to read")
}

// TestCheck_MergedAndAliasedMappingsAreRead pins that objects are read through
// `<<` and aliases, as the parser reads them.
func TestCheck_MergedAndAliasedMappingsAreRead(t *testing.T) {
	doc := `
openapi: 3.1.0
x-base: &base {title: t, version: v}
info:
  <<: *base
  summary: s
components:
  schemas:
    A: &a {type: object}
    B: *a
`
	assert.Empty(t, NewChecker().Check(parse(t, doc), "3.1.0", Options{}))
	merged := parse(t, "{<<: {version: v}, title: t, license: {}}")
	diags := NewChecker().Check(merged, "3.1.0", Options{Start: Info})
	require.Len(t, diags, 1, "the merged version is read, the license is not named")
	assert.Equal(t, diag.Validation+"/validation-required-field", diags[0].Code)
}

// TestCheck_IsDeterministic pins that one document draws one report.
func TestCheck_IsDeterministic(t *testing.T) {
	doc := parse(t, `{title: [a], version: 1, license: {identifier: i, url: u}, contact: {email: x}}`)
	first := NewChecker().Check(doc, "3.1.0", Options{Start: Info})
	again := NewChecker().Check(doc, "3.1.0", Options{Start: Info})
	assert.NotEmpty(t, first)
	assert.Empty(t, cmp.Diff(first, again))
}

// TestCiteFor_PicksTheLatestCitationTheMinorHasReached pins the citation a
// finding names.
func TestCiteFor_PicksTheLatestCitationTheMinorHasReached(t *testing.T) {
	cites := []Cite{{Minor: V30, Sentence: "a"}, {Minor: V32, Sentence: "c"}, {Minor: V31, Sentence: "b"}}
	assert.Equal(t, "a", citeFor(cites, V30).Sentence)
	assert.Equal(t, "b", citeFor(cites, V31).Sentence)
	assert.Equal(t, "c", citeFor(cites, V32).Sentence)
	assert.Equal(t, "c", citeFor([]Cite{{Minor: V32, Sentence: "c"}}, V30).Sentence, "none reached: the first")
	assert.Equal(t, Cite{}, citeFor(nil, V30))
}

// TestValidFormat_TakesAnyStringWhereNoSyntaxIsNamed pins the format that
// asks for nothing.
func TestValidFormat_TakesAnyStringWhereNoSyntaxIsNamed(t *testing.T) {
	assert.True(t, validFormat(FormatNone, "anything at all"))
	assert.False(t, validFormat(ComponentName, "a b"))
	assert.Equal(t, "1 entry", plural(1))
	assert.Equal(t, "2 entries", plural(2))
}

// TestTemplateFault_NamesWhatIsWrong pins the wording of the template check.
func TestTemplateFault_NamesWhatIsWrong(t *testing.T) {
	assert.Empty(t, templateFault("https://{a}/{b}"))
	assert.Contains(t, templateFault("{a}/{a}"), `"a"`)
	assert.Contains(t, templateFault("{a"), "no }")
}
