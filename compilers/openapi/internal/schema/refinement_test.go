// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package schema_test

import (
	"encoding/json/v2"
	"fmt"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/compilers/openapi/internal/schema"
	"github.com/dexpace/morphic/ir"
)

// refinementTargets are what the refinements below name: an object, a string
// saying everything a use can inherit, a port, an enum, and a list.
const refinementTargets = `    Owner: {type: object, properties: {id: {type: string}}}
    Str: {type: string, maxLength: 64, title: A string, description: from the target}
    Port: {type: integer}
    Mode: {type: string, enum: [get, set], description: mode doc}
    Names: {type: array, items: {type: string}}
    Mail: {type: string, format: email}
    MailToo: {allOf: [{type: string, format: email}]}
    Int32: {type: integer, format: int32}
    OneOf: {oneOf: [{type: string}, {type: integer}]}
    AnyOf: {anyOf: [{type: string}, {type: integer}]}
`

// positionsDoc is a 3.0 document of targets, with written, a schema, at every
// position the compiler lowers one at.
func positionsDoc(targets, written string) string {
	return fmt.Sprintf(`openapi: 3.0.3
info: {title: t, version: "1"}
paths:
  /p/{id}:
    post:
      operationId: op
      parameters:
        - {name: id, in: path, required: true, schema: %[2]s}
      requestBody:
        content:
          application/json: {schema: %[2]s}
      responses:
        '200':
          description: ok
          headers:
            X-H: {schema: %[2]s}
          content:
            application/json: {schema: %[2]s}
components:
  schemas:
%[1]s    Holder:
      type: object
      properties:
        f: %[2]s
    Bag: {type: object, properties: {k: {type: string}}, additionalProperties: %[2]s}
    Items: {type: array, items: %[2]s}
    Variants: {oneOf: [%[2]s, {type: boolean}]}
    Named: %[2]s
`, targets, written)
}

// positionRefs returns the reference each position of positionsDoc lowered to,
// carriers and the rest apart, since they hold what a position writes in two
// different places.
func positionRefs(t *testing.T, doc *ir.Document) (carriers, nodes map[string]ir.TypeRef) {
	t.Helper()
	op := openapitest.FindOp(t, doc, "op")
	param, ok := openapitest.ParamBySource(op, "id")
	require.True(t, ok, "the operation declares id")
	require.Len(t, op.Responses, 1)
	require.Len(t, op.Responses[0].Headers, 1)
	require.NotNil(t, op.Request)
	require.NotNil(t, op.Responses[0].Payload)
	items, ok := typeByName(doc, "Items").(*ir.List)
	require.True(t, ok, "Items lowers to a list")
	union, ok := typeByName(doc, "Variants").(*ir.Union)
	require.True(t, ok, "Variants lowers to a union")
	bag, ok := typeByName(doc, "Bag").(*ir.Model)
	require.True(t, ok, "Bag lowers to a model")
	require.NotNil(t, bag.AdditionalProps)
	carriers = map[string]ir.TypeRef{
		"property":  propertyOf(t, doc, "Holder", "f").Type,
		"parameter": param.Type,
		"header":    op.Responses[0].Headers[0].Type,
	}
	nodes = map[string]ir.TypeRef{
		"request":  op.Request.Contents[0].Type,
		"response": op.Responses[0].Payload.Contents[0].Type,
		"map":      bag.AdditionalProps.Value,
		"items":    items.Elem,
		"variant":  union.Variants[0].Type,
	}
	return carriers, nodes
}

// aliasAt returns the alias a position's reference names.
func aliasAt(t *testing.T, doc *ir.Document, ref ir.TypeRef) *ir.Scalar {
	t.Helper()
	alias, ok := doc.Types[ref.Target].(*ir.Scalar)
	require.True(t, ok, "%s lowers to an alias, not %T", ref.Target, doc.Types[ref.Target])
	require.NotNil(t, alias.Base)
	return alias
}

// noDegradation requires a compile of a clean refinement to report nothing of
// it being kept.
func noDegradation(t *testing.T, diags []ir.Diagnostic) {
	t.Helper()
	assert.Empty(t, withCode(diags, diag.DegradedConstruct), "nothing is kept verbatim")
	assert.Empty(t, withCode(diags, diag.InferredNullable), "and nothing read in")
}

// TestRefinement_AnAnnotationOnlyBranchLandsTyped is what GitHub #660 asks: the
// branch of an allOf refining a $ref lands where the sibling spelling lands, on
// a model decorator where the target is an object, and on the reference itself
// where it is not.
func TestRefinement_AnAnnotationOnlyBranchLandsTyped(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, openapitest.ComponentSpecVer("3.0.3", refinementTargets+`    Holder:
      type: object
      properties:
        owner:
          allOf:
            - $ref: '#/components/schemas/Owner'
            - {nullable: true, readOnly: true, description: The owner}
        zone_id:
          allOf:
            - $ref: '#/components/schemas/Str'
            - {description: The zone ID, maxLength: 32}
`))
	openapitest.RequireNoErrorDiags(t, diags)
	assert.Empty(t, withCode(diags, diag.DegradedConstruct), "nothing is left to keep verbatim")

	owner := propertyOf(t, doc, "Holder", "owner")
	m, ok := doc.Types[owner.Type.Target].(*ir.Model)
	require.True(t, ok, "an object target keeps its composition")
	require.NotNil(t, m.Base)
	assert.Equal(t, componentID("Owner"), m.Base.Target)
	assert.Equal(t, "The owner", m.Docs.Description)
	assert.Empty(t, m.Unmodeled)
	assert.True(t, owner.Type.Nullable)
	assert.NotEmpty(t, owner.Type.Inferred, "the null is the policy's reading")
	assert.Equal(t, []ir.Lifecycle{ir.LifecycleRead, ir.LifecycleDelete, ir.LifecycleQuery}, owner.Visibility.Only)
	assert.Len(t, withCode(diags, diag.InferredNullable), 1, "reported once")

	zone := propertyOf(t, doc, "Holder", "zone_id")
	assert.Equal(t, componentID("Str"), zone.Type.Target, "the position is a use of the string")
	assert.Equal(t, "The zone ID", zone.Docs.Description, "over the target's")
	assert.Equal(t, "A string", zone.Docs.Summary, "and the target's where it writes none")
	require.NotNil(t, zone.Constraints)
	require.NotNil(t, zone.Constraints.MaxLength)
	assert.Equal(t, int64(32), *zone.Constraints.MaxLength)
	assert.Empty(t, zone.Unmodeled)
}

// TestRefinement_PrimitiveBranchesLowerToTheirMeet is GitHub #1043: every
// branch a kind, the position is the narrowest of them, in either order.
func TestRefinement_PrimitiveBranchesLowerToTheirMeet(t *testing.T) {
	t.Parallel()
	for name, branches := range map[string]string{
		"narrowest last":      "[{type: integer}, {type: integer, format: int32}]",
		"narrowest first":     "[{type: integer, format: int32}, {type: integer}]",
		"beside number":       "[{type: number}, {type: integer, format: int32}]",
		"beside the top type": "[{}, {type: integer, format: int32}]",
		"a number's width":    "[{type: integer}, {type: number, format: int32}]",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			doc, diags := parseFull(t, openapitest.ComponentSpecVer("3.0.3", "    Prim: {allOf: "+branches+"}\n"))
			assert.Empty(t, diags, "the branches narrow to one kind")
			alias, ok := typeByName(doc, "Prim").(*ir.Scalar)
			require.True(t, ok, "Prim is a named scalar, not %T", typeByName(doc, "Prim"))
			require.NotNil(t, alias.Base)
			prim, ok := doc.Types[alias.Base.Target].(*ir.Primitive)
			require.True(t, ok)
			assert.Equal(t, ir.PrimInt32, prim.Prim)
		})
	}
}

// TestRefinement_DisjointConjunctsStayAComposition pins what GitHub #1043 keeps:
// no value satisfies both branches, so the warnings stand on the model.
func TestRefinement_DisjointConjunctsStayAComposition(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		schema string
		warns  int
	}{
		"two kinds":        {"{allOf: [{type: integer}, {type: string}]}", 2},
		"a null alone":     {"{allOf: [{type: string, nullable: true}, {type: integer, nullable: true}]}", 2},
		"two value sets":   {"{allOf: [{type: string, enum: [a]}, {type: string, enum: [b]}]}", 2},
		"an object beside": {"{allOf: [{$ref: '#/components/schemas/Owner'}, {type: string}]}", 1},
		"an enum and type": {"{allOf: [{enum: [a]}, {type: integer}]}", 1},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			doc, diags := parseFull(t, openapitest.ComponentSpecVer("3.0.3", refinementTargets+"    Disj: "+tc.schema+"\n"))
			m, ok := typeByName(doc, "Disj").(*ir.Model)
			require.True(t, ok, "no value satisfies it, so nothing refines: got %T", typeByName(doc, "Disj"))
			assert.NotEmpty(t, m.Unmodeled, "each branch is kept")
			assert.Equal(t, tc.warns, openapitest.CountDiagsAt(diags, diag.DegradedConstruct, ir.SeverityWarning))
		})
	}
}

// TestRefinement_NamesItsTargetAtEveryPosition pins a refinement that writes
// nothing beside its $ref but what the target already says: every position
// names the target, with no node and no diagnostic.
func TestRefinement_NamesItsTargetAtEveryPosition(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		schema string
		target string
	}{
		"bare":              {"{allOf: [{$ref: '#/components/schemas/Str'}]}", "Str"},
		"a restated type":   {"{allOf: [{$ref: '#/components/schemas/Port'}], type: integer}", "Port"},
		"a branch restates": {"{allOf: [{$ref: '#/components/schemas/Str'}, {type: string}]}", "Str"},
		"an enum restates":  {"{allOf: [{$ref: '#/components/schemas/Mode'}, {enum: [get, set]}]}", "Mode"},
		"an enum first":     {"{allOf: [{enum: [get, set]}, {$ref: '#/components/schemas/Mode'}]}", "Mode"},
		"a wider enum":      {"{allOf: [{$ref: '#/components/schemas/Mode'}, {enum: [get, set, put]}]}", "Mode"},
		"a wider type":      {"{allOf: [{$ref: '#/components/schemas/Port'}, {type: number}]}", "Port"},
		"restated items": {
			"{allOf: [{$ref: '#/components/schemas/Names'}, {type: array, items: {type: string}}]}", "Names",
		},
		"a type leaving out null": {"{allOf: [{$ref: '#/components/schemas/Maybe'}, {type: string}]}", "Maybe"},
		"a oneOf of no object":    {"{allOf: [{$ref: '#/components/schemas/OneOf'}]}", "OneOf"},
		"an anyOf of no object":   {"{allOf: [{$ref: '#/components/schemas/AnyOf'}]}", "AnyOf"},
		"a restated format":       {"{allOf: [{$ref: '#/components/schemas/Mail'}, {type: string, format: email}]}", "Mail"},
		"a wider format":          {"{allOf: [{$ref: '#/components/schemas/Int32'}, {type: integer, format: int64}]}", "Int32"},
		"a format a conjunct of the target says": {
			"{allOf: [{$ref: '#/components/schemas/MailToo'}, {format: email}]}", "MailToo",
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			targets := refinementTargets + "    Maybe: {type: string, nullable: true}\n"
			doc, diags := parseFull(t, positionsDoc(targets, tc.schema))
			noDegradation(t, diags)
			carriers, nodes := positionRefs(t, doc)
			for position, ref := range carriers {
				assert.Equal(t, componentID(tc.target), ref.Target, position)
			}
			for position, ref := range nodes {
				assert.Equal(t, componentID(tc.target), ref.Target, position)
			}
			named, ok := typeByName(doc, "Named").(*ir.Scalar)
			require.True(t, ok, "a component keeps its name on an alias")
			require.NotNil(t, named.Base)
			assert.Equal(t, componentID(tc.target), named.Base.Target)
		})
	}
}

// TestRefinement_KeywordsLandAtEveryPosition pins where what a refinement says
// lands: on the carrier that holds the position, else on an alias it hoists,
// whether it is written beside the allOf or in a branch.
func TestRefinement_KeywordsLandAtEveryPosition(t *testing.T) {
	t.Parallel()
	for name, written := range map[string]string{
		"beside": "{allOf: [{$ref: '#/components/schemas/Str'}], description: mine, minLength: 2}",
		"branch": "{allOf: [{$ref: '#/components/schemas/Str'}, {description: mine, minLength: 2}]}",
		"split":  "{allOf: [{$ref: '#/components/schemas/Str'}, {minLength: 2}], description: mine}",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			doc, diags := parseFull(t, positionsDoc(refinementTargets, written))
			noDegradation(t, diags)
			carriers, nodes := positionRefs(t, doc)
			for position, ref := range carriers {
				assert.Equal(t, componentID("Str"), ref.Target, position)
			}
			for position, ref := range nodes {
				alias := aliasAt(t, doc, ref)
				assert.Equal(t, componentID("Str"), alias.Base.Target, position)
				assert.Equal(t, "mine", alias.Docs.Description, position)
				require.NotNil(t, alias.Constraints, position)
				require.NotNil(t, alias.Constraints.MinLength, position)
				assert.Equal(t, int64(2), *alias.Constraints.MinLength, position)
				assert.Empty(t, alias.Unmodeled, position)
			}
			p := propertyOf(t, doc, "Holder", "f")
			assert.Equal(t, "mine", p.Docs.Description)
			require.NotNil(t, p.Constraints)
			require.NotNil(t, p.Constraints.MinLength)
			assert.Equal(t, int64(2), *p.Constraints.MinLength)
			op := openapitest.FindOp(t, doc, "op")
			param, ok := openapitest.ParamBySource(op, "id")
			require.True(t, ok)
			assert.Equal(t, "mine", param.Docs.Description)
			require.NotNil(t, param.Constraints)
			assert.Equal(t, "mine", op.Responses[0].Headers[0].Docs.Description)
			named, ok := typeByName(doc, "Named").(*ir.Scalar)
			require.True(t, ok)
			assert.Equal(t, "mine", named.Docs.Description)
			require.NotNil(t, named.Constraints)
		})
	}
}

// TestRefinement_KeptKeywordsLandAtEveryPosition pins what no reference can
// hold: a narrower keyword beside the allOf or in a branch, kept verbatim where
// the position keeps what it writes, with one report.
func TestRefinement_KeptKeywordsLandAtEveryPosition(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		schema string
		key    string
		report string
	}{
		"a narrower type beside": {
			"{allOf: [{$ref: '#/components/schemas/Port'}], type: integer, format: int32}",
			"openapi:format", "no home for format declared beside it",
		},
		"a format in a branch": {
			"{allOf: [{$ref: '#/components/schemas/Port'}, {type: integer, format: int32}]}",
			"openapi:allOf/1", "the branch (format)",
		},
		"a type narrowing a number": {
			"{allOf: [{$ref: '#/components/schemas/Num'}, {type: integer}]}",
			"openapi:allOf/1", "the branch (type)",
		},
		"items narrowing a list": {
			"{allOf: [{$ref: '#/components/schemas/Names'}, {type: array, items: {type: string, minLength: 1}}]}",
			"openapi:allOf/1", "the branch (items)",
		},
		"a collection bound": {
			"{allOf: [{$ref: '#/components/schemas/Names'}, {minItems: 1}]}",
			"openapi:allOf/1", "the branch (minItems)",
		},
		"an overlapping enum": {
			"{allOf: [{$ref: '#/components/schemas/Mode'}, {enum: [set, put]}]}",
			"openapi:allOf/1", "the branch (enum)",
		},
		"an unknown keyword": {
			"{allOf: [{$ref: '#/components/schemas/Str'}, {fancy: 1}]}",
			"openapi:allOf/1", "the branch (fancy)",
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			targets := refinementTargets + "    Num: {type: number}\n"
			doc, diags := parseFull(t, positionsDoc(targets, tc.schema))
			openapitest.RequireNoErrorDiags(t, diags)
			held := func(where string, u ir.Unmodeled) {
				t.Helper()
				entry, ok := u[tc.key]
				if assert.True(t, ok, "%s keeps %s: got %v", where, tc.key, u) {
					assert.Equal(t, ir.ReasonDegradedLowering, entry.Reason, where)
				}
			}
			op := openapitest.FindOp(t, doc, "op")
			param, ok := openapitest.ParamBySource(op, "id")
			require.True(t, ok)
			held("a property", propertyOf(t, doc, "Holder", "f").Unmodeled)
			held("a parameter", param.Unmodeled)
			held("a header", op.Responses[0].Headers[0].Unmodeled)
			_, nodes := positionRefs(t, doc)
			for position, ref := range nodes {
				held(position, aliasAt(t, doc, ref).Unmodeled)
			}
			named, ok := typeByName(doc, "Named").(*ir.Scalar)
			require.True(t, ok)
			held("a component", named.Unmodeled)
			for _, target := range []string{"Port", "Num", "Names", "Mode", "Str"} {
				assert.NotContains(t, typeByName(doc, target).Common().Unmodeled, tc.key, "%s is shared", target)
			}
			var reports []string
			for _, d := range withCode(diags, diag.DegradedConstruct) {
				reports = append(reports, d.Message)
				assert.Contains(t, d.Message, tc.report)
				assert.Equal(t, ir.SeverityInfo, d.Severity)
			}
			assert.NotEmpty(t, reports)
		})
	}
}

// TestRefinement_ASecondRefStaysAComposition pins that no reference is ever
// dropped for another: two of them keep composing, each typed, whatever they
// admit (docs/ir/models.md#composition-entries).
func TestRefinement_ASecondRefStaysAComposition(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, openapitest.ComponentSpecVer("3.0.3", refinementTargets+
		"    Two: {allOf: [{$ref: '#/components/schemas/Str'}, {$ref: '#/components/schemas/Mode'}]}\n"))
	openapitest.RequireNoErrorDiags(t, diags)
	m, ok := typeByName(doc, "Two").(*ir.Model)
	require.True(t, ok, "got %T", typeByName(doc, "Two"))
	assert.Len(t, m.Mixins, 2)
}

// TestRefinement_ReachesNoFurtherThanItCanRead pins each input the reading
// declines: all of them stay the composition they were, with nothing dropped.
func TestRefinement_ReachesNoFurtherThanItCanRead(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		schemas string
		errs    bool
	}{
		"an object target": {schemas: "    S: {allOf: [{$ref: '#/components/schemas/Owner'}]}\n"},
		"a target saying nothing": {
			schemas: "    Any: {}\n    S: {allOf: [{$ref: '#/components/schemas/Any'}]}\n",
		},
		"a target with properties alone": {
			schemas: "    Bare: {properties: {a: {type: string}}}\n    S: {allOf: [{$ref: '#/components/schemas/Bare'}]}\n",
		},
		"a type beside a target with properties alone": {
			schemas: "    Bare: {properties: {a: {type: string}}}\n" +
				"    S: {allOf: [{$ref: '#/components/schemas/Bare'}, {type: string}]}\n",
		},
		"a target that may be an object": {
			schemas: "    Mixed: {type: [string, object]}\n    S: {allOf: [{$ref: '#/components/schemas/Mixed'}]}\n",
		},
		"a union that may be an object": {
			schemas: "    U: {oneOf: [{type: string}, {type: object}]}\n    S: {allOf: [{$ref: '#/components/schemas/U'}]}\n",
		},
		"an object value": {schemas: "    S: {allOf: [{enum: [{a: 1}]}]}\n"},
		"an object const": {schemas: "    S: {allOf: [{const: {a: 1}}]}\n"},
		"a true target":   {schemas: "    Yes: true\n    S: {allOf: [{$ref: '#/components/schemas/Yes'}]}\n"},
		"a false target": {
			schemas: "    No: false\n    S: {allOf: [{$ref: '#/components/schemas/No'}, {type: string}]}\n",
		},
		"a type no schema defines": {schemas: "    S: {allOf: [{type: file}]}\n", errs: true},
		"a false branch":           {schemas: "    S: {allOf: [{type: string}, false]}\n"},
		"a cycle":                  {schemas: "    S: {allOf: [{$ref: '#/components/schemas/S'}], type: string}\n"},
		"a cycle of two": {
			schemas: "    S: {allOf: [{$ref: '#/components/schemas/R'}]}\n    R: {allOf: [{$ref: '#/components/schemas/S'}]}\n",
		},
		"an unresolved $ref": {schemas: "    S: {allOf: [{$ref: '#/components/schemas/Gone'}, {type: string}]}\n", errs: true},
		"a union on a branch": {
			schemas: "    S: {allOf: [{$ref: '#/components/schemas/Str'}, {oneOf: [{minLength: 1}, {maxLength: 2}]}]}\n",
		},
		"a union beside": {
			schemas: "    S: {allOf: [{$ref: '#/components/schemas/Str'}], oneOf: [{minLength: 1}, {maxLength: 2}]}\n",
		},
		"properties in a branch": {schemas: "    S: {allOf: [{type: string, properties: {a: {type: string}}}]}\n"},
		"required beside":        {schemas: "    S: {allOf: [{$ref: '#/components/schemas/Str'}], required: [a]}\n"},
		"a target narrowing an object": {
			schemas: "    S: {allOf: [{$ref: '#/components/schemas/Owner'}, {type: object}]}\n",
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			doc, diags := parseFull(t, openapitest.ComponentSpecVer("3.1.0", refinementTargets+tc.schemas))
			_, failed := ir.FirstError(diags)
			assert.Equal(t, tc.errs, failed, "%+v", diags)
			_, ok := typeByName(doc, "S").(*ir.Model)
			assert.True(t, ok, "got %T", typeByName(doc, "S"))
		})
	}
}

// TestRefinement_NothingSaidIsTheTopType pins an allOf with no conjunct saying
// what a value is: nothing makes it an object, so it is the top type a schema
// saying as little is, and what its branches do say lands beside it.
func TestRefinement_NothingSaidIsTheTopType(t *testing.T) {
	t.Parallel()
	for name, written := range map[string]string{
		"an annotation":   "{allOf: [{description: mine}]}",
		"a bound":         "{allOf: [{minLength: 1}]}",
		"a true branch":   "{allOf: [true, {description: mine}]}",
		"an empty branch": "{allOf: [{}, {description: mine}]}",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			doc, diags := parseFull(t, openapitest.ComponentSpecVer("3.1.0", "    S: "+written+"\n"))
			assert.Empty(t, diags)
			alias, ok := typeByName(doc, "S").(*ir.Scalar)
			require.True(t, ok, "got %T", typeByName(doc, "S"))
			require.NotNil(t, alias.Base)
			_, ok = doc.Types[alias.Base.Target].(*ir.Primitive)
			assert.True(t, ok, "over the top type")
		})
	}
}

// TestRefinement_ATypeBesideTheAllOfIsThePosition pins an allOf whose only
// statement of a kind is the type beside it: that type decides what the
// position lowers to, as if no allOf were written, and each branch folds.
func TestRefinement_ATypeBesideTheAllOfIsThePosition(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, openapitest.ComponentSpecVer("3.0.3", `    Holder:
      type: object
      properties:
        plain: {type: string, allOf: [{maxLength: 3}, {description: mine}]}
        hoisted: {type: string, format: email, allOf: [{maxLength: 3}]}
        list: {type: array, minItems: 1, items: {type: string}, allOf: [{maxLength: 3, description: mine}]}
    Own: {type: string, format: email, allOf: [{maxLength: 3}]}
    Bare: {type: array, items: {type: string}, allOf: [{maxLength: 3, description: mine}]}
`))
	openapitest.RequireNoErrorDiags(t, diags)
	plain := propertyOf(t, doc, "Holder", "plain")
	assert.Equal(t, "mine", plain.Docs.Description)
	require.NotNil(t, plain.Constraints)
	_, ok := doc.Types[plain.Type.Target].(*ir.Primitive)
	assert.True(t, ok, "the shared primitive")

	hoisted := propertyOf(t, doc, "Holder", "hoisted")
	sc, ok := doc.Types[hoisted.Type.Target].(*ir.Scalar)
	require.True(t, ok)
	require.NotNil(t, sc.Encoding)
	assert.Equal(t, "email", sc.Encoding.Name)
	require.NotNil(t, sc.Constraints, "the bound lands on the scalar the position owns")
	require.NotNil(t, sc.Constraints.MaxLength)

	own, ok := typeByName(doc, "Own").(*ir.Scalar)
	require.True(t, ok)
	require.NotNil(t, own.Constraints)

	bare, ok := typeByName(doc, "Bare").(*ir.List)
	require.True(t, ok, "got %T", typeByName(doc, "Bare"))
	assert.Equal(t, "mine", bare.Docs.Description)
	_, kept := bare.Unmodeled["openapi:allOf/0"]
	assert.True(t, kept, "a list holds no value bound, so the branch keeps it: got %v", bare.Unmodeled)
	listed, ok := doc.Types[propertyOf(t, doc, "Holder", "list").Type.Target].(*ir.List)
	require.True(t, ok)
	assert.NotContains(t, listed.Unmodeled, "openapi:minItems", "the list holds what it is written beside")
}

// TestRefinement_AValueSetNarrowsTheRefItBeats is GitHub #1042's position-level
// half: a branch's enum is the position's value set, and the $ref it narrows
// still speaks for the field and stays verbatim.
func TestRefinement_AValueSetNarrowsTheRefItBeats(t *testing.T) {
	t.Parallel()
	const targets = `    Op:
      type: string
      enum: [get, set]
      title: Op title
      description: op doc
      default: set
      example: set
      deprecated: true
      readOnly: true
      externalDocs: {url: 'https://example.com/op'}
    Pw: {type: string, format: password, description: pw doc}
`
	for name, written := range map[string]string{
		"in a branch": "{allOf: [{$ref: '#/components/schemas/Op'}, {enum: [set]}]}",
		"typed":       "{allOf: [{$ref: '#/components/schemas/Op'}, {type: string, enum: [set]}]}",
		"beside":      "{allOf: [{$ref: '#/components/schemas/Op'}], type: string, enum: [set]}",
		"a const":     "{allOf: [{$ref: '#/components/schemas/Op'}, {const: set}]}",
		"own const":   "{allOf: [{$ref: '#/components/schemas/Op'}], const: set}",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			doc, diags := parseFull(t, openapitest.ComponentSpecVer("3.1.0", targets+"    Holder:\n"+
				"      type: object\n      properties:\n        f: "+written+"\n"))
			openapitest.RequireNoErrorDiags(t, diags)
			p := propertyOf(t, doc, "Holder", "f")
			assert.Equal(t, "op doc", firstDocs(doc, p).Description, "the field keeps what the $ref says of it")
			assert.Equal(t, "Op title", firstDocs(doc, p).Summary)
			assert.Len(t, firstDocs(doc, p).ExternalDocs, 1)
			assert.NotEqual(t, componentID("Op"), p.Type.Target, "the value set is what the field holds")
			assert.True(t, p.Deprecation != nil || doc.Types[p.Type.Target].Common().Deprecation != nil,
				"a deprecated target deprecates the field")
			assert.NotEmpty(t, p.Visibility.Only, "and a read-only one keeps it out of a request")
			require.NotNil(t, p.Default, "a member of the narrowed set is the field's default")
			assert.Equal(t, "set", p.Default.Str)
			assert.Empty(t, p.Examples, "a target's example describes the target, never a use of it")
			assert.Empty(t, doc.Types[p.Type.Target].Common().Examples)
			assert.Len(t, doc.Types[componentID("Op")].Common().Examples, 1, "and stays on its node")
		})
	}

	doc, diags := parseFull(t, openapitest.ComponentSpecVer("3.1.0", targets+`    Holder:
      type: object
      properties:
        sibling: {allOf: [{$ref: '#/components/schemas/Op', description: beside}, {enum: [set]}]}
        off: {allOf: [{$ref: '#/components/schemas/Op'}, {enum: [get]}]}
        ownOff: {allOf: [{$ref: '#/components/schemas/Op'}], enum: [get]}
        secret: {allOf: [{$ref: '#/components/schemas/Pw'}, {enum: [a]}]}
`))
	openapitest.RequireNoErrorDiags(t, diags)
	assert.Equal(t, "beside", propertyOf(t, doc, "Holder", "sibling").Docs.Description,
		"what a lost $ref writes beside itself speaks as its target does")
	assert.Nil(t, propertyOf(t, doc, "Holder", "off").Default, "the narrowed set does not take the target's default")
	assert.Nil(t, propertyOf(t, doc, "Holder", "ownOff").Default)
	assert.True(t, propertyOf(t, doc, "Holder", "secret").Secret, "a target's redaction outlives the type")
	off := propertyOf(t, doc, "Holder", "off")
	en, ok := doc.Types[off.Type.Target].(*ir.Enum)
	require.True(t, ok, "got %T", doc.Types[off.Type.Target])
	require.Len(t, en.Members, 1)
	entry, ok := off.Unmodeled["openapi:allOf/0"]
	if assert.True(t, ok, "the $ref stays verbatim: got %v", off.Unmodeled) {
		assert.Contains(t, string(entry.Value), "#/components/schemas/Op")
		assert.Equal(t, ir.ReasonDegradedLowering, entry.Reason)
	}
	assert.Contains(t, openapitest.DiagMessageAt(t, diags, diag.DegradedConstruct, ir.SeverityInfo,
		"/components/schemas/Holder/properties/off/allOf/0"), "narrow")
}

// firstDocs is the documentation the field reads: the property's, else its
// node's where the position owns one.
func firstDocs(doc *ir.Document, p ir.Property) ir.Docs {
	if p.Docs.Description != "" {
		return p.Docs
	}
	return doc.Types[p.Type.Target].Common().Docs
}

// TestRefinement_ANarrowingKeepsItsTargetOnANode pins the narrowing at a
// position no carrier holds: the alias holding the $ref's verbatim branch holds
// what the $ref says of the position too.
func TestRefinement_ANarrowingKeepsItsTargetOnANode(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, positionsDoc(refinementTargets, "{allOf: [{$ref: '#/components/schemas/Mode'}, {enum: [set]}]}"))
	openapitest.RequireNoErrorDiags(t, diags)
	_, nodes := positionRefs(t, doc)
	for position, ref := range nodes {
		alias := aliasAt(t, doc, ref)
		assert.Equal(t, "mode doc", alias.Docs.Description, position)
		_, ok := alias.Unmodeled["openapi:allOf/0"]
		assert.True(t, ok, position)
		en, ok := doc.Types[alias.Base.Target].(*ir.Enum)
		require.True(t, ok, position)
		assert.Len(t, en.Members, 1, position)
	}
}

// TestRefinement_ANarrowedRefStillLowersItsTarget pins what a kept $ref names: a
// position no component holds is lowered by a reference alone, and a narrowing
// keeps the reference, so the target's node stands as it does beneath a Base.
func TestRefinement_ANarrowedRefStillLowersItsTarget(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, openapitest.ComponentSpecVer("3.1.0", `    Host:
      type: object
      $defs:
        Mode: {type: string, enum: [a, b], example: b}
    Narrow: {allOf: [{$ref: '#/components/schemas/Host/$defs/Mode'}, {enum: [a]}]}
`))
	openapitest.RequireNoErrorDiags(t, diags)
	en, ok := doc.Types["t/anon/components/schemas/Host/$defs/Mode"].(*ir.Enum)
	require.True(t, ok, "nothing but the kept $ref names the target")
	assert.Len(t, en.Members, 2)
	assert.NotEmpty(t, en.Examples, "the target's examples stay on its node")
}

// TestRefinement_ACarrierReadsThroughARefinement pins GitHub #1029's sibling: a
// $ref to a component refining another reaches what the second one says, as a
// chain of $refs does, and one hop further where it refines in turn.
func TestRefinement_ACarrierReadsThroughARefinement(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, openapitest.ComponentSpecVer("3.0.3", `    Identifier: {type: string, readOnly: true, description: the identifier, deprecated: true}
    ScanID: {description: a scan, example: x, allOf: [{$ref: '#/components/schemas/Identifier'}]}
    Nested: {allOf: [{$ref: '#/components/schemas/ScanID'}, {title: nested}]}
    Lent: {allOf: [{$ref: '#/components/schemas/Identifier'}, {default: lent, description: lent doc}]}
    Beside: {allOf: [{$ref: '#/components/schemas/Identifier', description: written beside}]}
    Holder:
      type: object
      properties:
        id: {$ref: '#/components/schemas/ScanID'}
        nested: {$ref: '#/components/schemas/Nested'}
        lent: {$ref: '#/components/schemas/Lent'}
        beside: {$ref: '#/components/schemas/Beside'}
        direct: {allOf: [{$ref: '#/components/schemas/ScanID'}], example: y}
`))
	openapitest.RequireNoErrorDiags(t, diags)
	id := propertyOf(t, doc, "Holder", "id")
	assert.Equal(t, []ir.Lifecycle{ir.LifecycleRead, ir.LifecycleDelete, ir.LifecycleQuery}, id.Visibility.Only,
		"readOnly sits one refinement past the $ref")
	assert.Equal(t, "a scan", id.Docs.Description, "the nearer schema wins")
	assert.NotNil(t, id.Deprecation)

	nested := propertyOf(t, doc, "Holder", "nested")
	assert.NotEmpty(t, nested.Visibility.Only, "two refinements past it")
	assert.Equal(t, "nested", nested.Docs.Summary, "a target's branch counts as the target does")
	assert.Equal(t, "a scan", nested.Docs.Description)

	beside := propertyOf(t, doc, "Holder", "beside")
	assert.Equal(t, "written beside", beside.Docs.Description, "what a $ref branch writes speaks too")

	lent := propertyOf(t, doc, "Holder", "lent")
	assert.Equal(t, "lent doc", lent.Docs.Description)
	require.NotNil(t, lent.Default, "what a target's branch writes it lends a use")

	direct := propertyOf(t, doc, "Holder", "direct")
	assert.NotEmpty(t, direct.Visibility.Only)
	assert.Equal(t, "a scan", direct.Docs.Description)
}

// TestRefinement_AHoistingFormatNarrowsItsKind pins the judge in both orders: a
// format a Scalar hoists for is narrower than its bare kind, and so are items
// than none, as a field redeclared the same two ways is
// (docs/compilers/openapi.md#allof-redeclared-property).
func TestRefinement_AHoistingFormatNarrowsItsKind(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		a, b string
		want func(t *testing.T, doc *ir.Document, id ir.TypeID)
	}{
		"email over string": {"{type: string}", "{type: string, format: email}",
			func(t *testing.T, doc *ir.Document, id ir.TypeID) {
				t.Helper()
				sc, ok := doc.Types[id].(*ir.Scalar)
				require.True(t, ok, "%T", doc.Types[id])
				require.NotNil(t, sc.Encoding)
				assert.Equal(t, "email", sc.Encoding.Name)
			}},
		"byte over string": {"{type: string}", "{type: string, format: byte}",
			func(t *testing.T, doc *ir.Document, id ir.TypeID) {
				t.Helper()
				sc, ok := doc.Types[id].(*ir.Scalar)
				require.True(t, ok, "%T", doc.Types[id])
				require.NotNil(t, sc.Encoding)
				assert.Equal(t, "base64", sc.Encoding.Name)
			}},
		"uuid over string": {"{type: string}", "{type: string, format: uuid}",
			func(t *testing.T, doc *ir.Document, id ir.TypeID) {
				t.Helper()
				prim, ok := doc.Types[id].(*ir.Primitive)
				require.True(t, ok, "%T", doc.Types[id])
				assert.Equal(t, ir.PrimUUID, prim.Prim)
			}},
		"items over none": {"{type: array}", "{type: array, items: {type: string}}",
			func(t *testing.T, doc *ir.Document, id ir.TypeID) {
				t.Helper()
				list, ok := doc.Types[id].(*ir.List)
				require.True(t, ok, "%T", doc.Types[id])
				prim, ok := doc.Types[list.Elem.Target].(*ir.Primitive)
				require.True(t, ok)
				assert.Equal(t, ir.PrimString, prim.Prim)
			}},
		"narrower items": {"{type: array, items: {type: string}}", "{type: array, items: {type: string, format: uuid}}",
			func(t *testing.T, doc *ir.Document, id ir.TypeID) {
				t.Helper()
				list, ok := doc.Types[id].(*ir.List)
				require.True(t, ok, "%T", doc.Types[id])
				prim, ok := doc.Types[list.Elem.Target].(*ir.Primitive)
				require.True(t, ok)
				assert.Equal(t, ir.PrimUUID, prim.Prim)
			}},
		"a subset of an enum": {"{enum: [a, b]}", "{enum: [a]}",
			func(t *testing.T, doc *ir.Document, id ir.TypeID) {
				t.Helper()
				en, ok := doc.Types[id].(*ir.Enum)
				require.True(t, ok, "%T", doc.Types[id])
				assert.Len(t, en.Members, 1)
			}},
		"a redaction names no shape": {"{type: string, format: password}", "{type: string, format: uuid}",
			func(t *testing.T, doc *ir.Document, id ir.TypeID) {
				t.Helper()
				prim, ok := doc.Types[id].(*ir.Primitive)
				require.True(t, ok, "%T", doc.Types[id])
				assert.Equal(t, ir.PrimUUID, prim.Prim)
			}},
		"a type array's member": {"{type: [string, integer]}", "{type: string, format: email}",
			func(t *testing.T, doc *ir.Document, id ir.TypeID) {
				t.Helper()
				_, ok := doc.Types[id].(*ir.Scalar)
				assert.True(t, ok, "%T", doc.Types[id])
			}},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			for _, branches := range [][2]string{{tc.a, tc.b}, {tc.b, tc.a}} {
				doc, diags := parseFull(t, openapitest.ComponentSpecVer("3.1.0", "    Holder:\n      type: object\n"+
					"      properties:\n        f: {allOf: ["+branches[0]+", "+branches[1]+"]}\n"))
				assert.Empty(t, withCode(diags, diag.DegradedConstruct), branches)
				p := propertyOf(t, doc, "Holder", "f")
				tc.want(t, doc, p.Type.Target)
				assert.Empty(t, p.Unmodeled, "the wider branch says nothing the narrower does not")
			}
		})
	}
}

// TestRefinement_AValueComparesAsAnEnumEntryDoes pins a value set read by what its
// members are, not how they are spelled: JSON Schema holds 1 and 1.0 to be one
// value, so neither what a set narrows nor a default it takes turns on the
// spelling, and two lists of them share their member.
func TestRefinement_AValueComparesAsAnEnumEntryDoes(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, openapitest.ComponentSpecVer("3.0.3", `    Level: {type: integer, enum: [1, 2], default: 1}
    Whole: {type: number, default: 1.0}
    Narrowed: {allOf: [{$ref: '#/components/schemas/Level'}, {enum: [1.0]}]}
    Meet: {allOf: [{type: number, enum: [1.0, 2.0]}, {type: integer, enum: [1, 2]}]}
    Lists: {allOf: [{enum: [[1.0, 2]]}, {enum: [[1, 2.0], [3]]}]}
    Exponent: {allOf: [{enum: [15.0e-1]}, {enum: [1.5]}]}
    Holder:
      type: object
      properties:
        level: {allOf: [{$ref: '#/components/schemas/Level'}, {enum: [1.0]}]}
        whole: {allOf: [{$ref: '#/components/schemas/Whole'}, {enum: [1]}]}
`))
	openapitest.RequireNoErrorDiags(t, diags)
	for _, name := range []string{"Narrowed", "Meet", "Lists", "Exponent"} {
		assert.NotEqual(t, ir.KindModel, typeByName(doc, name).Kind(), name)
	}
	for _, wire := range []string{"level", "whole"} {
		p := propertyOf(t, doc, "Holder", wire)
		assert.IsType(t, &ir.Enum{}, doc.Types[p.Type.Target], wire)
		assert.NotNil(t, p.Default, "%s: the set takes the $ref's default", wire)
	}
}

// TestRefinement_ATieTakesTheFirstAndKeepsTheOther pins the one place order
// decides: two kinds neither of which holds the other, of which the first
// lowers and the other stays verbatim with a report.
func TestRefinement_ATieTakesTheFirstAndKeepsTheOther(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		schema string
		key    string
		report string
	}{
		"two hoisting formats": {
			"{allOf: [{type: string, format: email}, {type: string, format: hostname}]}",
			"openapi:allOf/1", "the branch (format)",
		},
		"two table formats": {
			"{allOf: [{type: string, format: date}, {type: string, format: uuid}]}",
			"openapi:allOf/1", "the branch (format)",
		},
		"a format and a bare wider one": {
			"{allOf: [{type: integer, format: int32}, {type: integer, format: uint32}]}",
			"openapi:allOf/1", "the branch (format)",
		},
		"an overlap of value sets": {
			"{allOf: [{enum: [a, b]}, {enum: [b, c]}]}",
			"openapi:allOf/1", "the branch (enum)",
		},
		"two element types": {
			"{allOf: [{type: array, items: {type: string}}, {type: array, items: {type: integer}}]}",
			"openapi:allOf/1", "the branch (items)",
		},
		"beside the allOf": {
			"{type: string, format: email, allOf: [{type: string, format: hostname}]}",
			"openapi:allOf/0", "the branch (format)",
		},
		"a value set reaching past a type": {
			"{allOf: [{type: string}, {enum: [a, 1]}]}",
			"openapi:allOf/1", "the branch (enum)",
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			doc, diags := parseFull(t, openapitest.ComponentSpecVer("3.1.0", "    Holder:\n      type: object\n"+
				"      properties:\n        f: "+tc.schema+"\n"))
			openapitest.RequireNoErrorDiags(t, diags)
			p := propertyOf(t, doc, "Holder", "f")
			held := p.Unmodeled
			if owned, ok := doc.Types[p.Type.Target]; ok && len(held) == 0 {
				held = owned.Common().Unmodeled
			}
			_, ok := held[tc.key]
			assert.True(t, ok, "%s is kept: got %v", tc.key, held)
			assert.NotEqual(t, ir.PrimTypeID(ir.PrimAny), p.Type.Target, "the first conjunct lowers")
			var said string
			for _, d := range withCode(diags, diag.DegradedConstruct) {
				said += d.Message
			}
			assert.Contains(t, said, tc.report)
		})
	}
}

// TestRefinement_DecidedByTheSetInEveryOrder pins that the branches' order and
// where a target is declared decide nothing, by two whole documents.
func TestRefinement_DecidedByTheSetInEveryOrder(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct{ a, b string }{
		"annotations":              {"{description: mine}", "{$ref: '#/components/schemas/Str'}"},
		"a narrowing":              {"{enum: [a]}", "{$ref: '#/components/schemas/Str'}"},
		"a meet":                   {"{type: integer}", "{type: integer, format: int32}"},
		"two voices":               {"{description: mine}", "{minLength: 1}"},
		"a kept one":               {"{$ref: '#/components/schemas/Port'}", "{format: int32}"},
		"a format with no kind":    {"{type: integer}", "{format: int32}"},
		"items with no kind":       {"{type: array}", "{items: {type: string}}"},
		"a value set with no kind": {"{enum: [a]}", "{format: email}"},
		"an agreement": {
			"{description: same, $ref: '#/components/schemas/Str'}", "{description: same}",
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			compile := func(first, second, tail string) (*ir.Document, []string) {
				doc, diags := parseFull(t, openapitest.ComponentSpecVer("3.1.0",
					"    Holder:\n      type: object\n      properties:\n"+
						"        f: {allOf: ["+first+", "+second+"]}\n"+tail))
				out := reported(diags)
				for i, r := range out {
					out[i] = strings.NewReplacer("allOf/0", "allOf/N", "allOf/1", "allOf/N").Replace(r)
				}
				return doc, out
			}
			after, reportsA := compile(tc.a, tc.b, refinementTargets)
			before, reportsB := compile(tc.b, tc.a, refinementTargets)
			if diff := cmp.Diff(typeShapes(after), typeShapes(before)); diff != "" {
				t.Errorf("the branches' order decided the node (-a +b):\n%s", diff)
			}
			assert.ElementsMatch(t, reportsA, reportsB)

			first, _ := parseFull(t, openapitest.ComponentSpecVer("3.1.0", refinementTargets+
				"    Holder:\n      type: object\n      properties:\n        f: {allOf: ["+tc.a+", "+tc.b+"]}\n"))
			if diff := cmp.Diff(after.Types, first.Types, orderInvariantIR()...); diff != "" {
				t.Errorf("the declaration order decided the document (-after +before):\n%s", diff)
			}
		})
	}
}

// TestRefinement_ANoKindConjunctIsNeverThePrincipal pins that a conjunct naming
// no kind cannot decide what the position is: its `format` or `items` qualifies a
// kind, so it stays beside the conjunct that names one, in either order, and the
// position is no top type.
func TestRefinement_ANoKindConjunctIsNeverThePrincipal(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		kinded, unkinded string
		want             ir.TypeID
	}{
		"format": {"{type: integer}", "{format: int32}", "t/prim/integer"},
		"items":  {"{type: string}", "{items: {type: string}}", "t/prim/string"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			for _, branches := range []string{tc.kinded + ", " + tc.unkinded, tc.unkinded + ", " + tc.kinded} {
				doc, diags := parseFull(t, openapitest.ComponentSpec("    T: {allOf: ["+branches+"]}\n"))
				alias, ok := doc.Types[componentID("T")].(*ir.Scalar)
				require.True(t, ok, "%s: %#v", branches, doc.Types[componentID("T")])
				assert.Equal(t, tc.want, alias.Base.Target, branches)
				assert.Len(t, alias.Unmodeled, 1, "the conjunct naming no kind is kept: %s", branches)
				assert.Len(t, diags, 1, "and reported once: %s", branches)
			}
		})
	}
}

// TestRefinement_BooleanItemsReadAsWhatTheyAdmit pins `items: true`, which says
// nothing, so a list saying more beats it, and `items: false`, which admits no
// element, so it beats a list saying less. Each element type stands in either
// order, and the other branch keeps nothing, since nothing it says is left unsaid.
func TestRefinement_BooleanItemsReadAsWhatTheyAdmit(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		list, other string
		elem        ir.TypeKind
	}{
		"true":            {"{type: array, items: {type: string}}", "{type: array, items: true}", ir.KindPrimitive},
		"false":           {"{type: array, items: {type: string}}", "{type: array, items: false}", ir.KindModel},
		"false, a format": {"{type: array, items: {type: string, format: uuid}}", "{type: array, items: false}", ir.KindModel},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			for _, branches := range []string{tc.list + ", " + tc.other, tc.other + ", " + tc.list} {
				doc, _ := parseFull(t, openapitest.ComponentSpecVer("3.1.0", "    T: {allOf: ["+branches+"]}\n"))
				alias, ok := doc.Types[componentID("T")].(*ir.Scalar)
				require.True(t, ok, "%s: %#v", branches, doc.Types[componentID("T")])
				list, ok := doc.Types[alias.Base.Target].(*ir.List)
				require.True(t, ok, "%s: %#v", branches, doc.Types[alias.Base.Target])
				assert.Equal(t, tc.elem, doc.Types[list.Elem.Target].Kind(), branches)
				assert.NotEqual(t, ir.TypeID("t/prim/any"), list.Elem.Target, branches)
				assert.Empty(t, alias.Unmodeled, branches)
			}
		})
	}
}

// TestRefinement_NoConjunctNamingAKindLeavesTheTopType pins the same rule where
// nothing names a kind: no conjunct decides, so the position admits what the
// keywords do and the branch stays whole.
func TestRefinement_NoConjunctNamingAKindLeavesTheTopType(t *testing.T) {
	t.Parallel()
	doc, _ := parseFull(t, openapitest.ComponentSpec("    T: {allOf: [{format: int32}]}\n"))
	alias, ok := doc.Types[componentID("T")].(*ir.Scalar)
	require.True(t, ok, "%#v", doc.Types[componentID("T")])
	assert.Equal(t, ir.TypeID("t/prim/any"), alias.Base.Target)
	assert.Contains(t, alias.Unmodeled, "openapi:allOf/0")
}

// typeShapes renders what the first property of Holder says and the kind of what
// it refers to, every position of an allOf branch spelled alike, so two orders of
// one list compare equal. Which branch of two orders holds a kept keyword differs
// by its index alone, so only the count of them is compared.
func typeShapes(doc *ir.Document) string {
	m := doc.Types[componentID("Holder")].(*ir.Model)
	p := m.Properties[0]
	td := doc.Types[p.Type.Target]
	rendered, err := json.Marshal(struct {
		Docs        ir.Docs
		Constraints *ir.Constraints
		Nullable    bool
		Visibility  ir.Visibility
		Kept        int
		Kind        string
	}{p.Docs, p.Constraints, p.Type.Nullable, p.Visibility, len(p.Unmodeled), fmt.Sprintf("%T", td)})
	if err != nil {
		return err.Error()
	}
	if _, ok := td.(*ir.Primitive); ok {
		return string(rendered) + string(p.Type.Target)
	}
	return string(rendered)
}

// TestRefinement_TwinsTheRefSpelling holds a 3.0 allOf refining a $ref to the
// 3.1 $ref writing the same keywords beside it: for annotations and value
// bounds the two are one document, at every position. Provenance differs only
// in the pointer a keyword kept in a branch is read at, and a decorator's own
// hint, which no emitter renders, by what docs/compilers/openapi.md#union says.
func TestRefinement_TwinsTheRefSpelling(t *testing.T) {
	t.Parallel()
	keywords := map[string]string{
		"a description":   "description: mine",
		"a title":         "title: mine",
		"an example":      "example: mine",
		"deprecated":      "deprecated: true",
		"a bound":         "maxLength: 3",
		"a pattern":       "pattern: '^a'",
		"an extension":    "x-kept: 1",
		"a redaction":     "format: password",
		"readOnly":        "readOnly: true",
		"writeOnly":       "writeOnly: true",
		"a default":       "default: mine",
		"a negation":      "not: {enum: [x]}",
		"external docs":   "externalDocs: {url: 'https://example.com'}",
		"xml":             "xml: {name: mine}",
		"several at once": "description: mine, maxLength: 3, default: mine",
	}
	options := append(orderInvariantIR(), cmpopts.IgnoreFields(ir.Provenance{}, "Pointer"),
		cmpopts.IgnoreFields(ir.UnmodeledEntry{}, "Provenance"), cmpopts.IgnoreFields(ir.TypeCommon{}, "Name"))
	for name, keyword := range keywords {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			const ref = "$ref: '#/components/schemas/Str'"
			twin, twinDiags := parseFull(t, strings.Replace(positionsDoc(refinementTargets, "{"+ref+", "+keyword+"}"),
				"openapi: 3.0.3", "openapi: 3.1.0", 1))
			for spelling, written := range map[string]string{
				"beside":   "{allOf: [{" + ref + "}], " + keyword + "}",
				"a branch": "{allOf: [{" + ref + "}, {" + keyword + "}]}",
			} {
				doc, diags := parseFull(t, positionsDoc(refinementTargets, written))
				if diff := cmp.Diff(twin.Types, doc.Types, options...); diff != "" {
					t.Errorf("%s: the types differ from the $ref's (-ref +allOf):\n%s", spelling, diff)
				}
				if diff := cmp.Diff(twin.Services, doc.Services, options...); diff != "" {
					t.Errorf("%s: the services differ from the $ref's (-ref +allOf):\n%s", spelling, diff)
				}
				assert.Len(t, withCode(diags, diag.DegradedConstruct), len(withCode(twinDiags, diag.DegradedConstruct)), spelling)
			}
		})
	}
}

// TestRefinement_StructuralKeywordsPartFromTheRefSpelling pins where the twin
// above stops: a structural keyword the reference already says is consumed
// here, and kept beside a 3.1 $ref, which reads no target.
func TestRefinement_StructuralKeywordsPartFromTheRefSpelling(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, openapitest.ComponentSpecVer("3.1.0", refinementTargets+`    Holder:
      type: object
      properties:
        allOf: {allOf: [{$ref: '#/components/schemas/Port'}], type: integer}
        beside: {$ref: '#/components/schemas/Port', type: integer}
`))
	openapitest.RequireNoErrorDiags(t, diags)
	assert.Empty(t, propertyOf(t, doc, "Holder", "allOf").Unmodeled)
	assert.Contains(t, propertyOf(t, doc, "Holder", "beside").Unmodeled, "openapi:type")
}

// TestRefinement_ABranchHoldsWhatItsNodeDoes pins an inline principal: one that
// lowers to a node of its own keeps what it writes there, under its own
// pointer, and one that reduces to a shared primitive speaks at the position.
func TestRefinement_ABranchHoldsWhatItsNodeDoes(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, openapitest.ComponentSpecVer("3.1.0", `    Holder:
      type: object
      properties:
        shared: {allOf: [{type: string, description: mine, x-kept: 1, default: d, readOnly: true}]}
        owned: {allOf: [{type: string, enum: [a], description: mine, x-kept: 1, default: a, readOnly: true, fancy: 1}]}
    Items:
      type: array
      items: {allOf: [{type: string, enum: [a], default: a}]}
`))
	openapitest.RequireNoErrorDiags(t, diags)
	shared := propertyOf(t, doc, "Holder", "shared")
	assert.Equal(t, "mine", shared.Docs.Description)
	assert.Contains(t, shared.Unmodeled, "openapi:x-kept")
	require.NotNil(t, shared.Default)
	assert.NotEmpty(t, shared.Visibility.Only)

	owned := propertyOf(t, doc, "Holder", "owned")
	assert.Equal(t, ir.TypeID("t/anon/components/schemas/Holder/properties/owned/allOf/0"), owned.Type.Target,
		"the node stands at the pointer naming its schema")
	en, ok := doc.Types[owned.Type.Target].(*ir.Enum)
	require.True(t, ok)
	assert.Equal(t, "mine", en.Docs.Description)
	assert.Contains(t, en.Unmodeled, "openapi:x-kept")
	assert.NotContains(t, owned.Unmodeled, "openapi:x-kept", "one home per declaration")
	assert.NotContains(t, owned.Unmodeled, "openapi:allOf/0", "and what its node keeps is no residue")
	assert.Contains(t, en.Unmodeled, "openapi:fancy")
	assert.Equal(t, "owned", en.Name.Hint, "named for the position, not the branch's index")
	require.NotNil(t, owned.Default, "what binds a use reaches the carrier all the same")
	assert.NotEmpty(t, owned.Visibility.Only)

	items, ok := typeByName(doc, "Items").(*ir.List)
	require.True(t, ok)
	elem, ok := doc.Types[items.Elem.Target].(*ir.Enum)
	require.True(t, ok, "got %T", doc.Types[items.Elem.Target])
	assert.Contains(t, elem.Unmodeled, "openapi:default", "and with no carrier its node keeps it")
}

// TestRefinement_ARefBranchKeepsItsSiblings pins a $ref branch writing keywords
// beside itself: the branch hoists its alias, as it does in a composition, and
// a carrier reads them as it reads the target's.
func TestRefinement_ARefBranchKeepsItsSiblings(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, openapitest.ComponentSpecVer("3.1.0", refinementTargets+`    Holder:
      type: object
      properties:
        f: {allOf: [{$ref: '#/components/schemas/Str', description: beside, readOnly: true}]}
`))
	openapitest.RequireNoErrorDiags(t, diags)
	p := propertyOf(t, doc, "Holder", "f")
	alias := aliasAt(t, doc, p.Type)
	assert.Equal(t, componentID("Str"), alias.Base.Target)
	assert.Equal(t, "beside", p.Docs.Description)
	assert.NotEmpty(t, p.Visibility.Only)
}

// TestRefinement_VariantNamedAsItsRef pins a union branch refining a $ref: named
// as the $ref is, in every spelling, and by its position where it holds none.
func TestRefinement_VariantNamedAsItsRef(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, openapitest.ComponentSpecVer("3.0.3", refinementTargets+`    U:
      oneOf:
        - allOf: [{$ref: '#/components/schemas/Str'}]
        - allOf: [{$ref: '#/components/schemas/Port'}, {description: mine}]
        - allOf: [{$ref: '#/components/schemas/Mode'}, {enum: [set]}]
        - allOf: [{type: boolean}]
`))
	openapitest.RequireNoErrorDiags(t, diags)
	u, ok := typeByName(doc, "U").(*ir.Union)
	require.True(t, ok)
	require.Len(t, u.Variants, 4)
	assert.Equal(t, "str", u.Variants[0].Name.Hint)
	assert.Equal(t, "port", u.Variants[1].Name.Hint)
	assert.Equal(t, "mode", u.Variants[2].Name.Hint)
	assert.Equal(t, "variant_3", u.Variants[3].Name.Hint)
}

// TestRefinement_FlattensAsItsFlatSpelling pins that nesting changes nothing: a
// wrapper's entries are conjuncts, and a wrapper writing the type decides as
// one written beside them does.
func TestRefinement_FlattensAsItsFlatSpelling(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, openapitest.ComponentSpecVer("3.1.0", refinementTargets+`    Holder:
      type: object
      properties:
        nested: {allOf: [{allOf: [{$ref: '#/components/schemas/Str'}], description: mine}]}
        typed: {allOf: [{type: string, allOf: [{maxLength: 3}]}, {description: mine}]}
`))
	assert.Empty(t, withCode(diags, diag.DegradedConstruct))
	nested := propertyOf(t, doc, "Holder", "nested")
	assert.Equal(t, componentID("Str"), nested.Type.Target)
	assert.Equal(t, "mine", nested.Docs.Description)

	typed := propertyOf(t, doc, "Holder", "typed")
	prim, ok := doc.Types[typed.Type.Target].(*ir.Primitive)
	require.True(t, ok, "got %T", doc.Types[typed.Type.Target])
	assert.Equal(t, ir.PrimString, prim.Prim)
	require.NotNil(t, typed.Constraints)
	assert.Equal(t, "mine", typed.Docs.Description)
}

// TestRefinement_ANullOnlyBranchIsTheNull pins `type: "null"`: the top type a
// null lowers to, nullable, not the empty object.
func TestRefinement_ANullOnlyBranchIsTheNull(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, openapitest.ComponentSpecVer("3.1.0", `    Holder:
      type: object
      properties:
        f: {allOf: [{type: 'null'}, {type: [string, 'null']}]}
`))
	assert.Empty(t, diags)
	p := propertyOf(t, doc, "Holder", "f")
	assert.True(t, p.Type.Nullable)
	_, ok := doc.Types[p.Type.Target].(*ir.Any)
	if !ok {
		_, ok = doc.Types[p.Type.Target].(*ir.Primitive)
	}
	assert.True(t, ok, "got %T", doc.Types[p.Type.Target])
}

// TestRefinement_MissingItemsIsAskedOfTheBranchThatLowers pins
// docs/compilers/openapi.md#missing-array-items beside a refinement: the branch
// a list lowers from is held to the rule, and one that only restates a $ref's
// type lowers nothing.
func TestRefinement_MissingItemsIsAskedOfTheBranchThatLowers(t *testing.T) {
	t.Parallel()
	_, diags := parseFull(t, openapitest.ComponentSpecVer("3.0.3", refinementTargets+`    Lowers: {allOf: [{type: array}]}
    Restates: {allOf: [{$ref: '#/components/schemas/Names'}, {type: array}]}
`))
	assert.Equal(t, []string{"/components/schemas/Lowers/allOf/0"}, pointerStrings(openapitest.PointersOf(diags, diag.MissingArrayItems)))
}

// pointerStrings renders pointers for a comparison with literals.
func pointerStrings[P ~string](pointers []P) []string {
	out := make([]string, 0, len(pointers))
	for _, p := range pointers {
		out = append(out, string(p))
	}
	return out
}

// TestRefinement_PastItsBudgetStaysAComposition pins the bounds on the reading: a
// target it cannot finish reading leaves the position as it was, and one read to
// the last step refines (docs/compilers/openapi.md#allof-refinement).
func TestRefinement_PastItsBudgetStaysAComposition(t *testing.T) {
	t.Parallel()
	assert.Equal(t, 256, schema.MaxAdmissionReads, "docs/compilers/openapi.md#allof-refinement says so")
	assert.Equal(t, 4096, schema.MaxAdmissionMembers, "docs/compilers/openapi.md#allof-refinement says so")
	assert.Equal(t, 64, schema.MaxRefinedConjuncts, "docs/compilers/openapi.md#allof-refinement says so")
	enum := func(members int) string {
		var b strings.Builder
		b.WriteString("    Big:\n      type: string\n      enum:\n")
		for i := range members {
			fmt.Fprintf(&b, "        - m%d\n", i)
		}
		return b.String() + "    S: {allOf: [{$ref: '#/components/schemas/Big'}]}\n"
	}
	// A link reads the $ref naming it and the schema it lands on.
	chain := func(links int) string {
		var b strings.Builder
		b.WriteString("    S: {allOf: [{$ref: '#/components/schemas/C0'}]}\n")
		for i := range links - 1 {
			fmt.Fprintf(&b, "    C%d: {allOf: [{$ref: '#/components/schemas/C%d'}]}\n", i, i+1)
		}
		fmt.Fprintf(&b, "    C%d: {type: string}\n", links-1)
		return b.String()
	}
	for name, tc := range map[string]struct{ within, past string }{
		"enum members": {enum(schema.MaxAdmissionMembers), enum(schema.MaxAdmissionMembers + 1)},
		"schemas read": {chain(schema.MaxAdmissionReads / 2), chain(schema.MaxAdmissionReads/2 + 1)},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			doc, _ := parseFull(t, openapitest.ComponentSpecVer("3.1.0", tc.within))
			_, ok := typeByName(doc, "S").(*ir.Scalar)
			assert.True(t, ok, "at the bound it refines: got %T", typeByName(doc, "S"))
			doc, _ = parseFull(t, openapitest.ComponentSpecVer("3.1.0", tc.past))
			_, ok = typeByName(doc, "S").(*ir.Model)
			assert.True(t, ok, "past it, it does not: got %T", typeByName(doc, "S"))
		})
	}
}

// TestRefinement_PastItsConjunctBoundStaysAComposition pins the bound on the
// election, which compares every pair of conjuncts.
func TestRefinement_PastItsConjunctBoundStaysAComposition(t *testing.T) {
	t.Parallel()
	branches := func(n int) string {
		return "[" + strings.TrimSuffix(strings.Repeat("{type: string}, ", n), ", ") + "]"
	}
	doc, _ := parseFull(t, openapitest.ComponentSpecVer("3.1.0", "    S: {allOf: "+branches(schema.MaxRefinedConjuncts)+"}\n"))
	_, ok := typeByName(doc, "S").(*ir.Scalar)
	assert.True(t, ok, "at the bound it refines: got %T", typeByName(doc, "S"))
	doc, _ = parseFull(t, openapitest.ComponentSpecVer("3.1.0", "    S: {allOf: "+branches(schema.MaxRefinedConjuncts+1)+"}\n"))
	_, ok = typeByName(doc, "S").(*ir.Model)
	assert.True(t, ok, "past it, it does not: got %T", typeByName(doc, "S"))
}

// TestRefinement_ACappedBranchStaysAComposition pins a wrapper past the
// flattening depth, whose own allOf the reading does not reach.
func TestRefinement_ACappedBranchStaysAComposition(t *testing.T) {
	t.Parallel()
	body := "{type: string}"
	for range schema.MaxAllOfFlattenDepth + 2 {
		body = "{allOf: [" + body + "]}"
	}
	doc, diags := parseFull(t, openapitest.ComponentSpecVer("3.1.0", "    S: "+body+"\n"))
	openapitest.RequireNoErrorDiags(t, diags)
	_, ok := typeByName(doc, "S").(*ir.Model)
	assert.True(t, ok, "got %T", typeByName(doc, "S"))
}

// TestRefinement_AKeptBranchTheIRCannotHoldIsNotAnnounced pins the report to what
// was kept: a branch that does not convert is reported as lost, and nothing says it
// was preserved.
func TestRefinement_AKeptBranchTheIRCannotHoldIsNotAnnounced(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, openapitest.ComponentSpecVer("3.1.0", refinementTargets+`    Holder:
      type: object
      properties:
        f: {allOf: [{$ref: '#/components/schemas/Str'}, {fancy: `+unpreservableValue+`}]}
`))
	assert.NotContains(t, propertyOf(t, doc, "Holder", "f").Unmodeled, "openapi:allOf/1")
	assert.True(t, openapitest.HasDiag(diags, diag.UnpreservableConstruct))
	assert.Empty(t, openapitest.PreservationClaims(diags))
}

// TestRefinement_RawTextIsComparedWithinADocument pins restated items: a $ref in
// another document's copy resolves elsewhere, so the same text there restates
// nothing.
func TestRefinement_RawTextIsComparedWithinADocument(t *testing.T) {
	t.Parallel()
	doc, diags, _ := compileBeside(t, map[string]string{"other.yaml": `openapi: 3.1.0
info: {title: other, version: "1"}
paths: {}
components:
  schemas:
    Names: {type: array, items: {$ref: '#/components/schemas/Item'}}
    Item: {type: integer}
`}, `openapi: 3.1.0
info: {title: t, version: "1"}
paths: {}
components:
  schemas:
    Item: {type: string}
    Holder:
      type: object
      properties:
        f: {allOf: [{$ref: 'other.yaml#/components/schemas/Names'}, {type: array, items: {$ref: '#/components/schemas/Item'}}]}
        same: {allOf: [{$ref: '#/components/schemas/Local'}, {type: array, items: {$ref: '#/components/schemas/Item'}}]}
    Local: {type: array, items: {$ref: '#/components/schemas/Item'}}
`)
	openapitest.RequireNoErrorDiags(t, diags)
	assert.Contains(t, propertyOf(t, doc, "Holder", "f").Unmodeled, "openapi:allOf/1")
	assert.Empty(t, propertyOf(t, doc, "Holder", "same").Unmodeled)
}

// TestRefinement_ADynamicRefIsNotRead pins that a $dynamicRef, on a branch or in
// what a $ref lands on, ends the reading: what it names is settled elsewhere.
func TestRefinement_ADynamicRefIsNotRead(t *testing.T) {
	t.Parallel()
	for name, s := range map[string]string{
		"on a branch": "    S: {allOf: [{$dynamicRef: '#node'}, {type: string}]}\n",
		"in a target": "    S: {allOf: [{$ref: '#/components/schemas/D'}, {type: string}]}\n    D: {$dynamicRef: '#node'}\n",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			doc, _ := parseFull(t, openapitest.ComponentSpecVer("3.1.0",
				"    Node: {$dynamicAnchor: node, type: string}\n"+s))
			_, ok := typeByName(doc, "S").(*ir.Model)
			assert.True(t, ok, "got %T", typeByName(doc, "S"))
		})
	}
}

// TestRefinement_AUnionTargetIsReadByItsBranches pins the reading of a target
// holding a union: a value set each branch fixes is the set, and a branch with no
// value set leaves none.
func TestRefinement_AUnionTargetIsReadByItsBranches(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, openapitest.ComponentSpecVer("3.1.0", `    Holder:
      type: object
      properties:
        anyOfSet: {allOf: [{$ref: '#/components/schemas/Set'}, {enum: [a]}]}
        open: {allOf: [{$ref: '#/components/schemas/Open'}, {enum: [a]}]}
        restated: {allOf: [{$ref: '#/components/schemas/Set'}, {enum: [a, b]}]}
    Set: {anyOf: [{enum: [a]}, {enum: [b]}]}
    Open: {oneOf: [{enum: [a]}, {type: string}]}
`))
	openapitest.RequireNoErrorDiags(t, diags)
	for _, wire := range []string{"anyOfSet", "open"} {
		_, ok := doc.Types[propertyOf(t, doc, "Holder", wire).Type.Target].(*ir.Enum)
		assert.True(t, ok, wire)
	}
	assert.Equal(t, componentID("Set"), propertyOf(t, doc, "Holder", "restated").Type.Target)
}

// TestRefinement_ValuesCompareAsJSON pins a value set of each kind against the
// type beside it.
func TestRefinement_ValuesCompareAsJSON(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		schema string
		model  bool
	}{
		"booleans":                 {schema: "{allOf: [{type: boolean}, {enum: [true]}]}"},
		"integers":                 {schema: "{allOf: [{type: integer}, {enum: [1, 2]}]}"},
		"a fraction of a number":   {schema: "{allOf: [{type: number}, {enum: [1.5]}]}"},
		"an integer with a point":  {schema: "{allOf: [{type: integer}, {enum: [1.0]}]}"},
		"an exponent":              {schema: "{allOf: [{type: integer}, {enum: [1e3]}]}"},
		"a fraction is no integer": {schema: "{allOf: [{type: integer}, {enum: [1.5]}]}", model: true},
		"a null":                   {schema: "{allOf: [{type: [string, 'null']}, {enum: [a, null]}]}"},
		"a list":                   {schema: "{allOf: [{type: array}, {enum: [[1]]}]}"},
		"a fraction is no string":  {schema: "{allOf: [{type: string}, {enum: [1.5]}]}", model: true},
		"a list is no string":      {schema: "{allOf: [{type: string}, {enum: [[1]]}]}", model: true},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			doc, _ := parseFull(t, openapitest.ComponentSpecVer("3.1.0", "    S: "+tc.schema+"\n"))
			_, isModel := typeByName(doc, "S").(*ir.Model)
			assert.Equal(t, tc.model, isModel, "got %T", typeByName(doc, "S"))
		})
	}
}

// TestRefinement_AConstUnreadableStaysAComposition pins a value set no reading can
// convert: an instance it cannot compare is one it cannot answer for.
func TestRefinement_AConstUnreadableStaysAComposition(t *testing.T) {
	t.Parallel()
	doc, _ := parseFull(t, openapitest.ComponentSpecVer("3.1.0",
		"    S: {allOf: [{const: "+unpreservableValue+"}]}\n"))
	_, ok := typeByName(doc, "S").(*ir.Model)
	assert.True(t, ok, "got %T", typeByName(doc, "S"))
}

// TestRefinement_DecidingSchemaIsAskedOfAWholeChain pins the reader of a body's
// shape through two refinements, one a $ref's and one inline, and past an enum.
func TestRefinement_DecidingSchemaIsAskedOfAWholeChain(t *testing.T) {
	t.Parallel()
	l, _ := schema.LoweredFor(t, openapitest.ComponentSpecVer("3.1.0", `    Blob: {type: string, format: binary}
    Inner: {allOf: [{$ref: '#/components/schemas/Blob'}], description: inner}
    Outer: {allOf: [{$ref: '#/components/schemas/Inner'}, {description: outer}]}
    Inline: {allOf: [{type: string, format: binary}, {description: inline}]}
    Bare: {allOf: [{description: says nothing}]}
`))
	schemas := l.Ctx.Doc.Components.GetSchemas()
	for name, format := range map[string]string{"Outer": "binary", "Inline": "binary", "Bare": ""} {
		js, ok := schemas.Get(name)
		require.True(t, ok, name)
		assert.Equal(t, format, schema.DecidingSchema(l.Ctx, js.GetSchema()).GetFormat(), name)
	}
}

// TestRefinement_ItemsItCannotRenderRestateNothing pins the comparison of two
// `items`, one of which holds what no JSON renders: neither restates the other,
// in either order.
func TestRefinement_ItemsItCannotRenderRestateNothing(t *testing.T) {
	t.Parallel()
	bad := "{type: array, items: {type: string, x-t: " + unpreservableValue + "}}"
	good := "{type: array, items: {type: string}}"
	for _, branches := range []string{bad + ", " + good, good + ", " + bad} {
		doc, _ := parseFull(t, openapitest.ComponentSpecVer("3.1.0", "    S: {allOf: ["+branches+"]}\n"))
		_, ok := typeByName(doc, "S").(*ir.Scalar)
		assert.True(t, ok, "got %T", typeByName(doc, "S"))
	}
}

// TestRefinement_AnEmptyAllOfOnABranchIsKept pins a branch writing `allOf: []`:
// nothing flattens it, so its keyword stays, as in a composition.
func TestRefinement_AnEmptyAllOfOnABranchIsKept(t *testing.T) {
	t.Parallel()
	doc, _ := parseFull(t, openapitest.ComponentSpecVer("3.1.0", `    Holder:
      type: object
      properties:
        f: {allOf: [{type: string, allOf: []}]}
`))
	assert.Contains(t, propertyOf(t, doc, "Holder", "f").Unmodeled, "openapi:allOf/0")
}

// TestRefinement_KeywordsBesideARefAreNotConjuncts pins where a refinement stops:
// an allOf beside a $ref, or beside a $dynamicRef, is no refinement, and a branch
// of it speaks for nothing.
func TestRefinement_KeywordsBesideARefAreNotConjuncts(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, openapitest.ComponentSpecVer("3.1.0", refinementTargets+`    Node: {$dynamicAnchor: node, type: string}
    Holder:
      type: object
      properties:
        beside: {$ref: '#/components/schemas/Str', allOf: [{description: branch, default: b}]}
        dynamic: {$dynamicRef: '#node', allOf: [{description: branch}]}
        dynamicRef: {$dynamicRef: '#node', allOf: [{$ref: '#/components/schemas/Bound'}]}
        elected: {enum: [a], allOf: [{description: branch}]}
    Bound: {type: string, readOnly: true}
    Items:
      type: array
      items: {$ref: '#/components/schemas/Str', allOf: [{description: branch}]}
`))
	openapitest.RequireNoErrorDiags(t, diags)
	beside := propertyOf(t, doc, "Holder", "beside")
	assert.Equal(t, "from the target", beside.Docs.Description, "the branch is kept, not read")
	assert.Nil(t, beside.Default)
	assert.Contains(t, beside.Unmodeled, "openapi:allOf")
	assert.Empty(t, propertyOf(t, doc, "Holder", "dynamic").Docs.Description)
	assert.Empty(t, propertyOf(t, doc, "Holder", "dynamicRef").Visibility.Only)
	elected := propertyOf(t, doc, "Holder", "elected")
	assert.Empty(t, doc.Types[elected.Type.Target].Common().Docs.Description, "an election's loser speaks for nothing")
	items, ok := typeByName(doc, "Items").(*ir.List)
	require.True(t, ok)
	assert.Empty(t, doc.Types[items.Elem.Target].Common().Docs.Description, "nor does a branch beside a $ref")
}

// TestRefinement_DeclaredBeforeItsTarget pins a use lowered before what it names:
// the reference needs no node to stand yet, which it would to hold anything.
func TestRefinement_DeclaredBeforeItsTarget(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, openapitest.ComponentSpecVer("3.0.3", `    Items:
      type: array
      items: {allOf: [{$ref: '#/components/schemas/Late'}]}
    Late: {type: string}
`))
	assert.Empty(t, diags)
	items, ok := typeByName(doc, "Items").(*ir.List)
	require.True(t, ok)
	assert.Equal(t, componentID("Late"), items.Elem.Target)
}

// TestRefinement_TheValueSetWithinMostOthersWins pins the election past a value set
// within every other: none is here, and the one within the most others wins, what
// holds it consumed and the one overlapping it kept.
func TestRefinement_TheValueSetWithinMostOthersWins(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, openapitest.ComponentSpecVer("3.1.0", `    Holder:
      type: object
      properties:
        f: {allOf: [{enum: [a, b, c]}, {enum: [a, b]}, {enum: [b, c]}]}
`))
	openapitest.RequireNoErrorDiags(t, diags)
	p := propertyOf(t, doc, "Holder", "f")
	en, ok := doc.Types[p.Type.Target].(*ir.Enum)
	require.True(t, ok, "got %T", doc.Types[p.Type.Target])
	assert.Len(t, en.Members, 2)
	assert.NotContains(t, p.Unmodeled, "openapi:allOf/0", "it holds the narrower set")
	assert.Contains(t, p.Unmodeled, "openapi:allOf/2", "and this one overlaps it")
}

// TestRefinement_ANullBothConjunctsAdmitIsAValue pins the reading of OpenAPI 3.0's
// nullable: it adds null to a type, so a value set of null alone shares a value
// with one, where in 3.1 the keyword reads as nothing and no value is shared.
func TestRefinement_ANullBothConjunctsAdmitIsAValue(t *testing.T) {
	t.Parallel()
	const s = "    S: {allOf: [{type: string, nullable: true}, {enum: [null]}]}\n"
	doc, _ := parseFull(t, openapitest.ComponentSpecVer("3.0.3", s))
	_, isModel := typeByName(doc, "S").(*ir.Model)
	assert.False(t, isModel, "3.0: got %T", typeByName(doc, "S"))
	doc, _ = parseFull(t, openapitest.ComponentSpecVer("3.1.0", s))
	_, isModel = typeByName(doc, "S").(*ir.Model)
	assert.True(t, isModel, "3.1: got %T", typeByName(doc, "S"))
}

// TestRefinement_ABranchDisagreeingStaysWhole pins the fold in a refinement: two
// branches disagreeing keep every keyword whole, on the carrier and on a node, and
// neither reaches a typed field, in either order.
func TestRefinement_ABranchDisagreeingStaysWhole(t *testing.T) {
	t.Parallel()
	const ref = "{$ref: '#/components/schemas/Str'}"
	doc, diags := parseFull(t, openapitest.ComponentSpecVer("3.1.0", refinementTargets+`    Holder:
      type: object
      properties:
        one: {allOf: [`+ref+`, {description: a}, {description: b}]}
        two: {allOf: [{description: b}, {description: a}, `+ref+`]}
    Items:
      type: array
      items: {allOf: [`+ref+`, {maxLength: 2}, {maxLength: 3}]}
`))
	openapitest.RequireNoErrorDiags(t, diags)
	assert.Len(t, withCode(diags, diag.DegradedConstruct), 6)
	for wire, keys := range map[string][]string{
		"one": {"openapi:allOf/1", "openapi:allOf/2"},
		"two": {"openapi:allOf/0", "openapi:allOf/1"},
	} {
		p := propertyOf(t, doc, "Holder", wire)
		assert.Equal(t, "from the target", p.Docs.Description, wire)
		for _, key := range keys {
			assert.Contains(t, p.Unmodeled, key, wire)
		}
	}
	items, ok := typeByName(doc, "Items").(*ir.List)
	require.True(t, ok)
	alias := aliasAt(t, doc, items.Elem)
	assert.Nil(t, alias.Constraints, "no bound stands for the two")
	assert.Contains(t, alias.Unmodeled, "openapi:allOf/1")
	assert.Contains(t, alias.Unmodeled, "openapi:allOf/2")
}
