package load

import (
	"encoding/json/jsontext"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"

	oas3 "github.com/speakeasy-api/openapi/jsonschema/oas3"
	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/speakeasy-api/openapi/validation"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	yaml "gopkg.in/yaml.v3"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/compilers/openapi/internal/scan"
	"github.com/dexpace/morphic/ir"
)

const minimal31 = `openapi: 3.1.0
info: {title: T, version: "1"}
paths: {}
`

func TestLoad_Minimal31(t *testing.T) {
	t.Parallel()
	got, diags, err := Load(t.Context(), 0, compilers.Source{Path: "spec.yaml", Data: []byte(minimal31)}, Options{})
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "openapi@3.1", got.Source.Format, "3.1.0 normalizes to 3.1")
	assert.Equal(t, "spec.yaml", got.Source.Path)
	assert.Len(t, got.Source.Hash, 64)
	for _, d := range diags {
		assert.NotEqual(t, ir.SeverityError, d.Severity, "unexpected error diagnostic: %+v", d)
	}
}

func TestLoad_UnsupportedVersion(t *testing.T) {
	t.Parallel()
	src := compilers.Source{Path: "old.yaml", Data: []byte("swagger: \"2.0\"\ninfo: {title: T, version: \"1\"}\npaths: {}\n")}
	got, diags, err := Load(t.Context(), 0, src, Options{})
	require.NoError(t, err) // spec problems are diagnostics, not Go errors
	assert.Nil(t, got)
	require.NotEmpty(t, diags)
	assert.Equal(t, diag.UnsupportedVersion, diags[0].Code)
	assert.Equal(t, ir.SeverityError, diags[0].Severity)
}

func TestLoad_ValidationErrorsBecomeDiagnostics(t *testing.T) {
	t.Parallel()
	// paths entry with a bogus structure triggers library validation errors.
	src := compilers.Source{Path: "bad.yaml", Data: []byte("openapi: 3.1.0\ninfo: {title: T, version: \"1\"}\npaths: {/x: {get: {responses: \"nope\"}}}\n")}
	_, diags, err := Load(t.Context(), 0, src, Options{})
	require.NoError(t, err)
	require.NotEmpty(t, diags)
	found := false
	for _, d := range diags {
		if d.Provenance.Position != (ir.Position{}) {
			found = true
		}
	}
	assert.True(t, found, "diagnostics should carry position provenance")
}

// TestLoad_OperationIDUniquenessIsTheCompilers pins compilerOwned's one member:
// the library's own operationId-uniqueness finding never reaches a diagnostic,
// at any severity, because the service lowering judges every claim itself once
// it has seen them all (GitHub #502).
//
// The fixture is the alias form, which the library reads as a repeat, and the
// test first confirms the library does raise its finding for it. Without that,
// a library that stopped raising it would leave this passing with nothing
// dropped. TestLoad_DuplicateParameterStillReported is the control: without it,
// this would pass just as well if findings dropped every finding.
func TestLoad_OperationIDUniquenessIsTheCompilers(t *testing.T) {
	t.Parallel()
	const aliasedOperationID = `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /a: &item
    get:
      operationId: dup
      responses: {"200": {description: ok}}
  /b: *item
`
	// Load's own steps up to the finding, anchors released as Load releases them.
	root, _, err := decodeStream([]byte(aliasedOperationID))
	require.NoError(t, err)
	releaseAnchors(root)
	_, valErrs, err := unmarshal(t.Context(), []byte(aliasedOperationID), root)
	require.NoError(t, err)
	require.True(t, slices.ContainsFunc(valErrs, func(e error) bool {
		verr, ok := asValidationError(e)
		return ok && verr.Rule == validation.RuleValidationOperationIdUnique
	}), "the library raises its own finding for the fixture: %v", valErrs)

	src := compilers.Source{Path: "spec.yaml", Data: []byte(aliasedOperationID)}
	_, diags, err := Load(t.Context(), 0, src, Options{})
	require.NoError(t, err)
	code := diag.Validation + "/" + validation.RuleValidationOperationIdUnique
	assert.False(t, slices.ContainsFunc(diags, func(d ir.Diagnostic) bool { return d.Code == code }),
		"the library's finding never reaches a diagnostic: %+v", diags)
}

// TestLoad_DuplicateParameterStillReported is the control for
// TestLoad_OperationIDUniquenessIsTheCompilers: compilerOwned names one rule,
// not every validation error the library raises inside an operation, so a
// duplicated parameter, a defect the compiler has no rule of its own for, must
// still surface.
func TestLoad_DuplicateParameterStillReported(t *testing.T) {
	t.Parallel()
	const duplicateParameter = `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /a:
    get:
      parameters:
        - {name: q, in: query, schema: {type: string}}
        - {name: q, in: query, schema: {type: integer}}
      responses: {"200": {description: ok}}
`
	src := compilers.Source{Path: "spec.yaml", Data: []byte(duplicateParameter)}
	_, diags, err := Load(t.Context(), 0, src, Options{})
	require.NoError(t, err)
	assert.Equal(t, 1, countErrorsAt(diags, diag.Validation+"/"+validation.RuleValidationOperationParameters),
		"a rule the compiler does not own must still surface: %+v", diags)
}

// TestLoad_ExternalRefResolutionErrors pins how a finding inside a document an
// external reference names is reported: under its own rule and severity, at the
// $ref that brought the document in, with its position there in the message. It
// used to arrive as openapi/unresolved-ref at error severity, with the external
// document's line and column against the source's index (GitHub #537).
//
// Severity, code and provenance are Morphic's own and are pinned exactly. The
// message is matched with Contains, since the text before the position is the
// library's rendering of the finding, which a later version could reword.
func TestLoad_ExternalRefResolutionErrors(t *testing.T) {
	t.Parallel()
	path := "../../../../testdata/openapi/resolve_main_external.yaml"
	target := "../../../../testdata/openapi/resolve_target_invalid.yaml" // as the resolver keys it
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	ld, diags, loadErr := Load(t.Context(), 0, compilers.Source{Path: path, Data: data},
		Options{AllowExternalRefs: true})
	require.NoError(t, loadErr)
	require.NotNil(t, ld)

	want := []struct {
		severity ir.Severity
		code     string
		pointer  jsontext.Pointer
		contains string
	}{
		{ir.SeverityError, "openapi/validation/validation-type-mismatch", "/paths/~1a/get/responses/200",
			"cannot unmarshal !!str `notabool` into bool, at 10:21 of " + target},
		{ir.SeverityError, "openapi/validation/validation-required-field", "/paths/~1a/get/responses/200",
			"`response.description` is required, at 7:7 of " + target},
	}
	require.Len(t, diags, len(want), "%+v", diags)
	for i, w := range want {
		assert.Equal(t, w.severity, diags[i].Severity, "entry %d", i)
		assert.Equal(t, w.code, diags[i].Code, "entry %d", i)
		assert.Equal(t, ir.Provenance{Source: 0, Pointer: w.pointer}, diags[i].Provenance, "entry %d", i)
		assert.Contains(t, diags[i].Message, w.contains, "entry %d", i)
	}
}

// parseSpec runs the two steps Load runs back to back when no overlay comes
// between them, for tests that want the parsed document and not the split.
func parseSpec(t *testing.T, spec string) (*soa.OpenAPI, []error) {
	t.Helper()
	root, _, err := decodeStream([]byte(spec))
	require.NoError(t, err)
	doc, valErrs, err := unmarshal(t.Context(), []byte(spec), root)
	require.NoError(t, err)
	require.NotNil(t, doc)
	return doc, valErrs
}

// TestUnmarshal_RecoversParserPanic pins the no-panics-escape invariant: the
// third-party parser faults on a whitespace-only document, and unmarshal must
// convert that panic into an ErrParse error instead of letting it escape.
//
// The decode ahead of it succeeds — whitespace is well-formed YAML — so this
// still lands in unmarshal rather than being caught a step earlier.
func TestUnmarshal_RecoversParserPanic(t *testing.T) {
	t.Parallel()
	root, _, err := decodeStream([]byte(" "))
	require.NoError(t, err, "whitespace decodes; it is the model build that faults")

	doc, valErrs, err := unmarshal(t.Context(), []byte(" "), root)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrParse)
	assert.Nil(t, doc)
	assert.Nil(t, valErrs)
}

// TestUnmarshal_EmptySourceIsRejected pins the guard soa.Unmarshal made before
// the decode was lifted out of it: zero bytes are not a document, and reporting
// that must not depend on the library still being the one holding the check.
func TestUnmarshal_EmptySourceIsRejected(t *testing.T) {
	t.Parallel()
	root, _, err := decodeStream(nil)
	require.NoError(t, err, "no bytes is well-formed YAML; it is not a document")

	doc, valErrs, err := unmarshal(t.Context(), nil, root)
	require.Error(t, err)
	assert.Nil(t, doc)
	assert.Nil(t, valErrs)
}

// resolverPanicSpec is a document the parser accepts and the resolver faults on:
// `B: {$ref}` is a mapping whose $ref key carries no value, so populating the
// response reference that points at it nil-dereferences inside speakeasy.
// FuzzCycleDetector found it; the same bytes are committed as a corpus entry.
const resolverPanicSpec = "openapi: 3.0\ncomponents:\n responses:\n  000: {$ref: '#/B'}\nB: {$ref}"

func TestMapSeverity(t *testing.T) {
	t.Parallel()
	assert.Equal(t, ir.SeverityWarning, mapSeverity(validation.Severity("warning")))
	assert.Equal(t, ir.SeverityInfo, mapSeverity(validation.Severity("hint")))
	assert.Equal(t, ir.SeverityError, mapSeverity(validation.Severity("error")))
	assert.Equal(t, ir.SeverityError, mapSeverity(validation.Severity("")))
}

func TestAsValidationError(t *testing.T) {
	t.Parallel()
	v := validation.Error{Rule: "r"}
	got, ok := asValidationError(v)
	assert.True(t, ok, "by value")
	assert.Equal(t, "r", got.Rule)

	pv := &validation.Error{Rule: "p"}
	got, ok = asValidationError(pv)
	assert.True(t, ok, "by pointer")
	assert.Equal(t, "p", got.Rule)

	_, ok = asValidationError(errors.New("plain"))
	assert.False(t, ok, "plain error is not a validation error")
}

func TestValidationDiag(t *testing.T) {
	t.Parallel()
	at := &yaml.Node{Kind: yaml.ScalarNode, Line: 4, Column: 9}
	structured := validationDiag(scan.InSource(0),
		validation.Error{Severity: "warning", Rule: "dup-tag", UnderlyingError: errors.New("x"), Node: at})
	assert.Equal(t, ir.SeverityWarning, structured.Severity)
	assert.Equal(t, diag.Validation+"/dup-tag", structured.Code)
	assert.Equal(t, ir.Provenance{Source: 0, Position: ir.Position{Line: 4, Column: 9}}, structured.Provenance,
		"anchored where the locator puts the finding's node")
	assert.Equal(t, "x", structured.Message,
		"the finding alone: the severity, rule and position the library prefixes are the diagnostic's own fields")

	elsewhere := validationDiag(scan.InSource(0),
		validation.Error{Severity: "error", Rule: "r", UnderlyingError: errors.New("x"), DocumentLocation: "other.yaml"})
	assert.Equal(t, "x (document: other.yaml)", elsewhere.Message,
		"a finding about another document keeps saying so; no field holds that")

	hollow := validationDiag(scan.InSource(0), validation.Error{Severity: "error", Rule: "r"})
	assert.Equal(t, "r", hollow.Message,
		"a finding with nothing underneath — which the library never produces — names its rule rather than faulting")

	unanchored := validationDiag(scan.InSource(0),
		validation.Error{Severity: "warning", Rule: "dup-tag", UnderlyingError: errors.New("x")})
	assert.Equal(t, ir.Provenance{Source: 0}, unanchored.Provenance,
		"a finding with no node names the source and no position — not a position it does not have")

	bare := validationDiag(scan.InSource(3), errors.New("plain problem"))
	assert.Equal(t, ir.SeverityError, bare.Severity)
	assert.Equal(t, diag.Validation, bare.Code)
	assert.Equal(t, ir.Provenance{Source: 3}, bare.Provenance)
}

// TestIsNumericBoundKeyword_UnderlyingNotTypeMismatch drives the errors.As guard:
// a type-mismatch-ruled finding whose underlying error is not a *TypeMismatchError
// names no bound keyword, so the classifier declines to suppress it.
func TestIsNumericBoundKeyword_UnderlyingNotTypeMismatch(t *testing.T) {
	t.Parallel()
	verr := validation.Error{
		Rule:            validation.RuleValidationTypeMismatch,
		UnderlyingError: errors.New("not a type mismatch"),
	}
	assert.False(t, isNumericBoundKeyword(verr))
}

// TestIsNumericBoundKeyword_NonBoundKeyword covers the not-in-map arm: a genuine
// type-mismatch on a keyword Morphic does not own (here `type`) is never
// suppressed, so the library's finding is kept.
func TestIsNumericBoundKeyword_NonBoundKeyword(t *testing.T) {
	t.Parallel()
	verr := validation.Error{
		Rule:            validation.RuleValidationTypeMismatch,
		UnderlyingError: &validation.TypeMismatchError{ParentName: "schema.type"},
	}
	assert.False(t, isNumericBoundKeyword(verr))
}

// TestIsNumericBoundKeyword_BoundKeyword covers the in-map arm: a type-mismatch on
// a numeric-bound keyword is recognized (whatever the parent path's prefix) so
// load suppresses the library's redundant float64 finding on it.
func TestIsNumericBoundKeyword_BoundKeyword(t *testing.T) {
	t.Parallel()
	verr := validation.Error{
		Rule:            validation.RuleValidationTypeMismatch,
		UnderlyingError: &validation.TypeMismatchError{ParentName: "schema.properties.n.minimum"},
	}
	assert.True(t, isNumericBoundKeyword(verr))
}

// TestIsNumericBoundKeyword_BareKeyword covers the no-separator arm: a parent path
// of one segment is its own trailing segment, so a bare keyword is recognized too.
func TestIsNumericBoundKeyword_BareKeyword(t *testing.T) {
	t.Parallel()
	verr := validation.Error{
		Rule:            validation.RuleValidationTypeMismatch,
		UnderlyingError: &validation.TypeMismatchError{ParentName: "minimum"},
	}
	assert.True(t, isNumericBoundKeyword(verr))
}

// TestInvalidSyntaxOnValidNumbers_NilNode covers the nil guard.
func TestInvalidSyntaxOnValidNumbers_NilNode(t *testing.T) {
	t.Parallel()
	assert.False(t, invalidSyntaxOnValidNumbers(nil))
}

// TestWalkNumericScalars_NilAndDepthGuards covers the recursion guards: neither a
// nil node nor a node past the scan-depth cap visits any scalar.
func TestWalkNumericScalars_NilAndDepthGuards(t *testing.T) {
	t.Parallel()
	var visited int
	visit := func(*yaml.Node) { visited++ }
	walkNumericScalars(nil, 0, visit)
	walkNumericScalars(&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!int", Value: "5"}, maxSchemaScanDepth+1, visit)
	assert.Zero(t, visited)
}

// TestInvalidSyntaxOnValidNumbers_Candidacy pins which literals can excuse a
// JSON-syntax finding. Only a spelling JSON rejects is a candidate cause, and
// every candidate must be one Morphic recovers.
func TestInvalidSyntaxOnValidNumbers_Candidacy(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		scalars []*yaml.Node
		want    bool
	}{
		{"leading dot", []*yaml.Node{openapitest.ScalarNode("!!float", ".5")}, true},
		{"octal", []*yaml.Node{openapitest.ScalarNode("!!int", "0644")}, true},
		{"separators", []*yaml.Node{openapitest.ScalarNode("!!int", "1_000")}, true},
		{"recoverable beside a json-valid literal",
			[]*yaml.Node{openapitest.ScalarNode("!!float", ".5"), openapitest.ScalarNode("!!int", "42")}, true},

		{"nothing to recover", []*yaml.Node{openapitest.ScalarNode("!!int", "42")}, false},
		{"infinity", []*yaml.Node{openapitest.ScalarNode("!!float", ".inf")}, false},
		{"recoverable beside an unrecoverable literal",
			[]*yaml.Node{openapitest.ScalarNode("!!float", ".5"), openapitest.ScalarNode("!!float", ".inf")}, false},
		// JSON accepts "-0", so normalizing it to "0" is not evidence that it
		// provoked anything.
		{"negative zero", []*yaml.Node{openapitest.ScalarNode("!!int", "-0")}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			node := &yaml.Node{Kind: yaml.SequenceNode, Content: tc.scalars}
			assert.Equal(t, tc.want, invalidSyntaxOnValidNumbers(node))
		})
	}
}

// TestMatchSchemas_StopsOnMatchError drives the branch no document can reach:
// WalkItem.Match returns exactly what the matcher handed it, and the collector
// schemaFindings passes returns nil unconditionally. A hand-built item is the
// only way to observe the stop, and observing it is what says the walk ends
// there rather than skipping the item and carrying on — the difference between
// a smaller reconciliation and a wrong one.
func TestMatchSchemas_StopsOnMatchError(t *testing.T) {
	t.Parallel()
	visited := 0
	failing := soa.WalkItem{Match: func(soa.Matcher) error { return errors.New("walk stopped") }}
	after := soa.WalkItem{Match: func(m soa.Matcher) error {
		visited++
		return nil
	}}
	matchSchemas(func(yield func(soa.WalkItem) bool) {
		if !yield(failing) {
			return
		}
		yield(after)
	}, func(*oas3.JSONSchema[oas3.Referenceable]) error { return nil })
	assert.Zero(t, visited, "a failed match ends the walk rather than skipping one item")
}

// TestMetaSchemaVersionArtifacts_OtherMinorsUntouched pins the gate: only 3.2
// findings are reconciled, because the library defaults to the 3.1 meta-schema
// and reconciling 3.0 changes what a 3.0 document reports rather than removing a
// false positive.
func TestMetaSchemaVersionArtifacts_OtherMinorsUntouched(t *testing.T) {
	t.Parallel()
	doc, _ := parseSpec(t, minimal31)
	assert.Nil(t, metaSchemaVersionArtifacts(t.Context(), doc, "3.1"))
	assert.Nil(t, metaSchemaVersionArtifacts(t.Context(), doc, "3.0"))
}

// countErrorsAt counts the error-severity diagnostics carrying code. Severity is
// fixed rather than a parameter because every refusal this package reports is an
// error, and a lower severity here would be a different assertion than any test
// makes.
func countErrorsAt(diags []ir.Diagnostic, code string) int {
	var n int
	for _, d := range diags {
		if d.Code == code && d.Severity == ir.SeverityError {
			n++
		}
	}
	return n
}

// TestUnmarshal_RejectsADocumentNodeHoldingMoreThanOneRoot pins the model
// build's other failure exit: the library refuses a document node that does not
// wrap exactly one root, and that refusal is a Go error rather than a
// validation finding about the spec.
//
// The node is built rather than decoded because yaml.v3 always wraps exactly
// one root. An overlay mutates the tree between the decode and the build and
// could hand over another shape, so the branch is the compiler's to handle, and
// only a built node reaches it.
func TestUnmarshal_RejectsADocumentNodeHoldingMoreThanOneRoot(t *testing.T) {
	t.Parallel()
	root := &yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{
		{Kind: yaml.MappingNode}, {Kind: yaml.MappingNode},
	}}

	doc, valErrs, err := unmarshal(t.Context(), []byte("openapi: 3.1.0\n"), root)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "unmarshal document", "the failing step is named")
	assert.Nil(t, doc)
	assert.Nil(t, valErrs)
}

// aliasBomb writes a document whose schema L<depth> holds four properties that
// all alias L<depth-1>, down to a scalar leaf: one declared line per level, and
// an expansion of 4^depth beneath the top.
func aliasBomb(depth int) string {
	var b strings.Builder
	b.WriteString("openapi: 3.1.0\ninfo: {title: T, version: \"1\"}\npaths: {}\ncomponents:\n  schemas:\n")
	b.WriteString("    L0: &l0 {type: string}\n")
	for k := 1; k <= depth; k++ {
		fmt.Fprintf(&b, "    L%d: &l%d {type: object, properties: {a: *l%d, b: *l%d, c: *l%d, d: *l%d}}\n",
			k, k, k-1, k-1, k-1, k-1)
	}
	return b.String()
}

// TestUnmarshal_ExpandsAliasesIntoTheModel pins why scan's alias weigher is the
// defence against a billion-laughs document and not a backstop to one. The
// decode expands nothing — a node tree holds an alias as a pointer — but the
// model build follows every alias as though its target were written in place,
// so a document one line longer per level yields a model four times larger per
// level. Nothing between the decode and this build bounds that; the weigher
// runs ahead of it because of exactly this (GitHub #27, #479).
func TestUnmarshal_ExpandsAliasesIntoTheModel(t *testing.T) {
	t.Parallel()
	walked := func(depth int) int {
		doc, _ := parseSpec(t, aliasBomb(depth))
		n := 0
		matchSchemas(soa.Walk(t.Context(), doc), func(*oas3.JSONSchema[oas3.Referenceable]) error {
			n++
			return nil
		})
		return n
	}

	prev := walked(1)
	for depth := 2; depth <= 4; depth++ {
		next := walked(depth)
		assert.GreaterOrEqual(t, next, 4*prev,
			"one more declared line at depth %d, at least four times the model", depth)
		prev = next
	}
}
