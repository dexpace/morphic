package schema_test

import (
	"encoding/json/jsontext"
	"slices"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/annotation"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/compilers/openapi/internal/schema"
	"github.com/dexpace/morphic/ir"
)

func TestLower_ValidationOnlyKeywords(t *testing.T) {
	t.Parallel()
	spec := openapitest.ComponentSpec(`    V:
      type: object
      properties: {a: {type: string}}
      if: {required: [a]}
      then: {required: [b]}
      else: {required: [c]}
      dependentSchemas: {a: {required: [d]}}
      propertyNames: {pattern: "^[a-z]+$"}
      contains: {type: string}
      minContains: 1
      unevaluatedProperties: {type: string}
      unevaluatedItems: {type: integer}
`)
	doc, diags := lowerSpec(t, spec)
	openapitest.RequireNoErrorDiags(t, diags)
	m := typeByName(doc, "V").(*ir.Model)
	// An entry the source writes as one keyword is located at that keyword; the
	// three that synthesize several into one object fall back to the schema,
	// since no single node addresses the synthesized value.
	wantPointer := map[string]string{
		"openapi:dependentSchemas": "/components/schemas/V/dependentSchemas",
		"openapi:propertyNames":    "/components/schemas/V/propertyNames",
		"openapi:if-then-else":     "/components/schemas/V",
		"openapi:contains":         "/components/schemas/V",
		"openapi:unevaluated":      "/components/schemas/V",
	}
	for key, want := range wantPointer {
		entry, ok := m.Unmodeled[key]
		require.True(t, ok, "keyword %s preserved", key)
		assert.Equal(t, jsontext.Pointer(want), entry.Provenance.Pointer, "entry provenance for %s", key)
	}
	assert.GreaterOrEqual(t, openapitest.CountDiagsAt(diags, diag.ValidationOnlyKeyword, ir.SeverityInfo), 5)
}

// TestLower_PropertyNamesPreserved pins the whole §4.7 contract for
// propertyNames end to end: the verbatim payload, the reason a validation
// emitter selects on, and the one info diagnostic naming the keyword (#117).
func TestLower_PropertyNamesPreserved(t *testing.T) {
	t.Parallel()
	spec := openapitest.ComponentSpec(`    Codes:
      type: object
      propertyNames: {type: string, pattern: "^[a-z]+$"}
      additionalProperties: {type: integer}
`)
	doc, diags := lowerSpec(t, spec)
	openapitest.RequireNoErrorDiags(t, diags)

	mt, ok := typeByName(doc, "Codes").(*ir.MapT)
	require.True(t, ok, "a map and nothing else is a MapT; got %T", typeByName(doc, "Codes"))
	entry, ok := mt.Unmodeled["openapi:propertyNames"]
	require.True(t, ok, "propertyNames kept verbatim; got %v", mt.Unmodeled)
	assert.JSONEq(t, `{"type":"string","pattern":"^[a-z]+$"}`, string(entry.Value))
	assert.Equal(t, ir.ReasonValidationOnly, entry.Reason)
	assert.Equal(t, 1, openapitest.CountDiagsAt(diags, diag.ValidationOnlyKeyword, ir.SeverityInfo))

	// The keyword constrains keys only: the map's value lowering is untouched.
	assert.Equal(t, ir.TypeID("t/prim/integer"), mt.Value.Target)
	assert.Nil(t, mt.Key, "a key *constraint* is not a key type")
}

func TestModel_ValidationOnlyKeywordPreserved(t *testing.T) {
	t.Parallel()
	spec := openapitest.ComponentSpec(`    S:
      type: object
      properties: {a: {type: string}}
      not: {required: [b]}
`)
	doc, diags := lowerSpec(t, spec)
	m := doc.Types[componentID("S")].(*ir.Model)
	raw, ok := m.Unmodeled["openapi:not"]
	require.True(t, ok, "not-keyword must be preserved verbatim")
	assert.JSONEq(t, `{"required":["b"]}`, string(raw.Value))
	assert.Equal(t, ir.ReasonValidationOnly, raw.Reason)
	assert.Equal(t, jsontext.Pointer("/components/schemas/S/not"), raw.Provenance.Pointer,
		"the entry locates the keyword, not the schema that carried it")
	found := false
	for _, d := range diags {
		if d.Code == diag.ValidationOnlyKeyword {
			found = true
			assert.Equal(t, ir.SeverityInfo, d.Severity)
			assert.Equal(t, jsontext.Pointer("/components/schemas/S"), d.Provenance.Pointer,
				"the diagnostic still reports against the declaring schema")
		}
	}
	assert.True(t, found, "expected a validation-only-keyword info diagnostic")
}

func TestModel_SchemaExtensionPreserved(t *testing.T) {
	t.Parallel()
	spec := openapitest.ComponentSpec(`    S:
      type: object
      x-rate-limit: 100
      properties: {a: {type: string}}
`)
	doc, diags := lowerSpec(t, spec)
	openapitest.RequireNoErrorDiags(t, diags)
	m := doc.Types[componentID("S")].(*ir.Model)
	raw, ok := m.Unmodeled["openapi:x-rate-limit"]
	require.True(t, ok)
	assert.JSONEq(t, "100", string(raw.Value))
	assert.Equal(t, ir.ReasonVendorExtension, raw.Reason)
	assert.Equal(t, jsontext.Pointer("/components/schemas/S/x-rate-limit"), raw.Provenance.Pointer,
		"an entry locates the construct itself, not the node that carries it")
}

func TestModel_TitleDescriptionDocs(t *testing.T) {
	t.Parallel()
	spec := openapitest.ComponentSpec(`    S:
      type: object
      title: "My Title"
      description: "My Desc"
      properties: {a: {type: string}}
`)
	doc, diags := lowerSpec(t, spec)
	openapitest.RequireNoErrorDiags(t, diags)
	m := doc.Types[componentID("S")].(*ir.Model)
	assert.Equal(t, "My Title", m.Docs.Summary)
	assert.Equal(t, "My Desc", m.Docs.Description)
}

func TestSchema_UnserializableExtension(t *testing.T) {
	t.Parallel()
	spec := openapitest.ComponentSpec(`    S:
      type: object
      properties: {a: {type: string}}
      x-bad: {n: .nan}
`)
	doc, diags := lowerSpec(t, spec)
	require.NotNil(t, doc)
	assert.True(t, openapitest.HasDiagAt(diags, diag.DegradedConstruct, ir.SeverityWarning), "unserializable extension warns")
	m := typeByName(doc, "S").(*ir.Model)
	_, hasBad := m.Unmodeled["openapi:x-bad"]
	assert.False(t, hasBad, "unserializable extension is dropped, not stored")
}

// TestInlinePosition_DeclarationOwnsANode asserts that a schema declaring
// position-scoped annotations at an inline position keeps every one of them, at
// each position that has no carrier to hold them (GitHub #116). Before the fix
// each of these lowered straight to the shared string primitive and dropped
// them all, with no diagnostic.
func TestInlinePosition_DeclarationOwnsANode(t *testing.T) {
	t.Parallel()
	for _, pos := range inlinePositions() {
		t.Run(pos.name, func(t *testing.T) {
			t.Parallel()
			doc, diags := parseFull(t, pos.spec(openapitest.InlineProbeBody))
			openapitest.RequireNoErrorDiags(t, diags)

			sc, ok := doc.Types[pos.id].(*ir.Scalar)
			require.True(t, ok, "the declaration owns a Scalar at its own pointer; got %v", doc.Types[pos.id])
			require.NotNil(t, sc.Base)
			assert.Equal(t, pos.target, sc.Base.Target, "the alias still names the shared primitive it reduced to")
			assertProbeAnnotationsKept(t, sc)
		})
	}
}

// TestInlinePosition_BareScalarStaysShared is the control that bounds the fix:
// a position declaring nothing of its own gains nothing by owning a node, so it
// must keep resolving straight to the shared primitive rather than growing an
// anonymous alias per element position.
func TestInlinePosition_BareScalarStaysShared(t *testing.T) {
	t.Parallel()
	for _, pos := range inlinePositions() {
		t.Run(pos.name, func(t *testing.T) {
			t.Parallel()
			doc, diags := parseFull(t, pos.spec("{type: string}"))
			openapitest.RequireNoErrorDiags(t, diags)
			assert.NotContains(t, doc.Types, pos.id, "a bare declaration must not hoist a node")
			assert.Contains(t, doc.Types, pos.target, "it resolves to the shared primitive instead")
		})
	}
}

// TestInlinePosition_NullStrippedScalarKeepsAnnotations covers the shape that
// reaches the shared primitive by a second route: effectiveTypes strips the
// "null" member before dispatch, so `type: [string, "null"]` lands on the same
// shared node a bare string does. Nullability still lifts onto the refs.
func TestInlinePosition_NullStrippedScalarKeepsAnnotations(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, openapitest.ComponentSpec(
		"    A: {type: array, items: {type: [string, \"null\"], description: DOC}}\n"))
	openapitest.RequireNoErrorDiags(t, diags)

	sc, ok := doc.Types["t/anon/components/schemas/A/items"].(*ir.Scalar)
	require.True(t, ok, "a null-stripped scalar declaration owns a node like any other")
	assert.Equal(t, "DOC", sc.Docs.Description)
	require.NotNil(t, sc.Base)
	assert.True(t, sc.Base.Nullable, "the alias records that its base admits null")

	list, ok := doc.Types[componentID("A")].(*ir.List)
	require.True(t, ok)
	assert.True(t, list.Elem.Nullable, "and the element position still reads as nullable")
}

// TestInlinePosition_RefSiblingsKeepTheirPosition covers a $ref written at an
// inline position with keywords beside it: those bind the position, not the
// referent, so they cannot go on the shared target and the position must hold
// them itself.
func TestInlinePosition_RefSiblingsKeepTheirPosition(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, openapitest.ComponentSpec(
		"    S: {type: string}\n"+
			"    A: {type: array, items: {$ref: '#/components/schemas/S', maxLength: 25, description: D}}\n"))
	openapitest.RequireNoErrorDiags(t, diags)

	sc, ok := doc.Types["t/anon/components/schemas/A/items"].(*ir.Scalar)
	require.True(t, ok, "siblings beside a $ref give the position a node of its own")
	require.NotNil(t, sc.Base)
	assert.Equal(t, componentID("S"), sc.Base.Target, "the alias points at what it referenced")
	assert.Equal(t, "D", sc.Docs.Description)
	require.NotNil(t, sc.Constraints)
	require.NotNil(t, sc.Constraints.MaxLength)
	assert.Equal(t, int64(25), *sc.Constraints.MaxLength)

	target, ok := doc.Types[componentID("S")].(*ir.Scalar)
	require.True(t, ok)
	assert.Nil(t, target.Constraints, "the referent stays unbounded")
	assert.Empty(t, target.Docs.Description, "and undocumented")
}

// TestInlinePosition_BareRefStaysDirect is the control for the case above: a
// $ref with nothing beside it still points straight at its target.
func TestInlinePosition_BareRefStaysDirect(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, openapitest.ComponentSpec(
		"    S: {type: string}\n"+
			"    A: {type: array, items: {$ref: '#/components/schemas/S'}}\n"))
	openapitest.RequireNoErrorDiags(t, diags)

	list, ok := doc.Types[componentID("A")].(*ir.List)
	require.True(t, ok)
	assert.Equal(t, componentID("S"), list.Elem.Target, "a bare $ref needs no alias")
	assert.NotContains(t, doc.Types, ir.TypeID("t/anon/components/schemas/A/items"))
}

// TestInlinePosition_ReachedAgainWhileBuildingIsLeftToItsBuilder is GitHub
// #749: written reference first, a $ref reaching back above a node still being
// built once found it unbuilt and attached nothing. A reference now only
// promises its target, so no walk revisits a node mid-build, and the guards for
// one are pinned by the LeavesANodeStillBeingBuiltToItsBuilder tests.
//
// Each row is written reference first and must match the declaration-first
// order: the node keeps the description, unhomed format and, at a HomeOwnNode
// position, readOnly it declares.
func TestInlinePosition_ReachedAgainWhileBuildingIsLeftToItsBuilder(t *testing.T) {
	t.Parallel()
	const (
		property = "/components/schemas/X/properties/p/properties/d"
		items    = "/components/schemas/X/properties/p/items"
		response = "/paths/~1b/get/responses/200/content/application~1json/schema/items"
	)
	ref := func(pointer string) string { return "    Y: {$ref: '#" + pointer + "'}\n" }
	mapping := func(pointer string) string {
		return `    Pet:
      type: object
      required: [kind]
      properties: {kind: {type: string}}
      discriminator:
        propertyName: kind
        mapping: {d: '#` + pointer + "'}\n"
	}
	ownerProperty := `    X:
      type: object
      properties:
        p:
          type: object
          properties:
            d:
              type: object
              description: kept
              format: wat
              properties:
                back: {$ref: '#/components/schemas/X/properties/p'}
`
	ownerItems := `    X:
      type: object
      properties:
        p:
          type: array
          items:
            type: object
            readOnly: true
            description: kept
            format: wat
            properties:
              back: {$ref: '#/components/schemas/X/properties/p'}
`
	ownerSubtype := `    X:
      type: object
      properties:
        p:
          type: object
          properties:
            d:
              allOf: [{$ref: '#/components/schemas/Pet'}]
              type: object
              description: kept
              format: wat
              properties:
                back: {$ref: '#/components/schemas/X/properties/p'}
`
	pathRef := `  /a:
    get:
      operationId: getA
      responses:
        "200":
          description: ok
          content:
            application/json:
              schema: {$ref: '#` + response + `'}
`
	pathOwner := `  /b:
    get:
      operationId: getB
      responses:
        "200":
          description: ok
          content:
            application/json:
              schema:
                type: array
                items:
                  type: object
                  description: kept
                  format: wat
                  properties:
                    up: {$ref: '#/paths/~1b/get/responses/200/content/application~1json/schema'}
`
	cases := []struct {
		name             string
		spec             func(string) string
		reference, owner string
		revisited        string
		wantUnmodeled    []string
	}{
		{"a property, through a $ref", openapitest.ComponentSpec, ref(property), ownerProperty,
			property, []string{"openapi:format"}},
		{"items, through a $ref", openapitest.ComponentSpec, ref(items), ownerItems,
			items, []string{"openapi:format", "openapi:readOnly"}},
		{"a property, through a discriminator mapping", openapitest.ComponentSpec, mapping(property), ownerSubtype,
			property, []string{"openapi:format"}},
		{"items beneath /paths", openapitest.PathsSpec, pathRef, pathOwner,
			response, []string{"openapi:format"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			refFirst, diags := parseFull(t, tc.spec(tc.reference+tc.owner))
			openapitest.RequireNoErrorDiags(t, diags)
			declFirst, diags := parseFull(t, tc.spec(tc.owner+tc.reference))
			openapitest.RequireNoErrorDiags(t, diags)

			id := ir.TypeID("t/anon" + tc.revisited)
			for _, doc := range []*ir.Document{refFirst, declFirst} {
				td, ok := doc.Types[id]
				require.True(t, ok, "the revisited position is interned")
				assert.Equal(t, "kept", td.Common().Docs.Description, "its declaration's annotations reach it")
				for _, key := range tc.wantUnmodeled {
					assert.Contains(t, td.Common().Unmodeled, key, "and what it declares with no home is kept")
				}
			}
			assert.Empty(t, cmp.Diff(refFirst.Types, declFirst.Types),
				"the registry is the same whichever lowering reaches the position first")
		})
	}
}

// TestInlinePosition_OutsideRefDoesNotMoveTheHome pins that a $ref naming an
// inline position does not change what the component lowers to. Such a $ref
// names the position's home ahead of its declaration, and the position
// must not then take the shared node its body reduced to, which would leave the
// annotations on a node only the outside reference sees and resolve `items` to
// the bare primitive: a declaration-order dependence ir-design §4.3 rules out.
//
// The first assertion is the strongest statement of the rule: the lowering
// cannot depend on whether an unrelated schema points at an inner pointer.
func TestInlinePosition_OutsideRefDoesNotMoveTheHome(t *testing.T) {
	t.Parallel()
	for _, pos := range stolenPositions() {
		t.Run(pos.name, func(t *testing.T) {
			t.Parallel()
			alone, diags := parseFull(t, openapitest.ComponentSpec(pos.owner))
			openapitest.RequireNoErrorDiags(t, diags)
			refFirst, diags := parseFull(t, pos.spec(true))
			openapitest.RequireNoErrorDiags(t, diags)
			refLast, diags := parseFull(t, pos.spec(false))
			openapitest.RequireNoErrorDiags(t, diags)

			for _, with := range []*ir.Document{refFirst, refLast} {
				assert.Empty(t, cmp.Diff(alone.Types[componentID("A")], with.Types[componentID("A")]),
					"an outside reference to an inner pointer must not change what A lowers to")
			}
			assert.Empty(t, cmp.Diff(refFirst, refLast, orderInvariantIR()...),
				"declaring that reference before or after A must not change the IR")

			sc, ok := refFirst.Types[pos.id].(*ir.Scalar)
			require.True(t, ok, "the position still owns its home; got %v", refFirst.Types[pos.id])
			assertProbeAnnotationsKept(t, sc)
		})
	}
}

// TestInlinePosition_HoistGateFollowsWhatIsKept walks every keyword that only a
// node of its own can hold, one at a time, and requires each to give the
// position that node. The gate and the three things that fill it —
// attachDeclaredAnnotations, internAlias and recordDeclarationResidue — have to
// agree keyword for
// keyword: one listed here but stored nowhere hoists an empty node, and one
// stored but missing here is still dropped.
func TestInlinePosition_HoistGateFollowsWhatIsKept(t *testing.T) {
	t.Parallel()
	cases := []struct{ name, keyword string }{
		{"title", "title: T"},
		{"externalDocs", "externalDocs: {url: 'https://e.example'}"},
		{"deprecated", "deprecated: true"},
		{"xml", "xml: {name: X}"},
		{"extension", "x-vendor: V"},
		{"example", "example: a"},
		{"examples", "examples: [a]"},
		{"minimum", "minimum: 1"},
		{"maximum", "maximum: 9"},
		{"multipleOf", "multipleOf: 2"},
		{"exclusiveMinimum", "exclusiveMinimum: 1"},
		{"exclusiveMaximum", "exclusiveMaximum: 9"},
		{"minLength", "minLength: 1"},
		{"maxLength", "maxLength: 9"},
		{"pattern", "pattern: '^a'"},
		{"minProperties", "minProperties: 1"},
		{"maxProperties", "maxProperties: 9"},
		{"not", "not: {const: N}"},
		{"contains", "contains: {type: string}"},
		{"minContains", "minContains: 1"},
		{"maxContains", "maxContains: 9"},
		{"if", "if: {const: a}"},
		{"then", "then: {const: a}"},
		{"else", "else: {const: a}"},
		{"dependentSchemas", "dependentSchemas: {a: {type: string}}"},
		{"propertyNames", "propertyNames: {maxLength: 4}"},
		{"unevaluatedItems", "unevaluatedItems: {type: string}"},
		{"unevaluatedProperties", "unevaluatedProperties: {type: string}"},
		{"default", "default: a"},
		{"readOnly", "readOnly: true"},
		{"writeOnly", "writeOnly: true"},
		{"dependentRequired", "dependentRequired: {a: [b]}"},
		{"contentEncoding", "contentEncoding: base64"},
		{"contentMediaType", "contentMediaType: image/png"},
		{"contentSchema", "contentSchema: {type: object}"},
		{"$dynamicRef", "$dynamicRef: '#nosuch'"},
		{"$id", "$id: 'urn:example:i'"},
		{"$schema", "$schema: 'https://json-schema.org/draft/2020-12/schema'"},
		{"$vocabulary", "$vocabulary: {'https://json-schema.org/draft/2020-12/vocab/core': true}"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			doc, diags := parseFull(t, openapitest.ComponentSpec(
				"    A: {type: array, items: {type: string, "+tc.keyword+"}}\n"))
			openapitest.RequireNoErrorDiags(t, diags)
			assert.Contains(t, doc.Types, ir.TypeID("t/anon/components/schemas/A/items"),
				"%s binds the position it is written at, so the position must own a node", tc.keyword)
		})
	}
}

// TestInlinePosition_NothingToHoldHoistsNoNode is the negative half of that
// gate: a schema that declares nothing position-scoped, and one whose only
// extra keyword is a `false` unevaluatedProperties — model openness rather than
// a preserved keyword. Hoisting for either would produce a node holding nothing.
func TestInlinePosition_NothingToHoldHoistsNoNode(t *testing.T) {
	t.Parallel()
	cases := []struct{ name, keyword string }{
		{"none", "type: string"},
		{"unevaluatedProperties-false", "type: string, unevaluatedProperties: false"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			doc, diags := parseFull(t, openapitest.ComponentSpec(
				"    A: {type: array, items: {"+tc.keyword+"}}\n"))
			openapitest.RequireNoErrorDiags(t, diags)
			assert.NotContains(t, doc.Types, ir.TypeID("t/anon/components/schemas/A/items"))
			list, ok := doc.Types[componentID("A")].(*ir.List)
			require.True(t, ok)
			assert.Equal(t, ir.TypeID("t/prim/string"), list.Elem.Target)
		})
	}
}

// TestInlinePosition_ResidueIsKeptAtEveryHomeOwnNodePosition pins the other half
// of the residue rule: default/readOnly/writeOnly bind a *use* of a type, and
// the position that writes one is usually not a property — so every position
// that owns its node keeps them, not just a named component declaration
// (GitHub #138).
func TestInlinePosition_ResidueIsKeptAtEveryHomeOwnNodePosition(t *testing.T) {
	t.Parallel()
	cases := []struct{ name, body, at string }{
		{"items", "    A: {type: array, items: {type: string, RESIDUE}}\n",
			"t/anon/components/schemas/A/items"},
		{"additionalProperties", "    A: {type: object, additionalProperties: {type: string, RESIDUE}}\n",
			"t/anon/components/schemas/A/additionalProperties"},
		{"patternProperties", "    A: {type: object, patternProperties: {'^a': {type: string, RESIDUE}}}\n",
			"t/anon/components/schemas/A/patternProperties/^a"},
		{"prefixItems", "    A: {type: array, prefixItems: [{type: string, RESIDUE}]}\n",
			"t/anon/components/schemas/A/prefixItems/0"},
		{"oneOf branch", "    A: {oneOf: [{type: string, RESIDUE}, {type: integer}]}\n",
			"t/anon/components/schemas/A/oneOf/0"},
		{"ref sibling", "    B: {type: string}\n" +
			"    A: {type: array, items: {$ref: '#/components/schemas/B', RESIDUE}}\n",
			"t/anon/components/schemas/A/items"},
	}
	const residue = "default: a, readOnly: true"
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			doc, diags := parseFull(t, openapitest.ComponentSpec(strings.ReplaceAll(tc.body, "RESIDUE", residue)))
			openapitest.RequireNoErrorDiags(t, diags)

			td, ok := doc.Types[ir.TypeID(tc.at)]
			require.True(t, ok, "the position owns a node to hold what it wrote")
			assertResidueKeptAndAnnounced(t, td.Common().Unmodeled, diags, "default", `"a"`)
			assertResidueKeptAndAnnounced(t, td.Common().Unmodeled, diags, "readOnly", `true`)
		})
	}
}

// TestPropertyPosition_ResidueStaysOutOfTheTypeNode is the deliberate non-goal.
// A property lands all three in their real fields, so recording them again as
// residue would restate what the carrier already holds.
func TestPropertyPosition_ResidueStaysOutOfTheTypeNode(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, openapitest.ComponentSpec(
		"    A: {type: object, properties: {p: {type: array, items: {type: string}, "+
			"default: [], readOnly: true}}}\n"))
	openapitest.RequireNoErrorDiags(t, diags)

	p := propertyOf(t, doc, "A", "p")
	require.NotNil(t, p.Default, "default lands in the property's own field")
	assert.Equal(t, ir.Visibility{Only: []ir.Lifecycle{
		ir.LifecycleRead, ir.LifecycleDelete, ir.LifecycleQuery,
	}}, p.Visibility, "readOnly lands in the property's own field")

	td, ok := doc.Types[p.Type.Target]
	require.True(t, ok)
	assert.NotContains(t, td.Common().Unmodeled, "openapi:default",
		"the carrier holds it, so the node must not restate it")
	assert.NotContains(t, td.Common().Unmodeled, "openapi:readOnly",
		"the carrier holds it, so the node must not restate it")
	assert.Empty(t, p.Unmodeled, "nor does the property preserve what it modelled")
}

// TestDialectKeywords_KeptOutOfScope pins the exclusion the rule
// docs/compilers/openapi.md#dialect-keywords records: the IR has no resource or
// dialect axis, so these are kept verbatim under the reason that says no IR
// node is coming for them — never dropped in silence, and never read as a base
// URI for reference resolution.
//
// The keyword list is written out here rather than read from
// annotation.DialectKeywords: ranging over the production slice would let a
// keyword dropped from it narrow this test instead of failing it, so the two
// are compared as sets and each is then exercised.
func TestDialectKeywords_KeptOutOfScope(t *testing.T) {
	t.Parallel()
	want := []string{"$id", "$schema", "$vocabulary"}
	require.ElementsMatch(t, want, annotation.DialectKeywords,
		"a keyword joining or leaving the exclusion must be decided here too")

	doc, diags := parseFull(t, openapitest.ComponentSpec(
		"    A:\n"+
			"      $id: 'urn:example:a'\n"+
			"      $schema: 'https://json-schema.org/draft/2020-12/schema'\n"+
			"      $vocabulary: {'https://json-schema.org/draft/2020-12/vocab/core': true}\n"+
			"      $comment: not for end users\n"+
			"      type: string\n"))
	openapitest.RequireNoErrorDiags(t, diags)

	td, ok := doc.Types[componentID("A")]
	require.True(t, ok)
	for _, keyword := range want {
		entry, ok := td.Common().Unmodeled["openapi:"+keyword]
		require.True(t, ok, "%s must be kept verbatim", keyword)
		assert.Equal(t, ir.ReasonOutOfScope, entry.Reason)
		openapitest.AssertInfoDiagAt(t, diags, string(entry.Provenance.Pointer))
	}
	assert.NotContains(t, td.Common().Unmodeled, "openapi:$comment",
		"2020-12 §8.3 forbids presenting $comment, so it is dropped rather than kept")
}

// TestResidueKeywords_HandsBackACopy pins the reason this is a function and not
// the slice. One list is the whole point — a keyword added to it has to reach
// every position that preserves one — so the list itself must not be reachable
// from outside. An exported slice would let any importer rewrite what every
// schema position in the process preserves, for the life of the process.
func TestResidueKeywords_HandsBackACopy(t *testing.T) {
	t.Parallel()
	first := schema.ResidueKeywords()
	require.NotEmpty(t, first, "an empty list would make this pass vacuously")

	clobbered := slices.Clone(first)
	for i := range first {
		first[i] = "clobbered"
	}

	assert.Equal(t, clobbered, schema.ResidueKeywords(),
		"writing through one caller's slice must not change what the next one reads")
}

func inlinePositions() []inlinePosition {
	const prim = ir.TypeID("t/prim/string")
	return []inlinePosition{
		{"items", func(b string) string { return openapitest.ComponentSpec("    A: {type: array, items: " + b + "}\n") },
			"t/anon/components/schemas/A/items", prim},
		{"nested-items", func(b string) string {
			return openapitest.ComponentSpec("    A: {type: array, items: {type: array, items: " + b + "}}\n")
		}, "t/anon/components/schemas/A/items/items", prim},
		{"additionalProperties", func(b string) string {
			return openapitest.ComponentSpec("    A: {type: object, additionalProperties: " + b + "}\n")
		}, "t/anon/components/schemas/A/additionalProperties", prim},
		{"prefixItems", func(b string) string {
			return openapitest.ComponentSpec("    A: {type: array, prefixItems: [" + b + "]}\n")
		}, "t/anon/components/schemas/A/prefixItems/0", prim},
		{"patternProperties", func(b string) string {
			return openapitest.ComponentSpec("    A: {type: object, patternProperties: {\"^x\": " + b + "}}\n")
		}, "t/anon/components/schemas/A/patternProperties/^x", prim},
		{"oneOf-branch", func(b string) string {
			return openapitest.ComponentSpec("    A: {oneOf: [" + b + ", {type: integer}]}\n")
		}, "t/anon/components/schemas/A/oneOf/0", prim},
		{"anyOf-branch", func(b string) string {
			return openapitest.ComponentSpec("    A: {anyOf: [" + b + ", {type: integer}]}\n")
		}, "t/anon/components/schemas/A/anyOf/0", prim},
		{"request-media-type", func(b string) string {
			return openapitest.PathsSpec("  /x:\n    post:\n      operationId: p\n      requestBody:\n" +
				"        content: {application/json: {schema: " + b + "}}\n" +
				"      responses: {\"204\": {description: ok}}\n")
		}, "t/anon/paths/~1x/post/requestBody/content/application~1json/schema", prim},
		{"response-media-type", func(b string) string {
			return openapitest.PathsSpec("  /x:\n    get:\n      operationId: g\n      responses:\n" +
				"        \"200\":\n          description: ok\n" +
				"          content: {application/json: {schema: " + b + "}}\n")
		}, "t/anon/paths/~1x/get/responses/200/content/application~1json/schema", prim},
	}
}

func stolenPositions() []stolenPosition {
	return []stolenPosition{
		{"items", "    A: {type: array, items: " + openapitest.InlineProbeBody + "}\n",
			"t/anon/components/schemas/A/items"},
		{"additionalProperties", "    A: {type: object, additionalProperties: " + openapitest.InlineProbeBody + "}\n",
			"t/anon/components/schemas/A/additionalProperties"},
		{"prefixItems", "    A: {type: array, prefixItems: [" + openapitest.InlineProbeBody + "]}\n",
			"t/anon/components/schemas/A/prefixItems/0"},
		{"tuple rest", "    A: {type: array, prefixItems: [{type: string}], items: " + openapitest.InlineProbeBody + "}\n",
			"t/anon/components/schemas/A/items"},
		{"patternProperties", "    A: {type: object, patternProperties: {\"^x\": " + openapitest.InlineProbeBody + "}}\n",
			"t/anon/components/schemas/A/patternProperties/^x"},
		{"oneOf-branch", "    A: {oneOf: [" + openapitest.InlineProbeBody + ", {type: integer}]}\n",
			"t/anon/components/schemas/A/oneOf/0"},
	}
}

// assertResidueKeptAndAnnounced checks one residue keyword's entry and its
// diagnostic together. The end-to-end tests' assertResidueKept and
// assertResidueDiag (compilers/openapi/e2e) do the same job in two calls; this
// package cannot import them, so the pairing lives here under a name that is
// not a transposition of either.
func assertResidueKeptAndAnnounced(t *testing.T, p ir.Unmodeled, diags []ir.Diagnostic, keyword, wantJSON string) {
	t.Helper()
	entry, ok := p["openapi:"+keyword]
	require.True(t, ok, "%s must be kept verbatim under Unmodeled", keyword)
	assert.JSONEq(t, wantJSON, string(entry.Value))
	assert.Equal(t, ir.ReasonNoIRHome, entry.Reason)

	for _, d := range diags {
		if d.Code == diag.DegradedConstruct && d.Severity == ir.SeverityInfo &&
			strings.HasSuffix(string(d.Provenance.Pointer), "/"+keyword) {
			return
		}
	}
	assert.Fail(t, "missing residue diagnostic", "nothing announced %s; got %+v", keyword, diags)
}

// inlinePosition is one schema position with no ir.Property or ir.Parameter to
// carry a declaration's annotations, so the declaration must own a node.
type inlinePosition struct {
	name string
	// spec writes body at the position under test.
	spec func(body string) string
	// id is the node the position's declaration owns once it declares anything.
	id ir.TypeID
	// target is what a bare {type: string} at the position resolves to instead.
	target ir.TypeID
}
