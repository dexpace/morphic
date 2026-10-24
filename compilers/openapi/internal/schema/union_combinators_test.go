package schema_test

import (
	"maps"
	"slices"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/compilers/openapi/internal/schema"
	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/pass"
)

// TestUnionCombinators_CoDeclaredKeepsTheBoundsWrittenBesideIt pins that
// keeping a union verbatim does not cost the position the value constraints
// written beside it. The alias holding the union owns the pointer, so
// hoistDeclarationHome does not hoist the alias that would carry the bounds,
// and this alias must carry them itself (GitHub #343). Both reasons that keep a
// union hoist the same alias, and anyOf takes the same path as oneOf. The last
// case pins that a co-declared bound pair adds nothing beside the union.
func TestUnionCombinators_CoDeclaredKeepsTheBoundsWrittenBesideIt(t *testing.T) {
	t.Parallel()
	three := int64(3)
	ten, five, zero := ir.BigVal("10"), ir.BigVal("5"), ir.BigVal("0")
	cases := []struct {
		name, schemas, unionKey string
		reason                  ir.UnmodeledReason
		want                    ir.Constraints
		wantKept                []string
	}{
		{
			name:     "a validation-only union",
			schemas:  "    A: {type: string, minLength: 3, oneOf: [{minLength: 1}, {minLength: 2}]}\n",
			unionKey: "openapi:oneOf",
			reason:   ir.ReasonValidationOnly,
			want:     ir.Constraints{MinLength: &three},
			wantKept: []string{"openapi:oneOf"},
		},
		{
			name:     "a union kept as a degraded lowering",
			schemas:  "    A: {type: number, minimum: 10, multipleOf: 5, oneOf: [{type: string}, {type: integer}]}\n",
			unionKey: "openapi:oneOf",
			reason:   ir.ReasonDegradedLowering,
			want:     ir.Constraints{Min: &ten, MultipleOf: &five},
			wantKept: []string{"openapi:oneOf"},
		},
		{
			name:     "an anyOf kept in place of a oneOf",
			schemas:  "    A: {type: string, minLength: 3, anyOf: [{minLength: 1}, {minLength: 2}]}\n",
			unionKey: "openapi:anyOf",
			reason:   ir.ReasonValidationOnly,
			want:     ir.Constraints{MinLength: &three},
			wantKept: []string{"openapi:anyOf"},
		},
		{
			// Both bound keywords reach a field, so the union is the only
			// entry on the node: a co-declared pair adds nothing beside it.
			name:     "co-declared bounds beside a union",
			schemas:  "    A: {type: number, minimum: 10, exclusiveMinimum: 0, oneOf: [{minLength: 1}, {minLength: 2}]}\n",
			unionKey: "openapi:oneOf",
			reason:   ir.ReasonValidationOnly,
			want:     ir.Constraints{Min: &ten, ExclusiveMin: &zero},
			wantKept: []string{"openapi:oneOf"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			doc, diags := schema.LowerSpec(t, openapitest.ComponentSpec(tc.schemas))
			openapitest.RequireNoErrorDiags(t, diags)

			sc, ok := typeByName(doc, "A").(*ir.Scalar)
			require.True(t, ok, "the preserved union hoists an alias over the shared primitive")
			entry, ok := sc.Unmodeled[tc.unionKey]
			require.True(t, ok, "and keeps the union on it")
			assert.Equal(t, tc.reason, entry.Reason)
			require.NotNil(t, sc.Constraints, "while keeping the bounds written beside it")
			assert.Empty(t, cmp.Diff(tc.want, *sc.Constraints))
			assert.Equal(t, tc.wantKept, unmodeledKeys(sc.Unmodeled),
				"the union is the only entry kept on the node")
		})
	}
}

// TestUnionCombinators_PassedOverBranchSetIsKept covers the preference nothing
// used to record (GitHub #35). unionBranches takes oneOf whenever it is written
// and falls back to anyOf only when it is not, so a schema declaring both lost
// the anyOf outright — no variant, no Unmodeled entry, no diagnostic. The two
// conjoin, and one ir.Union carries one branch set, so the elected set stays the
// union and the other is kept beside it.
func TestUnionCombinators_PassedOverBranchSetIsKept(t *testing.T) {
	t.Parallel()
	doc, diags := schema.LowerSpec(t, openapitest.ComponentSpec(`    S:
      oneOf: [{type: string}, {type: integer}]
      anyOf: [{type: number}, {type: boolean}]
`))
	openapitest.RequireNoErrorDiags(t, diags)

	u, ok := typeByName(doc, "S").(*ir.Union)
	require.True(t, ok, "the elected branch set still becomes the union")
	assert.Len(t, u.Variants, 2, "one variant per oneOf branch")
	entry, ok := u.Unmodeled["openapi:anyOf"]
	require.True(t, ok, "the set unionBranches passed over is kept; got %v", u.Unmodeled)
	assert.Equal(t, ir.ReasonDegradedLowering, entry.Reason)
	assert.JSONEq(t, `[{"type":"number"},{"type":"boolean"}]`, string(entry.Value))
	assert.Contains(t,
		openapitest.DiagMessageAt(t, diags, diag.DegradedConstruct, ir.SeverityInfo, "/components/schemas/S"),
		"lowered as its oneOf, with anyOf kept verbatim under Unmodeled")
}

// TestUnionCombinators_NullBranchDoesNotCollapsePastTheAnyOf covers the second
// site of the same preference. A {X, null} oneOf normally collapses to nullable
// X, which resolves the position straight to X's own node — the shared string
// primitive here — leaving the co-declared anyOf nowhere of its own to sit; it
// was dropped there too. A schema writing both combinators is not nullable X in
// the first place, so it stays a Union and keeps the loser on it.
func TestUnionCombinators_NullBranchDoesNotCollapsePastTheAnyOf(t *testing.T) {
	t.Parallel()
	doc, diags := schema.LowerSpec(t, openapitest.ComponentSpec(`    S:
      oneOf: [{type: string}, {type: "null"}]
      anyOf: [{type: number}, {type: boolean}]
`))
	openapitest.RequireNoErrorDiags(t, diags)

	u, ok := typeByName(doc, "S").(*ir.Union)
	require.True(t, ok, "the collapse is declined; got %v", typeByName(doc, "S"))
	require.Len(t, u.Variants, 1, "the null branch still lifts off the variant list")
	assert.Equal(t, ir.TypeID("t/prim/string"), u.Variants[0].Type.Target)
	assert.Contains(t, u.Unmodeled, "openapi:anyOf",
		"and the node the position now owns carries the set passed over")
}

// TestUnionCombinators_UnpreservableIsNotAnnounced pins the pairing GitHub #144
// exists for, at this site: a position may only announce what it actually kept.
// The anyOf here fails to convert, so the union lowers, the failure is reported
// at the keyword, and nothing claims the branch set survived.
func TestUnionCombinators_UnpreservableIsNotAnnounced(t *testing.T) {
	t.Parallel()
	doc, diags := schema.LowerSpec(t, openapitest.ComponentSpec(`    S:
      oneOf: [{type: string}, {type: integer}]
      anyOf: [{type: number, x-t: `+unpreservableValue+`}]
`))

	u, ok := typeByName(doc, "S").(*ir.Union)
	require.True(t, ok)
	assert.NotContains(t, u.Unmodeled, "openapi:anyOf", "the conversion failed, so nothing was kept")
	assert.True(t, openapitest.HasDiag(diags, diag.UnpreservableConstruct), "the failure itself is reported")
	assert.Empty(t, openapitest.PreservationClaims(diags),
		"nothing was written under Unmodeled, so nothing may announce that it was")
}

// TestUnionCombinators_KeepingIsOrderIndependent states the property over the
// whole document: declining the collapse gives this position a Union of its own
// and makes its branches ordinary union branches, so an outside $ref naming one
// of them must reach the same IR whichever is declared first.
//
// The branch writes a description deliberately. A bare `{type: string}` branch
// resolves through the union to the shared primitive, so the outside $ref alone
// hoists a node at the branch pointer and no hint ever has to agree with
// another. Pinning the union's own variant to that node puts both lowerings on
// the pointer (#181).
func TestUnionCombinators_KeepingIsOrderIndependent(t *testing.T) {
	t.Parallel()
	host := `    S:
      oneOf:
        - {type: string, description: the branch}
        - {type: "null"}
      anyOf: [{type: number}, {type: boolean}]
`
	outsider := "    Outsider: {$ref: '#/components/schemas/S/oneOf/0'}\n"
	first, diags := parseFull(t, openapitest.ComponentSpec(outsider+host))
	openapitest.RequireNoErrorDiags(t, diags)
	last, diags := parseFull(t, openapitest.ComponentSpec(host+outsider))
	openapitest.RequireNoErrorDiags(t, diags)

	for _, doc := range []*ir.Document{first, last} {
		u, ok := typeByName(doc, "S").(*ir.Union)
		require.True(t, ok, "the position is the Union that keeps the passed-over set")
		require.Len(t, u.Variants, 1, "the null branch still lifts off the variant list")
		require.Equal(t, ir.TypeID("t/anon/components/schemas/S/oneOf/0"), u.Variants[0].Type.Target,
			"the union reaches the branch's own node, or the $ref is the only lowering that does")
	}
	assert.Empty(t, cmp.Diff(first, last, orderInvariantIR()...),
		"declaring the reference before or after the union must not change the IR")
	assert.Empty(t, cmp.Diff(first.Types, last.Types), "nor any name hint in the registry")
}

// TestCollapsedUnion_KeepsItsDiscriminatorAtEveryPosition pins that a
// discriminator beside a oneOf/anyOf that collapses to a nullable reference is
// kept verbatim, whole, by whatever owns the position, with an info diagnostic,
// instead of vanishing (GitHub #595). The reference itself is unchanged. The
// component, the property, an array's items, additionalProperties, a parameter
// and a response body each reach the collapse by their own path.
func TestCollapsedUnion_KeepsItsDiscriminatorAtEveryPosition(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, collapseSpec)
	openapitest.RequireNoErrorDiags(t, diags)
	assert.Empty(t, pass.Validate(doc))
	cat := componentID("Cat")

	w, ok := typeByName(doc, "W").(*ir.Scalar)
	require.True(t, ok, "the component is an alias over the nullable reference")
	assert.Equal(t, ir.TypeRef{Target: cat, Nullable: true}, *w.Base, "the reference is unchanged")
	entry, ok := w.Unmodeled["openapi:discriminator"]
	require.True(t, ok, "the component keeps its discriminator")
	assert.Equal(t, ir.ReasonDegradedLowering, entry.Reason)
	assert.Contains(t, string(entry.Value), "defaultMapping", "the whole object is kept, not just the property name")
	assert.Contains(t, string(entry.Value), `"mapping"`)

	holder, ok := typeByName(doc, "Holder").(*ir.Model)
	require.True(t, ok)
	require.Len(t, holder.Properties, 1)
	assert.Equal(t, ir.TypeRef{Target: cat, Nullable: true}, holder.Properties[0].Type, "the property's reference is unchanged")
	assert.Contains(t, holder.Properties[0].Unmodeled, "openapi:discriminator", "the property carrier keeps it")

	for _, id := range []ir.TypeID{"t/anon/components/schemas/L/items", "t/anon/components/schemas/M/additionalProperties"} {
		node, ok := doc.Types[id]
		require.True(t, ok, id)
		assert.Contains(t, node.Common().Unmodeled, "openapi:discriminator", "%s keeps it on the node it hoists", id)
	}

	for _, pointer := range []string{
		"/components/schemas/W", "/components/schemas/Holder/properties/w",
		"/components/schemas/L/items", "/components/schemas/M/additionalProperties",
		"/paths/~1p/get/parameters/0/schema", "/paths/~1p/get/responses/200/content/application~1json/schema",
	} {
		assert.Contains(t,
			openapitest.DiagMessageAt(t, diags, diag.DegradedConstruct, ir.SeverityInfo, pointer),
			"lowered to a nullable reference to its one non-null branch, which has no home for discriminator", pointer)
	}
	assert.Equal(t, 6, strings.Count(marshalForTest(t, doc), `"openapi:discriminator"`),
		"one entry per position, including the parameter and the response, none duplicated")
}

// TestCollapsedUnion_KeepsEveryCensusKeywordItCannotHold pins that the collapse
// takes the whole census a $ref site takes, not a discriminator rule: a format
// or a collection bound is no more at home on a nullable reference. A null
// type, which the reference's Nullable bit carries, is, so the component's
// alias keeps nothing for it.
func TestCollapsedUnion_KeepsEveryCensusKeywordItCannotHold(t *testing.T) {
	t.Parallel()
	spec := openapitest.ComponentSpec(`    Cat: {type: object, properties: {kind: {type: string}}}
    Bounded: {` + nullableCat + `, format: uuid, maxItems: 2}
    NullTyped: {` + nullableCat + `, type: 'null'}
`)
	doc, diags := parseFull(t, spec)
	openapitest.RequireNoErrorDiags(t, diags)

	bounded, ok := typeByName(doc, "Bounded").(*ir.Scalar)
	require.True(t, ok)
	assert.Equal(t, []string{"openapi:format", "openapi:maxItems"}, slices.Sorted(maps.Keys(bounded.Unmodeled)))
	assert.Contains(t, openapitest.DiagMessageAt(t, diags, diag.DegradedConstruct, ir.SeverityInfo,
		"/components/schemas/Bounded"), "no home for format, maxItems")

	assert.Empty(t, typeByName(doc, "NullTyped").Common().Unmodeled)
	assert.False(t, openapitest.HasDiagCodeAt(diags, diag.DegradedConstruct, "/components/schemas/NullTyped"))
}

// TestCollapsedUnion_WithoutACensusKeywordHoistsNothing pins that the same
// positions without one stay as they were: no entry, no diagnostic, and no
// node hoisted for a position that has none.
func TestCollapsedUnion_WithoutACensusKeywordHoistsNothing(t *testing.T) {
	t.Parallel()
	spec := openapitest.ComponentSpec(`    Cat: {type: object, properties: {kind: {type: string}}}
    L: {type: array, items: {` + nullableCat + `}}
`)
	doc, diags := parseFull(t, spec)
	openapitest.RequireNoErrorDiags(t, diags)
	assert.Zero(t, openapitest.CountDiagsAt(diags, diag.DegradedConstruct, ir.SeverityInfo), "%+v", diags)
	assert.NotContains(t, marshalForTest(t, doc), "openapi:discriminator")
	assert.NotContains(t, doc.Types, ir.TypeID("t/anon/components/schemas/L/items"), "a bare nullable reference needs no node")
}

// collapseSpec declares a nullable Cat at every position a schema can sit at
// with a discriminator beside it, plus a parameter and a response body.
const collapseSpec = `openapi: 3.2.0
info: {title: T, version: "1"}
paths:
  /p:
    get:
      parameters:
        - name: q
          in: query
          schema:
            ` + nullableCat + `
            discriminator: {propertyName: kind}
      responses:
        "200":
          description: ok
          content:
            application/json:
              schema:
                ` + nullableCat + `
                discriminator: {propertyName: kind}
components:
  schemas:
    Cat: {type: object, properties: {kind: {type: string}}}
    W:
      ` + nullableCat + `
      discriminator: {propertyName: kind, defaultMapping: '#/components/schemas/Cat', mapping: {cat: '#/components/schemas/Cat'}}
    Holder:
      type: object
      properties:
        w:
          anyOf: [{$ref: '#/components/schemas/Cat'}, {type: 'null'}]
          discriminator: {propertyName: kind}
    L:
      type: array
      items:
        ` + nullableCat + `
        discriminator: {propertyName: kind}
    M:
      type: object
      additionalProperties:
        ` + nullableCat + `
        discriminator: {propertyName: kind}
`

const nullableCat = "oneOf: [{$ref: '#/components/schemas/Cat'}, {type: 'null'}]"
