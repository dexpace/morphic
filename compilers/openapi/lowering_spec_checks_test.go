package openapi_test

import (
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"maps"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/internal/keydoc"
)

// goodSpec is a specification every check reads as sound against goodEnv: a
// rule naming a case, a code it says when it fires and two tests, and a rule
// a test pins whose Status names its issue.
const goodSpec = "# Spec\n\n## 1. Section\n\n" +
	"### `first-rule`\n\nA construct lowers, reported `openapi/a-code` as a warning, as\n" +
	"[`second-rule`](#second-rule) says; `#7` is code, not an issue.\n\n" +
	"- Cases: `case-a`\n- Codes: `openapi/a-code`\n- Tests: `openapi.TestA`,\n  `load.TestB`\n\n" +
	"### `second-rule`\n\nA construct is not built yet.\n\n" +
	"- Tests: `openapi.TestA`\n- Status: not built (#12).\n"

// specEnv is what the checks hold a specification to besides its text.
type specEnv struct {
	corpus   map[string]bool
	declared map[string]bool
	outside  map[string]string
	funcs    map[string]map[string]bool
	cites    []keydoc.Citation
}

// goodEnv is the code goodSpec describes.
func goodEnv() specEnv {
	return specEnv{
		corpus:   map[string]bool{"case-a": true},
		declared: map[string]bool{"openapi/a-code": true, "openapi/internal": true},
		outside:  map[string]string{"openapi/internal": "a defect of the compiler"},
		funcs:    map[string]map[string]bool{"openapi": {"TestA": true}, "load": {"TestB": true}},
		cites:    []keydoc.Citation{{Path: "a.go", Line: 1, Key: "first-rule"}},
	}
}

// specProblems runs every check over text against env, as the tests over the
// real specification do.
func specProblems(text string, env specEnv) []string {
	rules, problems := parseSpec(text)
	problems = append(problems, evidenceProblems(rules)...)
	problems = append(problems, caseProblems(rules, env.corpus)...)
	problems = append(problems, codeProblems(rules, env.declared, env.outside)...)
	problems = append(problems, testProblems(rules, func(pkg string) (map[string]bool, error) {
		funcs, ok := env.funcs[pkg]
		if !ok {
			return nil, errors.New("holds no test file")
		}
		return funcs, nil
	})...)
	return append(problems, citationProblems(rules, env.cites)...)
}

// TestSpecChecks_ReadAGoodSpecAsSound is the control the plants below are
// read against: with nothing planted, no check reports anything.
func TestSpecChecks_ReadAGoodSpecAsSound(t *testing.T) {
	t.Parallel()
	assert.Empty(t, specProblems(goodSpec, goodEnv()))
	rules, _ := parseSpec(goodSpec)
	require.Len(t, rules, 2)
	assert.Equal(t, rule{key: "first-rule", line: 5,
		text:  "A construct lowers, reported `openapi/a-code` as a warning, as\n[`second-rule`](#second-rule) says; `#7` is code, not an issue.",
		cases: []string{"case-a"}, codes: []string{"openapi/a-code"}, tests: []string{"openapi.TestA", "load.TestB"}},
		rules[0])
	assert.Equal(t, "not built (#12).", rules[1].status)
}

// TestSpecChecks_ReportEachPlantedDefect plants one defect at a time, in the
// specification or in the code it is held to, and requires exactly the
// problem it makes.
func TestSpecChecks_ReportEachPlantedDefect(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		plant func(t *testing.T, text *string, env *specEnv)
		want  string
	}{
		{"a shape keydoc refuses", replace("### `second-rule`", "### Second rule"), "is not a key"},
		{"a field naming nothing", replace("- Tests: `openapi.TestA`\n- Status", "- Tests: none\n- Status"),
			`second-rule writes "Tests" naming nothing`},
		{"a repeated key", replace("### `second-rule`", "### `first-rule`"), "repeats the key of line 5"},
		{"a rule with no evidence", replace("- Tests: `openapi.TestA`\n- Status: not built (#12).\n", ""),
			"second-rule names no case or test"},
		{"a Status naming no tracker", replace("not built (#12).", "not built."), "names no issue"},
		{"a case the corpus lacks", replace("- Cases: `case-a`", "- Cases: `case-a`, `case-z`"),
			`name the case "case-z"`},
		{"a case no rule names", func(_ *testing.T, _ *string, env *specEnv) { env.corpus["case-b"] = true },
			`conformance case "case-b" is named by no rule`},
		{"a code nothing declares", both(
			replace("reported `openapi/a-code` as", "reported `openapi/a-code` or `openapi/z` as"),
			replace("- Codes: `openapi/a-code`", "- Codes: `openapi/a-code`, `openapi/z`")),
			`name the code "openapi/z", which`},
		{"a code listed and named", func(_ *testing.T, _ *string, env *specEnv) { env.outside["openapi/a-code"] = "x" },
			"delete its codesOutsideTheRules line"},
		{"an outside code nothing declares", func(_ *testing.T, _ *string, env *specEnv) { env.outside["openapi/gone"] = "x" },
			`codesOutsideTheRules lists "openapi/gone"`},
		{"a declared code no rule names", func(_ *testing.T, _ *string, env *specEnv) { env.declared["openapi/b"] = true },
			`code "openapi/b" is named by no rule`},
		{"a test named without its package", replace("`load.TestB`", "`TestB`"), "is not package.TestName"},
		{"a test named with a subtest", replace("`load.TestB`", "`load.TestB/sub`"), "is not package.TestName"},
		{"a test the package lacks", func(_ *testing.T, _ *string, env *specEnv) { delete(env.funcs["load"], "TestB") },
			`names "load.TestB", which internal/load does not declare`},
		{"a package with no tests", func(_ *testing.T, _ *string, env *specEnv) { delete(env.funcs, "load") },
			`names "load.TestB": holds no test file`},
		{"a citation naming no rule", func(_ *testing.T, _ *string, env *specEnv) {
			env.cites = append(env.cites, keydoc.Citation{Path: "b.md", Line: 3, Key: "gone"})
		}, `b.md:3 cites "gone", which is no rule`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			text, env := goodSpec, goodEnv()
			env.corpus, env.declared = maps.Clone(env.corpus), maps.Clone(env.declared)
			tc.plant(t, &text, &env)
			problems := specProblems(text, env)
			require.Len(t, problems, 1, "%v", problems)
			assert.Contains(t, problems[0], tc.want)
		})
	}
}

// replace plants a defect by replacing the one occurrence of from with to.
func replace(from, to string) func(*testing.T, *string, *specEnv) {
	return func(t *testing.T, text *string, _ *specEnv) {
		t.Helper()
		require.Equal(t, 1, strings.Count(*text, from), "the plant must have one site")
		*text = strings.Replace(*text, from, to, 1)
	}
}

// both plants two edits as one defect.
func both(a, b func(*testing.T, *string, *specEnv)) func(*testing.T, *string, *specEnv) {
	return func(t *testing.T, text *string, env *specEnv) {
		t.Helper()
		a(t, text, env)
		b(t, text, env)
	}
}

// TestIsTestFunc_ReadsOnlyATestsSignature holds the Tests check to the
// functions go test runs: a top-level Test… taking a *testing.T alone.
func TestIsTestFunc_ReadsOnlyATestsSignature(t *testing.T) {
	t.Parallel()
	const src = `package p

func TestA(t *testing.T)    {}
func TestB()                {}
func TestC(t *testing.B)    {}
func TestD(t testing.T)     {}
func TestE(t *T)            {}
func TestF(t *other.T)      {}
func Helper(t *testing.T)   {}
func (r) TestG(t *testing.T) {}
func TestH(t *testing.T, n int) {}
`
	f, err := parser.ParseFile(token.NewFileSet(), "p_test.go", src, parser.SkipObjectResolution)
	require.NoError(t, err)
	var tests []string
	for _, decl := range f.Decls {
		if fn, ok := decl.(*ast.FuncDecl); ok && isTestFunc(fn) {
			tests = append(tests, fn.Name.Name)
		}
	}
	assert.Equal(t, []string{"TestA"}, tests)
}
