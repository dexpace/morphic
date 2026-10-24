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

// inferred is a reference whose null only the nullable-widening policy admits.
func inferred(target ir.TypeID) ir.TypeRef {
	return ir.TypeRef{Target: target, Nullable: true, Inferred: openapi.NullableWideningHeuristic}
}

// prim is the shared primitive of kind k.
func prim(k ir.PrimKind) ir.TypeID { return ir.PrimTypeID(k) }

// holderProps returns the properties of the component schema Holder by wire
// name.
func holderProps(t *testing.T, doc *ir.Document) map[string]ir.Property {
	t.Helper()
	m, ok := doc.Types[componentID("Holder")].(*ir.Model)
	require.True(t, ok, "Holder lowers to a model")
	return openapitest.PropsByWire(m.Properties)
}

// assertNullableKeyword31 pins each 3.1 reading of the case: every reference
// whose null rests on `nullable` is marked, a type array's is not, and each
// keyword the policy honours is reported once, at itself.
func assertNullableKeyword31(t *testing.T, doc *ir.Document, diags []ir.Diagnostic) {
	props := holderProps(t, doc)
	assert.Equal(t, inferred(prim(ir.PrimString)), props["inline"].Type)
	assert.Equal(t, inferred(componentID("Name")), props["viaRef"].Type, "a target's nullable reaches its use")
	assert.Equal(t, inferred(componentID("Plain")), props["besideRef"].Type, "one beside the $ref widens the usage")
	assert.Equal(t, ir.TypeRef{Target: prim(ir.PrimString), Nullable: true}, props["declared"].Type,
		"the type array admits null, so the bit is the document's own")
	assert.Equal(t, ir.TypeRef{Target: prim(ir.PrimString)}, props["notNullable"].Type)

	counts, ok := doc.Types[props["counts"].Type.Target].(*ir.List)
	require.True(t, ok)
	assert.Equal(t, inferred(prim(ir.PrimInteger)), counts.Elem, "an element's reference is marked as any other")
	described, ok := doc.Types[props["described"].Type.Target].(*ir.List)
	require.True(t, ok)
	alias := ir.TypeID("t/anon/components/schemas/Holder/properties/described/items")
	assert.Equal(t, inferred(alias), described.Elem, "an alias homing the element's docs keeps the marker")
	shaped, ok := doc.Types[props["shaped"].Type.Target].(*ir.List)
	require.True(t, ok)
	assert.Equal(t, inferred("t/anon/components/schemas/Holder/properties/shaped/items"), shaped.Elem,
		"an element whose own node homes its docs keeps the marker")

	assert.Equal(t, []jsontext.Pointer{
		"/components/schemas/Holder/properties/besideRef/nullable",
		"/components/schemas/Holder/properties/counts/items/nullable",
		"/components/schemas/Holder/properties/described/items/nullable",
		"/components/schemas/Holder/properties/inline/nullable",
		"/components/schemas/Holder/properties/shaped/items/nullable",
		"/components/schemas/Name/nullable",
	}, openapitest.PointersOf(diags, diag.InferredNullable))
	assert.Empty(t, openapitest.PointersOf(diags, diag.UnknownSchemaKeyword), "the policy reads the keyword, so none is unknown")
}

// assertNullableKeyword30 pins each 3.0 shape the declared reading gives no
// null, as the clarified text has it, against the two it does.
func assertNullableKeyword30(t *testing.T, doc *ir.Document, diags []ir.Diagnostic) {
	props := holderProps(t, doc)
	assert.Equal(t, ir.TypeRef{Target: prim(ir.PrimString), Nullable: true}, props["typed"].Type,
		"nullable beside a type is the document's own null")
	assert.Equal(t, inferred(prim(ir.PrimAny)), props["untyped"].Type)
	for _, name := range []string{"composed", "unlisted", "typedComposed"} {
		assert.Equal(t, openapi.NullableWideningHeuristic, props[name].Type.Inferred, name)
		assert.True(t, props[name].Type.Nullable, name)
	}
	assert.True(t, props["listed"].Type.Nullable)
	assert.Empty(t, props["listed"].Type.Inferred, "an enum listing null agrees with the type nullable widens")

	why := map[jsontext.Pointer]string{
		"/components/schemas/Holder/properties/untyped/nullable":       "writes none",
		"/components/schemas/Holder/properties/composed/nullable":      "writes none",
		"/components/schemas/Holder/properties/unlisted/nullable":      "enum lists no null",
		"/components/schemas/Holder/properties/typedComposed/nullable": "allOf branch",
	}
	got := map[jsontext.Pointer]string{}
	for _, d := range diags {
		if d.Code == diag.InferredNullable {
			assert.Equal(t, ir.SeverityInfo, d.Severity)
			got[d.Provenance.Pointer] = d.Message
		}
	}
	require.Len(t, got, len(why))
	for at, reason := range why {
		assert.Contains(t, got[at], reason, at)
	}
}

// assertSchemaRefSiblings30 pins GitHub #871's example: the keys beside a 3.0
// schema's $ref are read as 3.1 reads them and each is reported, and the
// nullable among them is read by the policy, which says so once more.
func assertSchemaRefSiblings30(t *testing.T, doc *ir.Document, diags []ir.Diagnostic) {
	pet, ok := doc.Types[componentID("Pet")].(*ir.Model)
	require.True(t, ok)
	owner, ok := propByWire(pet, "owner")
	require.True(t, ok)
	assert.Equal(t, inferred(componentID("Person")), owner.Type)
	assert.Equal(t, "The pet's owner.", owner.Docs.Description, "the use site's description wins")
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

// TestSchemaRefSiblings_LowerAsIn31 is GitHub #871's rule as a test: the case
// declared 3.0.3 lowers to the document it lowers to declared 3.1.0, and the
// diagnostics differ only by a warning at each key beside a $ref.
func TestSchemaRefSiblings_LowerAsIn31(t *testing.T) {
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
		for _, d := range diags {
			line := fmt.Sprintf("%s %s %s", d.Severity, d.Code, d.Provenance.Pointer)
			if d.Code == diag.LaterVersionConstruct {
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

// TestNullableWidening_AKeywordIsReportedOnceAtItself pins where the policy's
// report lands: at the keyword, once, however many references read it and in
// whichever order the document declares them, while every reference whose
// null rests on it carries the marker. A report made at each use would grow
// with the uses and move with the order.
func TestNullableWidening_AKeywordIsReportedOnceAtItself(t *testing.T) {
	t.Parallel()
	n := inferred(componentID("N"))
	for _, swapped := range []bool{false, true} {
		doc, diags := compileNullable(t, reportedOnceSpec(swapped), false)
		assert.Equal(t, []jsontext.Pointer{"/components/schemas/N/nullable"},
			openapitest.PointersOf(diags, diag.InferredNullable), "swapped: %v", swapped)

		props := holderProps(t, doc)
		assert.Equal(t, n, props["a"].Type)
		list, ok := doc.Types[props["b"].Type.Target].(*ir.List)
		require.True(t, ok)
		assert.Equal(t, n, list.Elem)
		union, ok := doc.Types[props["c"].Type.Target].(*ir.Union)
		require.True(t, ok)
		require.Len(t, union.Variants, 2)
		assert.Equal(t, n, union.Variants[0].Type)

		op := openapitest.FindOp(t, doc, "post")
		require.Len(t, op.Params, 1)
		assert.Equal(t, n, op.Params[0].Type)
		require.NotNil(t, op.Request)
		require.Len(t, op.Request.Contents, 1)
		assert.Equal(t, n, op.Request.Contents[0].Type)
	}
}

// TestNullableWidening_DisabledKeepsWhatTheDialectDoesNotDefine pins the policy
// off: a `nullable` in a schema read as JSON Schema 2020-12, which is every 3.1
// one and one beside a 3.0 $ref, is a keyword that dialect does not define,
// kept verbatim where any unknown keyword is and reported as one. A 3.0
// schema's own nullable is read by its declared effect instead, and the policy
// on reads every one, so neither keeps anything.
func TestNullableWidening_DisabledKeepsWhatTheDialectDoesNotDefine(t *testing.T) {
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
		{"3.0 off", "3.0.3", true, []jsontext.Pointer{"/components/schemas/Holder/properties/r/nullable"}},
		{"3.1 on", "3.1.0", false, nil},
		{"3.0 on", "3.0.3", false, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			src := openapitest.HolderSpec(tc.version, "{type: string, nullable: true}",
				"        r: {$ref: '#/components/schemas/Plain', nullable: true}\n"+rest)
			doc, diags := compileNullable(t, src, tc.off)
			assert.Equal(t, tc.kept, openapitest.PointersOf(diags, diag.UnknownSchemaKeyword))
			props := holderProps(t, doc)
			for name, prop := range props {
				entry, ok := prop.Unmodeled["openapi:nullable"]
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

// TestNullableWidening_ACompositionKeepsTheMarker pins the two lowerings that
// build a reference out of others. An allOf redeclaration admits null where
// both branches do, so it rests on the policy where either branch's null did,
// and a distributed variant admits it where its branch does and the body
// does not exclude it, so it keeps the branch's marker. With the policy off
// neither admits null, and a body excluding null takes even a declared one out.
func TestNullableWidening_ACompositionKeepsTheMarker(t *testing.T) {
	t.Parallel()
	for _, off := range []bool{false, true} {
		doc, _ := compileNullable(t, compositionSpec, off)
		merged, ok := doc.Types[componentID("Merged")].(*ir.Model)
		require.True(t, ok)
		a, ok := propByWire(merged, "a")
		require.True(t, ok)
		narrowed, ok := doc.Types[componentID("Narrowed")].(*ir.Union)
		require.True(t, ok)
		require.Len(t, narrowed.Variants, 2)
		assert.Equal(t, ir.TypeRef{Target: "t/composed/components/schemas/Narrowed/oneOf/0"}, narrowed.Variants[0].Type,
			"off: %v", off)
		union, ok := doc.Types[componentID("Distributed")].(*ir.Union)
		require.True(t, ok)
		require.Len(t, union.Variants, 2)
		variant := ir.TypeID("t/composed/components/schemas/Distributed/oneOf/0")
		if off {
			assert.Equal(t, ir.TypeRef{Target: prim(ir.PrimString)}, a.Type, "the first branch's type excludes null")
			assert.Equal(t, ir.TypeRef{Target: variant}, union.Variants[0].Type)
			continue
		}
		assert.Equal(t, inferred(prim(ir.PrimString)), a.Type)
		assert.Equal(t, inferred(variant), union.Variants[0].Type)
		assert.False(t, union.Variants[1].Type.Nullable)
	}
}
