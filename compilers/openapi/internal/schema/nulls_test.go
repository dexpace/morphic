package schema_test

import (
	"encoding/json/jsontext"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/compilers/openapi/internal/schema"
	"github.com/dexpace/morphic/ir"
)

// inferred is a reference whose null only the nullable-widening policy admits.
func inferred(target ir.TypeID) ir.TypeRef {
	return ir.TypeRef{Target: target, Nullable: true, Inferred: lowering.NullableWideningHeuristic}
}

// prim is the shared primitive of kind k.
func prim(k ir.PrimKind) ir.TypeID { return ir.PrimTypeID(k) }

// compileNullable compiles src, a whole document, with the nullable-widening
// policy on or off.
func compileNullable(t *testing.T, src string, off bool) (*ir.Document, []ir.Diagnostic) {
	t.Helper()
	opts := compilers.Options{FormatOptions: openapi.Options{NullableWidening: openapi.NullableWidening{Disabled: off}}}
	doc, diags, err := openapi.New().Compile(t.Context(),
		[]compilers.Source{{Path: "spec.yaml", Data: []byte(src)}}, opts)
	require.NoError(t, err)
	require.NotNil(t, doc)
	return doc, diags
}

// holderP is the type of Holder's property p.
func holderP(t *testing.T, doc *ir.Document) ir.TypeRef {
	t.Helper()
	m, ok := typeByName(doc, "Holder").(*ir.Model)
	require.True(t, ok, "Holder lowers to a model")
	return openapitest.PropsByWire(m.Properties)["p"].Type
}

// diagMessage returns the message of the first diagnostic of code in diags.
func diagMessage(diags []ir.Diagnostic, code string) string {
	for _, d := range diags {
		if d.Code == code {
			return d.Message
		}
	}
	return ""
}

// TestNullableWidening_EachShapeReadsByItsVersion holds every shape of
// `nullable: true` to both readings: declared, as the document's version
// defines the keyword, which the policy turned off gives, and the policy's,
// which marks a reference where only it admits null and reports the keyword
// once, at itself, saying why. A shape both readings admit is neither marked
// nor reported.
func TestNullableWidening_EachShapeReadsByItsVersion(t *testing.T) {
	t.Parallel()
	const at = "/components/schemas/Holder/properties/p"
	anon := ir.TypeID("t/anon/components/schemas/Holder/properties/p")
	str := ir.TypeRef{Target: prim(ir.PrimString)}
	for _, tc := range []struct {
		name, version, p, rest string
		on, off                ir.TypeRef
		// report is where the policy reports the null it reads in, and why a
		// word of its message; empty where it reports none. also is a second
		// place a case writes two spellings the policy reads.
		report jsontext.Pointer
		why    string
		also   jsontext.Pointer
	}{
		{"3.1 beside a type", "3.1.0", "{type: string, nullable: true}", "",
			inferred(prim(ir.PrimString)), str, at + "/nullable", "declares OpenAPI 3.1.0", ""},
		{"3.2 beside a type", "3.2.0", "{type: string, nullable: true}", "",
			inferred(prim(ir.PrimString)), str, at + "/nullable", "declares OpenAPI 3.2.0", ""},
		{"3.1 beside a type array admitting null", "3.1.0", `{type: [string, "null"], nullable: true}`, "",
			ir.TypeRef{Target: prim(ir.PrimString), Nullable: true}, ir.TypeRef{Target: prim(ir.PrimString), Nullable: true}, "", "", ""},
		{"3.1 false", "3.1.0", "{type: string, nullable: false}", "", str, str, "", "", ""},
		{"3.0 beside a type", "3.0.3", "{type: string, nullable: true}", "",
			ir.TypeRef{Target: prim(ir.PrimString), Nullable: true}, ir.TypeRef{Target: prim(ir.PrimString), Nullable: true}, "", "", ""},
		{"3.0 with no type", "3.0.3", "{nullable: true}", "",
			inferred(prim(ir.PrimAny)), ir.TypeRef{Target: prim(ir.PrimAny)}, at + "/nullable", "writes none", ""},
		{"3.0 beside an enum listing no null", "3.0.3", "{type: string, nullable: true, enum: [a, b]}", "",
			inferred(anon), ir.TypeRef{Target: anon}, at + "/nullable", "enum lists no null", ""},
		{"3.0 beside an enum listing null", "3.0.3", "{type: string, nullable: true, enum: [a, null]}", "",
			ir.TypeRef{Target: anon, Nullable: true}, ir.TypeRef{Target: anon, Nullable: true}, "", "", ""},
		{"3.0 beside a const", "3.0.3", "{type: string, nullable: true, const: a}", "",
			inferred(anon), ir.TypeRef{Target: anon}, at + "/nullable", "const is not null", ""},
		{"3.0 beside an allOf excluding null", "3.0.3",
			"{type: object, nullable: true, allOf: [{$ref: '#/components/schemas/Pet'}]}", "",
			inferred(anon), ir.TypeRef{Target: anon}, at + "/nullable", "allOf branch", ""},
		{"3.0 beside a $ref", "3.0.3", "{$ref: '#/components/schemas/Plain', nullable: true}", "",
			inferred(componentID("Plain")), ir.TypeRef{Target: componentID("Plain")}, at + "/nullable", "beside a $ref", ""},
		{"3.1 beside a $ref", "3.1.0", "{$ref: '#/components/schemas/Plain', nullable: true}", "",
			inferred(componentID("Plain")), ir.TypeRef{Target: componentID("Plain")}, at + "/nullable", "beside a $ref", ""},
		{"3.1 on the target a $ref lands on", "3.1.0", "{$ref: '#/components/schemas/N'}",
			"    N: {type: string, nullable: true}\n",
			inferred(componentID("N")), ir.TypeRef{Target: componentID("N")}, "/components/schemas/N/nullable", "declares OpenAPI 3.1.0", ""},
		{"3.1 beside an alias's own $ref", "3.1.0", "{$ref: '#/components/schemas/Mid'}",
			"    Mid: {$ref: '#/components/schemas/Plain', nullable: true}\n",
			inferred(componentID("Mid")), ir.TypeRef{Target: componentID("Mid")}, "/components/schemas/Mid/nullable", "beside a $ref", ""},
		{"3.0 on a type an alias reaches", "3.0.3", "{$ref: '#/components/schemas/Mid'}",
			"    Mid: {$ref: '#/components/schemas/Base'}\n    Base: {type: string, nullable: true}\n",
			ir.TypeRef{Target: componentID("Mid"), Nullable: true}, ir.TypeRef{Target: componentID("Mid"), Nullable: true}, "", "", ""},
		{"3.1 a type array beside a $ref", "3.1.0", `{$ref: '#/components/schemas/Plain', type: [string, "null"]}`, "",
			inferred(componentID("Plain")), ir.TypeRef{Target: componentID("Plain")}, at, "keywords beside this $ref", ""},
		{"3.1 a type array beside an alias's own $ref", "3.1.0", "{$ref: '#/components/schemas/Mid'}",
			"    Mid: {$ref: '#/components/schemas/Plain', type: [string, \"null\"]}\n",
			inferred(componentID("Mid")), ir.TypeRef{Target: componentID("Mid")}, "/components/schemas/Mid",
			"keywords beside this $ref", ""},
		{"3.1 a type beside a $ref narrowing a target admitting null", "3.1.0",
			"{$ref: '#/components/schemas/N', type: [string]}", "    N: {type: [string, \"null\"]}\n",
			ir.TypeRef{Target: componentID("N")}, ir.TypeRef{Target: componentID("N")}, "", "", ""},
		{"3.1 a type array and a nullable beside a $ref", "3.1.0",
			`{$ref: '#/components/schemas/Plain', type: [string, "null"], nullable: true}`, "",
			inferred(componentID("Plain")), ir.TypeRef{Target: componentID("Plain")}, at, "keywords beside this $ref",
			at + "/nullable"},
		{"3.1 in an allOf branch", "3.1.0", "{allOf: [{type: string, nullable: true}]}", "",
			inferred(anon), ir.TypeRef{Target: anon}, at + "/allOf/0/nullable", "declares OpenAPI 3.1.0", ""},
		{"3.1 beside a type and an enum listing null", "3.1.0", "{type: string, nullable: true, enum: [a, null]}", "",
			inferred(anon), ir.TypeRef{Target: anon}, at + "/nullable", "declares OpenAPI 3.1.0", ""},
		{"3.1 in a union whose null branch declares it", "3.1.0",
			`{oneOf: [{$ref: '#/components/schemas/N'}, {type: "null"}]}`, "    N: {type: string, nullable: true}\n",
			ir.TypeRef{Target: componentID("N"), Nullable: true}, ir.TypeRef{Target: componentID("N"), Nullable: true},
			"/components/schemas/N/nullable", "declares OpenAPI 3.1.0", ""},
		{"3.0 beside a $ref and a type, over an untyped target", "3.0.3",
			"{$ref: '#/components/schemas/Free', type: string, nullable: true}", "    Free: {}\n",
			inferred(componentID("Free")), ir.TypeRef{Target: componentID("Free")}, at + "/nullable", "beside a $ref", ""},
		{"3.1 in an allOf branch of the target a $ref lands on", "3.1.0", "{$ref: '#/components/schemas/N'}",
			"    N: {allOf: [{type: string, nullable: true}]}\n",
			inferred(componentID("N")), ir.TypeRef{Target: componentID("N")}, "/components/schemas/N/allOf/0/nullable",
			"declares OpenAPI 3.1.0", ""},
		{"3.1 a type array beside a $ref to a target admitting null", "3.1.0",
			`{$ref: '#/components/schemas/N', type: [string, "null"]}`, "    N: {type: [string, \"null\"]}\n",
			ir.TypeRef{Target: componentID("N"), Nullable: true}, ir.TypeRef{Target: componentID("N"), Nullable: true}, "", "", ""},
		{"3.1 beside an alias's $ref, over an alias narrowing what it lands on", "3.1.0",
			"{$ref: '#/components/schemas/Outer'}",
			"    Outer: {$ref: '#/components/schemas/Inner', nullable: true}\n" +
				"    Inner: {$ref: '#/components/schemas/End', type: [string]}\n    End: {type: [string, \"null\"]}\n",
			inferred(componentID("Outer")), ir.TypeRef{Target: componentID("Outer")}, "/components/schemas/Outer/nullable",
			"beside a $ref", ""},
		{"3.1 beside an expanded $dynamicRef", "3.1.0", "{$dynamicRef: '#node', nullable: true}",
			"    Node: {$dynamicAnchor: node, type: string}\n",
			inferred(componentID("Node")), ir.TypeRef{Target: componentID("Node")}, at + "/nullable", "declares OpenAPI 3.1.0", ""},
		{"3.1 beside a union", "3.1.0", "{nullable: true, oneOf: [{type: string}, {type: integer}]}", "",
			inferred(anon), ir.TypeRef{Target: anon}, at + "/nullable", "declares OpenAPI 3.1.0", ""},
		{"3.1 beside a union and the properties it intersects with", "3.1.0",
			"{type: object, nullable: true, properties: {k: {type: string}}, " +
				"oneOf: [{$ref: '#/components/schemas/Pet'}, {type: object}]}", "",
			inferred(anon), ir.TypeRef{Target: anon}, at + "/nullable", "declares OpenAPI 3.1.0", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			src := openapitest.HolderSpec(tc.version, tc.p, tc.rest)
			for _, off := range []bool{false, true} {
				doc, diags := compileNullable(t, src, off)
				want, reports := tc.on, openapitest.PointersOf(diags, diag.InferredNullable)
				if off {
					want = tc.off
				}
				assert.Equal(t, want, holderP(t, doc), "policy off: %v", off)
				if off || tc.report == "" {
					assert.Empty(t, reports, "policy off: %v", off)
					continue
				}
				wantReports := []jsontext.Pointer{tc.report}
				if tc.also != "" {
					wantReports = append(wantReports, tc.also)
				}
				assert.Equal(t, wantReports, reports)
				assert.Contains(t, diagMessage(diags, diag.InferredNullable), tc.why)
			}
		})
	}
}

// TestNullableWidening_AnEnumsNullMemberFollowsTheReadingInForce pins the one
// lowering besides a reference's bit that asks whether a schema admits null: a
// null member lifts off an enum onto the reference only where the reading the
// policy selects admits null. On, the 3.1 schema below admits it and is an Enum
// of one member; off, its type excludes null, the member stays, and the enum
// is a union of literals as it would be with no nullable written at all.
func TestNullableWidening_AnEnumsNullMemberFollowsTheReadingInForce(t *testing.T) {
	t.Parallel()
	src := openapitest.HolderSpec("3.1.0", "{type: string, nullable: true, enum: [a, null]}", "")
	at := ir.TypeID("t/anon/components/schemas/Holder/properties/p")

	on, _ := compileNullable(t, src, false)
	enum, ok := on.Types[at].(*ir.Enum)
	require.True(t, ok, "the null member lifts onto the reference")
	require.Len(t, enum.Members, 1)
	assert.Equal(t, "a", enum.Members[0].Value.Str)

	off, _ := compileNullable(t, src, true)
	_, isUnion := off.Types[at].(*ir.Union)
	assert.True(t, isUnion, "the null member stays a member")
}

// chainSpec is a document of the version whose Holder.p reaches end through n
// aliases, each a bare $ref to the one before.
func chainSpec(version, end string, n int) string {
	var b strings.Builder
	b.WriteString("    End: " + end + "\n")
	prev := "End"
	for i := range n {
		fmt.Fprintf(&b, "    A%d: {$ref: '#/components/schemas/%s'}\n", i, prev)
		prev = fmt.Sprintf("A%d", i)
	}
	return openapitest.HolderSpec(version, "{$ref: '#/components/schemas/"+prev+"'}", b.String())
}

// TestNullableWidening_ALongChainOfAliasesReadsAsAShortOne pins how a $ref's
// chain is read: hop by hop, so each alias's keywords count, but in a loop
// the chain's own length bounds rather than from the walk's budget. Charged
// per hop, a chain longer than the budget would read as silent and drop the
// null its end admits.
func TestNullableWidening_ALongChainOfAliasesReadsAsAShortOne(t *testing.T) {
	t.Parallel()
	for _, n := range []int{1, maxChainProbe} {
		last := componentID(fmt.Sprintf("A%d", n-1))
		doc, _ := compileNullable(t, chainSpec("3.0.3", "{type: string, nullable: true}", n), true)
		assert.Equal(t, ir.TypeRef{Target: last, Nullable: true}, holderP(t, doc), "a 3.0 end through %d aliases", n)
		doc, _ = compileNullable(t, chainSpec("3.1.0", "{type: string, nullable: true}", n), false)
		assert.Equal(t, inferred(last), holderP(t, doc), "a 3.1 end through %d aliases", n)
	}
}

// maxChainProbe is a chain longer than the walk's whole budget.
const maxChainProbe = 300

// TestAllOf_NullabilityFollowsTheConjuncts pins that a composition over a
// null-admitting conjunct reads as nullable at every usage naming it, and that
// the bit is derived from the conjuncts rather than asserted. A composition
// declares no nullability of its own and Model.Base carries no Nullable bit, so
// the null would otherwise be recorded nowhere (GitHub #279).
//
// WrapPlain and WrapBoth keep the rule from being "a conjunction is nullable":
// a conjunct declaring `type: object` forbids null, and one forbidding conjunct
// decides the conjunction however many of its siblings admit it.
func TestAllOf_NullabilityFollowsTheConjuncts(t *testing.T) {
	t.Parallel()
	spec := openapitest.ComponentSpec(`    NullableName: {type: [string, "null"]}
    NullableModel:
      type: [object, "null"]
      properties: {a: {type: string}}
    PlainModel:
      type: object
      properties: {b: {type: string}}
    WrapScalar: {allOf: [{$ref: '#/components/schemas/NullableName'}]}
    WrapModel: {allOf: [{$ref: '#/components/schemas/NullableModel'}]}
    WrapPlain: {allOf: [{$ref: '#/components/schemas/PlainModel'}]}
    WrapBoth:
      allOf:
        - {$ref: '#/components/schemas/NullableModel'}
        - {$ref: '#/components/schemas/PlainModel'}
    Holder:
      type: object
      properties:
        direct: {$ref: '#/components/schemas/NullableName'}
        viaAllOf: {$ref: '#/components/schemas/WrapScalar'}
        directModel: {$ref: '#/components/schemas/NullableModel'}
        viaAllOfModel: {$ref: '#/components/schemas/WrapModel'}
        viaAllOfPlain: {$ref: '#/components/schemas/WrapPlain'}
        viaAllOfBoth: {$ref: '#/components/schemas/WrapBoth'}
`)
	doc, diags := schema.LowerSpec(t, spec)
	openapitest.RequireNoErrorDiags(t, diags)
	holder, ok := typeByName(doc, "Holder").(*ir.Model)
	require.True(t, ok, "Holder is a model")
	props := openapitest.PropsByWire(holder.Properties)
	require.Len(t, props, 6)

	assert.True(t, props["viaAllOf"].Type.Nullable, "a conjunction over a nullable scalar admits null")
	assert.Equal(t, props["direct"].Type.Nullable, props["viaAllOf"].Type.Nullable,
		"wrapping a nullable scalar in an allOf does not change what the position admits")
	assert.True(t, props["viaAllOfModel"].Type.Nullable, "a conjunction over a nullable model admits null")
	assert.Equal(t, props["directModel"].Type.Nullable, props["viaAllOfModel"].Type.Nullable,
		"a model target has no second hop to recover the null from, so the conjunction must carry it")
	assert.False(t, props["viaAllOfPlain"].Type.Nullable, "a conjunction over an object admits no null")
	assert.False(t, props["viaAllOfBoth"].Type.Nullable,
		"one conjunct forbidding null decides the conjunction")

	// The bit lives on the usage, not on the conjunct: Base and Mixins name one
	// side of a conjunction, which is the rule conjoinBranch and fillAllOf share.
	for _, name := range []string{"WrapScalar", "WrapModel", "WrapPlain"} {
		m, isModel := typeByName(doc, name).(*ir.Model)
		require.True(t, isModel, "%s is a model", name)
		require.NotNil(t, m.Base, "%s composes a sole $ref as its Base", name)
		assert.False(t, m.Base.Nullable, "%s.Base names a conjunct and carries no Nullable bit", name)
	}
}

// TestConst_FixesWhetherTheValueSpaceHoldsNull pins that `const` decides null
// admission the way a non-empty `enum` does — it is the one-member spelling of
// the same keyword.
//
// `{type: [string, "null"], const: "a"}` is the const form of the enum case that
// used to overstate: the type array puts null in the type space and the const
// takes it back out of the value space, so the position does not admit it.
func TestConst_FixesWhetherTheValueSpaceHoldsNull(t *testing.T) {
	t.Parallel()
	spec := openapitest.ComponentSpec(`    S:
      type: object
      properties:
        justNull: {const: null}
        narrowed: {type: [string, "null"], const: "a"}
`)
	doc, diags := schema.LowerSpec(t, spec)
	openapitest.RequireNoErrorDiags(t, diags)
	m, ok := typeByName(doc, "S").(*ir.Model)
	require.True(t, ok, "S is a model")
	props := openapitest.PropsByWire(m.Properties)
	require.Len(t, props, 2)

	assert.True(t, props["justNull"].Type.Nullable, "a const of null admits null")
	lit, isLit := doc.Types[props["justNull"].Type.Target].(*ir.Literal)
	require.True(t, isLit, "the const still lowers to its Literal")
	assert.Equal(t, ir.Value{Kind: ir.ValueNull}, lit.Value)

	assert.False(t, props["narrowed"].Type.Nullable,
		"a non-null const conjoins with the type array and takes the null back out")
}

func TestSchemaRef_NullableNormalization(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name, version, schema string // YAML fragment under components/schemas/S/properties/p
		wantNullable          bool
		wantTarget            ir.TypeID
	}{
		{"3.0 nullable", "3.0.3", "{type: string, nullable: true}", true, "t/prim/string"},
		{"3.1 type array", "3.1.0", `{type: [string, "null"]}`, true, "t/prim/string"},
		{"plain", "3.1.0", "{type: string}", false, "t/prim/string"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			spec := "openapi: " + tc.version + "\n" +
				"info: {title: T, version: \"1\"}\n" +
				"paths: {}\n" +
				"components:\n" +
				"  schemas:\n" +
				"    S:\n" +
				"      type: object\n" +
				"      properties:\n" +
				"        p: " + tc.schema + "\n"
			doc, diags := schema.LowerSpec(t, spec)
			openapitest.RequireNoErrorDiags(t, diags)
			model, ok := doc.Types[componentID("S")].(*ir.Model)
			require.True(t, ok)
			require.Len(t, model.Properties, 1)
			assert.Equal(t, tc.wantTarget, model.Properties[0].Type.Target)
			assert.Equal(t, tc.wantNullable, model.Properties[0].Type.Nullable)
		})
	}
}

func TestSchema_Ref30NullableSiblings(t *testing.T) {
	t.Parallel()
	spec := openapitest.ComponentSpecVer("3.0.3", `    Owner:
      type: object
      properties:
        p: {$ref: '#/components/schemas/Target', nullable: true}
    Target: {type: string}
`)
	doc, diags := schema.LowerSpec(t, spec)
	require.NotNil(t, doc)
	_ = diags
	m := typeByName(doc, "Owner").(*ir.Model)
	assert.True(t, m.Properties[0].Type.Nullable, "3.0 nullable at a $ref site lifts to the ref")
}

// TestSchema_RefNullableAcrossSpellings pins that every spelling of "admits
// null" — 3.0 nullable: true, a 3.1 type array, and a oneOf/anyOf null branch —
// normalizes to the same Nullable bit at a $ref site, across direct refs,
// chained refs, sub-schema refs, array/scalar/union targets, and ref-site
// siblings, with negative controls for each shape (issue #28).
func TestSchema_RefNullableAcrossSpellings(t *testing.T) {
	t.Parallel()
	targetID := componentID("Target")
	midID := componentID("Mid")
	// A hoisted sub-schema lands in the anonymous namespace, not the component
	// one — the ref must resolve to that node, never to a synthesized name.
	subSchemaID := ir.TypeID("t/anon/components/schemas/Holder/properties/inner")
	cases := []struct {
		name         string
		version      string
		schemas      string
		wantNullable bool
		wantTarget   ir.TypeID
		msg          string
	}{
		{
			name:    "3.0 direct ref to nullable object component",
			version: "3.0.3",
			schemas: `    Owner:
      type: object
      properties:
        p: {$ref: '#/components/schemas/Target'}
    Target: {type: object, nullable: true}
`,
			wantNullable: true,
			wantTarget:   targetID,
			msg:          "3.0 nullable on the ref target lifts to the ref",
		},
		{
			name:    "3.1 direct ref to nullable object component (type array)",
			version: "3.1.0",
			schemas: `    Owner:
      type: object
      properties:
        p: {$ref: '#/components/schemas/Target'}
    Target: {type: [object, "null"]}
`,
			wantNullable: true,
			wantTarget:   targetID,
			msg:          "3.1 type-array null on the ref target lifts to the ref",
		},
		{
			name:    "3.0 direct ref to plain component (negative control)",
			version: "3.0.3",
			schemas: `    Owner:
      type: object
      properties:
        p: {$ref: '#/components/schemas/Target'}
    Target: {type: object}
`,
			wantNullable: false,
			wantTarget:   targetID,
			msg:          "a non-nullable 3.0 ref target stays non-nullable at the ref",
		},
		{
			name:    "3.1 direct ref to plain component (negative control)",
			version: "3.1.0",
			schemas: `    Owner:
      type: object
      properties:
        p: {$ref: '#/components/schemas/Target'}
    Target: {type: object}
`,
			wantNullable: false,
			wantTarget:   targetID,
			msg:          "a non-nullable 3.1 ref target stays non-nullable at the ref",
		},
		{
			name:    "3.0 chained ref lifts nullability from the base",
			version: "3.0.3",
			schemas: `    Owner:
      type: object
      properties:
        p: {$ref: '#/components/schemas/Mid'}
    Mid: {$ref: '#/components/schemas/Base'}
    Base: {type: object, nullable: true}
`,
			wantNullable: true,
			wantTarget:   midID,
			msg:          "3.0 nullable on a chain's base lifts through Mid to the ref",
		},
		{
			name:    "3.1 chained ref lifts nullability from the base (type array)",
			version: "3.1.0",
			schemas: `    Owner:
      type: object
      properties:
        p: {$ref: '#/components/schemas/Mid'}
    Mid: {$ref: '#/components/schemas/Base'}
    Base: {type: [object, "null"]}
`,
			wantNullable: true,
			wantTarget:   midID,
			msg:          "3.1 type-array null on a chain's base lifts through Mid to the ref",
		},
		{
			name:    "3.0 ref to a nullable non-component sub-schema",
			version: "3.0.3",
			schemas: `    Holder:
      type: object
      properties:
        inner: {type: object, nullable: true}
    Owner:
      type: object
      properties:
        p: {$ref: '#/components/schemas/Holder/properties/inner'}
`,
			wantNullable: true,
			wantTarget:   subSchemaID,
			msg:          "3.0 nullable on a non-component sub-schema lifts to a ref at its pointer",
		},
		{
			name:    "3.1 ref to a nullable non-component sub-schema (type array)",
			version: "3.1.0",
			schemas: `    Holder:
      type: object
      properties:
        inner: {type: [object, "null"]}
    Owner:
      type: object
      properties:
        p: {$ref: '#/components/schemas/Holder/properties/inner'}
`,
			wantNullable: true,
			wantTarget:   subSchemaID,
			msg:          "3.1 type-array null on a non-component sub-schema lifts to a ref at its pointer",
		},
		{
			name:    "3.1 nullable array component via ref",
			version: "3.1.0",
			schemas: `    Owner:
      type: object
      properties:
        p: {$ref: '#/components/schemas/Target'}
    Target: {type: [array, "null"], items: {type: string}}
`,
			wantNullable: true,
			wantTarget:   targetID,
			msg:          "3.1 type-array null on a nullable array component lifts to the ref",
		},
		{
			name:    "3.1 nullable scalar component via ref",
			version: "3.1.0",
			schemas: `    Owner:
      type: object
      properties:
        p: {$ref: '#/components/schemas/Target'}
    Target: {type: [string, "null"]}
`,
			wantNullable: true,
			wantTarget:   targetID,
			msg:          "3.1 type-array null on a nullable scalar component lifts to the ref",
		},
		{
			name:    "3.1 ref-site sibling type array widens a target excluding null",
			version: "3.1.0",
			schemas: `    Owner:
      type: object
      properties:
        p: {$ref: '#/components/schemas/Target', type: ["null"]}
    Target: {type: string}
`,
			wantNullable: true,
			wantTarget:   targetID,
			msg:          "the nullable-widening policy reads a null beside the $ref as a nullable reference",
		},
		{
			name:    "3.1 ref-site sibling type array conjoins with a target admitting null",
			version: "3.1.0",
			schemas: `    Owner:
      type: object
      properties:
        p: {$ref: '#/components/schemas/Target', type: [string, "null"]}
    Target: {type: [string, "null"]}
`,
			wantNullable: true,
			wantTarget:   targetID,
			msg:          "both conjuncts admit null, so the usage does",
		},
		{
			name:    "3.1 ref-site sibling type narrows a target admitting null",
			version: "3.1.0",
			schemas: `    Owner:
      type: object
      properties:
        p: {$ref: '#/components/schemas/Target', type: [string]}
    Target: {type: [string, "null"]}
`,
			wantNullable: false,
			wantTarget:   targetID,
			msg:          "a sibling type without null takes it out of what the target admits",
		},
		{
			name:    "3.1 non-null ref-site sibling type array (negative control)",
			version: "3.1.0",
			schemas: `    Owner:
      type: object
      properties:
        p: {$ref: '#/components/schemas/Target', type: [string]}
    Target: {type: string}
`,
			wantNullable: false,
			wantTarget:   targetID,
			msg:          "a $ref-site type array without null leaves the ref non-nullable",
		},
		{
			name:    "3.1 ref to a collapsed oneOf null component",
			version: "3.1.0",
			schemas: `    Owner:
      type: object
      properties:
        p: {$ref: '#/components/schemas/Target'}
    Target:
      oneOf: [{type: string}, {type: "null"}]
`,
			wantNullable: true,
			wantTarget:   targetID,
			msg:          "a oneOf null branch that collapses to nullable X lifts to the ref",
		},
		{
			name:    "3.1 ref to a multi-branch union with a null branch",
			version: "3.1.0",
			schemas: `    Owner:
      type: object
      properties:
        p: {$ref: '#/components/schemas/Target'}
    Target:
      oneOf: [{type: string}, {type: integer}, {type: "null"}]
`,
			wantNullable: true,
			wantTarget:   targetID,
			msg:          "a null branch stripped from a Union lifts to the ref, not into the variants",
		},
		{
			name:    "3.1 ref to an anyOf null component",
			version: "3.1.0",
			schemas: `    Owner:
      type: object
      properties:
        p: {$ref: '#/components/schemas/Target'}
    Target:
      anyOf: [{type: object}, {type: "null"}]
`,
			wantNullable: true,
			wantTarget:   targetID,
			msg:          "an anyOf null branch lifts to the ref just as a oneOf one does",
		},
		{
			name:    "3.1 ref to a union without a null branch (negative control)",
			version: "3.1.0",
			schemas: `    Owner:
      type: object
      properties:
        p: {$ref: '#/components/schemas/Target'}
    Target:
      oneOf: [{type: string}, {type: integer}]
`,
			wantNullable: false,
			wantTarget:   targetID,
			msg:          "a union with no null branch stays non-nullable at the ref",
		},
		{
			name:    "3.1 ref to a null branch intersected by structural siblings",
			version: "3.1.0",
			schemas: `    Owner:
      type: object
      properties:
        p: {$ref: '#/components/schemas/Target'}
    Target:
      type: object
      properties: {a: {type: string}}
      oneOf: [{type: string}, {type: "null"}]
`,
			wantNullable: false,
			wantTarget:   targetID,
			msg:          "a union co-declared with a structural body intersects with it, so its null branch admits nothing",
		},
		{
			name:    "3.1 ref to a closed enum with a null branch sibling",
			version: "3.1.0",
			schemas: `    Owner:
      type: object
      properties:
        p: {$ref: '#/components/schemas/Target'}
    Target:
      enum: ["open", "closed"]
      oneOf: [{type: string}, {type: "null"}]
`,
			wantNullable: false,
			wantTarget:   targetID,
			msg:          "an enum with no null member must not read as nullable through a $ref",
		},
		{
			name:    "3.1 ref to a nullable enum component",
			version: "3.1.0",
			schemas: `    Owner:
      type: object
      properties:
        p: {$ref: '#/components/schemas/Target'}
    Target:
      type: [string, "null"]
      enum: [red, green, null]
`,
			wantNullable: true,
			wantTarget:   targetID,
			// The bit comes from the type array, so this row covers the spelling
			// rather than the enum lowering; that the same declaration is also an
			// Enum of its non-null members is TestEnum_NullMemberNormalizesToNullable
			// and the nullable-enum-31 conformance case.
			msg: "a nullable enum target reads as nullable at a $ref like every other null spelling",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			spec := openapitest.ComponentSpecVer(tc.version, tc.schemas)
			doc, diags := schema.LowerSpec(t, spec)
			openapitest.RequireNoErrorDiags(t, diags)
			m := typeByName(doc, "Owner").(*ir.Model)
			require.Len(t, m.Properties, 1)
			assert.Equal(t, tc.wantNullable, m.Properties[0].Type.Nullable, tc.msg)
			assert.Equal(t, tc.wantTarget, m.Properties[0].Type.Target,
				"ref resolves to the expected target")
		})
	}
}

// TestSchema_NullabilityAgreesAcrossEnumSpellings pins that a value set and a
// type keyword conjoin, and that both ways of writing the same conjunction get
// the same answer (GitHub #288).
//
// `{type: [string, "null"], enum: [red, green]}` and
// `{enum: [red, green], oneOf: [{type: string}, {type: "null"}]}` say one
// thing: null is in the type space and out of the value space, so the position
// does not admit it.
//
// Both members of a pair share one document so "the same schema, two spellings"
// is a property of one compile. The admitting pair is there because a predicate
// hardcoded either way fails exactly one of the two.
func TestSchema_NullabilityAgreesAcrossEnumSpellings(t *testing.T) {
	t.Parallel()
	spec := openapitest.ComponentSpec(`    S:
      type: object
      properties:
        excludedByType: {type: [string, "null"], enum: [red, green]}
        excludedByUnion: {enum: [red, green], oneOf: [{type: string}, {type: "null"}]}
        admittedByType: {type: [string, "null"], enum: [red, green, null]}
        admittedByUnion: {enum: [red, green, null], oneOf: [{type: string}, {type: "null"}]}
`)
	doc, diags := schema.LowerSpec(t, spec)
	openapitest.RequireNoErrorDiags(t, diags)
	m, ok := typeByName(doc, "S").(*ir.Model)
	require.True(t, ok, "S is a model")
	props := openapitest.PropsByWire(m.Properties)
	require.Len(t, props, 4)

	pairs := []struct {
		name, typeSpelling, unionSpelling string
		want                              bool
	}{
		{"an enum listing no null member", "excludedByType", "excludedByUnion", false},
		{"an enum listing one", "admittedByType", "admittedByUnion", true},
	}
	for _, p := range pairs {
		t.Run(p.name, func(t *testing.T) {
			t.Parallel()
			got := props[p.typeSpelling].Type.Nullable
			assert.Equal(t, p.want, got, "the enum decides whether the position admits null")
			assert.Equal(t, got, props[p.unionSpelling].Type.Nullable,
				"the type-array and oneOf spellings of one constraint must agree")
		})
	}
}

// TestSchema_RefNullableMatchesInlineForUnionSiblings pins that one schema body
// lowers to the same Nullable bit whether it is written inline or reached
// through a $ref. The $ref site recomputes nullability, so it is the one place
// that can drift from the inline rule for a union carrying structural siblings.
func TestSchema_RefNullableMatchesInlineForUnionSiblings(t *testing.T) {
	t.Parallel()
	// Both positions are built from one body string, so "the same schema" is
	// structural rather than two hand-copied blocks that could drift apart.
	const body = `type: object
properties: {a: {type: string}}
oneOf: [{type: string}, {type: "null"}]`
	indent := func(n int) string {
		pad := strings.Repeat(" ", n)
		return pad + strings.ReplaceAll(body, "\n", "\n"+pad) + "\n"
	}
	spec := openapitest.ComponentSpec("    Target:\n" + indent(6) +
		`    Owner:
      type: object
      properties:
        viaRef: {$ref: '#/components/schemas/Target'}
        inline:
` + indent(10))
	doc, diags := schema.LowerSpec(t, spec)
	openapitest.RequireNoErrorDiags(t, diags)
	m := typeByName(doc, "Owner").(*ir.Model)
	props := openapitest.PropsByWire(m.Properties)
	require.Len(t, props, 2)

	assert.Equal(t, props["inline"].Type.Nullable, props["viaRef"].Type.Nullable,
		"the same body must not change nullability by being reached through a $ref")
	assert.False(t, props["viaRef"].Type.Nullable,
		"an intersected union's null branch admits nothing, so neither spelling is nullable")
}

// TestSchema_RefNullableAtNonPropertyPosition pins that the lifted bit reaches a
// $ref used as a list element, not just a model property.
func TestSchema_RefNullableAtNonPropertyPosition(t *testing.T) {
	t.Parallel()
	spec := openapitest.ComponentSpec(`    Owner:
      type: object
      properties:
        p:
          type: array
          items: {$ref: '#/components/schemas/Target'}
    Target:
      oneOf: [{type: string}, {type: integer}, {type: "null"}]
`)
	doc, diags := schema.LowerSpec(t, spec)
	openapitest.RequireNoErrorDiags(t, diags)
	list, ok := doc.Types["t/anon/components/schemas/Owner/properties/p"].(*ir.List)
	require.True(t, ok)
	assert.True(t, list.Elem.Nullable,
		"a nullable union target lifts to the ref wherever it is used, including a list element")
	assert.Equal(t, componentID("Target"), list.Elem.Target)
}
