package canary_test

// The validation oracle (GitHub #918): the compiler holds a document to the
// OpenAPI Specification's rules itself (compilers/openapi/internal/oas), and
// this holds what it finds to what the parser library's validation finds in
// the same document. The two agree except where the library reads the
// specification otherwise, or where another package owns a rule; each such
// disagreement is listed in validationDisagreements with the reason, and one
// the list does not hold fails.

import (
	"encoding/json/jsontext"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/speakeasy-api/openapi/marshaller"
	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/speakeasy-api/openapi/validation"
	"github.com/speakeasy-api/openapi/yml"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	yaml "gopkg.in/yaml.v3"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/load"
	"github.com/dexpace/morphic/compilers/openapi/internal/nodeview"
	"github.com/dexpace/morphic/compilers/openapi/internal/oas"
	"github.com/dexpace/morphic/ir"
)

const (
	// validationDisagreements lists every disagreement and its reason.
	validationDisagreements = "testdata/validation_disagreements.txt"
	// validationSpecs holds the documents written for the rules the table
	// states, each at a version that makes the difference visible.
	validationSpecs = "testdata/validation"
	// sideLibrary and sideCompiler name who found a finding the other did not.
	sideLibrary  = "library"
	sideCompiler = "compiler"
)

// validationReasons are the reasons a disagreement may give, each saying which
// side reads the specification otherwise, and what decides it.
var validationReasons = map[string]string{
	"empty-is-missing": "the library reads a required field written as the empty string, or as an empty map, as " +
		"missing; the specification requires the field to be written and " +
		"types it a string or a map, which an empty value is (3.1.2 §Info Object: 'title ... REQUIRED. The title of the " +
		"API'), and states no grammar that refuses it",
	"empty-refused": "the table reports an empty value under the rule of the specification it breaks, where the " +
		"library calls the field missing (empty-is-missing): a Server Object url from 3.2, whose grammar needs a " +
		"character (3.2.0 §Server Variable Object: 'server-url-template = 1*( literals / server-variable )'); a " +
		"Parameter Object in, which must be one of its listed values (3.2.0 §Parameter Object: 'Possible values " +
		"are'); a path parameter name, which must match a template expression (3.2.0 §Parameter Object: 'the name " +
		"field MUST correspond to a single template expression')",
	"relaxed-requirement": "the library requires a field the declared minor made optional, an Operation's responses " +
		"from 3.1 and a Response's description from 3.2; 3.0.4 §Operation " +
		"Object marks responses REQUIRED and 3.1.2 does not, 3.1.2 §Response Object marks description REQUIRED and " +
		"3.2.0 does not",
	"version-gate": "the library does not gate a field or value by the declared minor; the table warns, as " +
		"later-version-construct, of one that only " +
		"a later minor's Fixed Fields define, such as an Info Object summary (3.1.2 §Info Object) or a Server Object " +
		"name (3.2.0 §Server Object), and lowers it as that minor does",
	"ref-siblings": "the library reads a 3.0 schema's $ref and the keywords beside it and says nothing of them; " +
		"3.0 reads the position as a Reference Object (3.0.4 §Reference Object: 'This object cannot be extended " +
		"with additional properties, and any properties added SHALL be ignored'), while 3.1 applies them as the " +
		"schema's keywords, so the table warns of each, as later-version-construct, and lowers it as 3.1 does",
	"later-refused": "the library refuses a construct a later minor added as an error (additionalOperations before " +
		"3.2, a cookie parameter's style: cookie before 3.2, a discriminator's defaultMapping or an xml nodeType " +
		"before 3.2, which its 3.1 meta-schema closes); the table reports " +
		"it as the later-version-construct warning, since only a later minor defines it, and lowers it as that minor",
	"old-grammar": "the library checks a 3.2 document against the 3.1 grammar, a schema's own Validate discarding " +
		"the version it is handed, and refuses what 3.2 added: style: cookie on a cookie parameter (3.2.0 §Parameter Object), a discriminator's " +
		"defaultMapping (3.2.0 §Discriminator Object), an xml nodeType (3.2.0 §XML Object)",
	"duplicate-report": "the library reports one defect more than once, from its model and again from its " +
		"meta-schema or a second check of the same field: a Discriminator Object without propertyName, a Parameter " +
		"Object without in, a $ref where a schema belongs; the table reports it once",
	"position": "both sides report the defect and place it differently: the library on the key of the entry (the " +
		"additionalOperations method, the repeated key), the table on the entry's value, the node its pointer names",
	"numeric-literal": "the library holds a numeric keyword to a float64 and checks a schema as JSON text, so it " +
		"refuses a literal YAML reads as a number that JSON spells otherwise (.5, 5., 0644) or float64 cannot hold " +
		"(1e999); the table leaves the shape of a bound to the schema lowering, which reads the literal exactly and " +
		"reports one that is no finite decimal (docs/compilers/openapi.md#constraints)",
	"over-strict": "the library warns of a field written beside a type it does not apply to; the specification's " +
		"'Applies To' column says where a field has meaning and bars it nowhere (3.2.0 §Security Scheme Object: " +
		"bearerFormat applies to http), so the table accepts it, and the security scheme lowering keeps the field " +
		"under Unmodeled and says so",
	"owned": "another package reports the rule, and the table states it and leaves it to them: a repeated " +
		"operationId is judged once every operation is lowered (docs/compilers/openapi.md#operation-ids), a repeated " +
		"parameter by the load phase's check of every parameter list (docs/compilers/openapi.md#parameters), an " +
		"HTTP scheme written empty by the security scheme lowering (docs/compilers/openapi.md#security-requirements)",
	"beside-ref": "the library models a Path Item that writes a $ref as the reference alone, and validates none of " +
		"the fields beside it; the table reads them as the item's own, as the lowering does " +
		"(docs/compilers/openapi.md#path-item-ref-fields)",
	"inapplicable": "the library accepts allowReserved at every location; the specification gives it meaning for " +
		"query alone before 3.2 (3.1.2 §Parameter Object: 'This field only applies to parameters with an in value of " +
		"query') and for the locations and styles that percent-encode from 3.2, so the table warns and lowers it as " +
		"declared",
	"library-silent": "the library has no check for a rule the specification states and the table cites: a " +
		"Parameter or Header Object holds schema or content, not both (3.1.2 §Parameter Object: 'Parameter Objects " +
		"MUST include either a content field or a schema field, but not both'), an XML Object's nodeType bars wrapped " +
		"(3.2.0 §XML Object), a Header Object writes no in (3.2.0 §Header Object: 'in MUST NOT be specified'), and a " +
		"$ref is a URI reference, which holds no space (3.1.2 §Reference Object: 'This MUST be in the form of a URI'), and " +
		"a header name is a token, which a slash is not (3.1.2 §Parameter Object: 'states header names are case " +
		"insensitive')",
	"querystring-keywords": "the library accepts explode and allowReserved at in: querystring, which 3.2 bars " +
		"there as it bars style (3.2.0 §Parameter Object: 'These fields MUST NOT be used with in: \"querystring\"'); " +
		"the table warns of each as invalid-location-keyword: the content says what the parameter means, and the " +
		"value lowers as declared",
	"analogy": "the library has no check for it, and the specification words the rule only by reference: an Encoding " +
		"Object's nested encodings apply 'in the same manner' as the Media Type Object's, which MUST NOT be written " +
		"together (3.2.0 §Media Type Object); the table reads that as a warning",
	"component-names": "the library does not check the keys of the Components Object's maps, only a $ref naming " +
		"an empty component; the specification requires them to match ^[a-zA-Z0-9\\.\\-_]+$ (3.1.2 §Components " +
		"Object: 'All the fixed fields declared above are objects that MUST use keys that match the regular " +
		"expression'), which the table reports where the key is written",
	"path-templating": "the library does not match a path's template expressions to its path parameters, which the " +
		"specification requires (3.1.2 §Path Templating: 'Each template expression in the path MUST correspond to a " +
		"path parameter')",
}

// validationDoc is one document the oracle checks, under its repository path.
type validationDoc struct {
	name string
	data []byte
}

// finding is what the oracle compares of a finding: the rule it is reported
// under and the position of the node it is reported at.
type finding struct {
	class        string
	line, column int
}

// key identifies a finding whatever its side.
func (f finding) key() string { return fmt.Sprintf("%d:%d\t%s", f.line, f.column, f.class) }

// validationRow is a finding only one side makes.
type validationRow struct {
	doc  string
	f    finding
	side string
}

// key identifies a row whatever its reason.
func (r validationRow) key() string {
	return strings.Join([]string{r.doc, r.f.key(), r.side}, "\t")
}

// TestValidationOracle_AgreesWithTheLibrary is the oracle. For each document
// of the corpus that declares an OpenAPI 3 minor the table covers, it compares
// the findings of the library's validation with the table's, as multisets of
// the rule and the position, and requires every finding only one makes to be
// listed with its reason, and every listing to be one.
func TestValidationOracle_AgreesWithTheLibrary(t *testing.T) {
	t.Parallel()
	var rows []validationRow
	for _, d := range validationDocs(t) {
		library, compiler, skipped := compareDocument(t, d)
		if skipped != "" {
			t.Logf("skipped %s: %s", d.name, skipped)
			continue
		}
		rows = append(rows, onlyOneSide(d.name, library, compiler)...)
	}
	listed := readValidationRows(t)
	if *update {
		writeValidationRows(t, rows, listed)
		return
	}
	checkValidationRows(t, rows, listed)
}

// validationDocs returns every document of the corpus: the repository's
// testdata and the documents of validationSpecs, those that declare an
// OpenAPI 3 version.
func validationDocs(t *testing.T) []validationDoc {
	t.Helper()
	var docs []validationDoc
	for _, root := range []string{filepath.Join(repoRoot, "testdata"), validationSpecs} {
		err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return err
			}
			switch filepath.Ext(path) {
			case ".yaml", ".yml", ".json":
			default:
				return nil
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			if _, _, ok := declaredVersion(data); ok {
				docs = append(docs, validationDoc{name: repoPath(t, path), data: data})
			}
			return nil
		})
		require.NoError(t, err)
	}
	slices.SortFunc(docs, func(a, b validationDoc) int { return strings.Compare(a.name, b.name) })
	require.NotEmpty(t, docs)
	return docs
}

// repoPath is path relative to the repository, with forward slashes.
func repoPath(t *testing.T, path string) string {
	t.Helper()
	abs, err := filepath.Abs(path)
	require.NoError(t, err)
	root, err := filepath.Abs(repoRoot)
	require.NoError(t, err)
	rel, err := filepath.Rel(root, abs)
	require.NoError(t, err)
	return filepath.ToSlash(rel)
}

// declaredVersion returns the OpenAPI version a document declares, and false
// for data that is no YAML or JSON document declaring an "openapi" 3 string.
func declaredVersion(data []byte) (string, *yaml.Node, bool) {
	parsed, err := load.Decode(data)
	if err != nil {
		return "", nil, false
	}
	root := nodeview.DocumentRoot(parsed.Root())
	if root == nil || root.Kind != yaml.MappingNode {
		return "", nil, false
	}
	for i := 0; i+1 < len(root.Content); i += 2 {
		v := root.Content[i+1]
		if root.Content[i].Value == "openapi" && v.Kind == yaml.ScalarNode && strings.HasPrefix(v.Value, "3.") {
			return v.Value, parsed.Root(), true
		}
	}
	return "", nil, false
}

// compareDocument returns the findings of the library and of the table over
// one document, or why it is skipped: a version the table does not cover, or
// a document the library will not build.
func compareDocument(t *testing.T, d validationDoc) (library, compiler []finding, skipped string) {
	t.Helper()
	version, root, _ := declaredVersion(d.data)
	if _, ok := oas.ParseMinor(version); !ok {
		return nil, nil, fmt.Sprintf("declares %q, a version the table does not cover", version)
	}
	if refused := loadRefusal(t, d); refused != "" {
		return nil, nil, refused
	}
	compiler = tableFindings(root, version)
	library, err := libraryFindings(t, d.data, root)
	if err != nil {
		return nil, nil, err.Error()
	}
	return library, compiler, ""
}

// loadRefusal says why the load phase refuses a document before it validates
// it, or is empty when it does not: a document it never validates has no
// findings to compare, and some, such as an alias bomb, must not reach the
// library at all.
func loadRefusal(t *testing.T, d validationDoc) string {
	t.Helper()
	doc, diags, err := load.Load(t.Context(), 0, compilers.Source{Path: d.name, Data: d.data}, load.Options{})
	if err != nil {
		return "load fails: " + err.Error()
	}
	if doc != nil {
		return ""
	}
	for _, dg := range diags {
		if dg.Severity == ir.SeverityError {
			return "load refuses it: " + dg.Code
		}
	}
	return "load refuses it"
}

// tableFindings checks the tree with the table at the declared version.
func tableFindings(root *yaml.Node, version string) []finding {
	view := nodeview.New()
	locate := func(ptr jsontext.Pointer) ir.Provenance {
		path, _ := view.DocumentPath(nodeview.DocumentRoot(root), ptr)
		prov := ir.Provenance{Pointer: ptr}
		if len(path) > 0 {
			prov.Position = ir.Position{Line: path[len(path)-1].Line, Column: path[len(path)-1].Column}
		}
		return prov
	}
	diags := oas.NewChecker().Check(root, version, oas.Options{Locate: locate})
	out := make([]finding, 0, len(diags))
	for _, d := range diags {
		out = append(out, finding{class: classOf(d.Code), line: d.Provenance.Position.Line, column: d.Provenance.Position.Column})
	}
	return out
}

// classOf is the rule a diagnostic code reports: the part after the
// validation prefix, else the code without its package.
func classOf(code string) string {
	if class, ok := strings.CutPrefix(code, diag.Validation+"/"); ok {
		return class
	}
	return strings.TrimPrefix(code, "openapi/")
}

// libraryFindings builds the library's model of root as the load phase does
// and returns the findings of its decode and of its validation, or why it
// could not: an error, or a panic, which the parser raises on degenerate
// input (docs/parser-library.md#recoverable-panic).
func libraryFindings(t *testing.T, data []byte, root *yaml.Node) (out []finding, err error) {
	t.Helper()
	defer func() {
		if r := recover(); r != nil {
			out, err = nil, fmt.Errorf("the library panicked: %v", r)
		}
	}()
	releaseAnchorNames(root)
	var model soa.OpenAPI
	model.InitCache()
	model.GetCore().SetConfig(yml.GetConfigFromDoc(data, root))
	found, err := marshaller.UnmarshalNode(t.Context(), "", root, &model)
	if err != nil {
		return nil, fmt.Errorf("the library refuses the document: %w", err)
	}
	found = append(found, model.Validate(t.Context())...)
	for _, f := range found {
		out = append(out, libraryFinding(f))
	}
	return out, nil
}

// libraryFinding is the finding a validation error reports.
func libraryFinding(err error) finding {
	for e := err; e != nil; e = errors.Unwrap(e) {
		// Hand-walked: errors.As would call the library's errors.Error As
		// method, which matches any target named Error and panics on this one.
		switch v := any(e).(type) {
		case *validation.Error:
			return findingAt(v.Rule, v.Node)
		case validation.Error:
			return findingAt(v.Rule, v.Node)
		}
	}
	return finding{class: "unstructured"}
}

// findingAt is a finding of the rule at the node, or at no position.
func findingAt(rule string, n *yaml.Node) finding {
	if n == nil {
		return finding{class: rule}
	}
	return finding{class: rule, line: n.Line, column: n.Column}
}

// releaseAnchorNames clears the anchor names of the tree, as the load phase
// does before it hands the tree to the library.
func releaseAnchorNames(root *yaml.Node) {
	stack := []*yaml.Node{root}
	for len(stack) > 0 {
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		n.Anchor = ""
		stack = append(stack, n.Content...)
	}
}

// onlyOneSide returns a row for each finding of one side that the other lacks,
// compared as multisets: a finding the library makes twice and the table once
// leaves one row.
func onlyOneSide(doc string, library, compiler []finding) []validationRow {
	count := map[finding]int{}
	for _, f := range library {
		count[f]++
	}
	for _, f := range compiler {
		count[f]--
	}
	var rows []validationRow
	for f, n := range count {
		side := sideLibrary
		if n < 0 {
			side, n = sideCompiler, -n
		}
		for range n {
			rows = append(rows, validationRow{doc: doc, f: f, side: side})
		}
	}
	slices.SortFunc(rows, cmpRows)
	return rows
}

// cmpRows orders rows by document, position, rule and side.
func cmpRows(a, b validationRow) int {
	switch {
	case a.doc != b.doc:
		return strings.Compare(a.doc, b.doc)
	case a.f.line != b.f.line:
		return a.f.line - b.f.line
	case a.f.column != b.f.column:
		return a.f.column - b.f.column
	case a.f.class != b.f.class:
		return strings.Compare(a.f.class, b.f.class)
	}
	return strings.Compare(a.side, b.side)
}

// readValidationRows reads validationDisagreements: one row a line, its fields
// tab-separated, the reason last. A row repeats as often as the finding does.
func readValidationRows(t *testing.T) map[string][]string {
	t.Helper()
	data, err := os.ReadFile(validationDisagreements)
	if errors.Is(err, fs.ErrNotExist) && *update {
		return map[string][]string{}
	}
	require.NoError(t, err, "read %s (run with -update to create it)", validationDisagreements)
	out := map[string][]string{}
	for line := range strings.Lines(string(data)) {
		line = strings.TrimRight(line, "\n")
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Split(line, "\t")
		require.Len(t, fields, 5, "%s: %q is not a document, a position, a rule, a side and a reason",
			validationDisagreements, line)
		key := strings.Join(fields[:4], "\t")
		out[key] = append(out[key], fields[4])
	}
	return out
}

// checkValidationRows requires every row found to be listed, with a reason
// validationReasons names, and every row listed to be found.
func checkValidationRows(t *testing.T, found []validationRow, listed map[string][]string) {
	t.Helper()
	seen := map[string]int{}
	for _, r := range found {
		key := r.key()
		i := seen[key]
		seen[key]++
		if !assert.Greater(t, len(listed[key]), i, "a disagreement %s does not list: %s", validationDisagreements, key) {
			continue
		}
		reason := listed[key][i]
		assert.Contains(t, validationReasons, reason, "%s gives a reason validationReasons does not name: %s", key, reason)
	}
	used := map[string]bool{}
	for key, reasons := range listed {
		assert.LessOrEqual(t, len(reasons), seen[key],
			"%s lists a disagreement the oracle no longer finds: %s", validationDisagreements, key)
		for _, reason := range reasons {
			used[reason] = true
		}
	}
	for reason := range validationReasons {
		assert.True(t, used[reason], "validationReasons gives a reason no listed disagreement has: %s", reason)
	}
}

// validationHeader opens validationDisagreements.
const validationHeader = `# Generated by:
#   go test ./compilers/openapi/internal/canary -run TestValidationOracle_AgreesWithTheLibrary -update
#
# Each finding of the library's validation that the compiler's table does not
# make, or the table makes and the library does not: document, position, rule,
# the side that makes it, and the reason, a key of validationReasons. A rule
# is the library's, or a code of the compiler's from after "openapi/".
# Regenerating keeps each reason a disagreement still has and marks a new one
# unclassified, which fails until a reason is written for it.
`

// writeValidationRows writes validationDisagreements from what was found, each
// keeping the reason listed for it.
func writeValidationRows(t *testing.T, found []validationRow, listed map[string][]string) {
	t.Helper()
	var b strings.Builder
	b.WriteString(validationHeader)
	seen := map[string]int{}
	for _, r := range found {
		key := r.key()
		reason := unclassified
		if i := seen[key]; i < len(listed[key]) {
			reason = listed[key][i]
		}
		seen[key]++
		b.WriteString(key + "\t" + reason + "\n")
	}
	require.NoError(t, os.MkdirAll(filepath.Dir(validationDisagreements), 0o755))
	require.NoError(t, os.WriteFile(validationDisagreements, []byte(b.String()), 0o644))
}
