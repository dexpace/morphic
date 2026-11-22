// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package schema_test

import (
	"encoding/json/jsontext"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/compilers/openapi/internal/schema"
	"github.com/dexpace/morphic/ir"
)

func TestProperty_UnquotedDateDefaultPreserved(t *testing.T) {
	t.Parallel()
	// TestEnum_UnquotedDatesStayClosedEnum's default sits at the component
	// level, which nothing lowers by itself; this covers the path that actually
	// surfaces the bug in practice — a date default declared on an object
	// property.
	spec := openapitest.ComponentSpec(`    S:
      type: object
      properties:
        d:
          type: string
          format: date
          default: 2021-01-01
`)
	doc, diags := schema.LowerSpec(t, spec)
	assert.Empty(t, diags, "no diagnostics at all: the date default converts cleanly")
	model, ok := doc.Types[componentID("S")].(*ir.Model)
	require.True(t, ok)
	require.Len(t, model.Properties, 1)
	require.NotNil(t, model.Properties[0].Default)
	assert.Equal(t, ir.Value{Kind: ir.ValueString, String: "2021-01-01"}, *model.Properties[0].Default)
}

func TestLower_PropertyDetailRichSchema(t *testing.T) {
	t.Parallel()
	spec := openapitest.ComponentSpec(`    D:
      type: object
      externalDocs: {url: 'https://x', description: more}
      properties:
        withXml:
          type: string
          xml: {name: n, namespace: 'urn:x', prefix: p, wrapped: true, attribute: true}
        withExample: {type: string, example: hi, examples: [a, b]}
        withExt: {type: string, x-foo: bar}
        badDefault: {type: number, default: .inf}
`)
	doc, diags := schema.LowerSpec(t, spec)
	model := typeByName(doc, "D").(*ir.Model)
	assert.NotEmpty(t, model.Docs.ExternalDocs)
	byWire := openapitest.PropertiesByWire(model.Properties)
	require.NotNil(t, byWire["withXml"].XML)
	assert.Equal(t, new(true), byWire["withXml"].XML.Attribute)
	assert.Equal(t, "urn:x", byWire["withXml"].XML.Namespace)
	assert.Equal(t, new(true), byWire["withXml"].XML.Wrapped)
	assert.Len(t, byWire["withExample"].Examples, 3)
	assert.NotEmpty(t, byWire["withExt"].Unmodeled)
	var sawDefaultWarn bool
	for _, diagnostic := range diags {
		if diagnostic.Severity == ir.SeverityWarning && diagnostic.Code == diag.UnloweredConstruct {
			sawDefaultWarn = true
		}
	}
	assert.True(t, sawDefaultWarn, "malformed default warns")
	assert.Nil(t, byWire["badDefault"].Default, "and is dropped with its warning, not kept as a zero value")
}

func TestLower_RefTargetDescriptionFallback(t *testing.T) {
	t.Parallel()
	spec := openapitest.ComponentSpec(`    Owner:
      type: object
      properties:
        ref: {$ref: '#/components/schemas/Target'}
    Target:
      type: string
      description: target-desc
`)
	doc, diags := schema.LowerSpec(t, spec)
	openapitest.RequireNoErrorDiags(t, diags)
	model := typeByName(doc, "Owner").(*ir.Model)
	assert.Equal(t, "target-desc", model.Properties[0].Docs.Description)
}

func TestModel_DefaultBigLiteral(t *testing.T) {
	t.Parallel()
	spec := openapitest.ComponentSpec(`    S:
      type: object
      properties:
        n: {type: integer, default: 9007199254740993}
`)
	doc, diags := schema.LowerSpec(t, spec)
	openapitest.RequireNoErrorDiags(t, diags)
	model := doc.Types[componentID("S")].(*ir.Model)
	require.NotNil(t, model.Properties[0].Default)
	assert.Equal(t, ir.ValueNumber, model.Properties[0].Default.Kind)
	assert.Equal(t, ir.BigVal("9007199254740993"), model.Properties[0].Default.Number)
}

func TestFillPropertyDetail_UnconvertibleExampleDiagnosed(t *testing.T) {
	t.Parallel()
	// A custom tag is structurally unconvertible; the example must be skipped
	// (an example is an annotation, not a structural hole) but never silently —
	// the conversion error was previously discarded on the floor.
	spec := openapitest.ComponentSpec("    S:\n      type: object\n      properties:\n        n:\n          type: string\n          example: !foo bar\n")
	doc, diags := schema.LowerSpec(t, spec)
	model, ok := doc.Types[componentID("S")].(*ir.Model)
	require.True(t, ok)
	assert.Empty(t, model.Properties[0].Examples, "the unconvertible example is skipped, not appended")
	require.Equal(t, 1, openapitest.CountDiagsAt(diags, diag.DegradedConstruct, ir.SeverityWarning))
	diagnostic, ok := openapitest.FirstDegradedWarning(diags)
	require.True(t, ok)
	assert.Equal(t, jsontext.Pointer("/components/schemas/S/properties/n/example"), diagnostic.Provenance.Pointer)
	assert.Contains(t, diagnostic.Message, "example:")
}

func TestModel_ReadOnlyWriteOnlyVisibility(t *testing.T) {
	t.Parallel()
	spec := openapitest.ComponentSpec(`    S:
      type: object
      properties:
        r: {type: string, readOnly: true}
        w: {type: string, writeOnly: true}
`)
	doc, diags := schema.LowerSpec(t, spec)
	openapitest.RequireNoErrorDiags(t, diags)
	model := doc.Types[componentID("S")].(*ir.Model)
	byName := openapitest.PropertiesByWire(model.Properties)
	assert.Equal(t, ir.Visibility{Only: []ir.Lifecycle{ir.LifecycleRead, ir.LifecycleDelete, ir.LifecycleQuery}}, byName["r"].Visibility)
	assert.Equal(t, ir.Visibility{Only: []ir.Lifecycle{ir.LifecycleCreate, ir.LifecycleUpdate}}, byName["w"].Visibility)
}

// TestModel_ReadOnlyAndWriteOnlyTogetherAreVisibleNowhere pins the answer for a
// property whose schema writes both flags: they admit disjoint lifecycle sets,
// so the property is admitted by none. Declaring both used to lower exactly as
// declaring readOnly alone did, in silence, while the same pairing spread over
// two allOf entries already intersected to InvisibleInEveryLifecycle and warned
// (GitHub #276).
func TestModel_ReadOnlyAndWriteOnlyTogetherAreVisibleNowhere(t *testing.T) {
	t.Parallel()
	spec := openapitest.ComponentSpec(`    S:
      type: object
      properties:
        both: {type: string, readOnly: true, writeOnly: true}
        r: {type: string, readOnly: true}
`)
	doc, diags := schema.LowerSpec(t, spec)
	openapitest.RequireNoErrorDiags(t, diags)
	model := doc.Types[componentID("S")].(*ir.Model)
	byName := openapitest.PropertiesByWire(model.Properties)

	assert.Equal(t, ir.Visibility{InvisibleInEveryLifecycle: true}, byName["both"].Visibility)
	assert.NotEqual(t, byName["r"].Visibility, byName["both"].Visibility,
		"declaring both flags must not lower exactly as declaring readOnly alone does")
	message := openapitest.DiagMessageAt(t, diags, diag.DisjointVisibility, ir.SeverityWarning,
		"/components/schemas/S/properties/both")
	assert.Contains(t, message, "writeOnly", "the report names the keyword that was being dropped")
	assert.Equal(t, 1, openapitest.CountDiagsAt(diags, diag.DisjointVisibility, ir.SeverityWarning),
		"the property declaring one flag is not reported")
}

func TestModel_PasswordFormatSecret(t *testing.T) {
	t.Parallel()
	spec := openapitest.ComponentSpec(`    S:
      type: object
      properties:
        pw: {type: string, format: password}
`)
	doc, diags := schema.LowerSpec(t, spec)
	openapitest.RequireNoErrorDiags(t, diags)
	model := doc.Types[componentID("S")].(*ir.Model)
	assert.True(t, model.Properties[0].Secret)
}

func TestModel_PropertyDeprecation(t *testing.T) {
	t.Parallel()
	spec := openapitest.ComponentSpec(`    S:
      type: object
      properties:
        old: {type: string, deprecated: true}
`)
	doc, diags := schema.LowerSpec(t, spec)
	openapitest.RequireNoErrorDiags(t, diags)
	model := doc.Types[componentID("S")].(*ir.Model)
	assert.NotNil(t, model.Properties[0].Deprecation)
}

func TestModel_PropertyXML(t *testing.T) {
	t.Parallel()
	spec := openapitest.ComponentSpec(`    S:
      type: object
      properties:
        p: {type: string, xml: {name: n, attribute: true}}
`)
	doc, diags := schema.LowerSpec(t, spec)
	openapitest.RequireNoErrorDiags(t, diags)
	model := doc.Types[componentID("S")].(*ir.Model)
	require.NotNil(t, model.Properties[0].XML)
	assert.Equal(t, "n", model.Properties[0].XML.Name)
	assert.Equal(t, new(true), model.Properties[0].XML.Attribute)
}

func TestModel_RefSiblingDescriptionWins(t *testing.T) {
	t.Parallel()
	spec := openapitest.ComponentSpec(`    Target: {type: string, description: "target desc"}
    S:
      type: object
      properties:
        p:
          $ref: '#/components/schemas/Target'
          description: "sibling desc"
`)
	doc, diags := schema.LowerSpec(t, spec)
	openapitest.RequireNoErrorDiags(t, diags)
	model := doc.Types[componentID("S")].(*ir.Model)
	assert.Equal(t, "sibling desc", model.Properties[0].Docs.Description)
}

func TestSchema_RefTargetReadOnlyVisibility(t *testing.T) {
	t.Parallel()
	spec := openapitest.ComponentSpec(`    Owner:
      type: object
      properties:
        p: {$ref: '#/components/schemas/RO'}
    RO: {type: string, readOnly: true}
`)
	doc, diags := schema.LowerSpec(t, spec)
	openapitest.RequireNoErrorDiags(t, diags)
	model := typeByName(doc, "Owner").(*ir.Model)
	assert.NotEmpty(t, model.Properties[0].Visibility.Only, "readOnly from the ref target applies")
}

// TestPropertyAnnotations_KeptWhenAnOutsideRefNamesTheProperty covers
// TestInlinePosition_OutsideRefDoesNotMoveTheHome's collision at a position
// that has a carrier. A $ref naming a property's schema
// pointer hoists a node there, and the property then read that node as its own
// home and left its annotations to it — on top of the copy it had already
// written when it lowered first, and instead of any copy at all when it lowered
// second.
func TestPropertyAnnotations_KeptWhenAnOutsideRefNamesTheProperty(t *testing.T) {
	t.Parallel()
	owner := "    A: {type: object, properties: {p: " + openapitest.InlineProbeBody + "}}\n"
	outsider := "    Outsider: {$ref: '#/components/schemas/A/properties/p'}\n"
	for _, tc := range []struct{ name, spec string }{
		{"reference declared first", openapitest.ComponentSpec(outsider + owner)},
		{"reference declared last", openapitest.ComponentSpec(owner + outsider)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			doc, diags := parseFull(t, tc.spec)
			openapitest.RequireNoErrorDiags(t, diags)

			model, ok := doc.Types[componentID("A")].(*ir.Model)
			require.True(t, ok)
			property, ok := openapitest.PropertiesByWire(model.Properties)["p"]
			require.True(t, ok)
			assert.Equal(t, ir.TypeID("type/primitive/string"), property.Type.Target,
				"the property's own type is unchanged by the outside reference")
			openapitest.AssertProbeDocsKept(t, property.Docs)
			assert.Contains(t, property.Unmodeled, "openapi:x-vendor")
			assert.Contains(t, property.Unmodeled, "openapi:not")

			sc, ok := doc.Types["type/anonymous/components/schemas/A/properties/p"].(*ir.Scalar)
			require.True(t, ok, "and the referenced pointer still names the schema written there")
			assertProbeAnnotationsKept(t, sc)
		})
	}
}

// TestPropertyAnnotations_NotesAreThePropertysWhateverItsSchemaHoists pins the
// one home of a property's notes, its documentation, deprecation and examples,
// at every kind of node a property's own schema hoists: the property holds them,
// as where the schema hoists none, and the node holds none of them, while
// keeping what only a node holds, its XML hints, its redaction and its
// extensions (docs/ir/annotations.md#one-structural-home-per-declaration).
func TestPropertyAnnotations_NotesAreThePropertysWhateverItsSchemaHoists(t *testing.T) {
	t.Parallel()
	const notes = "title: T, description: DOC, deprecated: true, example: EX, xml: {name: X}, format: password, x-vendor: V"
	for _, tc := range []struct {
		name, schema string
		kind         ir.TypeKind
	}{
		{"a list", "{type: array, items: {type: string}, " + notes + "}", ir.KindList},
		{"a map", "{type: object, additionalProperties: {type: string}, " + notes + "}", ir.KindMap},
		{"an enum", "{type: string, enum: [EX], " + notes + "}", ir.KindEnum},
		{"a model", "{type: object, properties: {a: {type: string}}, " + notes + "}", ir.KindModel},
		{"a union", "{oneOf: [{type: string}, {type: integer}], " + notes + "}", ir.KindUnion},
		{"a literal", "{const: EX, " + notes + "}", ir.KindLiteral},
		{"a tuple", "{type: array, prefixItems: [{type: string}], " + notes + "}", ir.KindTuple},
		{"a scalar", "{type: string, contentMediaType: image/png, contentEncoding: base64, " + notes + "}", ir.KindScalar},
		{"an inline refinement", "{allOf: [{type: string, enum: [EX]}], " + notes + "}", ir.KindEnum},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			doc, diags := parseFull(t, openapitest.ComponentSpecVer("3.1.0",
				"    A:\n      type: object\n      properties:\n        p: "+tc.schema+"\n"))
			openapitest.RequireNoErrorDiags(t, diags)

			property := propertyOf(t, doc, "A", "p")
			require.Equal(t, ir.TypeID("type/anonymous/components/schemas/A/properties/p"), property.Type.Target,
				"the schema hoists a node of its own")
			assert.Equal(t, ir.Docs{Summary: "T", Description: "DOC"}, property.Docs)
			assert.Equal(t, &ir.Deprecation{}, property.Deprecation)
			require.Len(t, property.Examples, 1)
			assert.Equal(t, "EX", property.Examples[0].Value.String)
			assert.False(t, property.Secret, "the node keeps the redaction")
			assert.Nil(t, property.XML)
			assert.Empty(t, property.Unmodeled)

			node := doc.Types[property.Type.Target]
			require.NotNil(t, node)
			assert.Equal(t, tc.kind, node.Kind())
			common := node.Common()
			assert.Equal(t, ir.Docs{}, common.Docs, "one home")
			assert.Nil(t, common.Deprecation)
			assert.Empty(t, common.Examples)
			assert.True(t, common.Sensitive)
			require.NotNil(t, common.XML)
			assert.Equal(t, "X", common.XML.Name)
			assert.Contains(t, common.Unmodeled, "openapi:x-vendor")
		})
	}
}

// TestPropertyAnnotations_APromotedDeprecationIsTheCarriers pins the
// deprecation a key states or annotates on a schema hoisting a node: the
// property holds it with its members and the extension-promotion mark, and the
// node, which keeps the keys, holds neither, as a keyword's deprecation is the
// property's (docs/compilers/openapi.md#extension-promotion). A key no member
// holds is reported once, where it is written.
func TestPropertyAnnotations_APromotedDeprecationIsTheCarriers(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, openapitest.ComponentSpecVer("3.1.0", `    A:
      type: object
      properties:
        keyword: {type: array, items: {type: string}, deprecated: true, x-deprecated-reason: why, x-sensitive: true}
        key: {type: array, items: {type: string}, x-fern-availability: deprecated, x-sunset: "2030-01-01"}
        misfit: {type: array, items: {type: string}, deprecated: true, x-deprecated-since: 5}
        live: {type: array, items: {type: string}, x-deprecated-since: "2"}
`))
	openapitest.RequireNoErrorDiags(t, diags)
	for _, tc := range []struct {
		wire   string
		want   *ir.Deprecation
		key    string
		marked bool
	}{
		{"keyword", &ir.Deprecation{Message: "why"}, "openapi:x-deprecated-reason", true},
		{"key", &ir.Deprecation{RemovalDate: "2030-01-01"}, "openapi:x-sunset", true},
		{"misfit", &ir.Deprecation{}, "openapi:x-deprecated-since", false},
		{"live", nil, "openapi:x-deprecated-since", false},
	} {
		property := propertyOf(t, doc, "A", tc.wire)
		assert.Equal(t, tc.want, property.Deprecation, tc.wire)
		assert.Equal(t, tc.marked, property.Provenance.Inferred == "extension-promotion", "%s: the carrier is marked", tc.wire)
		assert.Empty(t, property.Unmodeled, tc.wire)
		node := doc.Types[property.Type.Target].Common()
		assert.Nil(t, node.Deprecation, tc.wire)
		assert.Contains(t, node.Unmodeled, tc.key, "%s: the node keeps the key", tc.wire)
	}
	keyword := doc.Types[propertyOf(t, doc, "A", "keyword").Type.Target].Common()
	assert.True(t, keyword.Sensitive, "a redaction stays on the node")
	assert.Equal(t, ir.Inferred("extension-promotion"), keyword.Provenance.Inferred, "which its mark explains")
	assert.False(t, propertyOf(t, doc, "A", "keyword").Secret)
	assert.Equal(t, ir.Inferred(""), doc.Types[propertyOf(t, doc, "A", "key").Type.Target].Common().Provenance.Inferred,
		"a node promoting nothing is not marked")

	misfits := diagsAtPointer(diags, diag.DegradedConstruct, "/components/schemas/A/properties/misfit/x-deprecated-since")
	assert.Len(t, misfits, 1, "the misreading is reported once, at its key")
}

// TestPropertyAnnotations_AUseOfARefKeepsItsNotesOnItsNode pins what the rule
// leaves: a position naming a $ref, beside its keywords or as a conjunct of its
// allOf, keeps its notes on the node it builds, which an emitter folds onto the
// carrier where it is a decorator (docs/emitters/refine/decorators.md). Each
// spelling of one use reads that alike, the union beside an allOf of the $ref
// included.
func TestPropertyAnnotations_AUseOfARefKeepsItsNotesOnItsNode(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, openapitest.ComponentSpecVer("3.1.0", `    Base: {type: object, properties: {b: {type: string}}}
    A:
      type: object
      properties:
        decorator: {allOf: [{$ref: '#/components/schemas/Base'}], description: DOC, deprecated: true}
        beside: {$ref: '#/components/schemas/Base', description: DOC, deprecated: true}
        composed: {allOf: [{$ref: '#/components/schemas/Base'}], properties: {x: {type: string}}, description: DOC, deprecated: true}
        wrapped: {allOf: [{$ref: '#/components/schemas/Base'}], oneOf: [{required: [b]}, {required: [x]}], description: DOC, deprecated: true}
        entry: {allOf: [{$ref: '#/components/schemas/Base'}, {oneOf: [{required: [b]}, {required: [x]}], description: DOC, deprecated: true}]}
`))
	openapitest.RequireNoErrorDiags(t, diags)
	for _, wire := range []string{"decorator", "beside", "composed", "wrapped", "entry"} {
		property := propertyOf(t, doc, "A", wire)
		require.Equal(t, ir.TypeID("type/anonymous/components/schemas/A/properties/"+wire), property.Type.Target, wire)
		assert.Empty(t, property.Docs.Description, wire)
		assert.Nil(t, property.Deprecation, wire)
		node := doc.Types[property.Type.Target].Common()
		assert.Equal(t, "DOC", node.Docs.Description, wire)
		assert.NotNil(t, node.Deprecation, wire)
	}
}

// TestPropertyAnnotations_CarriedWhenSchemaOwnsNoNode is the other half of that
// rule, and the reason a property never hoists an alias of its own: a scalar
// property's declaration has ir.Property to land on already.
func TestPropertyAnnotations_CarriedWhenSchemaOwnsNoNode(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, openapitest.ComponentSpec(
		"    A:\n      type: object\n      properties:\n        p: "+openapitest.InlineProbeBody+"\n"))
	openapitest.RequireNoErrorDiags(t, diags)

	owner, ok := doc.Types[componentID("A")].(*ir.Model)
	require.True(t, ok)
	property, ok := openapitest.PropertiesByWire(owner.Properties)["p"]
	require.True(t, ok)
	assert.Equal(t, ir.TypeID("type/primitive/string"), property.Type.Target, "a property keeps resolving to the shared primitive")
	assert.NotContains(t, doc.Types, ir.TypeID("type/anonymous/components/schemas/A/properties/p"))
	openapitest.AssertProbeDocsKept(t, property.Docs)
	assert.NotNil(t, property.Deprecation)
	openapitest.AssertProbeExample(t, property.Examples)
	require.NotNil(t, property.XML)
	assert.Equal(t, "X", property.XML.Name)
	assert.Contains(t, property.Unmodeled, "openapi:x-vendor")
	assert.Contains(t, property.Unmodeled, "openapi:not")
	require.NotNil(t, property.Constraints)
	require.NotNil(t, property.Constraints.MaxLength)
	assert.Equal(t, int64(3), *property.Constraints.MaxLength)
}

// TestPropertyDocs_RefTargetReachesTheCarrier pins the referent half of the
// documentation merge, keyword by keyword: a property that declares nothing but
// a $ref still carries what its referent documents, and the referent's own node
// keeps its copy. The rule docs/compilers/openapi.md#ref-use-site asks for that
// merge uniformly, so the three keywords stand or fall together — a fix that
// inherited only the description would leave the other two behind at every
// carrier.
func TestPropertyDocs_RefTargetReachesTheCarrier(t *testing.T) {
	t.Parallel()
	for _, kw := range carrierDocKeywords() {
		t.Run(kw.name, func(t *testing.T) {
			t.Parallel()
			doc, diags := parseFull(t, openapitest.ComponentSpec(docTarget()+
				"    Owner: {type: object, properties: {p: {$ref: '#/components/schemas/Target'}}}\n"))
			openapitest.RequireNoErrorDiags(t, diags)

			property := propertyOf(t, doc, "Owner", "p")
			assert.Equal(t, componentID("Target"), property.Type.Target, "a bare $ref needs no alias")
			assert.Equal(t, "REF-"+kw.name, kw.read(property.Docs), "the carrier reads the referent's %s", kw.name)
			assert.Equal(t, "REF-"+kw.name, kw.read(doc.Types[componentID("Target")].Common().Docs),
				"and the referent keeps its own %s", kw.name)
		})
	}
}

// TestPropertyDocs_UseSiteWinsKeywordByKeyword pins the other half: a keyword
// written beside the $ref replaces that field and only that field, the rest
// still arriving from the referent. Taking either schema whole fails every
// subtest.
func TestPropertyDocs_UseSiteWinsKeywordByKeyword(t *testing.T) {
	t.Parallel()
	for _, written := range carrierDocKeywords() {
		t.Run(written.name, func(t *testing.T) {
			t.Parallel()
			doc, diags := parseFull(t, openapitest.ComponentSpec(docTarget()+
				"    Owner: {type: object, properties: {p: {$ref: '#/components/schemas/Target', "+
				written.write("SITE")+"}}}\n"))
			openapitest.RequireNoErrorDiags(t, diags)

			property := propertyOf(t, doc, "Owner", "p")
			assert.Equal(t, componentID("Target"), property.Type.Target, "a carrier hoists no alias for its siblings")
			for _, kw := range carrierDocKeywords() {
				want := "REF-" + kw.name
				if kw.name == written.name {
					want = "SITE"
				}
				assert.Equal(t, want, kw.read(property.Docs), "%s, with %s written at the site", kw.name, written.name)
			}
		})
	}
}

// TestFillPropertyDetail_SecretFollowsTheReferent pins both sides of the $ref
// read: a property referencing a redaction schema is secret at the use, as it
// is deprecated when its referent is (docs/compilers/openapi.md#password), and
// one referencing a plain string component stays not secret.
func TestFillPropertyDetail_SecretFollowsTheReferent(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, openapitest.ComponentSpec(
		"    Pw: {type: string, format: password}\n"+
			"    Plain: {type: string}\n"+
			"    Login: {type: object, properties: {secret: {$ref: '#/components/schemas/Pw'}, "+
			"plain: {$ref: '#/components/schemas/Plain'}}}\n"))
	openapitest.RequireNoErrorDiags(t, diags)

	assert.True(t, propertyOf(t, doc, "Login", "secret").Secret,
		"the use site reads the referent's format")
	assert.False(t, propertyOf(t, doc, "Login", "plain").Secret,
		"a $ref to a plain component stays not secret")
}

// docTarget is a component writing all three keywords, each valued after itself
// so a carrier reading one of them cannot be confused with a carrier reading
// another.
func docTarget() string {
	parts := make([]string, 0, len(carrierDocKeywords()))
	for _, kw := range carrierDocKeywords() {
		parts = append(parts, kw.write("REF-"+kw.name))
	}
	return "    Target: {type: string, " + strings.Join(parts, ", ") + "}\n"
}

func carrierDocKeywords() []carrierDocKeyword {
	return []carrierDocKeyword{
		{"title",
			func(v string) string { return "title: " + v },
			func(docs ir.Docs) string { return docs.Summary }},
		{"description",
			func(v string) string { return "description: " + v },
			func(docs ir.Docs) string { return docs.Description }},
		{"externalDocs",
			func(v string) string { return "externalDocs: {url: 'https://e.example', description: " + v + "}" },
			func(docs ir.Docs) string {
				if len(docs.ExternalDocs) != 1 {
					return ""
				}
				return docs.ExternalDocs[0].Description
			}},
	}
}

// carrierDocKeyword is one of the three documentation keywords a $ref position
// merges onto its carrier: how to write it with a given value, and which
// ir.Docs field reading it back means the merge kept it.
type carrierDocKeyword struct {
	name  string
	write func(value string) string
	read  func(ir.Docs) string
}

// TestFillPropertyAnnotations_RefSiblingExampleBindsTheSite locks in the
// $ref-sibling-example behaviour at a property position: an example written
// beside a $ref binds the property, not the referent, because a $ref position
// hoists no node, so fillPropertyAnnotations records it on the property.
// Property property's $ref targets a named component (Target), so
// componentlookup.Scope.ComponentRef resolves it directly without ever calling
// hoistSubSchema. TestSchemaSiblings_RefdSubSchemaKeepsThem covers the
// analogous hoistSubSchema path, where the $ref target is an internal
// sub-schema pointer rather than a named component.
func TestFillPropertyAnnotations_RefSiblingExampleBindsTheSite(t *testing.T) {
	t.Parallel()
	lowerer, _ := schema.LoweredFor(t, `openapi: 3.1.0
info: {title: t, version: "1"}
paths: {}
components:
  schemas:
    Target: {type: string}
    Holder:
      type: object
      properties:
        f:
          $ref: '#/components/schemas/Target'
          example: at-reference
`)
	lowerer.Diags.AppendAll(schema.LowerComponentSchemas(t.Context(), lowerer.Ctx, lowerer.Types, lowerer.Schemas()))

	target := typeByName(lowerer.Document(t), "Target")
	require.NotNil(t, target)
	assert.Empty(t, target.Common().Examples,
		"an example beside a $ref must not attach to the referent")

	holder, ok := typeByName(lowerer.Document(t), "Holder").(*ir.Model)
	require.True(t, ok, "Holder must own a Model node")
	property, ok := openapitest.PropertiesByWire(holder.Properties)["f"]
	require.True(t, ok, "property f must be present")
	require.Len(t, property.Examples, 1, "the example beside the $ref must bind the property")
	require.NotNil(t, property.Examples[0].Value)
	assert.Equal(t, ir.Value{Kind: ir.ValueString, String: "at-reference"}, *property.Examples[0].Value)
}

// TestLoweredToOwnNode_ReadsWhatThePositionLoweredTo holds the reading to what a
// position lowered to, decided before anything is built: a reference naming the
// ID the position's pointer names is its own node whether or not that node is
// interned yet, and one naming any other ID is not, whatever the registry holds
// at the pointer (GitHub #1991).
func TestLoweredToOwnNode_ReadsWhatThePositionLoweredTo(t *testing.T) {
	t.Parallel()
	const src = "    A:\n      type: object\n      properties:\n        p: {type: string, format: byte}\n"
	const at = jsontext.Pointer("/components/schemas/A/properties/p")
	own := ir.TypeID("type/anonymous/components/schemas/A/properties/p")
	tests := []struct {
		name   string
		built  bool
		target ir.TypeID
		want   bool
	}{
		{"its own node, nothing built yet", false, own, true},
		{"its own node, built", true, own, true},
		{"a shared primitive, nothing built", false, "type/primitive/string", false},
		{"a shared primitive, its node built there", true, "type/primitive/string", false},
		{"another position's node", true, componentID("A"), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			lowerer, _ := schema.LoweredFor(t, openapitest.ComponentSpec(src))
			if tt.built {
				lowerer.Diags.AppendAll(schema.LowerComponentSchemas(t.Context(), lowerer.Ctx, lowerer.Types, lowerer.Schemas()))
				_, interned := lowerer.Types.Lookup(lowerer.Ctx.IDs().Coordinate(at))
				require.True(t, interned, "the format hoists a Scalar at the property")
			}
			assert.Equal(t, tt.want, schema.LoweredToOwnNode(lowerer.Ctx, at, ir.TypeRef{Target: tt.target}))
		})
	}
}
