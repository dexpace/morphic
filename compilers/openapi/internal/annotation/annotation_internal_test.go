// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package annotation

import (
	"encoding/json/jsontext"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/speakeasy-api/openapi/jsonschema/oas3"
	"github.com/speakeasy-api/openapi/marshaller"
	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/nodeview"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/ir"
)

// TestAnnotations_SiteOverridesReferent pins the precedence rule of
// docs/compilers/openapi.md#ref-use-site at the one place it is now decided. An
// annotation written beside a $ref describes the position; the target's is the
// fallback, not the winner.
func TestAnnotations_SiteOverridesReferent(t *testing.T) {
	t.Parallel()
	ref := &oas3.Schema{Description: new("SiteDesc")}
	tgt := &oas3.Schema{Description: new("TargetDesc"), Deprecated: new(true)}

	got, diags := Read(Site{Kind: Reference, Node: ref, Referent: tgt}, "/p", sourced(0))

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

	got, _ := Read(Site{Kind: Declaration, Node: node, Referent: stray}, "/p", sourced(0))

	assert.Equal(t, "OwnDesc", got.Docs.Description)
	assert.Empty(t, got.Docs.Summary, "a declaration inherits nothing, whatever it is handed")
	assert.False(t, got.Deprecated)
}

// TestAnnotations_RedactionFromTheSiteOrItsReferent pins how Read decides
// Sensitive. A reference site inherits its referent's redaction, as it inherits
// deprecation, and a format of its own cannot lift it: the two conjoin, and
// only a redaction is ever stated. A declaration consults no referent at all.
func TestAnnotations_RedactionFromTheSiteOrItsReferent(t *testing.T) {
	t.Parallel()
	password, byteFormat := &oas3.Schema{Format: new("password")}, &oas3.Schema{Format: new("byte")}
	for _, tc := range []struct {
		name string
		site Site
		want bool
	}{
		{"the site writes it", Site{Kind: Reference, Node: password}, true},
		{"the referent writes it", Site{Kind: Reference, Node: &oas3.Schema{}, Referent: password}, true},
		{"another format at the site", Site{Kind: Reference, Node: byteFormat, Referent: password}, true},
		{"a declaration's stray referent", Site{Kind: Declaration, Node: &oas3.Schema{}, Referent: password}, false},
		{"another format alone", Site{Kind: Reference, Node: byteFormat}, false},
		{"neither", Site{Kind: Reference, Node: &oas3.Schema{}, Referent: &oas3.Schema{}}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, diags := Read(tc.site, "/p", sourced(0))
			assert.Empty(t, diags)
			assert.Equal(t, tc.want, got.Sensitive)
		})
	}
	assert.False(t, DeclaresRedaction(nil), "a missing schema asks for nothing")
}

func TestAnnotations_ReadsEverySiteLocalAspect(t *testing.T) {
	t.Parallel()
	node := &oas3.Schema{
		Description: new("D"),
		XML:         &oas3.XML{Name: new("Q")},
		Example:     openapitest.YAMLNode(t, "hello"),
	}
	got, diags := Read(Site{Kind: Declaration, Node: node}, "/components/schemas/S", sourced(0))

	assert.Equal(t, "D", got.Docs.Description)
	require.NotNil(t, got.XML)
	assert.Equal(t, "Q", got.XML.Name)
	require.Len(t, got.Examples, 1)
	assert.Empty(t, diags)
}

// TestSchemaExamplesAt_UnconvertibleValueIsReportedNotDropped pins that a facet
// returns its findings rather than swallowing them — the property that lets a
// pure reader replace one that reached into a lowerer to report.
func TestSchemaExamplesAt_UnconvertibleValueIsReportedNotDropped(t *testing.T) {
	t.Parallel()
	node := &oas3.Schema{Example: &yaml.Node{Kind: yaml.Kind(99)}}

	got, diags := schemaExamplesAt(node, "/p", sourced(3))

	assert.Empty(t, got, "an unconvertible example yields no value")
	require.Len(t, diags, 1, "and is reported rather than dropped")
	assert.Equal(t, diag.DegradedConstruct, diags[0].Code)
	assert.Equal(t, 3, diags[0].Provenance.Source, "stamped with the source it was handed")
	assert.Equal(t, jsontext.Pointer("/p/example"), diags[0].Provenance.Pointer)
}

// TestDialectAt_KeepsEachKeywordOutOfScope pins that the resource and dialect
// keywords carry the reason meaning "no IR node is coming", not the one meaning
// "the field is missing for now".
func TestDialectAt_KeepsEachKeywordOutOfScope(t *testing.T) {
	t.Parallel()
	s := schemaFromYAML(t, "$id: 'urn:example:a'\n$schema: 'https://example.com/dialect'\ntype: string\n")

	got, diags := dialectAt(s, "/components/schemas/S", sourced(0))

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

// TestDialectAt_AttributesTheKeywordRatherThanTheSchema pins GitHub #522: an
// overlay that rewrites `$id` names the overlay as the source of both the kept
// entry and the diagnostic announcing it, since dialectAt locates each keyword
// at its own pointer rather than at the schema's.
func TestDialectAt_AttributesTheKeywordRatherThanTheSchema(t *testing.T) {
	t.Parallel()
	s := schemaFromYAML(t, "$id: 'urn:example:a'\ntype: string\n")

	got, diags := dialectAt(s, "/components/schemas/S", overlaid("/components/schemas/S/$id"))

	entry, ok := got["openapi:$id"]
	require.True(t, ok)
	assert.Equal(t, ir.Provenance{Source: 1, Pointer: "/components/schemas/S/$id"}, entry.Provenance)

	require.Len(t, diags, 1)
	assert.Equal(t, ir.SeverityInfo, diags[0].Severity)
	assert.Equal(t, ir.Provenance{Source: 1, Pointer: "/components/schemas/S/$id"}, diags[0].Provenance)
}

// schemaFromYAML unmarshals body as a bare schema through the same marshaller
// the compiler's loader parses documents with, so the raw nodes the verbatim
// readers read off are present. A schema built in Go carries none, which
// TestValidationOnlyAt_NeedsTheRawNode covers separately.
// pastMergeBound writes body as a flow mapping merged in through a chain of
// `<<` one level deeper than nodeview.MergeDepthLimit, which the parser reads
// and the compiler's view does not reach.
func pastMergeBound(body string) string {
	return openapitest.NestedMerges(nodeview.MergeDepthLimit+1, body)
}

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

// TestKind_String covers both named values and the default case, so an
// assertion failure or test diff over a Kind prints a name instead of a bare
// int.
func TestKind_String(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "Declaration", Declaration.String())
	assert.Equal(t, "Reference", Reference.String())
	assert.Equal(t, "Kind(99)", Kind(99).String())
}

// TestRead_NodeTypeIsReadFromTheRawXMLObject pins the one key this reader
// takes off the raw node: the library's XML model has no field for the
// `nodeType` OpenAPI 3.2 added, so a reader that consulted the model alone
// would drop a declaration the document makes (GitHub #615). The census is
// told the key was read, so it is not announced as undefined.
func TestRead_NodeTypeIsReadFromTheRawXMLObject(t *testing.T) {
	t.Parallel()
	s := schemaFromYAML(t, "type: string\nxml:\n  name: q\n  nodeType: attribute\n")

	got, diags := Read(Site{Kind: Declaration, Node: s}, "/components/schemas/S", sourced(0))

	require.NotNil(t, got.XML)
	assert.Equal(t, "q", got.XML.Name, "the modelled fields still come from the model")
	assert.Equal(t, "attribute", got.XML.NodeType)
	assert.Empty(t, diags, "a key the dialect defines is not announced as undefined")
}

// TestRead_NodeTypeWithNoXMLObjectHasNothingToFill covers applyNodeType's first
// branch. `nodeType` is written inside the xml object, so a schema declaring no
// xml object has no position the key could sit at — and no hints to carry it.
func TestRead_NodeTypeWithNoXMLObjectHasNothingToFill(t *testing.T) {
	t.Parallel()
	s := schemaFromYAML(t, "type: string\n")

	got, diags := Read(Site{Kind: Declaration, Node: s}, "/components/schemas/S", sourced(0))

	assert.Nil(t, got.XML)
	assert.Empty(t, diags)
}

// TestRead_NodeTypeOnlyADefinedValueIsPlaced pins what reaches XMLHints.NodeType
// on 3.2: one of the five node types the specification defines. Anything else is
// kept verbatim under the xml object's scope with a warning, an attribute flag
// the nodeType contradicts is kept the same way rather than overwritten, a
// nodeType merged in through `<<` is read as one written there is, and one
// merged in from past nodeview.MergeDepthLimit keeps the census's report.
func TestRead_NodeTypeOnlyADefinedValueIsPlaced(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name     string
		xml      string
		nodeType string
		kept     string // the Unmodeled key expected, "" for none
		code     string // the diagnostic expected at the key, "" for none
	}{
		{"defined", "{nodeType: cdata}", "cdata", "", ""},
		{"null", "{nodeType: ~}", "", "", ""},
		{"null beside attribute", "{attribute: true, nodeType: ~}", "attribute", "", ""},
		{"undefined value", "{nodeType: comment}", "", "openapi:xml/nodeType", diag.DegradedConstruct},
		{"not a string", "{nodeType: {a: 1}}", "", "openapi:xml/nodeType", diag.DegradedConstruct},
		{"agrees with attribute", "{attribute: true, nodeType: attribute}", "attribute", "", ""},
		{"contradicts attribute", "{attribute: true, nodeType: element}", "element",
			"openapi:xml/attribute", diag.DegradedConstruct},
		{"merged in", "{<<: {nodeType: cdata}, name: q}", "cdata", "", ""},
		{"merged in past the bound", pastMergeBound("{nodeType: cdata, name: q}"), "", "", diag.UnknownKeyUnreachable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			s := schemaFromYAML(t, "type: string\nxml: "+tc.xml+"\n")
			got, diags := Read(Site{Kind: Declaration, Node: s}, "/components/schemas/S", sourced(0))
			require.NotNil(t, got.XML)
			assert.Equal(t, tc.nodeType, got.XML.NodeType)
			if tc.kept == "" {
				assert.Empty(t, got.Unmodeled)
			} else {
				assert.Equal(t, ir.ReasonDegradedLowering, got.Unmodeled[tc.kept].Reason)
			}
			if tc.code == "" {
				assert.Empty(t, diags)
				return
			}
			require.Len(t, diags, 1, "got %v", diags)
			assert.Equal(t, tc.code, diags[0].Code)
		})
	}
}

// TestAt_ClassifiesEveryPositionShape pins the classification every annotation
// read starts from. A declaration misread as a reference would fall back to a
// referent that does not describe it, and a reference misread as a declaration
// would lose the fallback entirely. At classifies the position alone and asks
// nothing of where a $ref lands, so no site it builds holds a Referent: that is
// the readers that inherit, which build their own from the end of the chain.
func TestAt_ClassifiesEveryPositionShape(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		body     string
		wantKind Kind
		wantNode bool
	}{
		{name: "a written body is a declaration", body: "type: string\n", wantKind: Declaration, wantNode: true},
		{name: "a boolean schema has no body", body: "true\n", wantKind: Declaration},
		{
			name: "a $ref is a reference, whether or not it lands",
			body: "{$ref: '#/$defs/Missing'}\n", wantKind: Reference, wantNode: true,
		},
		{
			name: "a $ref with a body beside it is still a reference",
			body: "{$ref: '#/$defs/T', description: d}\n", wantKind: Reference, wantNode: true,
		},
		{
			name: "an empty $ref resolves nowhere, so it is a declaration",
			body: "{$ref: ''}\n", wantKind: Declaration, wantNode: true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := At(openapitest.SchemaFromYAML(t.Context(), t, tc.body))
			assert.Equal(t, tc.wantKind, got.Kind)
			assert.Equal(t, tc.wantNode, got.Node != nil, "Node presence")
			assert.Nil(t, got.Referent, "At fills no Referent")
		})
	}

	t.Run("a nil position is an empty declaration", func(t *testing.T) {
		t.Parallel()
		got := At(nil)
		assert.Equal(t, Declaration, got.Kind)
		assert.Nil(t, got.Node)
		assert.Nil(t, got.Referent)
	})
}

// TestAt_ReferenceSiteKeepsTheBodyWrittenBesideTheRef pins that a reference
// site's Node is the body written at the position, the $ref and what sits
// beside it, and not what the $ref names. The referent is no part of the site
// At builds (see TestAt_ClassifiesEveryPositionShape).
func TestAt_ReferenceSiteKeepsTheBodyWrittenBesideTheRef(t *testing.T) {
	t.Parallel()
	got := At(openapitest.SchemaFromYAML(t.Context(), t, "{$ref: '#/$defs/Target', description: fromSite}\n"))

	require.Equal(t, Reference, got.Kind)
	require.NotNil(t, got.Node, "the body written beside the $ref is the site's own")
	assert.Equal(t, "fromSite", got.Node.GetDescription())
	assert.Nil(t, got.Referent, "and the schema the $ref names is not read here")
}

// TestSchemaOf_ReturnsOnlyAWrittenBody pins the one nil-safe reader every site
// goes through: a position with no body written at it must report none, because
// a boolean schema and a nil position both admit no annotations at all.
func TestSchemaOf_ReturnsOnlyAWrittenBody(t *testing.T) {
	t.Parallel()
	assert.Nil(t, SchemaOf(nil), "a nil position writes no body")
	assert.Nil(t, SchemaOf(openapitest.SchemaFromYAML(t.Context(), t, "true\n")), "a boolean schema writes no body")
	assert.Nil(t, SchemaOf(openapitest.SchemaFromYAML(t.Context(), t, "false\n")), "including the false one")

	s := SchemaOf(openapitest.SchemaFromYAML(t.Context(), t, "type: string\ntitle: T\n"))
	require.NotNil(t, s)
	assert.Equal(t, "T", s.GetTitle())
}

// TestIsFalseSchema_OnlyTheFalseBoolean pins the predicate that distinguishes a
// structural mode from a schema. Reading `true` or an object schema as the false
// one would turn a permissive position into a forbidding one.
func TestIsFalseSchema_OnlyTheFalseBoolean(t *testing.T) {
	t.Parallel()
	assert.True(t, IsFalseSchema(openapitest.SchemaFromYAML(t.Context(), t, "false\n")))
	assert.False(t, IsFalseSchema(openapitest.SchemaFromYAML(t.Context(), t, "true\n")))
	assert.False(t, IsFalseSchema(openapitest.SchemaFromYAML(t.Context(), t, "type: string\n")))
	assert.False(t, IsFalseSchema(nil))
}

// TestIsNullLiteral_TellsANullFromFalse pins the three predicates that read a
// `null` written where a schema belongs. The parser decodes one as the boolean
// `false` schema, which admits no value where a null states nothing, so only
// the node tells them apart: IsNullLiteral reads every spelling of a null,
// through an alias, IsFalseSchema is true only for a written `false`, and
// Written is false for no schema and for a null.
func TestIsNullLiteral_TellsANullFromFalse(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, value string
		isNull      bool
		isFalse     bool
		written     bool
	}{
		{name: "the word null", value: "null", isNull: true},
		{name: "a tilde", value: "~", isNull: true},
		{name: "an empty value", value: "", isNull: true},
		{name: "a null that is anchored", value: "&n null", isNull: true},
		{name: "an alias to an anchored null", value: "*n", isNull: true},
		{name: "false", value: "false", isFalse: true, written: true},
		{name: "a false that is anchored", value: "&f false", isFalse: true, written: true},
		{name: "an alias to an anchored false", value: "*f", isFalse: true, written: true},
		{name: "true", value: "true", written: true},
		{name: "an object", value: "{type: string}", written: true},
		{name: "an empty object", value: "{}", written: true},
	}
	var props strings.Builder
	for i, tc := range tests {
		fmt.Fprintf(&props, "        p%d: %s\n", i, tc.value)
	}
	parsed, _, err := soa.Unmarshal(t.Context(), strings.NewReader("openapi: 3.1.0\n"+
		"info: {title: T, version: '1'}\npaths: {}\n"+
		"components:\n  schemas:\n    A:\n      properties:\n"+props.String()))
	require.NoError(t, err, "the parser reads a null schema; the load phase reports it")
	a, ok := parsed.Components.Schemas.Get("A")
	require.True(t, ok)
	require.Equal(t, len(tests), a.GetSchema().GetProperties().Len())

	for i, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			js, ok := a.GetSchema().GetProperties().Get(fmt.Sprintf("p%d", i))
			require.True(t, ok)
			require.NotNil(t, js)

			assert.Equal(t, tc.isNull, IsNullLiteral(js), "IsNullLiteral")
			assert.Equal(t, tc.isFalse, IsFalseSchema(js), "IsFalseSchema")
			assert.Equal(t, tc.written, Written(js), "Written")
		})
	}

	t.Run("no schema at all", func(t *testing.T) {
		t.Parallel()
		var absent *oas3.JSONSchema[oas3.Referenceable]
		assert.False(t, IsNullLiteral(absent))
		assert.False(t, IsFalseSchema(absent))
		assert.False(t, Written(absent))
	})

	t.Run("a false schema built in memory, which has no node", func(t *testing.T) {
		t.Parallel()
		built := oas3.NewJSONSchemaFromBool(false)
		assert.False(t, IsNullLiteral(built))
		assert.True(t, IsFalseSchema(built))
		assert.True(t, Written(built))
	})
}

// TestEffectiveVisibility_MapsTheFlagsToLifecycles pins the readOnly/writeOnly
// mapping of ir-design §5.2, the precedence a site holds over its referent, and
// the answer for the pairing that admits nothing.
//
// Precedence is per flag (the uniform merge of
// docs/compilers/openapi.md#ref-use-site): a site's readOnly settles readOnly
// only, so it does not cancel a referent's writeOnly. Both are in force, admit
// disjoint lifecycle sets, and leave the position visible in none. Until GitHub
// #276 that case read as plain readOnly, dropping the flag arriving second.
func TestEffectiveVisibility_MapsTheFlagsToLifecycles(t *testing.T) {
	t.Parallel()
	read := ir.Visibility{Only: []ir.Lifecycle{ir.LifecycleRead, ir.LifecycleDelete, ir.LifecycleQuery}}
	write := ir.Visibility{Only: []ir.Lifecycle{ir.LifecycleCreate, ir.LifecycleUpdate}}
	none := ir.Visibility{None: true}

	tests := []struct {
		name         string
		ref, tgt     string
		want         ir.Visibility
		wantDisjoint bool
	}{
		{name: "readOnly at the site", ref: "readOnly: true\n", want: read},
		{name: "writeOnly at the site", ref: "writeOnly: true\n", want: write},
		{name: "readOnly inherited from the referent", ref: "type: string\n", tgt: "readOnly: true\n", want: read},
		{name: "writeOnly inherited from the referent", ref: "type: string\n", tgt: "writeOnly: true\n", want: write},
		{name: "the site wins over the referent for the flag it writes", ref: "readOnly: true\n", tgt: "readOnly: false\n", want: read},
		{name: "a false flag is still the site's answer", ref: "readOnly: false\n", tgt: "readOnly: true\n"},
		{name: "both flags on one schema", ref: "readOnly: true\nwriteOnly: true\n", want: none, wantDisjoint: true},
		{name: "both flags on the referent", ref: "type: string\n", tgt: "readOnly: true\nwriteOnly: true\n", want: none, wantDisjoint: true},
		{name: "the site's readOnly leaves the referent's writeOnly in force", ref: "readOnly: true\n", tgt: "writeOnly: true\n", want: none, wantDisjoint: true},
		{name: "and the same the other way round", ref: "writeOnly: true\n", tgt: "readOnly: true\n", want: none, wantDisjoint: true},
		{name: "neither declares one", ref: "type: string\n", tgt: "type: string\n"},
		{name: "nothing at all", want: ir.Visibility{}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var ref, tgt *oas3.Schema
			if tc.ref != "" {
				ref = schemaFromYAML(t, tc.ref)
			}
			if tc.tgt != "" {
				tgt = schemaFromYAML(t, tc.tgt)
			}
			got, disjoint := EffectiveVisibility(ref, tgt)
			assert.Equal(t, tc.want, got)
			assert.Equal(t, tc.wantDisjoint, disjoint, "whether the flags left no lifecycle at all")
		})
	}
}

// TestEffectiveDefault_UseSiteOverridesReferent pins the precedence the carriers
// read a default by: the site's own default wins, and a site writing none falls
// through to its referent rather than reading its own absence as no default.
func TestEffectiveDefault_UseSiteOverridesReferent(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		ref, tgt string
		want     string // "" when no default is returned
	}{
		{name: "the site writes one", ref: "default: site\n", want: "site"},
		{name: "the site's wins over the referent's", ref: "default: site\n", tgt: "default: referent\n", want: "site"},
		{name: "an absent default falls through", ref: "type: string\n", tgt: "default: referent\n", want: "referent"},
		{name: "neither writes one", ref: "type: string\n", tgt: "type: string\n"},
		{name: "no referent", ref: "type: string\n"},
		{name: "no schemas at all"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var ref, tgt *oas3.Schema
			if tc.ref != "" {
				ref = schemaFromYAML(t, tc.ref)
			}
			if tc.tgt != "" {
				tgt = schemaFromYAML(t, tc.tgt)
			}
			got, diags := EffectiveDefault(ref, tgt, "/p", sourced(0))
			assert.Empty(t, diags)
			if tc.want == "" {
				assert.Nil(t, got)
				return
			}
			require.NotNil(t, got)
			assert.Equal(t, tc.want, got.Str)
		})
	}
}

// TestEffectiveDefault_UnconvertibleIsReportedAtTheSchema pins the failure
// half: a default no value can hold yields no value and one warning rather
// than being dropped silently. The warning is located at the schema the
// default was read at, not at the default, which departs from the values rule
// (GitHub #1101), so a fix moves this assertion with it.
func TestEffectiveDefault_UnconvertibleIsReportedAtTheSchema(t *testing.T) {
	t.Parallel()
	ref := schemaFromYAML(t, "type: number\ndefault: .nan\n")

	got, diags := EffectiveDefault(ref, nil, "/p", sourced(2))

	assert.Nil(t, got)
	require.Len(t, diags, 1)
	assert.Equal(t, ir.SeverityWarning, diags[0].Severity)
	assert.Equal(t, diag.DegradedConstruct, diags[0].Code)
	assert.Equal(t, ir.Provenance{Source: 2, Pointer: "/p"}, diags[0].Provenance)
}

// TestEffectiveDeprecated_UseSiteOverridesReferent pins the same precedence for
// the deprecated flag, including the case the nil-safe read exists for: a site
// that writes no flag at all must fall through to the referent rather than
// reading its own absence as false.
func TestEffectiveDeprecated_UseSiteOverridesReferent(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		ref, tgt string
		want     bool
	}{
		{name: "the site says so", ref: "deprecated: true\n", want: true},
		{name: "the site says not, over a referent that does", ref: "deprecated: false\n", tgt: "deprecated: true\n"},
		{name: "an absent flag falls through", ref: "type: string\n", tgt: "deprecated: true\n", want: true},
		{name: "neither says anything", ref: "type: string\n", tgt: "type: string\n"},
		{name: "no schemas at all"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var ref, tgt *oas3.Schema
			if tc.ref != "" {
				ref = schemaFromYAML(t, tc.ref)
			}
			if tc.tgt != "" {
				tgt = schemaFromYAML(t, tc.tgt)
			}
			assert.Equal(t, tc.want, EffectiveDeprecated(ref, tgt))
		})
	}
}

// TestFillTypeDocs_AssignsRatherThanAccumulates pins the "every field is
// assigned" rule. A pointer read twice — lowered in place and again through a
// $ref that hoists it — must not end up with two copies of its externalDocs.
func TestFillTypeDocs_AssignsRatherThanAccumulates(t *testing.T) {
	t.Parallel()
	s := schemaFromYAML(t, "title: T\ndescription: D\nexternalDocs: {url: 'https://x', description: E}\n")

	var d ir.Docs
	FillTypeDocs(&d, s)
	FillTypeDocs(&d, s)

	assert.Equal(t, "T", d.Summary)
	assert.Equal(t, "D", d.Description)
	assert.Equal(t, []ir.Link{{URL: "https://x", Description: "E"}}, d.ExternalDocs)
}

// TestFillTypeDocs_AnAbsentFieldOverwritesNothing pins the other half: a schema
// that writes no title must leave one already read from a referent standing,
// which is what makes the field-by-field merge in FillCarrierDocs work.
func TestFillTypeDocs_AnAbsentFieldOverwritesNothing(t *testing.T) {
	t.Parallel()
	d := ir.Docs{Summary: "kept", Description: "kept", ExternalDocs: []ir.Link{{URL: "kept"}}}

	FillTypeDocs(&d, schemaFromYAML(t, "type: string\n"))

	assert.Equal(t, ir.Docs{Summary: "kept", Description: "kept", ExternalDocs: []ir.Link{{URL: "kept"}}}, d)
}

// TestFillCarrierDocs_MergesFieldByField pins the rule
// docs/compilers/openapi.md#ref-use-site at the carrier: the referent's
// documentation arrives first and the site's overrides it one field at a time,
// so a site that writes only a description still inherits the referent's title
// rather than replacing the whole set.
func TestFillCarrierDocs_MergesFieldByField(t *testing.T) {
	t.Parallel()
	var d ir.Docs

	FillCarrierDocs(&d, schemaFromYAML(t, "description: fromSite\n"),
		schemaFromYAML(t, "title: fromTarget\ndescription: fromTarget\n"))

	assert.Equal(t, "fromTarget", d.Summary, "the title has no site-local answer")
	assert.Equal(t, "fromSite", d.Description, "the site wrote one, so it wins")
}

// TestXMLHints_ReadsEveryFieldDirectly pins the fields read off the XML object
// and the nil guard. The fields are read directly rather than through getters
// that dereference unset pointers, so a schema writing only some of them must
// not crash or invent the rest.
func TestXMLHints_ReadsEveryFieldDirectly(t *testing.T) {
	t.Parallel()
	assert.Nil(t, XMLHints(nil))

	s := schemaFromYAML(t, "type: string\nxml: {name: n, namespace: ns, prefix: p, wrapped: true, attribute: true}\n")
	assert.Equal(t, &ir.XMLHints{Name: "n", Namespace: "ns", Prefix: "p", Wrapped: true, NodeType: "attribute"},
		XMLHints(s.GetXML()))

	bare := schemaFromYAML(t, "type: string\nxml: {}\n")
	assert.Equal(t, &ir.XMLHints{}, XMLHints(bare.GetXML()), "an empty xml object invents nothing")

	notAttr := schemaFromYAML(t, "type: string\nxml: {attribute: false}\n")
	assert.Empty(t, XMLHints(notAttr.GetXML()).NodeType, "attribute: false is not the attribute node type")
}

// TestSchemaExamplesAt_ReadsBothKeywordsInOrder pins that `example` and each
// entry of `examples` are read, since a position may write either and the two
// are separate keywords rather than aliases.
func TestSchemaExamplesAt_ReadsBothKeywordsInOrder(t *testing.T) {
	t.Parallel()
	s := schemaFromYAML(t, "type: string\nexample: one\nexamples: [two, three]\n")

	got, diags := schemaExamplesAt(s, "/components/schemas/S", sourced(0))

	assert.Empty(t, diags)
	require.Len(t, got, 3)
	for i, want := range []string{"one", "two", "three"} {
		require.NotNil(t, got[i].Value)
		assert.Equal(t, want, got[i].Value.Str, "example %d", i)
	}
}

// TestSchemaExamplesAt_AttributesAnUnconvertibleExampleAtItsPointer pins
// GitHub #522 on the warning route: an overlay that rewrites `example` into
// something unconvertible is named by the diagnostic reporting the failure,
// because the diagnostic is located at the example's own pointer.
func TestSchemaExamplesAt_AttributesAnUnconvertibleExampleAtItsPointer(t *testing.T) {
	t.Parallel()
	s := schemaFromYAML(t, "type: number\nexample: .nan\n")

	got, diags := schemaExamplesAt(s, "/S", overlaid("/S/example"))

	assert.Empty(t, got, "an unconvertible example yields no value")
	require.Len(t, diags, 1)
	assert.Equal(t, ir.Provenance{Source: 1, Pointer: "/S/example"}, diags[0].Provenance)
}

// TestAppendExample_ConvertsAndAppends pins the converter every example site
// shares. The example's value is the converted node and nothing else about the
// prototype changes, so a site that fills in a name or a description keeps it.
func TestAppendExample_ConvertsAndAppends(t *testing.T) {
	t.Parallel()
	proto := ir.Example{Name: "n", Summary: "s", Description: "d"}

	out, diags := AppendExample(nil, proto, openapitest.StrNode("hello"), sourced(0), "/p", "examples", "n")

	assert.Empty(t, diags, "a convertible node is announced by nothing")
	require.Len(t, out, 1)
	require.NotNil(t, out[0].Value)
	assert.Equal(t, "hello", out[0].Value.Str)
	got := out[0]
	got.Value = nil
	assert.Equal(t, proto, got, "the prototype's own fields survive unchanged")
}

// TestAppendExample_UnconvertibleValueIsReported pins the other half: a node
// that cannot become a value adds nothing to the list, and the pointer it is
// reported at is built from the base and the segments — which is the only path
// that joins them, so a wrong join shows up nowhere else.
func TestAppendExample_UnconvertibleValueIsReported(t *testing.T) {
	t.Parallel()
	nan := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!float", Value: ".nan"}

	out, diags := AppendExample(nil, ir.Example{}, nan, sourced(0), "/p", "examples", "n")

	assert.Empty(t, out, "nothing is appended when the value does not convert")
	require.Len(t, diags, 1)
	assert.Equal(t, diag.DegradedConstruct, diags[0].Code)
	assert.Equal(t, jsontext.Pointer("/p/examples/n"), diags[0].Provenance.Pointer)
}

// sourced is the Locator for a compile with no overlay: every position belongs
// to the one source at index.
func sourced(index int) Locator {
	return func(pointer jsontext.Pointer, _ ...jsontext.Pointer) ir.Provenance {
		return ir.Provenance{Source: index, Pointer: pointer}
	}
}

// overlaid is the Locator for a compile where an overlay touched exactly
// pointers: each resolves to source 1, as ProvenanceAt resolves a position the
// overlay introduced or rewrote, and every other pointer resolves to source 0,
// the base document. It answers for a position alone and ignores from, so a test
// of a combined record uses recording instead.
func overlaid(pointers ...jsontext.Pointer) Locator {
	return func(pointer jsontext.Pointer, _ ...jsontext.Pointer) ir.Provenance {
		if slices.Contains(pointers, pointer) {
			return ir.Provenance{Source: 1, Pointer: pointer}
		}
		return ir.Provenance{Source: 0, Pointer: pointer}
	}
}
