// This file is a package-level suite, not a per-source-file test: it reads
// docs/compilers/openapi.md, the compiler's lowering specification, and holds
// each rule to the evidence it names and each citation of a rule to a rule.
package openapi_test // external test package — exercises only the public API

import (
	"fmt"
	"go/ast"
	"go/constant"
	"go/parser"
	"go/token"
	"go/types"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/internal/decls"
)

// specPath is the lowering specification, addressed relative to this test file.
const specPath = "../../docs/compilers/openapi.md"

// specRoot is the repository root, from this package's directory.
const specRoot = "../.."

// diagPackage declares the codes the compiler reports under.
const diagPackage = "github.com/dexpace/morphic/compilers/openapi/internal/diag"

// The fields a rule writes, each a bullet of its own.
const (
	casesField  = "- Cases: "
	codesField  = "- Codes: "
	testsField  = "- Tests: "
	statusField = "- Status: "
)

var (
	// ruleHeading is a rule's heading: its key, a lowercase slug, in code.
	ruleHeading = regexp.MustCompile("^### `([a-z0-9]+(?:-[a-z0-9]+)*)`$")
	// specHeading is any Markdown heading.
	specHeading = regexp.MustCompile(`^#{1,6} `)
	// codeSpan is one span of code.
	codeSpan = regexp.MustCompile("`([^`]+)`")
	// ruleCitation is a reference to a rule, from anywhere in the repository.
	ruleCitation = regexp.MustCompile(`compilers/openapi\.md#([A-Za-z0-9_-]+)`)
	// selfLink is a link from the specification to one of its own rules.
	selfLink = regexp.MustCompile(`\]\(#([A-Za-z0-9_-]+)\)`)
	// statusTracker is what a Status names to say who owns its gap: an issue,
	// or the milestone that schedules the work.
	statusTracker = regexp.MustCompile(`#[0-9]+\b|\bmilestone [0-9]+\b`)
)

// rule is one rule of the specification: its key, the line of its heading,
// and the evidence its fields name.
type rule struct {
	key    string
	line   int
	cases  []string
	codes  []string
	tests  []string
	status string
	fields map[string]bool
}

// readSpec returns the specification's rules, failing the test on any problem
// with their shape.
func readSpec(t *testing.T) []rule {
	t.Helper()
	text, err := os.ReadFile(specPath)
	require.NoError(t, err)
	rules, problems := parseSpec(string(text))
	require.Empty(t, problems, "%s is not in the shape its readers expect", specPath)
	require.NotEmpty(t, rules, "the specification holds no rule, so nothing below checks anything")
	return rules
}

// parseSpec reads the rules of a specification, and reports each problem with
// its shape: a third-level heading that is not a key, a field written twice or
// naming nothing, and a bullet no reader knows. A field runs on over the
// indented lines below it. Fenced code is not read.
func parseSpec(text string) ([]rule, []string) {
	var rules []rule
	var problems []string
	var cur *rule
	var field fieldLines
	flush := func() {
		if cur != nil && field.text != "" {
			problems = append(problems, readRuleField(cur, field.text, field.line)...)
		}
		field = fieldLines{}
	}
	fenced := false
	for i, line := range strings.Split(text, "\n") {
		n := i + 1
		if field.text != "" && strings.HasPrefix(line, "  ") && strings.TrimSpace(line) != "" {
			field.text += " " + strings.TrimSpace(line)
			continue
		}
		flush()
		if strings.HasPrefix(line, "```") {
			fenced = !fenced
			continue
		}
		switch {
		case fenced:
		case strings.HasPrefix(line, "### "):
			m := ruleHeading.FindStringSubmatch(line)
			if m == nil {
				problems = append(problems, fmt.Sprintf("line %d: a third-level heading is not a key: %q", n, line))
				cur = nil
				continue
			}
			rules = append(rules, rule{key: m[1], line: n, fields: map[string]bool{}})
			cur = &rules[len(rules)-1]
		case specHeading.MatchString(line):
			cur = nil
		case cur != nil && strings.HasPrefix(line, "- "):
			field = fieldLines{text: line, line: n}
		}
	}
	flush()
	return rules, problems
}

// fieldLines is one field of a rule as written: its bullet joined with the
// lines it runs on over, and the line the bullet opens on.
type fieldLines struct {
	text string
	line int
}

// readRuleField reads one field of r, returning the problems with it.
func readRuleField(r *rule, line string, n int) []string {
	field, spans := "", []string(nil)
	for _, f := range []string{casesField, codesField, testsField, statusField} {
		if strings.HasPrefix(line, f) {
			field = f
		}
	}
	if field == "" {
		return []string{fmt.Sprintf("line %d: %s writes a bullet no reader knows: %q", n, r.key, line)}
	}
	if r.fields[field] {
		return []string{fmt.Sprintf("line %d: %s writes %q twice", n, r.key, field)}
	}
	r.fields[field] = true
	rest := strings.TrimPrefix(line, field)
	for _, m := range codeSpan.FindAllStringSubmatch(rest, -1) {
		spans = append(spans, m[1])
	}
	switch field {
	case casesField:
		r.cases = spans
	case codesField:
		r.codes = spans
	case testsField:
		r.tests = spans
	default:
		r.status = strings.TrimSpace(rest)
		return nil
	}
	if len(spans) == 0 {
		return []string{fmt.Sprintf("line %d: %s writes %q naming nothing", n, r.key, field)}
	}
	return nil
}

// TestLoweringSpec_RulesAreKeyedAndCarryEvidence holds each rule to a key of
// its own and to evidence: a conformance case or a test that pins it, or a
// Status saying why it has neither. A rule nothing pins is one a change can
// break without any test going red, and a Status naming no issue or milestone
// is a gap nobody will schedule.
func TestLoweringSpec_RulesAreKeyedAndCarryEvidence(t *testing.T) {
	t.Parallel()
	seen := map[string]int{}
	for _, r := range readSpec(t) {
		if first, dup := seen[r.key]; dup {
			t.Errorf("line %d: %s repeats the key of line %d", r.line, r.key, first)
		}
		seen[r.key] = r.line
		if len(r.cases) == 0 && len(r.tests) == 0 && r.status == "" {
			t.Errorf("line %d: %s names no case or test that pins it, and no Status saying why", r.line, r.key)
		}
		if r.status != "" && !statusTracker.MatchString(r.status) {
			t.Errorf("line %d: %s writes a Status that names no issue (#N) or milestone tracking it", r.line, r.key)
		}
	}
}

// TestLoweringSpec_CasesAreTheCorpus holds the Cases fields to conformanceCases
// in both directions: a name no case carries is a rule citing evidence that is
// not there, and a case no rule names pins a behaviour no rule states.
func TestLoweringSpec_CasesAreTheCorpus(t *testing.T) {
	t.Parallel()
	named := map[string][]string{}
	for _, r := range readSpec(t) {
		for _, c := range r.cases {
			named[c] = append(named[c], r.key)
		}
	}
	corpus := map[string]bool{}
	for _, tc := range conformanceCases() {
		corpus[tc.file] = true
	}
	for _, c := range slices.Sorted(maps.Keys(named)) {
		assert.True(t, corpus[c], "rules %v name the case %q, which conformanceCases does not hold", named[c], c)
	}
	for _, c := range slices.Sorted(maps.Keys(corpus)) {
		assert.NotEmpty(t, named[c], "conformance case %q is named by no rule of %s", c, specPath)
	}
}

// TestLoweringSpec_CodesAreDeclared holds the Codes fields to the codes the
// diag package declares, in both directions. A code no rule names is listed
// in codesOutsideTheRules with the reason it has no rule; a code both named
// and listed fails, so closing the gap means deleting its line.
func TestLoweringSpec_CodesAreDeclared(t *testing.T) {
	t.Parallel()
	declared := declaredDiagCodes(t)
	named := map[string][]string{}
	for _, r := range readSpec(t) {
		for _, c := range r.codes {
			named[c] = append(named[c], r.key)
		}
	}
	outside := codesOutsideTheRules()
	for _, c := range slices.Sorted(maps.Keys(named)) {
		assert.True(t, declared[c], "rules %v name the code %q, which %s does not declare", named[c], c, diagPackage)
		assert.NotContains(t, outside, c, "%q is named by rules %v; delete its codesOutsideTheRules line", c, named[c])
	}
	for _, c := range slices.Sorted(maps.Keys(outside)) {
		assert.True(t, declared[c], "codesOutsideTheRules lists %q, which %s does not declare", c, diagPackage)
	}
	for _, c := range slices.Sorted(maps.Keys(declared)) {
		_, listed := outside[c]
		assert.True(t, listed || len(named[c]) > 0,
			"code %q is named by no rule of %s and not listed in codesOutsideTheRules", c, specPath)
	}
}

// codesOutsideTheRules lists the codes no lowering rule names, each with the
// reason. Each is reported while a source is read, patched or refused, before
// any construct lowers, so its own doc comment in package diag is where it is
// stated.
func codesOutsideTheRules() map[string]string {
	const (
		reading = "reports on reading the source, before any construct lowers"
		overlay = "reports on applying an overlay, before any construct lowers"
	)
	return map[string]string{
		"openapi/alias-amplification":       reading,
		"openapi/cycle-scan-failed":         reading,
		"openapi/cyclic-ref":                reading,
		"openapi/internal-invariant":        "reports a defect of the compiler, not of the source",
		"openapi/invalid-overlay":           overlay,
		"openapi/overlay-action":            overlay,
		"openapi/overlay-failed":            overlay,
		"openapi/overlay-origin-incomplete": overlay,
		"openapi/source-too-deep":           reading,
		"openapi/source-too-large":          reading,
		"openapi/stream-documents-dropped":  reading,
		"openapi/tagged-mapping":            reading,
		"openapi/undecodable-source":        reading,
		"openapi/unsupported-version":       reading,
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
// package.TestName with an optional /subtest, to be a test function declared
// in that package's test files: openapi for the compiler itself, or a package
// under its internal directory.
func TestLoweringSpec_TestsAreDeclared(t *testing.T) {
	t.Parallel()
	declared := map[string]map[string]bool{}
	for _, r := range readSpec(t) {
		for _, name := range r.tests {
			pkg, fn, ok := strings.Cut(strings.SplitN(name, "/", 2)[0], ".")
			if !assert.True(t, ok && strings.HasPrefix(fn, "Test"),
				"line %d: %s names %q, which is not package.TestName", r.line, r.key, name) {
				continue
			}
			if declared[pkg] == nil {
				declared[pkg] = testFuncs(t, pkg)
			}
			assert.True(t, declared[pkg][fn], "line %d: %s names %q, which %s does not declare",
				r.line, r.key, name, testDir(pkg))
		}
	}
}

// testDir returns the directory of the package a test name's prefix stands
// for, relative to this package.
func testDir(pkg string) string {
	if pkg == "openapi" {
		return "."
	}
	return filepath.Join("internal", pkg)
}

// testFuncs returns the name of every function the test files of pkg declare.
// It parses them rather than loading pkg through internal/decls, which reads no
// test files: a test function is a top-level declaration, so a parse sees it
// exactly as the compiler does.
func testFuncs(t *testing.T, pkg string) map[string]bool {
	t.Helper()
	paths, err := filepath.Glob(filepath.Join(testDir(pkg), "*_test.go"))
	require.NoError(t, err)
	require.NotEmpty(t, paths, "%s holds no test file", testDir(pkg))
	out := map[string]bool{}
	fset := token.NewFileSet()
	for _, path := range paths {
		f, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		require.NoError(t, err)
		for _, decl := range f.Decls {
			if fn, ok := decl.(*ast.FuncDecl); ok && fn.Recv == nil {
				out[fn.Name.Name] = true
			}
		}
	}
	return out
}

// TestLoweringSpec_CitationsNameARule requires every citation of the
// specification in the repository, and every link it makes to its own rules,
// to name a rule, so renaming or removing one fails until its citations follow.
func TestLoweringSpec_CitationsNameARule(t *testing.T) {
	t.Parallel()
	keys := map[string]bool{}
	for _, r := range readSpec(t) {
		keys[r.key] = true
	}
	spec, err := filepath.Abs(specPath)
	require.NoError(t, err)
	cited := 0
	err = filepath.WalkDir(specRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if path != specRoot && strings.HasPrefix(d.Name(), ".") {
				return filepath.SkipDir
			}
			return nil
		}
		if !citingFile(path) {
			return nil
		}
		n, problems, err := fileCitations(path, spec, keys)
		cited += n
		for _, p := range problems {
			t.Error(p)
		}
		return err
	})
	require.NoError(t, err)
	require.NotZero(t, cited, "found no citation of %s, so this proves nothing", specPath)
}

// citingFile reports whether a file at path is one a citation may be written
// in: Go and YAML sources, and Markdown.
func citingFile(path string) bool {
	switch filepath.Ext(path) {
	case ".go", ".yaml", ".yml", ".md":
		return true
	default:
		return false
	}
}

// fileCitations counts the citations in the file at path, and reports each
// naming no key. The specification's own links to its rules count too.
func fileCitations(path, spec string, keys map[string]bool) (int, []string, error) {
	text, err := os.ReadFile(path)
	if err != nil {
		return 0, nil, err
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return 0, nil, err
	}
	patterns := []*regexp.Regexp{ruleCitation}
	if abs == spec {
		patterns = append(patterns, selfLink)
	}
	n := 0
	var problems []string
	for i, line := range strings.Split(string(text), "\n") {
		for _, p := range patterns {
			for _, m := range p.FindAllStringSubmatch(line, -1) {
				n++
				if !keys[m[1]] {
					problems = append(problems, fmt.Sprintf("%s:%d cites %q, which is no rule of %s", path, i+1, m[1], specPath))
				}
			}
		}
	}
	return n, problems, nil
}
