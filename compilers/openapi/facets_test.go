package openapi

import (
	"testing"

	oas3 "github.com/speakeasy-api/openapi/jsonschema/oas3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	yaml "gopkg.in/yaml.v3"

	"github.com/dexpace/morphic/ir"
)

// str and flag build the pointer fields oas3.Schema uses for optional scalars.
func str(v string) *string { return &v }
func flag(v bool) *bool    { return &v }

// TestAnnotations_SiteOverridesReferent pins the §14 precedence rule at the one
// place it is now decided. An annotation written beside a $ref describes the
// position; the target's is the fallback, not the winner.
func TestAnnotations_SiteOverridesReferent(t *testing.T) {
	t.Parallel()
	ref := &oas3.Schema{Description: str("SiteDesc")}
	tgt := &oas3.Schema{Description: str("TargetDesc"), Deprecated: flag(true)}

	got, diags := annotations(site{Kind: siteReference, Node: ref, Referent: tgt}, "/p", 0)

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
	node := &oas3.Schema{Description: str("OwnDesc")}
	stray := &oas3.Schema{Title: str("StraySummary"), Deprecated: flag(true)}

	got, _ := annotations(site{Kind: siteDeclaration, Node: node, Referent: stray}, "/p", 0)

	assert.Equal(t, "OwnDesc", got.Docs.Description)
	assert.Empty(t, got.Docs.Summary, "a declaration inherits nothing, whatever it is handed")
	assert.False(t, got.Deprecated)
}

func TestAnnotations_ReadsEverySiteLocalAspect(t *testing.T) {
	t.Parallel()
	node := &oas3.Schema{
		Description: str("D"),
		XML:         &oas3.XML{Name: str("Q")},
		Example:     yamlNode(t, "hello"),
	}
	got, diags := annotations(site{Kind: siteDeclaration, Node: node}, "/components/schemas/S", 0)

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
	assert.Equal(t, codeDegradedConstruct, diags[0].Code)
	assert.Equal(t, 3, diags[0].Provenance.Source, "stamped with the source it was handed")
	assert.Equal(t, "/p/example", diags[0].Provenance.Pointer)
}

func TestPreserveKeywordInto_EmptyPayloadRecordsAndAnnouncesNothing(t *testing.T) {
	t.Parallel()
	for name, raw := range map[string]ir.RawValue{"nil": nil, "empty non-nil": {}} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			var p ir.Preserved
			diags := preserveKeywordInto(&p, "openapi:not", raw, "/p", "/p/not", "not", 0)
			assert.Nil(t, p)
			assert.Empty(t, diags, "nothing was kept, so nothing is announced")
		})
	}
}
