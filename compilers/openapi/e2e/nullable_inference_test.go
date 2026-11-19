// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package e2e_test

import (
	"encoding/json/jsontext"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/ir"
)

// inferred is a reference whose null only the nullable-inference policy admits.
func inferred(target ir.TypeID) ir.TypeRef {
	return ir.TypeRef{Target: target, Nullable: true, Inferred: openapi.NullableInferenceHeuristic}
}

// primitive is the shared primitive of kind primitiveKind.
func primitive(primitiveKind ir.PrimitiveKind) ir.TypeID { return ir.PrimitiveTypeID(primitiveKind) }

// holderProperties returns the properties of the component schema Holder by wire
// name.
func holderProperties(t *testing.T, doc *ir.Document) map[string]ir.Property {
	t.Helper()
	model, ok := doc.Types[componentID("Holder")].(*ir.Model)
	require.True(t, ok, "Holder lowers to a model")
	return openapitest.PropertiesByWire(model.Properties)
}

// assertNullableKeyword31 pins each 3.1 reading of the case: every reference
// whose null rests on `nullable` is marked, a type array's is not, and each
// keyword the policy honours is reported once, at itself.
func assertNullableKeyword31(t *testing.T, doc *ir.Document, diags []ir.Diagnostic) {
	properties := holderProperties(t, doc)
	assert.Equal(t, inferred(primitive(ir.PrimitiveString)), properties["inline"].Type)
	assert.Equal(t, inferred(componentID("Name")), properties["viaRef"].Type, "a target's nullable reaches its use")
	assert.Equal(t, inferred(componentID("Plain")), properties["besideRef"].Type, "one beside the $ref widens the usage")
	assert.Equal(t, ir.TypeRef{Target: primitive(ir.PrimitiveString), Nullable: true}, properties["declared"].Type,
		"the type array admits null, so the bit is the document's own")
	assert.Equal(t, ir.TypeRef{Target: primitive(ir.PrimitiveString)}, properties["notNullable"].Type)

	counts, ok := doc.Types[properties["counts"].Type.Target].(*ir.List)
	require.True(t, ok)
	assert.Equal(t, inferred(primitive(ir.PrimitiveInteger)), counts.ElementType, "an element's reference is marked as any other")
	described, ok := doc.Types[properties["described"].Type.Target].(*ir.List)
	require.True(t, ok)
	alias := ir.TypeID("type/anonymous/components/schemas/Holder/properties/described/items")
	assert.Equal(t, inferred(alias), described.ElementType, "an alias homing the element's docs keeps the marker")
	shaped, ok := doc.Types[properties["shaped"].Type.Target].(*ir.List)
	require.True(t, ok)
	assert.Equal(t, inferred("type/anonymous/components/schemas/Holder/properties/shaped/items"), shaped.ElementType,
		"an element whose own node homes its docs keeps the marker")

	assert.Equal(t, []jsontext.Pointer{
		"/components/schemas/Holder/properties/besideRef/nullable",
		"/components/schemas/Holder/properties/counts/items/nullable",
		"/components/schemas/Holder/properties/described/items/nullable",
		"/components/schemas/Holder/properties/inline/nullable",
		"/components/schemas/Holder/properties/shaped/items/nullable",
		"/components/schemas/Name/nullable",
	}, openapitest.PointersOf(diags, diag.InferredNullable))
	assert.Equal(t, []jsontext.Pointer{"/components/schemas/Holder/properties/notNullable/nullable"},
		openapitest.PointersOf(diags, diag.UnknownSchemaKeyword), "the policy reads only a true, so a false is unknown")
	notNullable := properties["notNullable"]
	require.Contains(t, notNullable.Unmodeled, "openapi:nullable")
	assert.Equal(t, ir.ReasonOutOfScope, notNullable.Unmodeled["openapi:nullable"].Reason)
	assert.JSONEq(t, "false", string(notNullable.Unmodeled["openapi:nullable"].Value))
}

// assertNullableKeyword30 pins each 3.0 shape the declared reading gives no
// null, as the clarified text has it, against the two it does.
func assertNullableKeyword30(t *testing.T, doc *ir.Document, diags []ir.Diagnostic) {
	properties := holderProperties(t, doc)
	assert.Equal(t, ir.TypeRef{Target: primitive(ir.PrimitiveString), Nullable: true}, properties["typed"].Type,
		"nullable beside a type is the document's own null")
	assert.Equal(t, inferred(primitive(ir.PrimitiveAny)), properties["untyped"].Type)
	for _, name := range []string{"composed", "unlisted", "typedComposed"} {
		assert.Equal(t, openapi.NullableInferenceHeuristic, properties[name].Type.Inferred, name)
		assert.True(t, properties[name].Type.Nullable, name)
	}
	assert.True(t, properties["listed"].Type.Nullable)
	assert.Empty(t, properties["listed"].Type.Inferred, "an enum listing null agrees with the type nullable widens")

	why := map[jsontext.Pointer]string{
		"/components/schemas/Holder/properties/untyped/nullable":       "writes none",
		"/components/schemas/Holder/properties/composed/nullable":      "writes none",
		"/components/schemas/Holder/properties/unlisted/nullable":      "enum lists no null",
		"/components/schemas/Holder/properties/typedComposed/nullable": "allOf branch",
	}
	got := map[jsontext.Pointer]string{}
	for _, diagnostic := range diags {
		if diagnostic.Code == diag.InferredNullable {
			assert.Equal(t, ir.SeverityInfo, diagnostic.Severity)
			got[diagnostic.Provenance.Pointer] = diagnostic.Message
		}
	}
	require.Len(t, got, len(why))
	for at, reason := range why {
		assert.Contains(t, got[at], reason, at)
	}
}

// assertSchemaRefSiblings30 pins GitHub #871's example: the keys beside a 3.0
// schema's $ref are applied as 3.1 applies them and each is reported, and the
// nullable among them, which no type beside it gives an effect, is read by the
// policy, which says so once more.
func assertSchemaRefSiblings30(t *testing.T, doc *ir.Document, diags []ir.Diagnostic) {
	pet, ok := doc.Types[componentID("Pet")].(*ir.Model)
	require.True(t, ok)
	owner, ok := propertyByWire(pet, "owner")
	require.True(t, ok)
	// The keywords beside the $ref are a conjunct of the use, so owner names
	// the decorator over Person they make, which holds the description.
	decorator := ir.TypeID("type/anonymous/components/schemas/Pet/properties/owner")
	assert.Equal(t, inferred(decorator), owner.Type)
	assert.Equal(t, "The pet's owner.", doc.Types[decorator].Common().Docs.Description, "the use site's description wins")
	assert.Equal(t, []ir.Lifecycle{ir.LifecycleRead, ir.LifecycleDelete, ir.LifecycleQuery}, owner.Visibility.Only)

	alias := doc.Types[componentID("Owner")]
	require.NotNil(t, alias)
	assert.Equal(t, "An owner.", alias.Common().Docs.Description)

	assert.Equal(t, []jsontext.Pointer{
		"/components/schemas/Owner/description",
		"/components/schemas/Pet/properties/keepers/items/title",
		"/components/schemas/Pet/properties/owner/description",
		"/components/schemas/Pet/properties/owner/nullable",
		"/components/schemas/Pet/properties/owner/readOnly",
	}, openapitest.PointersOf(diags, diag.LaterVersionConstruct))
	assert.Equal(t, []jsontext.Pointer{"/components/schemas/Pet/properties/owner/nullable"},
		openapitest.PointersOf(diags, diag.InferredNullable))
}

// TestSchemaRefSiblings_ApplyAsIn31 is GitHub #871's rule as a test: the case
// declared 3.0.3 lowers to the document it lowers to declared 3.1.0, and the
// diagnostics differ only by a warning at each key beside a $ref. Each keyword
// is read in its own document's dialect, and the case's keywords mean the same
// in both, which is why the two lower alike.
func TestSchemaRefSiblings_ApplyAsIn31(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile(filepath.Join(conformanceDir, "schema-ref-siblings-30.yaml"))
	require.NoError(t, err)
	compile := func(version string) (string, []string, []string) {
		t.Helper()
		src := []byte(strings.Replace(string(data), "openapi: 3.0.3", "openapi: "+version, 1))
		doc, diags, err := openapi.New().Compile(t.Context(),
			[]compilers.Source{{Path: "spec.yaml", Data: src}}, compilers.Options{})
		require.NoError(t, err)
		require.NotNil(t, doc)
		var later, others []string
		for _, diagnostic := range diags {
			line := fmt.Sprintf("%s %s %s", diagnostic.Severity, diagnostic.Code, diagnostic.Provenance.Pointer)
			if diagnostic.Code == diag.LaterVersionConstruct {
				later = append(later, line)
			} else {
				others = append(others, line)
			}
		}
		return withoutVersion(t, doc), later, others
	}
	body31, later31, others31 := compile("3.1.0")
	body30, later30, others30 := compile("3.0.3")
	assert.Equal(t, body31, body30, "a 3.0 document lowers as a 3.1 one")
	assert.Equal(t, others31, others30, "and reports what it does")
	assert.Empty(t, later31)
	assert.Len(t, later30, 5, "one warning at each key beside a $ref")
}

// TestSchemaRefSiblings_ReadInTheDeclaredDialect pins that a keyword beside a
// 3.0 $ref, applied as 3.1 applies it, is read as 3.0 defines it: its nullable
// adds null to the type beside it, under either policy, and its
// exclusiveMinimum is a boolean modifying the bound beside it, which the
// warning says. The 3.1 control reads its nullable as no keyword at all.
func TestSchemaRefSiblings_ReadInTheDeclaredDialect(t *testing.T) {
	t.Parallel()
	const at = "/components/schemas/Holder/properties/p"
	// The keywords beside the $ref are a conjunct of the use, so p names the
	// model over Maybe they make, as the allOf of the two does.
	model := ir.TypeID("type/anonymous" + at)
	maybe := ir.TypeRef{Target: model, Nullable: true}
	for _, tc := range []struct {
		name, version, p, rest string
		off                    bool
		want                   ir.TypeRef
		unknown                []jsontext.Pointer
	}{
		{"3.0 off", "3.0.3", "{$ref: '#/components/schemas/Maybe', type: object, nullable: true}",
			"    Maybe: {type: object, nullable: true}\n", true, maybe, nil},
		{"3.0 on", "3.0.3", "{$ref: '#/components/schemas/Maybe', type: object, nullable: true}",
			"    Maybe: {type: object, nullable: true}\n", false, maybe, nil},
		{"3.1 off", "3.1.0", "{$ref: '#/components/schemas/Maybe', type: object, nullable: true}",
			"    Maybe: {type: [object, \"null\"]}\n", true, ir.TypeRef{Target: model},
			[]jsontext.Pointer{at + "/nullable"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			doc, diags := compileNullable(t, openapitest.HolderSpec(tc.version, tc.p, tc.rest), tc.off)
			openapitest.RequireNoErrorDiags(t, diags)
			assert.Equal(t, tc.want, holderProperties(t, doc)["p"].Type)
			assert.Equal(t, tc.unknown, openapitest.PointersOf(diags, diag.UnknownSchemaKeyword))
			assert.Empty(t, openapitest.PointersOf(diags, diag.InferredNullable))
		})
	}

	t.Run("3.0 exclusiveMinimum", func(t *testing.T) {
		t.Parallel()
		src := openapitest.HolderSpec("3.0.3", "{$ref: '#/components/schemas/Int', minimum: 5, exclusiveMinimum: true}",
			"    Int: {type: integer}\n")
		doc, diags := compileNullable(t, src, false)
		openapitest.RequireNoErrorDiags(t, diags)
		cons := holderProperties(t, doc)["p"].Constraints
		require.NotNil(t, cons)
		require.NotNil(t, cons.ExclusiveMin)
		assert.Equal(t, ir.BigVal("5"), *cons.ExclusiveMin)
		var said string
		for _, diagnostic := range diags {
			if diagnostic.Code == diag.LaterVersionConstruct && diagnostic.Provenance.Pointer == at+"/exclusiveMinimum" {
				said = diagnostic.Message
			}
		}
		assert.Equal(t, `keyword "exclusiveMinimum" beside the $ref of a Schema Object is ignored by OpenAPI 3.0 `+
			`and applied by OpenAPI 3.1, and this document declares 3.0.3; it is applied as in an OpenAPI 3.1 `+
			`document and read as in an OpenAPI 3.0 one`, said)
	})
}

// compileNullable compiles src, a whole document, with the nullable-inference
// policy on or off. That reach through openapi is why openapitest cannot hold
// it — see that package's doc comment.
func compileNullable(t *testing.T, src string, off bool) (*ir.Document, []ir.Diagnostic) {
	t.Helper()
	opts := compilers.Options{FormatOptions: openapi.Options{NullableInference: openapi.NullableInference{Disabled: off}}}
	doc, diags, err := openapi.New().Compile(t.Context(),
		[]compilers.Source{{Path: "spec.yaml", Data: []byte(src)}}, opts)
	require.NoError(t, err)
	require.NotNil(t, doc)
	return doc, diags
}

// reportedOnceSpec references N, whose nullable 3.1 does not define, from a
// property, a list's element, a union variant, a parameter and a body, with N
// declared after the schemas using it or, swapped, before them.
func reportedOnceSpec(swapped bool) string {
	n := "    N: {type: string, nullable: true}\n"
	users := `    Holder:
      type: object
      properties:
        a: {$ref: '#/components/schemas/N'}
        b: {type: array, items: {$ref: '#/components/schemas/N'}}
        c: {oneOf: [{$ref: '#/components/schemas/N'}, {type: integer}]}
`
	schemas := users + n
	if swapped {
		schemas = n + users
	}
	return `openapi: 3.1.0
info: {title: t, version: "1"}
paths:
  /x:
    post:
      operationId: post
      parameters:
        - {name: q, in: query, schema: {$ref: '#/components/schemas/N'}}
      requestBody:
        content:
          application/json:
            schema: {$ref: '#/components/schemas/N'}
      responses: {"204": {description: ok}}
components:
  schemas:
` + schemas
}

// TestNullableInference_AKeywordIsReportedOnceAtItself pins where the policy's
// report lands: at the keyword, once, however many references read it and in
// whichever order the document declares them, while every reference whose
// null rests on it carries the marker. A report made at each use would grow
// with the uses and move with the order.
func TestNullableInference_AKeywordIsReportedOnceAtItself(t *testing.T) {
	t.Parallel()
	typeRef := inferred(componentID("N"))
	for _, swapped := range []bool{false, true} {
		doc, diags := compileNullable(t, reportedOnceSpec(swapped), false)
		assert.Equal(t, []jsontext.Pointer{"/components/schemas/N/nullable"},
			openapitest.PointersOf(diags, diag.InferredNullable), "swapped: %v", swapped)

		properties := holderProperties(t, doc)
		assert.Equal(t, typeRef, properties["a"].Type)
		list, ok := doc.Types[properties["b"].Type.Target].(*ir.List)
		require.True(t, ok)
		assert.Equal(t, typeRef, list.ElementType)
		union, ok := doc.Types[properties["c"].Type.Target].(*ir.Union)
		require.True(t, ok)
		require.Len(t, union.Variants, 2)
		assert.Equal(t, typeRef, union.Variants[0].Type)

		operation := openapitest.FindOperation(t, doc, "post")
		require.Len(t, operation.Params, 1)
		assert.Equal(t, typeRef, operation.Params[0].Type)
		require.NotNil(t, operation.Request)
		require.Len(t, operation.Request.Contents, 1)
		assert.Equal(t, typeRef, operation.Request.Contents[0].Type)
	}
}

// TestNullableInference_DisabledKeepsWhatTheDialectDoesNotDefine pins the policy
// off: a `nullable` in a 3.1 schema is a keyword that dialect does not define,
// kept verbatim where any unknown keyword is and reported as one. With the
// policy on it keeps only the one the policy does not read, a `false`. A 3.0
// schema's nullable, one beside its $ref included, is read by its declared
// effect instead, so it keeps nothing. The one beside a 3.0 $ref is still a
// later minor's construct.
func TestNullableInference_DisabledKeepsWhatTheDialectDoesNotDefine(t *testing.T) {
	t.Parallel()
	const rest = "    N: {type: string, nullable: false}\n"
	for _, tc := range []struct {
		name, version string
		off           bool
		kept          []jsontext.Pointer
	}{
		{"3.1 off", "3.1.0", true, []jsontext.Pointer{
			"/components/schemas/Holder/properties/p/nullable", "/components/schemas/Holder/properties/r/nullable",
			"/components/schemas/N/nullable",
		}},
		{"3.0 off", "3.0.3", true, nil},
		{"3.1 on", "3.1.0", false, []jsontext.Pointer{"/components/schemas/N/nullable"}},
		{"3.0 on", "3.0.3", false, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			src := openapitest.HolderSpec(tc.version, "{type: string, nullable: true}",
				"        r: {$ref: '#/components/schemas/Plain', nullable: true}\n"+rest)
			doc, diags := compileNullable(t, src, tc.off)
			assert.Equal(t, tc.kept, openapitest.PointersOf(diags, diag.UnknownSchemaKeyword))
			assert.Equal(t, tc.version == "3.0.3",
				slices.Contains(openapitest.PointersOf(diags, diag.LaterVersionConstruct),
					"/components/schemas/Holder/properties/r/nullable"),
				"the nullable beside a 3.0 $ref is a later minor's construct")
			properties := holderProperties(t, doc)
			for name, property := range properties {
				entry, ok := property.Unmodeled["openapi:nullable"]
				if !slices.Contains(tc.kept, jsontext.Pointer("/components/schemas/Holder/properties/"+name+"/nullable")) {
					assert.False(t, ok, name)
					continue
				}
				require.True(t, ok, name)
				assert.Equal(t, ir.ReasonOutOfScope, entry.Reason, name)
				assert.JSONEq(t, "true", string(entry.Value), name)
			}
			_, keptOnN := doc.Types[componentID("N")].Common().Unmodeled["openapi:nullable"]
			assert.Equal(t, slices.Contains(tc.kept, "/components/schemas/N/nullable"), keptOnN,
				"a nullable: false the dialect does not define is kept too")
		})
	}
}

// TestNullableInference_ANullableWithNoTypeIsWarnedOfUnderEitherPolicy pins
// that whether a 3.0 `nullable: true` has an effect is a fact of the
// specification, not of the policy: with no `type` beside it, it draws one
// no-effect warning at the keyword either way. With the policy on, the
// policy's info sits at the same pointer and the reference admits null, marked
// inferred; with it off, the reference admits none.
func TestNullableInference_ANullableWithNoTypeIsWarnedOfUnderEitherPolicy(t *testing.T) {
	t.Parallel()
	const at = jsontext.Pointer("/components/schemas/Holder/properties/p/nullable")
	for _, tc := range []struct {
		name     string
		off      bool
		inferred []jsontext.Pointer
		want     ir.TypeRef
	}{
		{"on", false, []jsontext.Pointer{at}, inferred(primitive(ir.PrimitiveAny))},
		{"off", true, nil, ir.TypeRef{Target: primitive(ir.PrimitiveAny)}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			doc, diags := compileNullable(t, openapitest.HolderSpec("3.0.3", "{nullable: true}", ""), tc.off)
			assert.Equal(t, []jsontext.Pointer{at}, openapitest.PointersOf(diags, diag.Validation+"/validation-no-effect"))
			assert.Equal(t, tc.inferred, openapitest.PointersOf(diags, diag.InferredNullable))
			for _, diagnostic := range diags {
				if diagnostic.Code == diag.Validation+"/validation-no-effect" {
					assert.Equal(t, ir.SeverityWarning, diagnostic.Severity)
				}
			}
			assert.Equal(t, tc.want, holderProperties(t, doc)["p"].Type)
		})
	}
}

// compositionSpec composes N, whose nullable 3.1 does not define, two ways: an
// allOf whose branches redeclare one property, the first through nullable and
// the second through a type array, and a union beside properties, which
// distributes over its branches. Narrowed distributes over D, whose type array
// admits null, beside a type that excludes it.
const compositionSpec = `openapi: 3.1.0
info: {title: t, version: "1"}
paths: {}
components:
  schemas:
    N: {type: object, nullable: true, properties: {x: {type: string}}}
    M: {type: object, properties: {y: {type: string}}}
    D: {type: [object, "null"], properties: {x: {type: string}}}
    Merged:
      type: object
      allOf:
        - {type: object, properties: {a: {type: string, nullable: true}}}
        - {type: object, properties: {a: {type: [string, "null"]}}}
    Distributed:
      properties: {kind: {type: string}}
      oneOf: [{$ref: '#/components/schemas/N'}, {$ref: '#/components/schemas/M'}]
    Narrowed:
      type: object
      properties: {kind: {type: string}}
      oneOf: [{$ref: '#/components/schemas/D'}, {$ref: '#/components/schemas/M'}]
`

// TestNullableInference_ACompositionKeepsTheMarker pins the two lowerings that
// build a reference out of others. An allOf redeclaration admits null where
// both branches do, so it rests on the policy where either branch's null did,
// and a distributed variant admits it where its branch does and the body
// does not exclude it, so it keeps the branch's marker. With the policy off
// neither admits null, and a body excluding null takes even a declared one out.
func TestNullableInference_ACompositionKeepsTheMarker(t *testing.T) {
	t.Parallel()
	for _, off := range []bool{false, true} {
		doc, _ := compileNullable(t, compositionSpec, off)
		merged, ok := doc.Types[componentID("Merged")].(*ir.Model)
		require.True(t, ok)
		a, ok := propertyByWire(merged, "a")
		require.True(t, ok)
		narrowed, ok := doc.Types[componentID("Narrowed")].(*ir.Union)
		require.True(t, ok)
		require.Len(t, narrowed.Variants, 2)
		assert.Equal(t, ir.TypeRef{Target: "type/composed/components/schemas/Narrowed/oneOf/0"}, narrowed.Variants[0].Type,
			"off: %v", off)
		union, ok := doc.Types[componentID("Distributed")].(*ir.Union)
		require.True(t, ok)
		require.Len(t, union.Variants, 2)
		variant := ir.TypeID("type/composed/components/schemas/Distributed/oneOf/0")
		if off {
			assert.Equal(t, ir.TypeRef{Target: primitive(ir.PrimitiveString)}, a.Type, "the first branch's type excludes null")
			assert.Equal(t, ir.TypeRef{Target: variant}, union.Variants[0].Type)
			continue
		}
		assert.Equal(t, inferred(primitive(ir.PrimitiveString)), a.Type)
		assert.Equal(t, inferred(variant), union.Variants[0].Type)
		assert.False(t, union.Variants[1].Type.Nullable)
	}
}

// TestNullableInference_DisabledKeepsItAtASharedNode pins that a nullable the
// dialect does not define reaches the IR where the position lowers to a shared
// node and has no carrier, on an alias hoisted for it, as a keyword the model
// does not name does.
func TestNullableInference_DisabledKeepsItAtASharedNode(t *testing.T) {
	t.Parallel()
	doc, diags := compileNullable(t, `openapi: 3.1.0
info: {title: t, version: "1"}
paths: {}
components:
  schemas:
    A: {type: array, items: {type: integer, nullable: true}}
`, true)
	alias, ok := doc.Types["type/anonymous/components/schemas/A/items"]
	require.True(t, ok)
	assert.Equal(t, `true`, string(unmodeledEntry(t, alias.Common().Unmodeled, "openapi:nullable").Value))
	assert.Equal(t, []ir.Severity{ir.SeverityInfo}, diagsAt(diags, "openapi/unknown-schema-keyword",
		"/components/schemas/A/items/nullable"))
}
