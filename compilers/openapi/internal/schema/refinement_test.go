// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package schema_test

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/compilers/openapi/internal/schema"
	"github.com/dexpace/morphic/compilers/openapi/internal/statement"
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
	require.NotNil(t, bag.AdditionalProps.Value, "Bag types every key")
	carriers = map[string]ir.TypeRef{
		"property":  propertyOf(t, doc, "Holder", "f").Type,
		"parameter": param.Type,
		"header":    op.Responses[0].Headers[0].Type,
	}
	nodes = map[string]ir.TypeRef{
		"request":  op.Request.Contents[0].Type,
		"response": op.Responses[0].Payload.Contents[0].Type,
		"map":      *bag.AdditionalProps.Value,
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

// TestRefinement_AnAnnotationOnlyEntryLandsTyped is what GitHub #660 asks: the
// entry of an allOf refining a $ref lands where the sibling spelling lands, on
// a model decorator where the target is an object, and on the reference itself
// where it is not.
func TestRefinement_AnAnnotationOnlyEntryLandsTyped(t *testing.T) {
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

// TestRefinement_PrimitiveEntriesLowerToTheirMeet is GitHub #1043: every
// entry a kind, the position is the narrowest of them, in either order.
func TestRefinement_PrimitiveEntriesLowerToTheirMeet(t *testing.T) {
	t.Parallel()
	for name, entries := range map[string]string{
		"narrowest last":      "[{type: integer}, {type: integer, format: int32}]",
		"narrowest first":     "[{type: integer, format: int32}, {type: integer}]",
		"beside number":       "[{type: number}, {type: integer, format: int32}]",
		"beside the top type": "[{}, {type: integer, format: int32}]",
		"a number's width":    "[{type: integer}, {type: number, format: int32}]",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			doc, diags := parseFull(t, openapitest.ComponentSpecVer("3.0.3", "    Prim: {allOf: "+entries+"}\n"))
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

// TestRefinement_AUnionEntryIsAConjunct pins a oneOf or anyOf written as an
// allOf entry where the conjuncts admit no object, read as
// docs/compilers/openapi.md#allof-refinement reads any conjunct. Beside a
// narrower one it lowers to the meet, at a property as at a component, saying
// nothing more where a branch says nothing that conjunct does not and, for a
// oneOf, shares a kind with no other; a branch's bound, pattern or annotation
// says more, so it stays with its branch. Where it is the narrowest, it is the
// principal, standing at the position, and where it shares no value with the
// rest, the allOf composes.
func TestRefinement_AUnionEntryIsAConjunct(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, openapitest.ComponentSpecVer("3.0.3", refinementTargets+`    Value:
      allOf:
        - anyOf: [{type: string}, {type: number}, {type: boolean}]
        - {type: string, maxLength: 1000}
    ValueString: {type: string, maxLength: 1000}
    ByRef: {allOf: [{oneOf: [{type: integer}, {$ref: '#/components/schemas/Owner'}, {type: string}]}, {$ref: '#/components/schemas/Str'}]}
    TwoMatch: {allOf: [{oneOf: [{type: string}, {type: string, maxLength: 3}]}, {type: string, format: uuid}]}
    BothWithin: {allOf: [{oneOf: [{type: string}, {type: string, format: uuid}]}, {type: string, format: uuid}]}
    FormatNotHeld: {allOf: [{anyOf: [{type: string, format: uuid}, {type: integer}]}, {type: string}]}
    Overlapping: {allOf: [{oneOf: [{type: string}, {type: string, format: date}]}, {$ref: '#/components/schemas/Str'}]}
    Disjoint: {allOf: [{anyOf: [{type: string}, {type: integer}]}, {type: boolean}]}
    Widest: {allOf: [{anyOf: [{type: string}, {type: integer}]}, {description: either}]}
    Bounded: {allOf: [{anyOf: [{type: string, maxLength: 5}, {type: integer}]}, {type: string, maxLength: 1000}]}
    BoundedOne: {allOf: [{oneOf: [{type: string, maxLength: 5}, {type: integer}]}, {type: string, maxLength: 1000}]}
    Patterned: {allOf: [{anyOf: [{type: string, pattern: '^a'}, {type: integer}]}, {type: string, maxLength: 1000}]}
    Described: {allOf: [{anyOf: [{type: string, description: a word}, {type: integer}]}, {type: string, maxLength: 1000}]}
    BesideDecides: {type: string, maxLength: 1000, allOf: [{anyOf: [{type: string}, {type: integer}]}]}
    SameText: {allOf: [{oneOf: [{type: string, maxLength: 5}, {type: integer}]}, {type: string, maxLength: 5}]}
    SameRef: {allOf: [{oneOf: [{$ref: '#/components/schemas/Str'}, {type: integer}]}, {$ref: '#/components/schemas/Str'}]}
    Holder:
      type: object
      properties:
        value:
          allOf:
            - anyOf: [{type: string}, {type: number}, {type: boolean}]
            - {type: string, maxLength: 1000}
`))
	openapitest.RequireNoErrorDiags(t, diags)

	value, ok := typeByName(doc, "Value").(*ir.Scalar)
	require.True(t, ok, "Value is a scalar, not %T", typeByName(doc, "Value"))
	want, ok := typeByName(doc, "ValueString").(*ir.Scalar)
	require.True(t, ok)
	assert.Equal(t, want.Base, value.Base, "Value lowers as ValueString does")
	assert.Equal(t, want.Constraints, value.Constraints)
	assert.Empty(t, value.Unmodeled, "the anyOf says nothing the string does not")
	for _, at := range []string{"/components/schemas/Value", "/components/schemas/Value/allOf/0", "/components/schemas/Value/allOf/1"} {
		assert.False(t, openapitest.HasDiagCodeAt(diags, diag.DegradedConstruct, at), at)
	}

	field := propertyOf(t, doc, "Holder", "value")
	assert.Equal(t, ir.TypeID("t/prim/string"), field.Type.Target, "a property lowers to the meet too")
	require.NotNil(t, field.Constraints)
	assert.Equal(t, int64(1000), *field.Constraints.MaxLength)
	assert.Empty(t, field.Unmodeled)

	beside, ok := typeByName(doc, "BesideDecides").(*ir.Scalar)
	require.True(t, ok, "BesideDecides is a scalar, not %T", typeByName(doc, "BesideDecides"))
	assert.Equal(t, ir.TypeID("t/prim/string"), beside.Base.Target, "the keywords beside the allOf decide it")
	assert.Empty(t, beside.Unmodeled, "with no principal branch, the anyOf is said all the same")

	for _, name := range []string{"SameText", "SameRef"} {
		assert.Empty(t, typeByName(doc, name).Common().Unmodeled, "%s: a branch written as the principal says nothing more", name)
		assert.False(t, openapitest.HasDiagCodeAt(diags, diag.DegradedConstruct, "/components/schemas/"+name+"/allOf/0"), name)
	}

	byRef, ok := typeByName(doc, "ByRef").(*ir.Scalar)
	require.True(t, ok, "ByRef is a use of Str, not %T", typeByName(doc, "ByRef"))
	assert.Equal(t, componentID("Str"), byRef.Base.Target)
	assert.Empty(t, byRef.Unmodeled, "Str lies within one branch and shares no kind with another")

	// The union is kept under its keyword with the reason it has beside the
	// allOf: validation logic where its branches declare no shape.
	for name, key := range map[string]string{
		"TwoMatch": "openapi:oneOf", "BothWithin": "openapi:oneOf", "FormatNotHeld": "openapi:anyOf",
		"Overlapping": "openapi:oneOf", "Bounded": "openapi:anyOf", "BoundedOne": "openapi:oneOf",
		"Patterned": "openapi:anyOf", "Described": "openapi:anyOf",
	} {
		reason, code := ir.ReasonDegradedLowering, diag.DegradedConstruct
		if name == "TwoMatch" || name == "BothWithin" {
			reason, code = ir.ReasonValidationOnly, diag.ValidationOnlyKeyword
		}
		kept := typeByName(doc, name).Common().Unmodeled[key]
		assert.Equal(t, reason, kept.Reason, "%s keeps the union, which says more", name)
		assert.True(t, openapitest.HasDiagCodeAt(diags, code, "/components/schemas/"+name+"/allOf/0"), name)
	}

	disjoint, ok := typeByName(doc, "Disjoint").(*ir.Scalar)
	require.True(t, ok, "a union sharing no kind with a boolean leaves the boolean")
	assert.Contains(t, disjoint.Unmodeled, "openapi:anyOf")
	assert.True(t, openapitest.HasDiagCodeAt(diags, diag.DegradedConstruct, "/components/schemas/Disjoint"))

	widest, ok := typeByName(doc, "Widest").(*ir.Union)
	require.True(t, ok, "Widest is its principal, the union, not %T", typeByName(doc, "Widest"))
	assert.Len(t, widest.Variants, 2)
	assert.Equal(t, "either", widest.Docs.Description)
}

// TestRefinement_AUnionBesideTheAllOfIsAConjunct pins that a union written beside
// an allOf is one of its conjuncts, as the same union on an entry is: at a
// component and at a property, in either declaration order, the two spellings
// lower alike, but for the key a union the principal does not say is kept under,
// and none composes a model (GitHub #1685). The union's branch is written first,
// as the election's tie goes to the keywords beside the allOf before any entry.
func TestRefinement_AUnionBesideTheAllOfIsAConjunct(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		allOf, union string
		kept         string // the key the beside spelling keeps its union under, "" for none
	}{
		"scalars":             {allOf: "{type: string, maxLength: 10}", union: "anyOf: [{type: string}, {type: number}]"},
		"a union saying more": {allOf: "{type: string, maxLength: 10}", union: "anyOf: [{type: string, minLength: 2}, {type: number}]", kept: "openapi:anyOf"},
		"a $ref principal":    {allOf: "{$ref: '#/components/schemas/Str'}", union: "anyOf: [{type: string}, {type: number}]"},
		"the union winning":   {allOf: "{type: [string, number]}", union: "anyOf: [{type: string, format: uuid}, {type: integer}]"},
		"a null tie":          {allOf: "{type: [string, 'null']}", union: "anyOf: [{type: string}, {type: 'null'}]"},
		"a oneOf overlapping": {allOf: "{type: string, maxLength: 10}", union: "oneOf: [{type: string}, {type: string, minLength: 2}, {type: number}]", kept: "openapi:oneOf"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			beside, branch := "{allOf: ["+tc.allOf+"], "+tc.union+"}", "{allOf: [{"+tc.union+"}, "+tc.allOf+"]}"
			positions := "    Beside: " + beside + "\n    Branch: " + branch + "\n    Holder:\n      type: object\n" +
				"      properties:\n        beside: " + beside + "\n        branch: " + branch + "\n"
			for _, doc := range unionConjunctOrders(t, positions) {
				assert.Equal(t, componentConjunct(doc, "Branch"), componentConjunct(doc, "Beside"), "the component")
				assert.Equal(t, propertyConjunct(t, doc, "branch"), propertyConjunct(t, doc, "beside"), "the property")
				_, composes := typeByName(doc, "Beside").(*ir.Model)
				assert.False(t, composes, "no conjunct admits an object")
				p := propertyOf(t, doc, "Holder", "beside")
				for _, kept := range []ir.Unmodeled{typeByName(doc, "Beside").Common().Unmodeled, p.Unmodeled} {
					_, keeps := kept[tc.kept]
					assert.Equal(t, tc.kept != "", keeps, "the union is kept under %q", tc.kept)
				}
			}
		})
	}
}

// TestRefinement_AUnionThePrincipalSaysIsConsumed pins a oneOf beside an allOf
// of a string whose string branch says nothing the principal does not and
// shares no kind with the other: the principal says the union, which is
// consumed. At 3.1 the component is an alias over the target and the property
// names it, neither keeping anything, and nothing is reported at either
// (docs/compilers/openapi.md#allof-refinement).
func TestRefinement_AUnionThePrincipalSaysIsConsumed(t *testing.T) {
	t.Parallel()
	const written = "{allOf: [{$ref: '#/components/schemas/Str'}], oneOf: [{type: string}, {type: boolean}]}"
	for _, doc := range unionConjunctOrders(t, "    S: "+written+"\n"+
		"    Holder:\n      type: object\n      properties:\n        f: "+written+"\n") {
		s, ok := typeByName(doc, "S").(*ir.Scalar)
		require.True(t, ok, "S is an alias over its principal, not %T", typeByName(doc, "S"))
		assert.Equal(t, componentID("Str"), s.Base.Target)
		assert.Empty(t, s.Unmodeled, "the alias keeps nothing")
		assert.Nil(t, s.Constraints, "the alias adds no bound")
		p := propertyOf(t, doc, "Holder", "f")
		assert.Equal(t, componentID("Str"), p.Type.Target, "the property names the target")
		assert.Empty(t, p.Unmodeled, "the property keeps nothing")
		for _, d := range doc.Diagnostics {
			at := string(d.Provenance.Pointer)
			for _, written := range []string{"/components/schemas/S", "/components/schemas/Holder/properties/f"} {
				assert.False(t, strings.HasSuffix(at, written) || strings.Contains(at, written+"/"),
					"nothing is reported at %s: %s %s", written, d.Code, at)
			}
		}
	}
}

// TestRefinement_TheKeywordsBesideWithAUnionLowerAsWithNoAllOf pins an allOf
// whose principal is the keywords written beside it, a union among them: the
// position lowers by them as if no allOf were written, a union alone as the
// union, one beside a kind as that kind with the union kept, never as a model
// (docs/compilers/openapi.md#allof-refinement).
func TestRefinement_TheKeywordsBesideWithAUnionLowerAsWithNoAllOf(t *testing.T) {
	t.Parallel()
	for _, doc := range unionConjunctOrders(t, `    Kind: {type: string, allOf: [{maxLength: 3}], anyOf: [{format: uuid}, {format: email}]}
    KindAlone: {type: string, maxLength: 3, anyOf: [{format: uuid}, {format: email}]}
    Union: {allOf: [{type: [string, number]}], anyOf: [{type: string, format: uuid}, {type: integer}]}
    UnionAlone: {anyOf: [{type: string, format: uuid}, {type: integer}]}
`) {
		for _, name := range []string{"Kind", "Union"} {
			assert.Equal(t, componentConjunct(doc, name+"Alone"), componentConjunct(doc, name), name)
		}
		assert.Equal(t, ir.ReasonDegradedLowering, typeByName(doc, "Kind").Common().Unmodeled["openapi:anyOf"].Reason)
		_, isUnion := typeByName(doc, "Union").(*ir.Union)
		assert.True(t, isUnion, "the union decides the position, got %T", typeByName(doc, "Union"))
	}
}

// TestRefinement_AShapelessUnionBesideIsValidationLogic pins a union beside an
// allOf that refines to its principal whose branches declare no shape: it narrows
// the use, as beside any body, so it is kept under validation_only and reported
// as such, at a component and at a carrier, where on a branch it stays with its
// branch (docs/compilers/openapi.md#allof-refinement).
func TestRefinement_AShapelessUnionBesideIsValidationLogic(t *testing.T) {
	t.Parallel()
	const union = "oneOf: [{minLength: 1}, {maxLength: 2}]"
	for _, doc := range unionConjunctOrders(t, "    S: {allOf: [{$ref: '#/components/schemas/Str'}], "+union+"}\n"+
		"    OnBranch: {allOf: [{$ref: '#/components/schemas/Str'}, {"+union+"}]}\n"+
		"    Holder:\n      type: object\n      properties:\n"+
		"        f: {allOf: [{$ref: '#/components/schemas/Str'}], "+union+"}\n") {
		s, ok := typeByName(doc, "S").(*ir.Scalar)
		require.True(t, ok, "S is an alias over its principal, not %T", typeByName(doc, "S"))
		assert.Equal(t, componentID("Str"), s.Base.Target)
		p := propertyOf(t, doc, "Holder", "f")
		assert.Equal(t, componentID("Str"), p.Type.Target)
		for at, kept := range map[string]ir.Unmodeled{"/components/schemas/S": s.Unmodeled, "/components/schemas/Holder/properties/f": p.Unmodeled} {
			assert.Equal(t, ir.ReasonValidationOnly, kept["openapi:oneOf"].Reason, at)
			assert.Len(t, diagsAtPointer(doc.Diagnostics, diag.ValidationOnlyKeyword, at), 1, at)
			assert.Empty(t, diagsAtPointer(doc.Diagnostics, diag.DegradedConstruct, at), at)
		}
		assert.Equal(t, ir.ReasonValidationOnly, typeByName(doc, "OnBranch").Common().Unmodeled["openapi:oneOf"].Reason,
			"on a branch, the union is kept as beside the allOf")
	}
}

// unionConjunctOrders compiles positions beside refinementTargets in both
// declaration orders, and requires each to validate with no error and the two
// to agree on their types and on what they report.
func unionConjunctOrders(t *testing.T, positions string) []*ir.Document {
	t.Helper()
	docs := make([]*ir.Document, 0, 2)
	for _, src := range []string{refinementTargets + positions, positions + refinementTargets} {
		doc, diags := parseFull(t, openapitest.ComponentSpecVer("3.1.0", src))
		openapitest.RequireNoErrorDiags(t, diags)
		assert.Empty(t, validated(t, doc), "the document must validate")
		docs = append(docs, doc)
	}
	assert.Empty(t, cmp.Diff(docs[0].Types, docs[1].Types, orderInvariantIR()...),
		"declaration order must not change the types")
	assert.Equal(t, reported(docs[0].Diagnostics), reported(docs[1].Diagnostics))
	return docs
}

// componentConjunct renders what the component name lowered to, as
// conjunctShape does: an alias by its base, any other node as written there.
func componentConjunct(doc *ir.Document, name string) string {
	if s, ok := typeByName(doc, name).(*ir.Scalar); ok && s.Base != nil {
		return conjunctShape(doc, *s.Base, s.Constraints, s.Unmodeled)
	}
	return conjunctShape(doc, ir.TypeRef{Target: componentID(name)}, nil, typeByName(doc, name).Common().Unmodeled)
}

// propertyConjunct renders what Holder's property wire lowered to.
func propertyConjunct(t *testing.T, doc *ir.Document, wire string) string {
	t.Helper()
	p := propertyOf(t, doc, "Holder", wire)
	return conjunctShape(doc, p.Type, p.Constraints, p.Unmodeled)
}

// unionKeptKey matches a key a union is kept under, whichever spelling wrote it.
var unionKeptKey = regexp.MustCompile(`^openapi:(oneOf|anyOf|allOf/\d+)$`)

// conjunctShape renders a position that lowered to ref, with the bounds and the
// kept keys a node or carrier holds beside it, for two spellings of one
// conjunction to compare: a union by its variants, wherever its node stands, and
// a key a union is kept under by its reason alone.
func conjunctShape(doc *ir.Document, ref ir.TypeRef, cons *ir.Constraints, kept ir.Unmodeled) string {
	target := []ir.TypeID{ref.Target}
	if u, ok := doc.Types[ref.Target].(*ir.Union); ok {
		target = target[:0]
		for _, v := range u.Variants {
			target = append(target, v.Type.Target)
		}
	}
	keys := map[string]ir.UnmodeledReason{}
	for key, entry := range kept {
		keys[unionKeptKey.ReplaceAllString(key, "openapi:union")] = entry.Reason
	}
	bounds, err := json.Marshal(cons)
	if err != nil {
		return err.Error()
	}
	return fmt.Sprintf("%v nullable=%t bounds=%s kept=%v", target, ref.Nullable, bounds, keys)
}

// TestRefinement_DisjointConjunctsStayAComposition pins what GitHub #1043 keeps:
// no value satisfies both entries, so the warnings stand on the model.
func TestRefinement_DisjointConjunctsStayAComposition(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		schema string
		warns  int
	}{
		"two kinds":        {"{allOf: [{type: integer}, {type: string}]}", 1},
		"a null alone":     {"{allOf: [{type: string, nullable: true}, {type: integer, nullable: true}]}", 2},
		"an object beside": {"{allOf: [{$ref: '#/components/schemas/Owner'}, {type: string}]}", 1},
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
// whether it is written beside the allOf or in an entry.
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

// TestRefinement_ACollectionBoundLandsAtEveryPosition pins where a refinement's
// collection bound lands, beside the allOf or in an entry: on the carrier that
// holds the position, else on the alias it hoists, as one beside a $ref does.
func TestRefinement_ACollectionBoundLandsAtEveryPosition(t *testing.T) {
	t.Parallel()
	const names = "{$ref: '#/components/schemas/Names'}"
	want := &ir.Constraints{MinItems: new(int64(1)), UniqueItems: true}
	for name, written := range map[string]string{
		"beside": "{allOf: [" + names + "], minItems: 1, uniqueItems: true}",
		"branch": "{allOf: [" + names + ", {minItems: 1, uniqueItems: true}]}",
		"split":  "{allOf: [" + names + ", {minItems: 1}], uniqueItems: true}",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			doc, diags := parseFull(t, positionsDoc(refinementTargets, written))
			noDegradation(t, diags)
			op := openapitest.FindOp(t, doc, "op")
			param, ok := openapitest.ParamBySource(op, "id")
			require.True(t, ok)
			assert.Equal(t, want, propertyOf(t, doc, "Holder", "f").Constraints, "property")
			assert.Equal(t, want, param.Constraints, "parameter")
			assert.Equal(t, want, op.Responses[0].Headers[0].Constraints, "header")
			_, nodes := positionRefs(t, doc)
			for position, ref := range nodes {
				alias := aliasAt(t, doc, ref)
				assert.Equal(t, componentID("Names"), alias.Base.Target, position)
				assert.Equal(t, want, alias.Constraints, position)
			}
			named, ok := typeByName(doc, "Named").(*ir.Scalar)
			require.True(t, ok)
			assert.Equal(t, want, named.Constraints)
		})
	}
}

// TestRefinement_KeptKeywordsLandAtEveryPosition pins what no reference can
// hold: a narrower keyword beside the allOf or in an entry, kept verbatim under
// its own key where the position keeps what it writes, with one report.
func TestRefinement_KeptKeywordsLandAtEveryPosition(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		schema string
		key    string
		report string
		// code and reason are those of a degraded lowering where empty.
		code   string
		reason ir.UnmodeledReason
	}{
		"a narrower type beside": {
			schema: "{allOf: [{$ref: '#/components/schemas/Port'}], type: integer, format: int32}",
			key:    "openapi:format", report: "no home for format declared beside it",
		},
		"a format in a branch": {
			schema: "{allOf: [{$ref: '#/components/schemas/Port'}, {type: integer, format: int32}]}",
			key:    "openapi:format", report: "no home for format written on this inline entry",
		},
		"a type narrowing a number": {
			schema: "{allOf: [{$ref: '#/components/schemas/Num'}, {type: integer}]}",
			key:    "openapi:type", report: "no home for type written on this inline entry",
		},
		"items narrowing a list": {
			schema: "{allOf: [{$ref: '#/components/schemas/Names'}, {type: array, items: {type: string, minLength: 1}}]}",
			key:    "openapi:items", report: "no home for items written on this inline entry",
		},
		"a collection bound no field states": {
			schema: "{allOf: [{$ref: '#/components/schemas/Names'}, {uniqueItems: false}]}",
			key:    "openapi:uniqueItems", report: "no home for uniqueItems written on this inline entry",
		},
		"an overlapping enum": {
			schema: "{allOf: [{$ref: '#/components/schemas/Mode'}, {enum: [set, put]}]}",
			key:    "openapi:enum", report: "no home for enum written on this inline entry",
		},
		"an unknown keyword": {
			schema: "{allOf: [{$ref: '#/components/schemas/Str'}, {fancy: 1}]}",
			key:    "openapi:fancy", report: `keyword "fancy" has no field`,
			code: diag.UnknownSchemaKeyword, reason: ir.ReasonOutOfScope,
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			targets := refinementTargets + "    Num: {type: number}\n"
			doc, diags := parseFull(t, positionsDoc(targets, tc.schema))
			openapitest.RequireNoErrorDiags(t, diags)
			code, reason := diag.DegradedConstruct, ir.ReasonDegradedLowering
			if tc.code != "" {
				code, reason = tc.code, tc.reason
			}
			held := func(where string, u ir.Unmodeled) {
				t.Helper()
				entry, ok := u[tc.key]
				if assert.True(t, ok, "%s keeps %s: got %v", where, tc.key, u) {
					assert.Equal(t, reason, entry.Reason, where)
				}
			}
			op := openapitest.FindOp(t, doc, "op")
			param, ok := openapitest.ParamBySource(op, "id")
			require.True(t, ok)
			held("a property", propertyOf(t, doc, "Holder", "f").Unmodeled)
			held("a parameter", schemaKept(param.Unmodeled))
			held("a header", schemaKept(op.Responses[0].Headers[0].Unmodeled))
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
			for _, d := range withCode(diags, code) {
				reports = append(reports, d.Message)
				assert.Contains(t, d.Message, tc.report)
				assert.Equal(t, ir.SeverityInfo, d.Severity)
			}
			assert.NotEmpty(t, reports)
			other := diag.UnknownSchemaKeyword
			if code == other {
				other = diag.DegradedConstruct
			}
			assert.Empty(t, withCode(diags, other), "no other report")
		})
	}
}

// TestRefinement_SeveralRefsUseTheNarrowest pins two $refs no object can
// satisfy: the position is a use of the one lying within more conjuncts, the
// enum within the string, and keeps the other with one info, in either order
// of the entries; two alike tie to the least reference text.
func TestRefinement_SeveralRefsUseTheNarrowest(t *testing.T) {
	t.Parallel()
	const str, mode = "{$ref: '#/components/schemas/Str'}", "{$ref: '#/components/schemas/Mode'}"
	for _, entries := range [][2]string{{str, mode}, {mode, str}} {
		doc, diags := parseFull(t, openapitest.ComponentSpecVer("3.0.3", refinementTargets+
			"    Two: {allOf: ["+entries[0]+", "+entries[1]+"]}\n"+
			"    Same: {allOf: [{$ref: '#/components/schemas/Mail'}, {$ref: '#/components/schemas/MailToo'}]}\n"))
		openapitest.RequireNoErrorDiags(t, diags)
		at := "/components/schemas/Two/allOf/0"
		if entries[0] == mode {
			at = "/components/schemas/Two/allOf/1"
		}
		two, ok := typeByName(doc, "Two").(*ir.Scalar)
		require.True(t, ok, "got %T", typeByName(doc, "Two"))
		assert.Equal(t, componentID("Mode"), two.Base.Target, entries)
		assert.Equal(t, []string{"openapi:$ref"}, unmodeledKeys(two.Unmodeled), entries)
		assert.Equal(t, jsontext.Pointer(at), two.Unmodeled["openapi:$ref"].Provenance.Pointer, entries)
		assert.Contains(t, openapitest.DiagMessageAt(t, diags, diag.DegradedConstruct, ir.SeverityInfo, at), "another $ref", entries)
		same, ok := typeByName(doc, "Same").(*ir.Scalar)
		require.True(t, ok, "got %T", typeByName(doc, "Same"))
		assert.Equal(t, componentID("Mail"), same.Base.Target, "the least reference text")
	}
}

// TestRefinement_ReachesNoFurtherThanItCanRead pins each input the reading
// declines, and each whose kinds are an object alone: all of them stay the
// composition they were, with nothing dropped.
func TestRefinement_ReachesNoFurtherThanItCanRead(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		schemas string
		errs    bool
	}{
		"an object target": {schemas: "    S: {allOf: [{$ref: '#/components/schemas/Owner'}]}\n"},
		"a target with properties alone": {
			schemas: "    Bare: {properties: {a: {type: string}}}\n    S: {allOf: [{$ref: '#/components/schemas/Bare'}]}\n",
		},
		"a false target": {
			schemas: "    No: false\n    S: {allOf: [{$ref: '#/components/schemas/No'}, {type: string}]}\n",
		},
		"a type no schema defines": {schemas: "    S: {allOf: [{type: file}]}\n", errs: true},
		"a false entry":            {schemas: "    S: {allOf: [{type: string}, false]}\n"},
		"a cycle":                  {schemas: "    S: {allOf: [{$ref: '#/components/schemas/S'}], type: string}\n"},
		"a cycle of two": {
			schemas: "    S: {allOf: [{$ref: '#/components/schemas/R'}]}\n    R: {allOf: [{$ref: '#/components/schemas/S'}]}\n",
		},
		"an unresolved $ref": {schemas: "    S: {allOf: [{$ref: '#/components/schemas/Gone'}, {type: string}]}\n", errs: true},
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

// TestRefinement_KindsBeyondAnObjectAreAUse pins each allOf whose kinds are
// more than an object, or another kind a keyword only a model holds speaks of:
// none is a model, which would assert an object the source does not, so each
// is a use of what decides it, keeping what it cannot hold.
func TestRefinement_KindsBeyondAnObjectAreAUse(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		schemas string
		base    ir.TypeID
	}{
		"a target saying nothing": {
			schemas: "    Any: {}\n    S: {allOf: [{$ref: '#/components/schemas/Any'}]}\n", base: componentID("Any"),
		},
		"a target that may be an object": {
			schemas: "    Mixed: {type: [string, object]}\n    S: {allOf: [{$ref: '#/components/schemas/Mixed'}]}\n",
			base:    componentID("Mixed"),
		},
		"a union that may be an object": {
			schemas: "    U: {oneOf: [{type: string}, {type: object}]}\n    S: {allOf: [{$ref: '#/components/schemas/U'}]}\n",
			base:    componentID("U"),
		},
		"a true target": {
			schemas: "    Yes: true\n    S: {allOf: [{$ref: '#/components/schemas/Yes'}]}\n", base: componentID("Yes"),
		},
		"properties in a branch": {
			schemas: "    S: {allOf: [{type: string, properties: {a: {type: string}}}]}\n",
			base:    "t/prim/string",
		},
		"required beside": {
			schemas: "    S: {allOf: [{$ref: '#/components/schemas/Str'}], required: [a]}\n", base: componentID("Str"),
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			doc, diags := parseFull(t, openapitest.ComponentSpecVer("3.1.0", refinementTargets+tc.schemas))
			openapitest.RequireNoErrorDiags(t, diags)
			s, ok := typeByName(doc, "S").(*ir.Scalar)
			require.True(t, ok, "got %T", typeByName(doc, "S"))
			assert.Equal(t, tc.base, s.Base.Target)
		})
	}
}

// TestRefinement_NothingSaidIsTheTopType pins an allOf with no conjunct saying
// what a value is: nothing makes it an object, so it is the top type a schema
// saying as little is, and what its entries do say lands beside it.
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
// position lowers to, as if no allOf were written, and each entry folds.
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
	_, kept := bare.Unmodeled["openapi:maxLength"]
	assert.True(t, kept, "a list holds no value bound, so the bound is kept: got %v", bare.Unmodeled)
	listed, ok := doc.Types[propertyOf(t, doc, "Holder", "list").Type.Target].(*ir.List)
	require.True(t, ok)
	assert.NotContains(t, listed.Unmodeled, "openapi:minItems", "the list holds what it is written beside")
}

// TestRefinement_AValueSetNarrowsTheRefItBeats is GitHub #1042's position-level
// half: an entry's enum is the position's value set, and the $ref it narrows
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
	assert.Equal(t, "beside", firstDocs(doc, propertyOf(t, doc, "Holder", "sibling")).Description,
		"what a lost $ref writes beside itself speaks as its target does")
	assert.Nil(t, propertyOf(t, doc, "Holder", "off").Default, "the narrowed set does not take the target's default")
	assert.Nil(t, propertyOf(t, doc, "Holder", "ownOff").Default)
	secret := propertyOf(t, doc, "Holder", "secret")
	assert.True(t, doc.Types[secret.Type.Target].Common().Sensitive, "a target's redaction outlives the type")
	off := propertyOf(t, doc, "Holder", "off")
	en, ok := doc.Types[off.Type.Target].(*ir.Enum)
	require.True(t, ok, "got %T", doc.Types[off.Type.Target])
	require.Len(t, en.Members, 1)
	assert.Empty(t, off.Unmodeled, "the node the position owns keeps it")
	entry, ok := en.Unmodeled["openapi:$ref"]
	if assert.True(t, ok, "the $ref stays verbatim: got %v", en.Unmodeled) {
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
// position no carrier holds: the enum standing at the position holds the
// $ref's verbatim entry and what the $ref says of the position too.
func TestRefinement_ANarrowingKeepsItsTargetOnANode(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, positionsDoc(refinementTargets, "{allOf: [{$ref: '#/components/schemas/Mode'}, {enum: [set]}]}"))
	openapitest.RequireNoErrorDiags(t, diags)
	_, nodes := positionRefs(t, doc)
	for position, ref := range nodes {
		en, ok := doc.Types[ref.Target].(*ir.Enum)
		require.True(t, ok, "%s: got %T", position, doc.Types[ref.Target])
		assert.Equal(t, ref.Target, en.ID, position)
		assert.NotContains(t, string(en.Provenance.Pointer), "/allOf/", position)
		assert.Equal(t, "mode doc", en.Docs.Description, position)
		assert.Contains(t, en.Unmodeled, "openapi:$ref", position)
		assert.Len(t, en.Members, 1, position)
	}
}

// TestRefinement_ANarrowedRefStillLowersItsTarget pins what a kept $ref names: a
// position no component holds is lowered by a reference alone, and a narrowing
// keeps the reference, so the target's node stands as it does beneath a Base.
func TestRefinement_ANarrowedRefStillLowersItsTarget(t *testing.T) {
	t.Parallel()
	const mode = "{$ref: '#/components/schemas/Host/$defs/Mode'}"
	for name, written := range map[string]string{
		"in a branch":  "{allOf: [" + mode + ", {enum: [a]}]}",
		"beside":       "{enum: [a], allOf: [" + mode + "]}",
		"const beside": "{const: a, allOf: [" + mode + "]}",
	} {
		doc, diags := parseFull(t, openapitest.ComponentSpecVer("3.1.0", `    Host:
      type: object
      $defs:
        Mode: {type: string, enum: [a, b], example: b}
    Narrow: `+written+"\n"))
		openapitest.RequireNoErrorDiags(t, diags)
		en, ok := doc.Types["t/anon/components/schemas/Host/$defs/Mode"].(*ir.Enum)
		require.True(t, ok, "%s: nothing but the kept $ref names the target", name)
		assert.Len(t, en.Members, 2, name)
		assert.NotEmpty(t, en.Examples, "%s: the target's examples stay on its node", name)
	}
}

// TestRefinement_AValueSetIsTypedByEveryConjunct pins GitHub #1641: the value
// set a refinement lowers to takes its ValueType from the type and format every
// conjunct declares, as one schema writing them all gives it, and keeps none of
// them. Two formats neither of which narrows the other select no one primitive,
// nor does one the value is not of or one written with no type, so each stays
// with its entry.
func TestRefinement_AValueSetIsTypedByEveryConjunct(t *testing.T) {
	t.Parallel()
	const uuid = `"0b5c6f4e-6b9e-4a77-9f0f-5b0b8d5c1a11"`
	for name, tc := range map[string]struct {
		written string
		want    ir.PrimKind
		kept    []string
	}{
		"a type on a branch":   {"{allOf: [{type: integer}, {const: 80.0}]}", ir.PrimInteger, nil},
		"in the other order":   {"{allOf: [{const: 80.0}, {type: integer}]}", ir.PrimInteger, nil},
		"a format on a branch": {"{allOf: [{type: integer, format: int32}, {const: 80}]}", ir.PrimInt32, nil},
		"an enum":              {"{allOf: [{type: string, format: uuid}, {enum: [" + uuid + "]}]}", ir.PrimUUID, nil},
		"a const beside":       {"{const: 80.0, allOf: [{type: integer}]}", ir.PrimInteger, nil},
		"a type beside":        {"{type: integer, format: int32, allOf: [{const: 80}]}", ir.PrimInt32, nil},
		"a wider type beside":  {"{type: number, const: 80, allOf: [{type: integer}]}", ir.PrimInteger, nil},
		"a wider format with the value": {"{allOf: [{type: integer, format: int32}, {type: integer, format: int64, const: 80}]}",
			ir.PrimInt32, nil},
		"a wider format on another branch": {"{allOf: [{type: integer, format: int64}, {type: integer, format: int32}, {const: 80}]}",
			ir.PrimInt32, nil},
		"a wider format beside": {"{type: integer, format: int64, allOf: [{enum: [80]}, {type: integer, format: int32}]}",
			ir.PrimInt32, nil},
		"a $ref": {"{allOf: [{$ref: '#/components/schemas/Int32'}, {enum: [80]}]}", ir.PrimInt32, []string{"openapi:$ref"}},
		"a $ref to a refinement": {"{allOf: [{$ref: '#/components/schemas/Port'}, {enum: [80]}]}", ir.PrimInt32,
			[]string{"openapi:$ref"}},
		"two formats": {"{allOf: [{type: integer, format: int8}, {type: integer, format: uint8}, {const: 80}]}",
			ir.PrimNumber, []string{"openapi:allOf/0", "openapi:allOf/1"}},
		"a format the value is not of": {"{allOf: [{type: integer, format: int8}, {const: 300}]}", ir.PrimInteger,
			[]string{"openapi:format"}},
		"a format a member is not of": {"{allOf: [{type: integer, format: int8}, {enum: [1, 300]}]}", ir.PrimInteger,
			[]string{"openapi:format"}},
		"a format with no type": {"{allOf: [{type: integer}, {format: int32}, {const: 80}]}", ir.PrimInteger,
			[]string{"openapi:format"}},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			doc, diags := parseFull(t, openapitest.ComponentSpecVer("3.1.0",
				"    Int32: {type: integer, format: int32}\n    Port: {allOf: [{$ref: '#/components/schemas/Int32'}]}\n"+
					"    S: "+tc.written+"\n"))
			openapitest.RequireNoErrorDiags(t, diags)
			node := typeByName(doc, "S")
			switch n := node.(type) {
			case *ir.Literal:
				assert.Equal(t, tc.want, n.ValueType)
			case *ir.Enum:
				assert.Equal(t, tc.want, n.ValueType)
			default:
				t.Fatalf("lowered to %T", node)
			}
			assert.Equal(t, tc.kept, unmodeledKeys(node.Common().Unmodeled), "the value set stands at the position")
		})
	}
}

// TestRefinement_ARefToAPrincipalNamesItsTypedNode pins what a $ref to a
// principal's own pointer names, declared before or after the refinement: the
// entry as a schema of its own, typed by what it writes, beside the node the
// refinement typed at the position, two nodes with two IDs.
func TestRefinement_ARefToAPrincipalNamesItsTypedNode(t *testing.T) {
	t.Parallel()
	const s = "    S: {allOf: [{type: integer, format: int32}, {const: 80}]}\n"
	const raw = "    Raw: {$ref: '#/components/schemas/S/allOf/1'}\n"
	orders := []string{s + raw, raw + s}
	docs := make([]*ir.Document, 0, len(orders))
	for _, order := range orders {
		doc, diags := parseFull(t, openapitest.ComponentSpecVer("3.1.0", order))
		openapitest.RequireNoErrorDiags(t, diags)
		lit, ok := doc.Types["t/anon/components/schemas/S/allOf/1"].(*ir.Literal)
		require.True(t, ok)
		assert.Equal(t, ir.PrimNumber, lit.ValueType, "the entry alone writes no type")
		at, ok := typeByName(doc, "S").(*ir.Literal)
		require.True(t, ok, "got %T", typeByName(doc, "S"))
		assert.Equal(t, ir.PrimInt32, at.ValueType, "the position's node is typed by every conjunct")
		docs = append(docs, doc)
	}
	assert.Empty(t, cmp.Diff(docs[0].Types, docs[1].Types))
}

// TestRefinement_AValueSetBesideTheAllOfIsAConjunct pins GitHub #1309: an enum or
// a const beside an allOf that refines is one of its conjuncts, not its rival. It
// lowers as if no allOf were written where it is the principal, with what the
// entries say folded as written beside it, and a narrower entry is the
// principal. An entry writing what the enum has no field for stays whole, though
// the enum holds its format. Beside an allOf that composes, the enum still wins
// and keeps the allOf.
func TestRefinement_AValueSetBesideTheAllOfIsAConjunct(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, openapitest.ComponentSpecVer("3.0.3", `    Flat: {type: string, enum: [a], description: the level, deprecated: true}
    Noted: {type: string, enum: [a], allOf: [{description: the level, deprecated: true}]}
    Const: {const: a, allOf: [{description: the level}]}
    Narrowed: {enum: [a, b], allOf: [{enum: [a]}]}
    Bounded: {enum: [1, 2000], allOf: [{type: integer, format: int64, maximum: 10}]}
    Composes: {enum: [x], allOf: [{type: object, properties: {p: {type: string}}}]}
`))
	openapitest.RequireNoErrorDiags(t, diags)
	flat, noted := typeByName(doc, "Flat"), typeByName(doc, "Noted")
	assert.Equal(t, ir.KindEnum, noted.Kind())
	assert.Equal(t, flat.Common().Docs, noted.Common().Docs)
	assert.Equal(t, flat.Common().Deprecation, noted.Common().Deprecation)
	assert.Empty(t, noted.Common().Unmodeled)
	lit, ok := typeByName(doc, "Const").(*ir.Literal)
	require.True(t, ok, "got %T", typeByName(doc, "Const"))
	assert.Equal(t, "the level", lit.Docs.Description)
	assert.Empty(t, lit.Unmodeled)

	en, ok := typeByName(doc, "Narrowed").(*ir.Enum)
	require.True(t, ok, "the narrower branch is the principal, standing at the position: got %T", typeByName(doc, "Narrowed"))
	assert.Len(t, en.Members, 1)
	assert.Empty(t, en.Unmodeled, "the wider set beside it says nothing more")
	for _, at := range []string{"/components/schemas/Noted", "/components/schemas/Const", "/components/schemas/Narrowed"} {
		assert.False(t, openapitest.HasDiagCodeAt(diags, diag.DegradedConstruct, at), "%s keeps nothing", at)
	}

	bounded, ok := typeByName(doc, "Bounded").(*ir.Enum)
	require.True(t, ok, "got %T", typeByName(doc, "Bounded"))
	assert.Equal(t, ir.PrimInt64, bounded.ValueType)
	assert.Equal(t, []string{"openapi:maximum"}, unmodeledKeys(bounded.Unmodeled))
	assert.Contains(t, openapitest.DiagMessageAt(t, diags, diag.DegradedConstruct, ir.SeverityInfo,
		"/components/schemas/Bounded/allOf/0"), "no home for maximum written on this inline entry")

	composes, ok := typeByName(doc, "Composes").(*ir.Enum)
	require.True(t, ok)
	assert.Equal(t, []string{"openapi:properties", "openapi:type"}, unmodeledKeys(composes.Unmodeled),
		"the election's loser is kept keyword by keyword")
	assert.Contains(t, openapitest.DiagMessageAt(t, diags, diag.DegradedConstruct, ir.SeverityInfo,
		"/components/schemas/Composes/allOf/0"), "no home for type, properties written on this inline entry")
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
	assert.Equal(t, "written beside", beside.Docs.Description, "what a $ref entry writes speaks too")

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
			for _, entries := range [][2]string{{tc.a, tc.b}, {tc.b, tc.a}} {
				doc, diags := parseFull(t, openapitest.ComponentSpecVer("3.1.0", "    Holder:\n      type: object\n"+
					"      properties:\n        f: {allOf: ["+entries[0]+", "+entries[1]+"]}\n"))
				assert.Empty(t, withCode(diags, diag.DegradedConstruct), entries)
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

// TestRefinement_ATieOfKindsNeitherWithinTheOtherElectsNone pins two
// conjuncts naming kinds neither of which lies within the other, as many
// conjuncts lying within each: no rule ranks them, so neither is the
// principal, and in either order the position is the primitive of the kinds
// they admit together, or the top type where those are an array, keeping both
// with a report at each: whole where they write one keyword apart, else each
// keyword under its own key. Beside the allOf, the keywords there decide, as
// they do wherever no entry is elected.
func TestRefinement_ATieOfKindsNeitherWithinTheOtherElectsNone(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		a, b string
		// prim is what the position is.
		prim ir.PrimKind
		// keys are what it keeps, both entries whole where nil.
		keys []string
	}{
		"two hoisting formats": {"{type: string, format: email}", "{type: string, format: hostname}", ir.PrimString, nil},
		"two table formats":    {"{type: string, format: date}", "{type: string, format: uuid}", ir.PrimString, nil},
		"a format and a bare wider one": {"{type: integer, format: int32}", "{type: integer, format: uint32}",
			ir.PrimInteger, nil},
		"an overlap of value sets": {"{enum: [a, b]}", "{enum: [b, c]}", ir.PrimString, nil},
		"two element types": {"{type: array, items: {type: string}}", "{type: array, items: {type: integer}}",
			ir.PrimAny, nil},
		"a value set reaching past a type": {"{type: string}", "{enum: [a, 1]}", ir.PrimString,
			[]string{"openapi:enum", "openapi:type"}},
		"two type lists": {"{type: [string, boolean]}", "{type: [string, integer]}", ir.PrimString, nil},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			for _, entries := range []string{tc.a + ", " + tc.b, tc.b + ", " + tc.a} {
				doc, diags := parseFull(t, openapitest.ComponentSpecVer("3.1.0", "    Holder:\n      type: object\n"+
					"      properties:\n        f: {allOf: ["+entries+"]}\n"))
				openapitest.RequireNoErrorDiags(t, diags)
				p := propertyOf(t, doc, "Holder", "f")
				assert.Equal(t, ir.PrimTypeID(tc.prim), p.Type.Target, entries)
				keys := tc.keys
				if keys == nil {
					keys = []string{"openapi:allOf/0", "openapi:allOf/1"}
				}
				assert.Equal(t, keys, slices.Sorted(maps.Keys(p.Unmodeled)), entries)
				assert.Equal(t, []jsontext.Pointer{
					"/components/schemas/Holder/properties/f/allOf/0", "/components/schemas/Holder/properties/f/allOf/1",
				}, openapitest.PointersOf(diags, diag.DegradedConstruct), entries)
			}
		})
	}
	doc, diags := parseFull(t, openapitest.ComponentSpecVer("3.1.0", "    Holder:\n      type: object\n"+
		"      properties:\n        f: {type: string, format: email, allOf: [{type: string, format: hostname}]}\n"))
	openapitest.RequireNoErrorDiags(t, diags)
	p := propertyOf(t, doc, "Holder", "f")
	assert.NotEqual(t, ir.PrimTypeID(ir.PrimAny), p.Type.Target, "beside the allOf, the keywords there decide")
	held := p.Unmodeled
	if owned, ok := doc.Types[p.Type.Target]; ok && len(held) == 0 {
		held = owned.Common().Unmodeled
	}
	assert.Contains(t, held, "openapi:allOf/0")
}

// TestRefinement_AlikeConjunctsPartOnWhatTheirNodeReads pins ties between
// conjuncts admitting alike, in either order: the one writing a keyword its
// node reads from it alone is the principal, keeping nothing; two writing such
// keywords otherwise elect neither, the string both admit keeping both; a union written
// twice is one, kept nowhere; and words beside the allOf tying with an entry
// stand for the position, the entry folding, so a value set there is the
// position's own node.
func TestRefinement_AlikeConjunctsPartOnWhatTheirNodeReads(t *testing.T) {
	t.Parallel()
	const union = "{oneOf: [{type: string}, {type: integer}]}"
	for name, tc := range map[string]struct {
		beside, a, b string
		kept         int
		top          bool
		node         ir.TypeID
		// reports is how many reports of a degraded lowering, where not kept.
		reports int
	}{
		"one writes content": {"", "{type: string}", "{type: string, contentEncoding: base64}", 0, false, "", 0},
		// The type both write alike is kept once; each content keyword is
		// reported at itself, and each entry's type at the entry.
		"each writes other": {"", "{type: string, contentEncoding: base64}", "{type: string, contentMediaType: text/plain}",
			3, false, ir.PrimTypeID(ir.PrimString), 4},
		"a union written twice":  {"", union, union, 0, false, "", 0},
		"words beside the allOf": {"type: string, ", "{type: string, description: d}", "{type: string, minLength: 1}", 0, false, "", 0},
		"a set beside the allOf": {"enum: [a, b], ", "{enum: [b, a]}", "{enum: [a, b], description: d}", 0, false,
			"t/anon/components/schemas/Holder/properties/f", 0},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			for _, entries := range []string{tc.a + ", " + tc.b, tc.b + ", " + tc.a} {
				doc, diags := parseFull(t, openapitest.ComponentSpecVer("3.1.0", "    Holder:\n      type: object\n"+
					"      properties:\n        f: {"+tc.beside+"allOf: ["+entries+"]}\n"))
				openapitest.RequireNoErrorDiags(t, diags)
				p := propertyOf(t, doc, "Holder", "f")
				assert.Equal(t, tc.top, p.Type.Target == ir.PrimTypeID(ir.PrimAny), entries)
				assert.Len(t, p.Unmodeled, tc.kept, entries)
				assert.Len(t, withCode(diags, diag.DegradedConstruct), max(tc.kept, tc.reports), entries)
				if tc.node != "" {
					assert.Equal(t, tc.node, p.Type.Target, entries)
				}
			}
		})
	}
}

// TestRefinement_DecidedByTheSetInEveryOrder pins that the entries' order and
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
// no kind cannot decide what the position is: its `format`, `maxItems` or
// `items` qualifies a kind, so it stays beside the conjunct that names one, in
// either order, and the position is no top type. The kind-inference policy
// reads an `items` as an array where nothing declares a kind, never against a
// conjunct that does.
func TestRefinement_ANoKindConjunctIsNeverThePrincipal(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		kinded, unkinded string
		want             ir.TypeID
	}{
		"format":   {"{type: integer}", "{format: int32}", "t/prim/integer"},
		"maxItems": {"{type: string}", "{maxItems: 2}", "t/prim/string"},
		"items":    {"{type: string}", "{items: {type: string}}", "t/prim/string"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			for _, entries := range []string{tc.kinded + ", " + tc.unkinded, tc.unkinded + ", " + tc.kinded} {
				doc, diags := parseFull(t, openapitest.ComponentSpec("    T: {allOf: ["+entries+"]}\n"))
				alias, ok := doc.Types[componentID("T")].(*ir.Scalar)
				require.True(t, ok, "%s: %#v", entries, doc.Types[componentID("T")])
				assert.Equal(t, tc.want, alias.Base.Target, entries)
				assert.Len(t, alias.Unmodeled, 1, "the conjunct naming no kind is kept: %s", entries)
				assert.Len(t, diags, 1, "and reported once: %s", entries)
			}
		})
	}
}

// TestRefinement_BooleanItemsReadAsWhatTheyAdmit pins `items: true`, which says
// nothing, so a list saying more beats it, and `items: false`, which admits no
// element, so it beats a list saying less. Each element type stands in either
// order, and the other entry keeps nothing, since nothing it says is left unsaid.
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
			for _, entries := range []string{tc.list + ", " + tc.other, tc.other + ", " + tc.list} {
				doc, _ := parseFull(t, openapitest.ComponentSpecVer("3.1.0", "    T: {allOf: ["+entries+"]}\n"))
				list, ok := doc.Types[componentID("T")].(*ir.List)
				require.True(t, ok, "%s: %#v", entries, doc.Types[componentID("T")])
				assert.Equal(t, tc.elem, doc.Types[list.Elem.Target].Kind(), entries)
				assert.NotEqual(t, ir.TypeID("t/prim/any"), list.Elem.Target, entries)
				assert.Empty(t, list.Unmodeled, entries)
			}
		})
	}
}

// TestRefinement_NoConjunctNamingAKindLeavesTheTopType pins the same rule where
// nothing names a kind: no conjunct decides, so the position admits what the
// keywords do and the entry's format is kept under its own key.
func TestRefinement_NoConjunctNamingAKindLeavesTheTopType(t *testing.T) {
	t.Parallel()
	doc, _ := parseFull(t, openapitest.ComponentSpec("    T: {allOf: [{format: int32}]}\n"))
	alias, ok := doc.Types[componentID("T")].(*ir.Scalar)
	require.True(t, ok, "%#v", doc.Types[componentID("T")])
	assert.Equal(t, ir.TypeID("t/prim/any"), alias.Base.Target)
	assert.Contains(t, alias.Unmodeled, "openapi:format")
}

// typeShapes renders what the first property of Holder says and the kind of what
// it refers to, every position of an allOf entry spelled alike, so two orders of
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
// in the pointer a keyword kept in an entry is read at, and a decorator's own
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
// TestRefinement_StructuralKeywordsBesideARefAreConsumedAsBesideTheAllOf pins
// that a type the reference already says is consumed beside a 3.1 $ref as
// beside an allOf of it: the two are one conjunction.
func TestRefinement_StructuralKeywordsBesideARefAreConsumedAsBesideTheAllOf(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, openapitest.ComponentSpecVer("3.1.0", refinementTargets+`    Holder:
      type: object
      properties:
        allOf: {allOf: [{$ref: '#/components/schemas/Port'}], type: integer}
        beside: {$ref: '#/components/schemas/Port', type: integer}
`))
	openapitest.RequireNoErrorDiags(t, diags)
	for _, name := range []string{"allOf", "beside"} {
		p := propertyOf(t, doc, "Holder", name)
		assert.Empty(t, p.Unmodeled, name)
		assert.Equal(t, componentID("Port"), p.Type.Target, name)
	}
}

// TestRefinement_AnEntryHoldsWhatItsNodeDoes pins an inline principal: a value
// set's node stands at the position, holding what the entry writes as any
// conjunct's voice, a keyword nothing reads kept with the entry whole, and one
// that reduces to a shared primitive speaks at the position.
func TestRefinement_AnEntryHoldsWhatItsNodeDoes(t *testing.T) {
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
	assert.Equal(t, ir.TypeID("t/anon/components/schemas/Holder/properties/owned"), owned.Type.Target,
		"the node stands at the position, whichever conjunct decides it")
	en, ok := doc.Types[owned.Type.Target].(*ir.Enum)
	require.True(t, ok)
	assert.Equal(t, "mine", en.Docs.Description)
	assert.Contains(t, en.Unmodeled, "openapi:x-kept")
	assert.NotContains(t, owned.Unmodeled, "openapi:x-kept", "one home per declaration")
	assert.Empty(t, owned.Unmodeled, "and its node keeps what the position does")
	assert.Equal(t, []string{"openapi:fancy", "openapi:x-kept"}, unmodeledKeys(en.Unmodeled))
	assert.Contains(t, openapitest.DiagMessageAt(t, diags, diag.UnknownSchemaKeyword, ir.SeverityInfo,
		"/components/schemas/Holder/properties/owned/allOf/0/fancy"), `keyword "fancy" has no field`)
	assert.Equal(t, "owned", en.Name.Hint, "named for the position, not the branch's index")
	require.NotNil(t, owned.Default, "what binds a use reaches the carrier all the same")
	assert.NotEmpty(t, owned.Visibility.Only)

	items, ok := typeByName(doc, "Items").(*ir.List)
	require.True(t, ok)
	elem, ok := doc.Types[items.Elem.Target].(*ir.Enum)
	require.True(t, ok, "got %T", doc.Types[items.Elem.Target])
	assert.Contains(t, elem.Unmodeled, "openapi:default", "and with no carrier its node keeps it")
}

// TestRefinement_ARefEntrysSiblingsAreThePositions pins a $ref entry writing
// keywords beside itself: they are conjuncts of the position, as the same
// keywords on an entry of their own are, so the carrier holds them and the
// position is the use of the target, with no node at the entry.
func TestRefinement_ARefEntrysSiblingsAreThePositions(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, openapitest.ComponentSpecVer("3.1.0", refinementTargets+`    Holder:
      type: object
      properties:
        f: {allOf: [{$ref: '#/components/schemas/Str', description: beside, readOnly: true}]}
`))
	openapitest.RequireNoErrorDiags(t, diags)
	p := propertyOf(t, doc, "Holder", "f")
	assert.Equal(t, componentID("Str"), p.Type.Target)
	assert.Equal(t, "beside", p.Docs.Description)
	assert.NotEmpty(t, p.Visibility.Only)
	assert.NotContains(t, doc.Types, ir.TypeID("t/anon/components/schemas/Holder/properties/f/allOf/0"))
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

// TestRefinement_ANullOnlyEntryIsTheNull pins `type: "null"` refining a
// nullable string, in either order: the Enum with no member a schema of null
// alone lowers to (docs/compilers/openapi.md#null-alone), nullable, not the
// empty object nor the string.
func TestRefinement_ANullOnlyEntryIsTheNull(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, openapitest.ComponentSpecVer("3.1.0", `    Holder:
      type: object
      properties:
        f: {allOf: [{type: 'null'}, {type: [string, 'null']}]}
        g: {allOf: [{type: [string, 'null']}, {type: 'null'}]}
`))
	assert.Empty(t, diags)
	for _, wire := range []string{"f", "g"} {
		p := propertyOf(t, doc, "Holder", wire)
		assert.True(t, p.Type.Nullable, wire)
		e, ok := doc.Types[p.Type.Target].(*ir.Enum)
		require.True(t, ok, "%s: got %T", wire, doc.Types[p.Type.Target])
		assert.Empty(t, e.Members, wire)
		assert.True(t, e.Closed, wire)
	}
}

// TestRefinement_MissingItemsIsAskedOfTheEntryThatLowers pins
// docs/compilers/openapi.md#missing-array-items beside a refinement: the entry
// a list lowers from is held to the rule, and one that only restates a $ref's
// type lowers nothing.
func TestRefinement_MissingItemsIsAskedOfTheEntryThatLowers(t *testing.T) {
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
// target it cannot finish reading leaves a position weighing two conjuncts as it
// was, and one read to the last step refines, while one $ref weighed alone
// refines alike on both sides of each bound
// (docs/compilers/openapi.md#allof-refinement).
func TestRefinement_PastItsBudgetStaysAComposition(t *testing.T) {
	t.Parallel()
	assert.Equal(t, 256, schema.MaxAdmissionReads, "docs/compilers/openapi.md#allof-refinement says so")
	assert.Equal(t, 4096, schema.MaxAdmissionMembers, "docs/compilers/openapi.md#allof-refinement says so")
	assert.Equal(t, 64, schema.MaxRefinedConjuncts, "docs/compilers/openapi.md#allof-refinement says so")
	// S and F weigh two conjuncts, which a read past a bound cannot compare,
	// F's naming no kind; One weighs one $ref alone, which it need not.
	uses := func(target string) string {
		return "    S: {allOf: [{$ref: '#/components/schemas/" + target + "'}, {type: string}]}\n" +
			"    F: {allOf: [{$ref: '#/components/schemas/" + target + "'}, {format: uuid}]}\n" +
			"    One: {allOf: [{$ref: '#/components/schemas/" + target + "'}], maxLength: 3}\n"
	}
	enum := func(members int) string {
		var b strings.Builder
		b.WriteString("    Big:\n      type: string\n      enum:\n")
		for i := range members {
			fmt.Fprintf(&b, "        - m%d\n", i)
		}
		return b.String() + uses("Big")
	}
	// A link reads the $ref naming it and the schema it lands on.
	chain := func(links int) string {
		var b strings.Builder
		b.WriteString(uses("C0"))
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
			for _, name := range []string{"S", "F"} {
				_, ok := typeByName(doc, name).(*ir.Scalar)
				assert.True(t, ok, "at the bound %s refines: got %T", name, typeByName(doc, name))
			}
			within, ok := typeByName(doc, "One").(*ir.Scalar)
			require.True(t, ok, "at the bound one $ref refines: got %T", typeByName(doc, "One"))
			doc, _ = parseFull(t, openapitest.ComponentSpecVer("3.1.0", tc.past))
			for _, name := range []string{"S", "F"} {
				_, ok := typeByName(doc, name).(*ir.Model)
				assert.True(t, ok, "past it, %s does not: got %T", name, typeByName(doc, name))
			}
			past, ok := typeByName(doc, "One").(*ir.Scalar)
			require.True(t, ok, "past it, one $ref still refines: got %T", typeByName(doc, "One"))
			assert.Equal(t, within.Base, past.Base)
			assert.Equal(t, within.Constraints, past.Constraints)
		})
	}
}

// TestRefinement_PastItsConjunctBoundStaysAComposition pins the bound on the
// election, which compares every pair of conjuncts.
func TestRefinement_PastItsConjunctBoundStaysAComposition(t *testing.T) {
	t.Parallel()
	entries := func(n int) string {
		return "[" + strings.TrimSuffix(strings.Repeat("{type: string}, ", n), ", ") + "]"
	}
	doc, _ := parseFull(t, openapitest.ComponentSpecVer("3.1.0", "    S: {allOf: "+entries(schema.MaxRefinedConjuncts)+"}\n"))
	_, ok := typeByName(doc, "S").(*ir.Scalar)
	assert.True(t, ok, "at the bound it refines: got %T", typeByName(doc, "S"))
	doc, _ = parseFull(t, openapitest.ComponentSpecVer("3.1.0", "    S: {allOf: "+entries(schema.MaxRefinedConjuncts+1)+"}\n"))
	_, ok = typeByName(doc, "S").(*ir.Model)
	assert.True(t, ok, "past it, it does not: got %T", typeByName(doc, "S"))
}

// TestRefinement_ACappedEntryStaysAComposition pins a wrapper past the
// flattening depth, whose own allOf the reading does not reach.
func TestRefinement_ACappedEntryStaysAComposition(t *testing.T) {
	t.Parallel()
	body := "{type: string}"
	for range statement.MaxFlattenDepth + 2 {
		body = "{allOf: [" + body + "]}"
	}
	doc, diags := parseFull(t, openapitest.ComponentSpecVer("3.1.0", "    S: "+body+"\n"))
	openapitest.RequireNoErrorDiags(t, diags)
	_, ok := typeByName(doc, "S").(*ir.Model)
	assert.True(t, ok, "got %T", typeByName(doc, "S"))
}

// TestRefinement_AKeptEntryTheIRCannotHoldIsNotAnnounced pins the report to what
// was kept: an entry that does not convert is reported as lost, and nothing says it
// was preserved.
func TestRefinement_AKeptEntryTheIRCannotHoldIsNotAnnounced(t *testing.T) {
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
	assert.Contains(t, propertyOf(t, doc, "Holder", "f").Unmodeled, "openapi:items")
	assert.Empty(t, propertyOf(t, doc, "Holder", "same").Unmodeled)
}

// TestRefinement_RawTextIsComparedAgainstOneBase pins restated items within one
// document: under an $id naming another URI, `item.json` names another schema,
// so g's second entry says what h's spells out and is kept as h's is. Text
// writing no $ref reads alike under any base, so k's restates its target's, and
// so does text writing only an absolute $ref, m's.
func TestRefinement_RawTextIsComparedAgainstOneBase(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, openapitest.ComponentSpecVer("3.1.0", `    A: {$id: 'https://ex.test/a/item.json', type: integer}
    B: {$id: 'https://ex.test/b/item.json', type: string}
    Names: {$id: 'https://ex.test/a/names.json', type: array, items: {$ref: 'item.json'}}
    Holder:
      $id: 'https://ex.test/b/holder.json'
      type: object
      properties:
        g: {allOf: [{$ref: 'https://ex.test/a/names.json'}, {type: array, items: {$ref: 'item.json'}}]}
        h: {allOf: [{$ref: 'https://ex.test/a/names.json'}, {type: array, items: {$ref: 'https://ex.test/b/item.json'}}]}
        k: {allOf: [{$ref: 'https://ex.test/a/words.json'}, {type: array, items: {type: string, minLength: 1}}]}
        m: {allOf: [{$ref: 'https://ex.test/a/absolute.json'}, {type: array, items: {$ref: 'https://ex.test/a/item.json'}}]}
    Words: {$id: 'https://ex.test/a/words.json', type: array, items: {type: string, minLength: 1}}
    Absolute: {$id: 'https://ex.test/a/absolute.json', type: array, items: {$ref: 'https://ex.test/a/item.json'}}
`))
	openapitest.RequireNoErrorDiags(t, diags)
	for _, name := range []string{"g", "h"} {
		assert.Contains(t, propertyOf(t, doc, "Holder", name).Unmodeled, "openapi:items", name)
	}
	for _, name := range []string{"k", "m"} {
		assert.Empty(t, propertyOf(t, doc, "Holder", name).Unmodeled, name)
	}
}

// TestRefinement_ADynamicRefMasksNoKind pins that a $dynamicRef masks no kind:
// one in what a $ref lands on, which that position expands, is read as the
// string it names, so beside a string the allOf is no model, and one on an
// entry, which no position expands, admits every kind, so the object its
// anchor names makes no model of a description beside it.
func TestRefinement_ADynamicRefMasksNoKind(t *testing.T) {
	t.Parallel()
	for name, s := range map[string]string{
		"on a branch": "    S: {allOf: [{$dynamicRef: '#obj'}, {description: d}]}\n",
		"in a target": "    S: {allOf: [{$ref: '#/components/schemas/D'}, {type: string}]}\n    D: {$dynamicRef: '#node'}\n",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			doc, diags := parseFull(t, openapitest.ComponentSpecVer("3.1.0",
				"    Node: {$dynamicAnchor: node, type: string}\n    Obj: {$dynamicAnchor: obj, type: object}\n"+s))
			openapitest.RequireNoErrorDiags(t, diags)
			_, ok := typeByName(doc, "S").(*ir.Scalar)
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
			doc, diags := parseFull(t, openapitest.ComponentSpecVer("3.1.0", "    S: "+tc.schema+"\n"))
			_, isModel := typeByName(doc, "S").(*ir.Model)
			assert.False(t, isModel, "a value set is elected before a composition: got %T", typeByName(doc, "S"))
			disjoint := openapitest.HasDiagCodeAt(diags, diag.DegradedConstruct, "/components/schemas/S") &&
				openapitest.CountDiagsAt(diags, diag.DegradedConstruct, ir.SeverityWarning) == 1
			assert.Equal(t, tc.model, disjoint, "the conjuncts share no value exactly where reported: %+v", diags)
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
// shape through two refinements, one a $ref's and one inline, and past an enum,
// one beside the allOf among them, to the narrower entry it refines to.
func TestRefinement_DecidingSchemaIsAskedOfAWholeChain(t *testing.T) {
	t.Parallel()
	l, _ := schema.LoweredFor(t, openapitest.ComponentSpecVer("3.1.0", `    Blob: {type: string, format: binary}
    Inner: {allOf: [{$ref: '#/components/schemas/Blob'}], description: inner}
    Outer: {allOf: [{$ref: '#/components/schemas/Inner'}, {description: outer}]}
    Inline: {allOf: [{type: string, format: binary}, {description: inline}]}
    Narrowed: {enum: [a, b], allOf: [{type: string, format: binary, enum: [a]}]}
    Bare: {allOf: [{description: says nothing}]}
`))
	schemas := l.Doc.Components.GetSchemas()
	for name, format := range map[string]string{"Outer": "binary", "Inline": "binary", "Narrowed": "binary", "Bare": ""} {
		js, ok := schemas.Get(name)
		require.True(t, ok, name)
		assert.Equal(t, format, statement.Parsed(schema.DecidingSchema(l.Ctx, l.Ctx.Statements().Of(js).Schema)).GetFormat(), name)
	}
}

// TestRefinement_ItemsItCannotRenderRestateNothing pins the comparison of two
// `items`, one of which holds what no JSON renders: neither restates the other,
// in either order, and the one elected is the list at the position, its element
// the one written beside what no JSON renders.
func TestRefinement_ItemsItCannotRenderRestateNothing(t *testing.T) {
	t.Parallel()
	bad := "{type: array, items: {type: string, x-t: " + unpreservableValue + "}}"
	good := "{type: array, items: {type: string}}"
	for i, entries := range []string{bad + ", " + good, good + ", " + bad} {
		doc, _ := parseFull(t, openapitest.ComponentSpecVer("3.1.0", "    S: {allOf: ["+entries+"]}\n"))
		list, ok := typeByName(doc, "S").(*ir.List)
		require.True(t, ok, "got %T", typeByName(doc, "S"))
		assert.Equal(t, ir.TypeID(fmt.Sprintf("t/anon/components/schemas/S/allOf/%d/items", i)), list.Elem.Target, entries)
	}
}

// TestRefinement_ANullKeywordRestatesNothingMore pins items restating a $ref's
// but for a keyword written as null, which reads as left out: the branch says
// nothing the $ref does not, so the position keeps no branch.
func TestRefinement_ANullKeywordRestatesNothingMore(t *testing.T) {
	t.Parallel()
	doc, _ := parseFull(t, openapitest.ComponentSpecVer("3.0.3", "    Arr: {type: array, items: {type: string}}\n"+
		"    S: {allOf: [{$ref: '#/components/schemas/Arr'}, {items: {type: string, description: null}}]}\n"))
	td := typeByName(doc, "S")
	require.NotNil(t, td)
	assert.Empty(t, td.Common().Unmodeled)
}

// TestRefinement_ATypeArrayPrincipalKeepsWhatNoVariantHolds pins an inline
// principal that lowers as a type array: what none of its variants holds, a
// bound no listed type takes, is kept on the union it lowers to, standing at the
// position, as a type array written alone keeps it, whether the keyword sits on
// the entry or beside the allOf, and a bound one takes lands on that variant.
func TestRefinement_ATypeArrayPrincipalKeepsWhatNoVariantHolds(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, openapitest.ComponentSpecVer("3.1.0",
		"    OnBranch: {allOf: [{type: [string, integer], minProperties: 1, minLength: 1}]}\n"+
			"    Beside: {type: [string, integer], minProperties: 1, minLength: 1, allOf: [{description: d}]}\n"))
	openapitest.RequireNoErrorDiags(t, diags)
	branch, ok := typeByName(doc, "OnBranch").(*ir.Union)
	require.True(t, ok, "got %T", typeByName(doc, "OnBranch"))
	assert.Equal(t, []string{"openapi:minProperties"}, unmodeledKeys(branch.Unmodeled))
	beside, ok := typeByName(doc, "Beside").(*ir.Union)
	require.True(t, ok, "got %T", typeByName(doc, "Beside"))
	assert.Equal(t, []string{"openapi:minProperties"}, unmodeledKeys(beside.Unmodeled))
	assert.Equal(t, "d", beside.Docs.Description)
	for _, u := range []*ir.Union{branch, beside} {
		str, isScalar := doc.Types[u.Variants[0].Type.Target].(*ir.Scalar)
		require.True(t, isScalar, "got %T", doc.Types[u.Variants[0].Type.Target])
		assert.Equal(t, &ir.Constraints{MinLength: new(int64(1))}, str.Constraints)
		assert.Equal(t, ir.TypeID("t/prim/integer"), u.Variants[1].Type.Target)
	}
}

// TestRefinement_AValueBoundOnAnEntryBesideATypeListLandsOnItsVariant pins a
// value bound on an entry of an allOf beside a type list: it folds onto the
// variant whose type takes it, as the same bound beside the allOf does, and the
// branch is consumed, in either branch order and with branches writing bounds of
// different groups. A bound no listed type takes stays with its branch, reported.
// A collection bound there lands on the list variant
// (TestCollectionBound_OnAnEntryLandsAsItsFlatSpelling).
func TestRefinement_AValueBoundOnAnEntryBesideATypeListLandsOnItsVariant(t *testing.T) {
	t.Parallel()
	str := &ir.Constraints{MaxLength: new(int64(2))}
	num := &ir.Constraints{Min: new(ir.BigVal("1"))}
	for _, tc := range []struct {
		name, allOf string
		held        [2]*ir.Constraints
		kept        []string
	}{
		{"one branch", "[{maxLength: 2}]", [2]*ir.Constraints{str, nil}, nil},
		{"after another", "[{description: d}, {maxLength: 2}]", [2]*ir.Constraints{str, nil}, nil},
		{"two groups", "[{maxLength: 2}, {minimum: 1}]", [2]*ir.Constraints{str, num}, nil},
		{"two groups reversed", "[{minimum: 1}, {maxLength: 2}]", [2]*ir.Constraints{str, num}, nil},
		{"no listed type takes it", "[{maxLength: 2}, {minProperties: 1}]", [2]*ir.Constraints{str, nil},
			[]string{"openapi:minProperties"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			doc, diags := parseFull(t, openapitest.ComponentSpecVer("3.1.0",
				"    OnBranch: {type: [string, integer], format: x-mine, allOf: "+tc.allOf+"}\n"))
			openapitest.RequireNoErrorDiags(t, diags)
			u, ok := typeByName(doc, "OnBranch").(*ir.Union)
			require.True(t, ok, "got %T", typeByName(doc, "OnBranch"))
			assert.Equal(t, tc.kept, unmodeledKeys(u.Unmodeled))
			for i, want := range tc.held {
				v, isScalar := doc.Types[u.Variants[i].Type.Target].(*ir.Scalar)
				require.True(t, isScalar, "got %T", doc.Types[u.Variants[i].Type.Target])
				assert.Equal(t, want, v.Constraints, "variant %d", i)
			}
			if tc.kept != nil {
				assert.Contains(t, openapitest.DiagMessageAt(t, diags, diag.DegradedConstruct, ir.SeverityInfo,
					"/components/schemas/OnBranch/allOf/1"), "no home for minProperties written on this inline entry")
				return
			}
			assert.Empty(t, diags)
		})
	}
}

// TestRefinement_AnEntryBoundBesideATypeListIsReadAtTheEntry pins where a type
// array's variant reads what a folding entry writes: at the entry's own
// pointer, so a count past 64 bits the variant keeps is kept and reported where
// the entry writes it, beside the count it states.
func TestRefinement_AnEntryBoundBesideATypeListIsReadAtTheEntry(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, openapitest.ComponentSpecVer("3.1.0",
		"    OnBranch: {type: [string, integer], allOf: [{maxLength: 99999999999999999999, minLength: 1}]}\n"))
	openapitest.RequireNoErrorDiags(t, diags)
	u, ok := typeByName(doc, "OnBranch").(*ir.Union)
	require.True(t, ok, "got %T", typeByName(doc, "OnBranch"))
	assert.Empty(t, u.Unmodeled, "the branch folded")
	str, ok := doc.Types[u.Variants[0].Type.Target].(*ir.Scalar)
	require.True(t, ok, "got %T", doc.Types[u.Variants[0].Type.Target])
	assert.Equal(t, &ir.Constraints{MinLength: new(int64(1))}, str.Constraints)
	kept, ok := str.Unmodeled["openapi:maxLength"]
	require.True(t, ok)
	assert.Equal(t, jsontext.Pointer("/components/schemas/OnBranch/allOf/0/maxLength"), kept.Provenance.Pointer)
	require.Len(t, diags, 1, "%+v", diags)
	assert.Equal(t, jsontext.Pointer("/components/schemas/OnBranch/allOf/0"), diags[0].Provenance.Pointer)
}

// TestRefinement_AnEmptyAllOfOnAnEntryIsKept pins an entry writing `allOf: []`:
// nothing flattens it, so its keyword stays, as in a composition.
func TestRefinement_AnEmptyAllOfOnAnEntryIsKept(t *testing.T) {
	t.Parallel()
	doc, _ := parseFull(t, openapitest.ComponentSpecVer("3.1.0", `    Holder:
      type: object
      properties:
        f: {allOf: [{type: string, allOf: []}]}
`))
	assert.Contains(t, propertyOf(t, doc, "Holder", "f").Unmodeled, "openapi:allOf/0")
}

// TestRefinement_KeywordsBesideARefAreNotConjuncts pins where a refinement stops:
// an allOf beside a $ref, or beside a $dynamicRef, is no refinement, and an entry
// TestRefinement_AnAllOfBesideARefJoinsItsConjunction pins that an allOf
// beside a $ref adds its entries to the conjunction the $ref is one conjunct
// of, so an entry speaks for the position as it would beside no $ref, and that
// a $dynamicRef the position does not expand, kept on its carrier, masks no
// kind: the conjuncts beside it decide, and nothing makes a model of a string.
func TestRefinement_AnAllOfBesideARefJoinsItsConjunction(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, openapitest.ComponentSpecVer("3.1.0", refinementTargets+`    Node: {$dynamicAnchor: node, type: string}
    Holder:
      type: object
      properties:
        beside: {$ref: '#/components/schemas/Str', allOf: [{description: branch, default: b}]}
        dynamic: {$dynamicRef: '#node', allOf: [{description: branch}]}
        dynamicRef: {$dynamicRef: '#node', allOf: [{$ref: '#/components/schemas/Bound'}]}
    Bound: {type: string, readOnly: true}
    Items:
      type: array
      items: {$ref: '#/components/schemas/Str', allOf: [{description: branch}]}
`))
	openapitest.RequireNoErrorDiags(t, diags)
	beside := propertyOf(t, doc, "Holder", "beside")
	assert.Equal(t, componentID("Str"), beside.Type.Target)
	assert.Equal(t, "branch", beside.Docs.Description, "the branch speaks for the position")
	require.NotNil(t, beside.Default)
	assert.Empty(t, beside.Unmodeled)
	dynamic := propertyOf(t, doc, "Holder", "dynamic")
	assert.Equal(t, "branch", dynamic.Docs.Description)
	assert.Contains(t, dynamic.Unmodeled, "openapi:$dynamicRef")
	dynamicRef := propertyOf(t, doc, "Holder", "dynamicRef")
	assert.Equal(t, componentID("Bound"), dynamicRef.Type.Target, "the $ref beside it decides")
	assert.NotEmpty(t, dynamicRef.Visibility.Only)
	assert.Contains(t, dynamicRef.Unmodeled, "openapi:$dynamicRef")
	items, ok := typeByName(doc, "Items").(*ir.List)
	require.True(t, ok)
	assert.Equal(t, "branch", doc.Types[items.Elem.Target].Common().Docs.Description, "an items beside a $ref reads its branch too")
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
// within every other: none is here, and the one within the most others wins, in
// either order, what holds it consumed and the one overlapping it kept.
func TestRefinement_TheValueSetWithinMostOthersWins(t *testing.T) {
	t.Parallel()
	for entries, overlapping := range map[string]string{
		"{enum: [a, b, c]}, {enum: [a, b]}, {enum: [b, c, d]}": "openapi:allOf/2",
		"{enum: [b, c, d]}, {enum: [a, b]}, {enum: [a, b, c]}": "openapi:allOf/0",
	} {
		doc, diags := parseFull(t, openapitest.ComponentSpecVer("3.1.0", "    Holder:\n      type: object\n"+
			"      properties:\n        f: {allOf: ["+entries+"]}\n"))
		openapitest.RequireNoErrorDiags(t, diags)
		p := propertyOf(t, doc, "Holder", "f")
		en, ok := doc.Types[p.Type.Target].(*ir.Enum)
		require.True(t, ok, "%s: got %T", entries, doc.Types[p.Type.Target])
		assert.Len(t, en.Members, 2, entries)
		assert.Empty(t, p.Unmodeled, "%s: the node the position owns keeps what it keeps", entries)
		assert.Equal(t, []string{overlapping}, slices.Sorted(maps.Keys(en.Unmodeled)),
			"%s: it holds the narrower set, and keeps the one overlapping it", entries)
	}
}

// TestRefinement_ANullBothConjunctsAdmitIsAValue pins the reading of OpenAPI 3.0's
// nullable: it adds null to a type, so a value set of null alone shares a value
// with one, where in 3.1 the keyword reads as nothing and no value is shared. A
// value set is elected before the composition either way, so both lower to it.
func TestRefinement_ANullBothConjunctsAdmitIsAValue(t *testing.T) {
	t.Parallel()
	const s = "    S: {allOf: [{type: string, nullable: true}, {enum: [null]}]}\n"
	for _, version := range []string{"3.0.3", "3.1.0"} {
		doc, _ := parseFull(t, openapitest.ComponentSpecVer(version, s))
		en, ok := typeByName(doc, "S").(*ir.Enum)
		require.True(t, ok, "%s: got %T", version, typeByName(doc, "S"))
		assert.Empty(t, en.Members, version)
	}
}

// TestRefinement_AnEntryDisagreeingStaysWhole pins the fold in a refinement: two
// entries disagreeing keep every keyword whole, on the carrier and on a node, and
// neither reaches a typed field, in either order.
func TestRefinement_AnEntryDisagreeingStaysWhole(t *testing.T) {
	t.Parallel()
	const ref = "{$ref: '#/components/schemas/Str'}"
	doc, diags := parseFull(t, openapitest.ComponentSpecVer("3.1.0", refinementTargets+`    Holder:
      type: object
      properties:
        one: {allOf: [`+ref+`, {description: a}, {description: b}]}
        two: {allOf: [{description: b}, {description: a}, `+ref+`]}
    Items:
      type: array
      items: {allOf: [`+ref+`, {pattern: a}, {pattern: b}]}
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
	assert.Nil(t, alias.Constraints, "no pattern stands for the two")
	assert.Contains(t, alias.Unmodeled, "openapi:allOf/1")
	assert.Contains(t, alias.Unmodeled, "openapi:allOf/2")
}

// TestRefinement_AnEntryReadAgainstAnotherBaseFoldsNothing pins a refinement's
// inline entry under an $id naming another URI: its `not` names A there, so it
// is kept whole, with the reason, rather than folded onto the use.
func TestRefinement_AnEntryReadAgainstAnotherBaseFoldsNothing(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, openapitest.ComponentSpecVer("3.1.0", `    A: {$id: 'https://ex.test/a/n.json', type: string}
    Str: {type: string}
    Holder:
      type: object
      properties:
        f: {allOf: [{$ref: '#/components/schemas/Str'}, {$id: 'https://ex.test/a/x.json', not: {$ref: 'n.json'}}]}
`))
	openapitest.RequireNoErrorDiags(t, diags)
	f := propertyOf(t, doc, "Holder", "f")
	assert.Contains(t, f.Unmodeled, "openapi:allOf/1")
	assert.NotContains(t, f.Unmodeled, "openapi:not")
	assert.Contains(t,
		openapitest.DiagMessageAt(t, diags, diag.DegradedConstruct, ir.SeverityInfo, "/components/schemas/Holder/properties/f/allOf/1"),
		"reads its $refs against another base")
}
