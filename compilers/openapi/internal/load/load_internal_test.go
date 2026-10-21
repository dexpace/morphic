package load

import (
	"encoding/json/jsontext"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	oas3 "github.com/speakeasy-api/openapi/jsonschema/oas3"
	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	yaml "gopkg.in/yaml.v3"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/engine/reader"
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

// TestLoad_OperationIDAndParameterUniquenessAreOthersToReport pins the two
// rules the table states and leaves to others: a repeated operationId is judged
// by the service lowering once it has seen every claim (GitHub #502), a repeated
// parameter by the load phase's check of every parameter list, which reports it
// once. The table adds no report of either, for the alias form the library read
// as a repeat or for a true one.
func TestLoad_OperationIDAndParameterUniquenessAreOthersToReport(t *testing.T) {
	t.Parallel()
	const (
		aliasedOperationID = `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /a: &item
    get:
      operationId: dup
      responses: {"200": {description: ok}}
  /b: *item
`
		repeatedOperationID = `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /a: {get: {operationId: dup, responses: {"200": {description: ok}}}}
  /b: {get: {operationId: dup, responses: {"200": {description: ok}}}}
`
		duplicateParameter = `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /a:
    get:
      parameters:
        - {name: q, in: query, schema: {type: string}}
        - {name: q, in: query, schema: {type: integer}}
      responses: {"200": {description: ok}}
`
	)
	for _, spec := range []string{aliasedOperationID, repeatedOperationID, duplicateParameter} {
		_, diags, err := Load(t.Context(), 0, compilers.Source{Path: "spec.yaml", Data: []byte(spec)}, Options{})
		require.NoError(t, err)
		for _, d := range diags {
			assert.False(t, strings.HasPrefix(d.Code, diag.Validation+"/"), "no finding of the table: %+v", d)
		}
	}
	_, diags, err := Load(t.Context(), 0, compilers.Source{Path: "spec.yaml", Data: []byte(duplicateParameter)}, Options{})
	require.NoError(t, err)
	assert.Equal(t, 1, countErrorsAt(diags, diag.DuplicateParameter), "%+v", diags)
}

// TestLoad_AnUnownedRuleIsReported is the control for the test above: the table
// leaves two rules to others, not every one inside an operation, so a path
// parameter not marked required must still surface.
func TestLoad_AnUnownedRuleIsReported(t *testing.T) {
	t.Parallel()
	const optionalPathParameter = `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /a/{id}:
    get:
      parameters:
        - {name: id, in: path, schema: {type: string}}
      responses: {"200": {description: ok}}
`
	src := compilers.Source{Path: "spec.yaml", Data: []byte(optionalPathParameter)}
	_, diags, err := Load(t.Context(), 0, src, Options{})
	require.NoError(t, err)
	assert.Equal(t, 1, countErrorsAt(diags, diag.Validation+"/validation-required-field"),
		"a rule the table does not leave to others must still surface: %+v", diags)
}

// TestLoad_ExternalRefResolutionErrors pins how a finding inside a document an
// external reference names is reported: under its own rule and severity, at the
// $ref that brought the document in, with its position there in the message
// (GitHub #537). Findings come in the table's order: an object's missing
// fields, then the fields it holds.
func TestLoad_ExternalRefResolutionErrors(t *testing.T) {
	t.Parallel()
	path := "../../../../testdata/openapi/resolve_main_external.yaml"
	target := "../../../../testdata/openapi/resolve_target_invalid.yaml" // as the resolver keys it
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	ld, diags, loadErr := Load(t.Context(), 0, compilers.Source{Path: path, Data: data},
		Options{Reader: reader.Disk{}})
	require.NoError(t, loadErr)
	require.NotNil(t, ld)

	want := []struct {
		severity ir.Severity
		code     string
		pointer  jsontext.Pointer
		contains string
	}{
		{ir.SeverityError, "openapi/validation/validation-required-field", "/paths/~1a/get/responses/200",
			"Response Object field \"description\" is REQUIRED in OpenAPI 3.1 (Response Object), at 7:7 of " + target},
		{ir.SeverityError, "openapi/validation/validation-type-mismatch", "/paths/~1a/get/responses/200",
			"Header Object field \"required\" must be a boolean in OpenAPI 3.1 (Header Object), at 10:21 of " + target},
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
func parseSpec(t *testing.T, spec string) *soa.OpenAPI {
	t.Helper()
	root, _, err := decodeStream([]byte(spec))
	require.NoError(t, err)
	doc, err := unmarshal(t.Context(), []byte(spec), root)
	require.NoError(t, err)
	require.NotNil(t, doc)
	return doc
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

	doc, err := unmarshal(t.Context(), []byte(" "), root)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrParse)
	assert.Nil(t, doc)
}

// TestUnmarshal_EmptySourceIsRejected pins the guard soa.Unmarshal made before
// the decode was lifted out of it: zero bytes are not a document, and reporting
// that must not depend on the library still being the one holding the check.
func TestUnmarshal_EmptySourceIsRejected(t *testing.T) {
	t.Parallel()
	root, _, err := decodeStream(nil)
	require.NoError(t, err, "no bytes is well-formed YAML; it is not a document")

	doc, err := unmarshal(t.Context(), nil, root)
	require.Error(t, err)
	assert.Nil(t, doc)
}

// resolverPanicSpec is a document the parser accepts and the resolver faults on:
// `B: {$ref}` is a mapping whose $ref key carries no value, so populating the
// response reference that points at it nil-dereferences inside speakeasy.
// FuzzCycleDetector found it; the same bytes are committed as a corpus entry.
const resolverPanicSpec = "openapi: 3.0\ncomponents:\n responses:\n  000: {$ref: '#/B'}\nB: {$ref}"

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

// errorSites returns where each error of the given code is reported, in order.
func errorSites(diags []ir.Diagnostic, code string) []jsontext.Pointer {
	var sites []jsontext.Pointer
	for _, d := range diags {
		if d.Code == code && d.Severity == ir.SeverityError {
			sites = append(sites, d.Provenance.Pointer)
		}
	}
	return sites
}

// countErrors counts the errors among diags, whatever their code.
func countErrors(diags []ir.Diagnostic) int {
	var n int
	for _, d := range diags {
		if d.Severity == ir.SeverityError {
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

	doc, err := unmarshal(t.Context(), []byte("openapi: 3.1.0\n"), root)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "unmarshal document", "the failing step is named")
	assert.Nil(t, doc)
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
		n := 0
		for item := range soa.Walk(t.Context(), parseSpec(t, aliasBomb(depth))) {
			require.NoError(t, item.Match(soa.Matcher{Schema: func(*oas3.JSONSchema[oas3.Referenceable]) error {
				n++
				return nil
			}}))
		}
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

// TestLoad_AFindingOnASourceNodeCarriesItsPointer pins that a finding on a node
// of the source is placed at the node's JSON pointer as well as its position
// (GitHub #803), the source it came from included. A node written under a
// `<<` merge key keeps its position and has no pointer: the pairs it merges
// are held at other pointers. A mapping that merely holds a merge keeps its own.
func TestLoad_AFindingOnASourceNodeCarriesItsPointer(t *testing.T) {
	t.Parallel()
	const header = "openapi: 3.1.0\ninfo: {title: T, version: \"1\"}\n"
	const (
		required = "openapi/validation/validation-required-field"
		allowed  = "openapi/validation/validation-allowed-values"
		inline   = header + "paths:\n  /x:\n    get:\n      parameters:\n        - %s\n      responses: {\"200\": {description: ok}}\n"
	)
	for _, c := range []struct {
		name, spec, marker, code string
		pointer                  jsontext.Pointer
	}{
		{"a mapping", header + "paths: {}\ncomponents:\n  securitySchemes:\n    s: {type: oauth2}\n",
			"{type: oauth2}", required, "/components/securitySchemes/s"},
		{"a value", fmt.Sprintf(inline, "{name: q, in: sideways, schema: {type: string}}"),
			"sideways", allowed, "/paths/~1x/get/parameters/0/in"},
		{"a value under a merge key", fmt.Sprintf(inline, "<<: {name: q, in: sideways, schema: {type: string}}"),
			"sideways", allowed, ""},
		{"a mapping holding a merge key", header + "paths: {}\ncomponents:\n  securitySchemes:\n    s:\n      <<: {type: oauth2}\n",
			"<<: {type: oauth2}", required, "/components/securitySchemes/s"},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			line, col := markerPosition(t, c.spec, c.marker)

			_, diags, err := Load(t.Context(), 2, openapitest.SourceOf(c.spec), Options{})

			require.NoError(t, err)
			require.Len(t, diags, 1, "%+v", diags)
			assert.Equal(t, c.code, diags[0].Code)
			assert.Empty(t, cmp.Diff(ir.Provenance{Source: 2, Pointer: c.pointer, Position: ir.Position{Line: line, Column: col}},
				diags[0].Provenance))
		})
	}
}
