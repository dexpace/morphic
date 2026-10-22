package schema_test

import (
	"encoding/json/jsontext"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
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

// holderSpec is a document of the version declaring Holder, whose property p
// is written as p, beside the schemas in rest.
func holderSpec(version, p, rest string) string {
	return "openapi: " + version + `
info: {title: t, version: "1"}
paths: {}
components:
  schemas:
    Plain: {type: string}
    Pet: {type: object, properties: {name: {type: string}}}
    Holder:
      type: object
      properties:
        p: ` + p + "\n" + rest
}

// holderP is the type of Holder's property p.
func holderP(t *testing.T, doc *ir.Document) ir.TypeRef {
	t.Helper()
	m, ok := typeByName(doc, "Holder").(*ir.Model)
	require.True(t, ok, "Holder lowers to a model")
	return openapitest.PropsByWire(m.Properties)["p"].Type
}

// pointersOf returns the pointers diags report code at, sorted.
func pointersOf(diags []ir.Diagnostic, code string) []jsontext.Pointer {
	var out []jsontext.Pointer
	for _, d := range diags {
		if d.Code == code {
			out = append(out, d.Provenance.Pointer)
		}
	}
	slices.Sort(out)
	return out
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
			src := holderSpec(tc.version, tc.p, tc.rest)
			for _, off := range []bool{false, true} {
				doc, diags := compileNullable(t, src, off)
				want, reports := tc.on, pointersOf(diags, diag.InferredNullable)
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
	src := holderSpec("3.1.0", "{type: string, nullable: true, enum: [a, null]}", "")
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
	return holderSpec(version, "{$ref: '#/components/schemas/"+prev+"'}", b.String())
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
