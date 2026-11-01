// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

// This file is a package-level suite, not a per-source-file test: it reads
// docs/compilers/openapi.md, the compiler's specification, and holds each rule
// to the evidence it names and each citation of a rule to a rule.
package e2e_test // external test package — exercises only the public API

import (
	"fmt"
	"go/ast"
	"go/constant"
	"go/parser"
	"go/token"
	"go/types"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/internal/decls"
	"github.com/dexpace/morphic/internal/keydoc"
)

// specRoot is the repository root, from this package's directory.
const specRoot = "../../.."

// specDoc is the specification, relative to specRoot.
const specDoc = "docs/compilers/openapi.md"

// specCitedAs is how a citation spells the specification, before the "#" and
// the key.
const specCitedAs = "compilers/openapi.md"

// diagPackage declares the codes the compiler reports under.
const diagPackage = "github.com/dexpace/morphic/compilers/openapi/internal/diag"

// The fields a rule writes, each a bullet of its own.
const (
	casesField  = "Cases"
	codesField  = "Codes"
	testsField  = "Tests"
	statusField = "Status"
)

var (
	// statusTracker is what a Status names to say who owns what it leaves
	// open: an issue, or the milestone that schedules the work.
	statusTracker = regexp.MustCompile(`#[0-9]+\b|\bmilestone [0-9]+\b`)
	// issueRef is an issue number written in prose.
	issueRef = regexp.MustCompile(`(?:^|[^\w&/])(#[0-9]+)\b`)
	// codeSpanText is one span of code, delimiters included.
	codeSpanText = regexp.MustCompile("`[^`]+`")
	// testName is how a Tests field names a test: package.TestName.
	testName = regexp.MustCompile(`^([a-z][a-z0-9]*)\.(Test[A-Za-z0-9_]*)$`)
)

// rule is one rule of the specification: its key, the line of its heading,
// its text, and what its fields name.
type rule struct {
	key    string
	line   int
	text   string
	cases  []string
	codes  []string
	tests  []string
	status string
}

// readSpec returns the specification's rules, failing the test on any problem
// with their shape.
func readSpec(t *testing.T) []rule {
	t.Helper()
	text, err := os.ReadFile(filepath.Join(specRoot, specDoc))
	require.NoError(t, err)
	rules, problems := parseSpec(string(text))
	require.Empty(t, problems, "%s is not in the shape its readers expect", specDoc)
	require.NotEmpty(t, rules, "the specification holds no rule, so nothing below checks anything")
	return rules
}

// parseSpec reads the rules of a specification through keydoc, and reports
// each problem with their shape: keydoc's, a field naming nothing, and prose in
// a Cases, Codes or Tests field, which lists spans of code alone.
func parseSpec(text string) ([]rule, []string) {
	entries, problems := keydoc.Parse(text, casesField, codesField, testsField, statusField)
	rules := make([]rule, 0, len(entries))
	for _, e := range entries {
		r := rule{key: e.Key, line: e.Line, text: e.Text}
		for _, f := range e.Fields {
			if f.Text() == "" {
				problems = append(problems, fmt.Sprintf("line %d: %s writes %q naming nothing", f.Line, r.key, f.Name))
				continue
			}
			if f.Name == statusField {
				r.status = f.Text()
				continue
			}
			spans := keydoc.Spans(f.Text())
			switch f.Name {
			case casesField:
				r.cases = spans
			case codesField:
				r.codes = spans
			default:
				r.tests = spans
			}
			if strings.Trim(codeSpanText.ReplaceAllString(f.Text(), ""), ", ") != "" {
				problems = append(problems, fmt.Sprintf("line %d: %s writes prose in %q, which lists spans of code "+
					"alone", f.Line, r.key, f.Name))
			}
		}
		rules = append(rules, r)
	}
	return rules, problems
}

// TestLoweringSpec_RulesAreKeyedAndCarryEvidence holds each rule to a key of
// its own and to evidence: a conformance case or a test that pins it, or a
// Status saying what it leaves open. A rule nothing pins is one a change can
// break without any test going red, and a Status naming no issue or milestone
// is a gap nobody will schedule.
func TestLoweringSpec_RulesAreKeyedAndCarryEvidence(t *testing.T) {
	t.Parallel()
	report(t, evidenceProblems(readSpec(t)))
}

// evidenceProblems reports a key two rules share, a rule naming no case, test
// or Status, and a Status naming no issue or milestone.
func evidenceProblems(rules []rule) []string {
	var problems []string
	seen := map[string]int{}
	for _, r := range rules {
		if first, dup := seen[r.key]; dup {
			problems = append(problems, fmt.Sprintf("line %d: %s repeats the key of line %d", r.line, r.key, first))
		}
		seen[r.key] = r.line
		if len(r.cases) == 0 && len(r.tests) == 0 && r.status == "" {
			problems = append(problems, fmt.Sprintf("line %d: %s names no case or test that pins it, "+
				"and no Status saying what it leaves open", r.line, r.key))
		}
		if r.status != "" && !statusTracker.MatchString(r.status) {
			problems = append(problems, fmt.Sprintf("line %d: %s writes a Status that names no issue (#N) "+
				"or milestone tracking it", r.line, r.key))
		}
	}
	return problems
}

// TestLoweringSpec_TextStatesEachCodeAndNoIssue holds a rule's text to the
// codes it lists and its Status to the issues it names. A code listed and
// never written in the text is one the rule reports without saying when; an
// issue named in the text is a gap or a question kept where no reader of the
// Status finds it, and a closed one is history the rule need not carry.
func TestLoweringSpec_TextStatesEachCodeAndNoIssue(t *testing.T) {
	t.Parallel()
	report(t, textProblems(readSpec(t)))
}

// textProblems reports each code a rule lists that its text does not write as
// a span of code, and each issue number its text writes outside one.
func textProblems(rules []rule) []string {
	var problems []string
	for _, r := range rules {
		spans := keydoc.Spans(r.text)
		for _, c := range r.codes {
			if !slices.Contains(spans, c) {
				problems = append(problems, fmt.Sprintf("line %d: %s lists %q, which its text never writes "+
					"as `%s` where it says when the code fires", r.line, r.key, c, c))
			}
		}
		if m := issueRef.FindStringSubmatch(codeSpanText.ReplaceAllString(r.text, "")); m != nil {
			problems = append(problems, fmt.Sprintf("line %d: %s names %s in its text; an issue belongs in "+
				"its Status", r.line, r.key, m[1]))
		}
	}
	return problems
}

// TestLoweringSpec_CasesAreTheCorpus holds the Cases fields to conformanceCases
// in both directions: a name no case carries is a rule citing evidence that is
// not there, and a case no rule names pins a behaviour no rule states.
func TestLoweringSpec_CasesAreTheCorpus(t *testing.T) {
	t.Parallel()
	corpus := map[string]bool{}
	for _, tc := range conformanceCases() {
		corpus[tc.file] = true
	}
	report(t, caseProblems(readSpec(t), corpus))
}

// caseProblems reports each case a rule names that corpus lacks, and each
// case of corpus no rule names.
func caseProblems(rules []rule, corpus map[string]bool) []string {
	named := map[string][]string{}
	for _, r := range rules {
		for _, c := range r.cases {
			named[c] = append(named[c], r.key)
		}
	}
	var problems []string
	for _, c := range slices.Sorted(maps.Keys(named)) {
		if !corpus[c] {
			problems = append(problems, fmt.Sprintf("rules %v name the case %q, which conformanceCases does not hold",
				named[c], c))
		}
	}
	for _, c := range slices.Sorted(maps.Keys(corpus)) {
		if len(named[c]) == 0 {
			problems = append(problems, fmt.Sprintf("conformance case %q is named by no rule of %s", c, specDoc))
		}
	}
	return problems
}

// TestLoweringSpec_CodesAreDeclared holds the Codes fields to the codes the
// diag package declares, in both directions. A code no rule names is listed
// in codesOutsideTheRules with the reason it has no rule; a code both named
// and listed fails, so closing the gap means deleting its line.
func TestLoweringSpec_CodesAreDeclared(t *testing.T) {
	t.Parallel()
	report(t, codeProblems(readSpec(t), declaredDiagCodes(t), codesOutsideTheRules()))
}

// codeProblems reports a code a rule names that declared lacks or outside
// lists, a code outside lists that declared lacks, and a declared code no rule
// names and outside does not list.
func codeProblems(rules []rule, declared map[string]bool, outside map[string]string) []string {
	named := map[string][]string{}
	for _, r := range rules {
		for _, c := range r.codes {
			named[c] = append(named[c], r.key)
		}
	}
	var problems []string
	for _, c := range slices.Sorted(maps.Keys(named)) {
		if !declared[c] {
			problems = append(problems, fmt.Sprintf("rules %v name the code %q, which %s does not declare",
				named[c], c, diagPackage))
		}
		if _, listed := outside[c]; listed {
			problems = append(problems, fmt.Sprintf("%q is named by rules %v; delete its codesOutsideTheRules line",
				c, named[c]))
		}
	}
	for _, c := range slices.Sorted(maps.Keys(outside)) {
		if !declared[c] {
			problems = append(problems, fmt.Sprintf("codesOutsideTheRules lists %q, which %s does not declare",
				c, diagPackage))
		}
	}
	for _, c := range slices.Sorted(maps.Keys(declared)) {
		if _, listed := outside[c]; !listed && len(named[c]) == 0 {
			problems = append(problems, fmt.Sprintf("code %q is named by no rule of %s and not listed in "+
				"codesOutsideTheRules", c, specDoc))
		}
	}
	return problems
}

// codesOutsideTheRules lists the codes no rule names, each with the reason.
func codesOutsideTheRules() map[string]string {
	return map[string]string{
		"openapi/internal-invariant": "reports a defect of the compiler, not anything about the source",
	}
}

// declaredDiagCodes returns the value of every exported string constant the
// diag package declares, read off the type checker as diag's own pin reads it.
func declaredDiagCodes(t *testing.T) map[string]bool {
	t.Helper()
	p, err := decls.Load(diagPackage)
	require.NoError(t, err)
	out := map[string]bool{}
	for c := range p.Objects[*types.Const]() {
		if c.Exported() && c.Val().Kind() == constant.String {
			out[constant.StringVal(c.Val())] = true
		}
	}
	require.NotEmpty(t, out, "found no declared codes")
	return out
}

// TestLoweringSpec_TestsAreDeclared requires each test a rule names, as
// package.TestName, to be a test function the test files of that package
// declare: openapi for the compiler itself, e2e for this package, or a package
// under the compiler's internal directory.
func TestLoweringSpec_TestsAreDeclared(t *testing.T) {
	t.Parallel()
	report(t, testProblems(readSpec(t), testFuncs))
}

// testProblems reports each test a rule names that is not written
// package.TestName, or that funcsOf finds no test function for.
func testProblems(rules []rule, funcsOf func(pkg string) (map[string]bool, error)) []string {
	declared := map[string]map[string]bool{}
	var problems []string
	for _, r := range rules {
		for _, name := range r.tests {
			m := testName.FindStringSubmatch(name)
			if m == nil {
				problems = append(problems, fmt.Sprintf("line %d: %s names %q, which is not package.TestName",
					r.line, r.key, name))
				continue
			}
			pkg, fn := m[1], m[2]
			if _, read := declared[pkg]; !read {
				funcs, err := funcsOf(pkg)
				if err != nil {
					problems = append(problems, fmt.Sprintf("line %d: %s names %q: %v", r.line, r.key, name, err))
				}
				declared[pkg] = funcs
			}
			if declared[pkg] != nil && !declared[pkg][fn] {
				problems = append(problems, fmt.Sprintf("line %d: %s names %q, which %s does not declare",
					r.line, r.key, name, packageDir(pkg)))
			}
		}
	}
	return problems
}

// packageDir returns the directory, from the repository root, of the package
// a test name's prefix stands for: the compiler itself, this package, or one
// under the compiler's internal directory.
func packageDir(pkg string) string {
	switch pkg {
	case "openapi":
		return "compilers/openapi"
	case "e2e":
		return "compilers/openapi/e2e"
	default:
		return "compilers/openapi/internal/" + pkg
	}
}

// testFuncs returns the name of every test function the test files of pkg
// declare: a top-level function named Test… taking a *testing.T alone. It
// parses them rather than loading pkg through internal/decls, which reads no
// test files: a test function is a top-level declaration, so a parse sees it
// exactly as the compiler does.
func testFuncs(pkg string) (map[string]bool, error) {
	paths, err := filepath.Glob(filepath.Join(specRoot, packageDir(pkg), "*_test.go"))
	if err != nil || len(paths) == 0 {
		return nil, fmt.Errorf("%s holds no test file", packageDir(pkg))
	}
	out := map[string]bool{}
	fset := token.NewFileSet()
	for _, path := range paths {
		f, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if err != nil {
			return nil, err
		}
		for _, decl := range f.Decls {
			if fn, ok := decl.(*ast.FuncDecl); ok && isTestFunc(fn) {
				out[fn.Name.Name] = true
			}
		}
	}
	return out, nil
}

// isTestFunc reports whether fn is a test function: TestName(t *testing.T).
// It matches testing by its literal name, so a test in a file that aliases
// testing is missed, and a rule naming it fails TestLoweringSpec_TestsAreDeclared
// (#1234). A T from another package named testing needs no reading: go test
// refuses to build a test taking one.
func isTestFunc(fn *ast.FuncDecl) bool {
	if fn.Recv != nil || !strings.HasPrefix(fn.Name.Name, "Test") || fn.Type.Params.NumFields() != 1 {
		return false
	}
	star, ok := fn.Type.Params.List[0].Type.(*ast.StarExpr)
	if !ok {
		return false
	}
	sel, ok := star.X.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	pkg, ok := sel.X.(*ast.Ident)
	return ok && pkg.Name == "testing" && sel.Sel.Name == "T"
}

// TestLoweringSpec_CitationsNameARule requires every citation of the
// specification in the repository, and every link it makes to its own rules,
// to name a rule, so renaming or removing one fails until its citations follow.
func TestLoweringSpec_CitationsNameARule(t *testing.T) {
	t.Parallel()
	cites, err := keydoc.Citations(specRoot, specDoc, specCitedAs)
	require.NoError(t, err)
	require.NotEmpty(t, cites, "found no citation of %s, so this proves nothing", specDoc)
	report(t, citationProblems(readSpec(t), cites))
}

// citationProblems reports each citation naming no rule.
func citationProblems(rules []rule, cites []keydoc.Citation) []string {
	keys := map[string]bool{}
	for _, r := range rules {
		keys[r.key] = true
	}
	var problems []string
	for _, c := range cites {
		if !keys[c.Key] {
			problems = append(problems, fmt.Sprintf("%s:%d cites %q, which is no rule of %s",
				c.Path, c.Line, c.Key, specDoc))
		}
	}
	return problems
}

// report fails t with each problem.
func report(t *testing.T, problems []string) {
	t.Helper()
	for _, p := range problems {
		t.Error(p)
	}
}
