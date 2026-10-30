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
	m, ok := doc.Types[componentID("S")].(*ir.Model)
	require.True(t, ok)
	require.Len(t, m.Properties, 1)
	require.NotNil(t, m.Properties[0].Default)
	assert.Equal(t, ir.Value{Kind: ir.ValueString, Str: "2021-01-01"}, *m.Properties[0].Default)
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
	m := typeByName(doc, "D").(*ir.Model)
	assert.NotEmpty(t, m.Docs.ExternalDocs)
	byWire := openapitest.PropsByWire(m.Properties)
	require.NotNil(t, byWire["withXml"].XML)
	assert.Equal(t, "attribute", byWire["withXml"].XML.NodeType)
	assert.Equal(t, "urn:x", byWire["withXml"].XML.Namespace)
	assert.True(t, byWire["withXml"].XML.Wrapped)
	assert.Len(t, byWire["withExample"].Examples, 3)
	assert.NotEmpty(t, byWire["withExt"].Unmodeled)
	var sawDefaultWarn bool
	for _, d := range diags {
		if d.Severity == ir.SeverityWarning && strings.Contains(d.Message, "default:") {
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
	m := typeByName(doc, "Owner").(*ir.Model)
	assert.Equal(t, "target-desc", m.Properties[0].Docs.Description)
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
	m := doc.Types[componentID("S")].(*ir.Model)
	require.NotNil(t, m.Properties[0].Default)
	assert.Equal(t, ir.ValueNumber, m.Properties[0].Default.Kind)
	assert.Equal(t, ir.BigVal("9007199254740993"), m.Properties[0].Default.Num)
}

func TestFillPropertyDetail_UnconvertibleExampleDiagnosed(t *testing.T) {
	t.Parallel()
	// A custom tag is structurally unconvertible; the example must be skipped
	// (an example is an annotation, not a structural hole) but never silently —
	// the conversion error was previously discarded on the floor.
	spec := openapitest.ComponentSpec("    S:\n      type: object\n      properties:\n        n:\n          type: string\n          example: !foo bar\n")
	doc, diags := schema.LowerSpec(t, spec)
	m, ok := doc.Types[componentID("S")].(*ir.Model)
	require.True(t, ok)
	assert.Empty(t, m.Properties[0].Examples, "the unconvertible example is skipped, not appended")
	require.Equal(t, 1, openapitest.CountDiagsAt(diags, diag.DegradedConstruct, ir.SeverityWarning))
	d, ok := openapitest.FirstDegradedWarning(diags)
	require.True(t, ok)
	assert.Equal(t, jsontext.Pointer("/components/schemas/S/properties/n/example"), d.Provenance.Pointer)
	assert.Contains(t, d.Message, "example:")
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
	m := doc.Types[componentID("S")].(*ir.Model)
	byName := openapitest.PropsByWire(m.Properties)
	assert.Equal(t, ir.Visibility{Only: []ir.Lifecycle{ir.LifecycleRead, ir.LifecycleDelete, ir.LifecycleQuery}}, byName["r"].Visibility)
	assert.Equal(t, ir.Visibility{Only: []ir.Lifecycle{ir.LifecycleCreate, ir.LifecycleUpdate}}, byName["w"].Visibility)
}

// TestModel_ReadOnlyAndWriteOnlyTogetherAreVisibleNowhere pins the answer for a
// property whose schema writes both flags: they admit disjoint lifecycle sets,
// so the property is admitted by none. Declaring both used to lower exactly as
// declaring readOnly alone did, in silence, while the same pairing spread over
// two allOf branches already intersected to None and warned (GitHub #276).
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
	m := doc.Types[componentID("S")].(*ir.Model)
	byName := openapitest.PropsByWire(m.Properties)

	assert.Equal(t, ir.Visibility{None: true}, byName["both"].Visibility)
	assert.NotEqual(t, byName["r"].Visibility, byName["both"].Visibility,
		"declaring both flags must not lower exactly as declaring readOnly alone does")
	msg := openapitest.DiagMessageAt(t, diags, diag.DisjointVisibility, ir.SeverityWarning,
		"/components/schemas/S/properties/both")
	assert.Contains(t, msg, "writeOnly", "the report names the keyword that was being dropped")
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
	m := doc.Types[componentID("S")].(*ir.Model)
	assert.True(t, m.Properties[0].Secret)
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
	m := doc.Types[componentID("S")].(*ir.Model)
	assert.NotNil(t, m.Properties[0].Deprecation)
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
	m := doc.Types[componentID("S")].(*ir.Model)
	require.NotNil(t, m.Properties[0].XML)
	assert.Equal(t, "n", m.Properties[0].XML.Name)
	assert.Equal(t, "attribute", m.Properties[0].XML.NodeType)
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
	m := doc.Types[componentID("S")].(*ir.Model)
	assert.Equal(t, "sibling desc", m.Properties[0].Docs.Description)
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
	m := typeByName(doc, "Owner").(*ir.Model)
	assert.NotEmpty(t, m.Properties[0].Visibility.Only, "readOnly from the ref target applies")
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

			m, ok := doc.Types[componentID("A")].(*ir.Model)
			require.True(t, ok)
			p, ok := openapitest.PropsByWire(m.Properties)["p"]
			require.True(t, ok)
			assert.Equal(t, ir.TypeID("t/prim/string"), p.Type.Target,
				"the property's own type is unchanged by the outside reference")
			openapitest.AssertProbeDocsKept(t, p.Docs)
			assert.Contains(t, p.Unmodeled, "openapi:x-vendor")
			assert.Contains(t, p.Unmodeled, "openapi:not")

			sc, ok := doc.Types["t/anon/components/schemas/A/properties/p"].(*ir.Scalar)
			require.True(t, ok, "and the referenced pointer still names the schema written there")
			assertProbeAnnotationsKept(t, sc)
		})
	}
}

// TestPropertyAnnotations_OneHomeWhenSchemaOwnsANode asserts the settlement of
// the double storage: a property whose schema hoists a node keeps that
// declaration's annotations there and not also on the ir.Property, so the two
// copies can never drift apart.
func TestPropertyAnnotations_OneHomeWhenSchemaOwnsANode(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, openapitest.ComponentSpec(
		"    A:\n      type: object\n      properties:\n"+
			"        p: {type: object, description: DOC, deprecated: true, xml: {name: X}, x-vendor: V}\n"))
	openapitest.RequireNoErrorDiags(t, diags)

	owner, ok := doc.Types[componentID("A")].(*ir.Model)
	require.True(t, ok)
	p, ok := openapitest.PropsByWire(owner.Properties)["p"]
	require.True(t, ok)
	assert.Empty(t, p.Docs.Description, "the node the schema owns is the one home")
	assert.Nil(t, p.Deprecation)
	assert.Nil(t, p.XML)
	assert.Empty(t, p.Unmodeled)

	inner := doc.Types["t/anon/components/schemas/A/properties/p"]
	require.NotNil(t, inner)
	c := inner.Common()
	assert.Equal(t, "DOC", c.Docs.Description)
	assert.NotNil(t, c.Deprecation)
	require.NotNil(t, c.XML)
	assert.Equal(t, "X", c.XML.Name)
	assert.Contains(t, c.Unmodeled, "openapi:x-vendor")
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
	p, ok := openapitest.PropsByWire(owner.Properties)["p"]
	require.True(t, ok)
	assert.Equal(t, ir.TypeID("t/prim/string"), p.Type.Target, "a property keeps resolving to the shared primitive")
	assert.NotContains(t, doc.Types, ir.TypeID("t/anon/components/schemas/A/properties/p"))
	openapitest.AssertProbeDocsKept(t, p.Docs)
	assert.NotNil(t, p.Deprecation)
	openapitest.AssertProbeExample(t, p.Examples)
	require.NotNil(t, p.XML)
	assert.Equal(t, "X", p.XML.Name)
	assert.Contains(t, p.Unmodeled, "openapi:x-vendor")
	assert.Contains(t, p.Unmodeled, "openapi:not")
	require.NotNil(t, p.Constraints)
	require.NotNil(t, p.Constraints.MaxLength)
	assert.Equal(t, int64(3), *p.Constraints.MaxLength)
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

			p := propertyOf(t, doc, "Owner", "p")
			assert.Equal(t, componentID("Target"), p.Type.Target, "a bare $ref needs no alias")
			assert.Equal(t, "REF-"+kw.name, kw.read(p.Docs), "the carrier reads the referent's %s", kw.name)
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

			p := propertyOf(t, doc, "Owner", "p")
			assert.Equal(t, componentID("Target"), p.Type.Target, "a carrier hoists no alias for its siblings")
			for _, kw := range carrierDocKeywords() {
				want := "REF-" + kw.name
				if kw.name == written.name {
					want = "SITE"
				}
				assert.Equal(t, want, kw.read(p.Docs), "%s, with %s written at the site", kw.name, written.name)
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
			func(d ir.Docs) string { return d.Summary }},
		{"description",
			func(v string) string { return "description: " + v },
			func(d ir.Docs) string { return d.Description }},
		{"externalDocs",
			func(v string) string { return "externalDocs: {url: 'https://e.example', description: " + v + "}" },
			func(d ir.Docs) string {
				if len(d.ExternalDocs) != 1 {
					return ""
				}
				return d.ExternalDocs[0].Description
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
// Property f's $ref targets a named component (Target), so
// resolve.Scope.ComponentRef resolves it directly without ever calling
// hoistSubSchema. TestSchemaSiblings_RefdSubSchemaKeepsThem covers the
// analogous hoistSubSchema path, where the $ref target is an internal
// sub-schema pointer rather than a named component.
func TestFillPropertyAnnotations_RefSiblingExampleBindsTheSite(t *testing.T) {
	t.Parallel()
	l, _ := schema.LoweredFor(t, `openapi: 3.1.0
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
	l.Diags.AppendAll(schema.LowerComponentSchemas(t.Context(), l.Ctx, l.Types))

	target := typeByName(l.Document(t), "Target")
	require.NotNil(t, target)
	assert.Empty(t, target.Common().Examples,
		"an example beside a $ref must not attach to the referent")

	holder, ok := typeByName(l.Document(t), "Holder").(*ir.Model)
	require.True(t, ok, "Holder must own a Model node")
	f, ok := openapitest.PropsByWire(holder.Properties)["f"]
	require.True(t, ok, "property f must be present")
	require.Len(t, f.Examples, 1, "the example beside the $ref must bind the property")
	require.NotNil(t, f.Examples[0].Value)
	assert.Equal(t, ir.Value{Kind: ir.ValueString, Str: "at-reference"}, *f.Examples[0].Value)
}
