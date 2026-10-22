package load

import (
	"fmt"
	"strings"
	"testing"

	oas3 "github.com/speakeasy-api/openapi/jsonschema/oas3"
	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	yaml "gopkg.in/yaml.v3"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
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
