package annotation

import (
	"strings"
	"testing"

	oas3 "github.com/speakeasy-api/openapi/jsonschema/oas3"
	"github.com/speakeasy-api/openapi/marshaller"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	yaml "gopkg.in/yaml.v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/ir"
)

// TestAnnotations_SiteOverridesReferent pins the §14 precedence rule at the one
// place it is now decided. An annotation written beside a $ref describes the
// position; the target's is the fallback, not the winner.
func TestAnnotations_SiteOverridesReferent(t *testing.T) {
	t.Parallel()
	ref := &oas3.Schema{Description: new("SiteDesc")}
	tgt := &oas3.Schema{Description: new("TargetDesc"), Deprecated: new(true)}

	got, diags := Read(Site{Kind: Reference, Node: ref, Referent: tgt}, "/p", 0)

	assert.Empty(t, diags)
	assert.Equal(t, "SiteDesc", got.Docs.Description, "the site's own description wins")
	assert.True(t, got.Deprecated, "and an annotation only the target declares still inherits")
}

// TestAnnotations_DeclarationIgnoresAnyReferent is the other half of the fork: a
// declaration has no target, so a Referent handed to it is not consulted.
// Without this the kind field could be dropped and the site test above would
// still pass.
func TestAnnotations_DeclarationIgnoresAnyReferent(t *testing.T) {
	t.Parallel()
	node := &oas3.Schema{Description: new("OwnDesc")}
	stray := &oas3.Schema{Title: new("StraySummary"), Deprecated: new(true)}

	got, _ := Read(Site{Kind: Declaration, Node: node, Referent: stray}, "/p", 0)

	assert.Equal(t, "OwnDesc", got.Docs.Description)
	assert.Empty(t, got.Docs.Summary, "a declaration inherits nothing, whatever it is handed")
	assert.False(t, got.Deprecated)
}

func TestAnnotations_ReadsEverySiteLocalAspect(t *testing.T) {
	t.Parallel()
	node := &oas3.Schema{
		Description: new("D"),
		XML:         &oas3.XML{Name: new("Q")},
		Example:     openapitest.YAMLNode(t, "hello"),
	}
	got, diags := Read(Site{Kind: Declaration, Node: node}, "/components/schemas/S", 0)

	assert.Equal(t, "D", got.Docs.Description)
	require.NotNil(t, got.XML)
	assert.Equal(t, "Q", got.XML.Name)
	require.Len(t, got.Examples, 1)
	assert.Empty(t, diags)
}

// TestValidationOnlyAt_NeedsTheRawNode records a real limit of reading these
// purely: the §4.7 keywords are preserved byte-faithfully from the source YAML,
// so the facet reads the schema's raw root node rather than its parsed fields. A
// schema built in Go carries no root node, so there is nothing to preserve —
// which is the correct answer, not a gap. The parsed-source path is covered by
// the retention suite.
func TestValidationOnlyAt_NeedsTheRawNode(t *testing.T) {
	t.Parallel()
	built := &oas3.Schema{Not: oas3.NewJSONSchemaFromSchema[oas3.Referenceable](&oas3.Schema{})}

	got, diags := validationOnlyAt(built, "/p", 0)

	assert.Nil(t, got, "no raw bytes to keep, so nothing is kept")
	assert.Empty(t, diags, "and nothing is announced for a keyword that was not kept")
}

// TestSchemaExamplesAt_UnconvertibleValueIsReportedNotDropped pins that a facet
// returns its findings rather than swallowing them — the property that lets a
// pure reader replace one that reached into a lowerer to report.
func TestSchemaExamplesAt_UnconvertibleValueIsReportedNotDropped(t *testing.T) {
	t.Parallel()
	node := &oas3.Schema{Example: &yaml.Node{Kind: yaml.Kind(99)}}

	got, diags := schemaExamplesAt(node, "/p", 3)

	assert.Empty(t, got, "an unconvertible example yields no value")
	require.Len(t, diags, 1, "and is reported rather than dropped")
	assert.Equal(t, diag.DegradedConstruct, diags[0].Code)
	assert.Equal(t, 3, diags[0].Provenance.Source, "stamped with the source it was handed")
	assert.Equal(t, "/p/example", diags[0].Provenance.Pointer)
}

func TestPreserveKeywordInto_EmptyPayloadRecordsAndAnnouncesNothing(t *testing.T) {
	t.Parallel()
	for name, raw := range map[string]ir.RawValue{"nil": nil, "empty non-nil": {}} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			var p ir.Unmodeled
			diags := PreserveKeywordInto(&p, "openapi:not", raw, "/p", "/p/not", "not", 0)
			assert.Nil(t, p)
			assert.Empty(t, diags, "nothing was kept, so nothing is announced")
		})
	}
}

// TestValidationOnlyAt_DependentRequiredJoinsItsSiblings pins the keyword the
// §4.7 family was missing. dependentRequired is pure validation logic exactly as
// dependentSchemas is, and oas3.Schema has no field for it, which is the whole
// reason it was dropped where its sibling was kept.
func TestValidationOnlyAt_DependentRequiredJoinsItsSiblings(t *testing.T) {
	t.Parallel()
	s := schemaFromYAML(t, "type: object\ndependentRequired:\n  a: [b]\n")

	got, diags := validationOnlyAt(s, "/components/schemas/S", 7)

	entry, ok := got["openapi:dependentRequired"]
	require.True(t, ok, "dependentRequired must be kept verbatim")
	assert.JSONEq(t, `{"a":["b"]}`, string(entry.Value))
	assert.Equal(t, ir.ReasonValidationOnly, entry.Reason)
	assert.Equal(t, "/components/schemas/S/dependentRequired", entry.Provenance.Pointer)

	require.Len(t, diags, 1, "and is announced exactly once")
	assert.Equal(t, diag.ValidationOnlyKeyword, diags[0].Code)
	assert.Equal(t, 7, diags[0].Provenance.Source)
}

// TestDialectAt_KeepsEachKeywordOutOfScope pins that the resource and dialect
// keywords carry the reason meaning "no IR node is coming", not the one meaning
// "the field is missing for now".
func TestDialectAt_KeepsEachKeywordOutOfScope(t *testing.T) {
	t.Parallel()
	s := schemaFromYAML(t, "$id: 'urn:example:a'\n$schema: 'https://example.com/dialect'\ntype: string\n")

	got, diags := dialectAt(s, "/components/schemas/S", 0)

	require.Len(t, got, 2, "one entry per keyword written")
	for _, keyword := range []string{"$id", "$schema"} {
		entry, ok := got["openapi:"+keyword]
		require.True(t, ok, "%s must be kept", keyword)
		assert.Equal(t, ir.ReasonOutOfScope, entry.Reason)
	}
	assert.Len(t, diags, 2, "each exclusion is announced where it was written")
	assert.True(t, DeclaresAny(s, DialectKeywords),
		"and the hoist gate agrees a node is needed to hold them")
}

// TestNoIRHomeAt_ContentSchemaIsKeptNotExcluded pins the reason split the other
// way: contentSchema is real data shape with no field yet, a gap expected to
// close, so it must not be filed as a deliberate exclusion.
func TestNoIRHomeAt_ContentSchemaIsKeptNotExcluded(t *testing.T) {
	t.Parallel()
	s := schemaFromYAML(t, "type: string\ncontentSchema: {type: object}\n")

	got, diags := noIRHomeAt(s, "/components/schemas/S", 0)

	entry, ok := got["openapi:contentSchema"]
	require.True(t, ok)
	assert.JSONEq(t, `{"type":"object"}`, string(entry.Value))
	assert.Equal(t, ir.ReasonNoIRHome, entry.Reason)
	require.Len(t, diags, 1)
	assert.Equal(t, "/components/schemas/S/contentSchema", diags[0].Provenance.Pointer)
}

// schemaFromYAML unmarshals body as a bare schema through the same marshaller
// the compiler's loader parses documents with, so the raw nodes the verbatim
// readers read off are present. A schema built in Go carries none, which
// TestValidationOnlyAt_NeedsTheRawNode covers separately.
func schemaFromYAML(t *testing.T, body string) *oas3.Schema {
	t.Helper()
	var js oas3.JSONSchema[oas3.Referenceable]
	valErrs, err := marshaller.Unmarshal(t.Context(), strings.NewReader(body), &js)
	require.NoError(t, err)
	require.Empty(t, valErrs, "the fixture parses cleanly")
	s := js.GetSchema()
	require.NotNil(t, s, "the fixture is a schema, not a bare boolean")
	return s
}

// TestNoIRHomeAt_ModelSetWithoutRawSourceRecordsNothing pins the guard between
// the model and the raw tree. contentSchema is kept verbatim, so it is read off
// the source node rather than the parsed model — and a schema built in memory,
// or one whose value cannot be converted to JSON, has a model field set with no
// bytes behind it. Recording an entry there would announce a preservation with
// nothing preserved, so the collector reports nothing instead.
func TestNoIRHomeAt_ModelSetWithoutRawSourceRecordsNothing(t *testing.T) {
	t.Parallel()
	inner := oas3.NewJSONSchemaFromSchema[oas3.Referenceable](
		&oas3.Schema{Type: oas3.NewTypeFromString(oas3.SchemaTypeObject)})
	s := &oas3.Schema{ContentSchema: inner}
	require.NotNil(t, s.GetContentSchema(), "the model reports the keyword as set")
	require.Nil(t, RawPropertyNode(s, "contentSchema"), "and no raw node backs it")

	got, diags := noIRHomeAt(s, "/components/schemas/A", 0)

	assert.Nil(t, got, "no entry is recorded when there are no bytes to record")
	assert.Empty(t, diags, "and nothing is announced, so the two channels agree")
}

// TestKind_String covers both named values and the default case, so an
// assertion failure or test diff over a Kind prints a name instead of a bare
// int.
func TestKind_String(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "Declaration", Declaration.String())
	assert.Equal(t, "Reference", Reference.String())
	assert.Equal(t, "Kind(99)", Kind(99).String())
}
