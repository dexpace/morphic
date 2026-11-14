// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package schema

import (
	"strings"
	"testing"

	"github.com/speakeasy-api/openapi/jsonschema/oas3"
	"github.com/speakeasy-api/openapi/marshaller"
	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/budget"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/ir"
)

// TestRefNullRef_AnUnresolvedRefIsNotNullable pins the guard on the second half
// of the question. A $ref site admits null as its own keywords and its target
// jointly say; a reference no binding resolved has no target to ask, and must
// read as not-nullable rather than dereference nothing.
func TestRefNullRef_AnUnresolvedRefIsNotNullable(t *testing.T) {
	t.Parallel()
	js := &oas3.JSONSchema[oas3.Referenceable]{}
	valErrs, err := marshaller.Unmarshal(t.Context(),
		strings.NewReader("$ref: '#/components/schemas/Missing'"), js)
	require.NoError(t, err)
	require.Empty(t, valErrs, "the fixture parses cleanly")

	ref, diags := refNullRef(NewRawLowerer(&soa.OpenAPI{}).Ctx, "t/x", js, siteAt("/x"))
	assert.Equal(t, ir.TypeRef{Target: "t/x"}, ref)
	assert.Empty(t, diags)
}

// TestNulls_TheConjunctWalkIsBounded pins the budget the conjunct
// walk runs on. Whether a schema admits null is decided partly by its allOf
// conjuncts, each reached through a $ref whose target is asked the same
// question, so a schema conjoining itself would otherwise not terminate.
//
// A budget spent per schema visited caps depth as well as breadth, and an
// exhausted walk answers "silent" — the verdict that claims the least, so a
// spec too deep to read is never reported as admitting a null it does not.
func TestNulls_TheConjunctWalkIsBounded(t *testing.T) {
	t.Parallel()
	nullable := &oas3.Schema{
		Type: oas3.NewTypeFromArray([]oas3.SchemaType{oas3.SchemaTypeString, oas3.SchemaTypeNull}),
	}

	c := NewRawLowerer(&soa.OpenAPI{}).Ctx
	require.Equal(t, alike(nullAdmitted), (&nulls{c: c, conjuncts: budget.New(budget.AtMost(1))}).schema(nullable, nullSite{}),
		"the fixture admits null while there is budget to read it")

	assert.Equal(t, alike(nullSilent), (&nulls{c: c, conjuncts: budget.New(budget.AtMost(0))}).schema(nullable, nullSite{}),
		"an exhausted budget stops the walk without claiming anything")
	assert.Positive(t, maxNullConjuncts, "the cap is a real bound, not zero")
}

// TestSchemaAdmitsNull_ReadsMaxNullConjunctsSchemas pins the budget as the
// lowering spends it: a null maxNullConjuncts schemas down an allOf chain is
// read, and one a schema deeper is not, the walk falling silent there.
func TestSchemaAdmitsNull_ReadsMaxNullConjunctsSchemas(t *testing.T) {
	t.Parallel()
	chain := func(levels int) *oas3.Schema {
		src := strings.Repeat("{allOf: [", levels) + "{type: [string, 'null']}" + strings.Repeat("]}", levels)
		js := &oas3.JSONSchema[oas3.Referenceable]{}
		valErrs, err := marshaller.Unmarshal(t.Context(), strings.NewReader(src), js)
		require.NoError(t, err)
		require.Empty(t, valErrs)
		return js.GetSchema()
	}
	c := NewRawLowerer(&soa.OpenAPI{}).Ctx
	assert.True(t, schemaAdmitsNull(c, chain(maxNullConjuncts-1)), "the null the last schema the budget reads admits")
	assert.False(t, schemaAdmitsNull(c, chain(maxNullConjuncts)), "one schema deeper, the walk is silent")
}

// TestNulls_ABranchWithNoSchemaSaysNothing pins the guard on an
// absent allOf entry. The parser never produces a nil branch, so nothing in the
// corpus reaches it; a conjunct that is not there constrains nothing, which is
// silence rather than a refusal — reading it as forbidding would let one
// missing entry strip a sibling's null.
func TestNulls_ABranchWithNoSchemaSaysNothing(t *testing.T) {
	t.Parallel()
	n := &nulls{c: NewRawLowerer(&soa.OpenAPI{}).Ctx, conjuncts: budget.New(budget.AtMost(maxNullConjuncts))}
	assert.Equal(t, alike(nullSilent), n.conjunct(nil, nullSite{}))
	assert.Equal(t, alike(nullSilent), n.conjunct(openapitest.EmptyEitherSchema(), nullSite{}),
		"a branch whose either-value holds neither schema nor bool says nothing either")
}

// TestNullAloneReading_IsTheWholeSchemasReading holds the reading a null branch
// is taken by to the one every Nullable site shares: over a schema whose own
// keywords leave no value but null, it is what reading the whole schema gives,
// under each reading and each dialect's nullable, so the union that lifts the
// branch and the branch lowered alone cannot disagree.
func TestNullAloneReading_IsTheWholeSchemasReading(t *testing.T) {
	t.Parallel()
	schemas := []string{
		`{type: "null"}`, `{type: ["null", "null"]}`, `{enum: [null]}`, `{const: null}`,
		`{type: string, const: null}`, `{type: [string, "null"], enum: [null]}`, `{type: string, enum: [null]}`,
		`{type: object, enum: [null], nullable: true}`, `{type: string, const: null, nullable: true}`,
		`{type: "null", nullable: false}`, `{enum: [null], const: 1}`,
	}
	for _, version := range []string{"3.0.3", "3.1.0"} {
		var body strings.Builder
		for i, s := range schemas {
			body.WriteString("    S" + string(rune('a'+i)) + ": " + s + "\n")
		}
		l, _ := LoweredFor(t, openapitest.ComponentSpecVer(version, body.String()))
		for i, src := range schemas {
			s := declared(t, l, "S"+string(rune('a'+i)))
			got, ok := nullAloneReading(l.Ctx, s)
			require.True(t, ok, "%s %s leaves no value but null", version, src)
			n := newNulls(l.Ctx)
			assert.Equal(t, n.schema(s, nullSite{}), got, "%s %s", version, src)
		}
	}
	l, _ := LoweredFor(t, openapitest.ComponentSpec("    S: {type: [string, \"null\"]}\n"))
	_, ok := nullAloneReading(l.Ctx, declared(t, l, "S"))
	assert.False(t, ok, "a type admitting a string leaves more than null")
}

// TestNullAloneReading_ReadsASchemaOnce pins that a schema's reading as a
// branch of null alone is read once a run, since each reader of a union asks of
// every branch, and an enum of nulls costs a read per member: it goes after the
// first read, which no lowering does, so only a remembered answer stays.
func TestNullAloneReading_ReadsASchemaOnce(t *testing.T) {
	t.Parallel()
	l, _ := LoweredFor(t, openapitest.ComponentSpec("    S: {enum: [null]}\n"))
	s := declared(t, l, "S")
	first, ok := nullAloneReading(l.Ctx, s)
	require.True(t, ok)
	s.Enum = nil
	again, ok := nullAloneReading(l.Ctx, s)
	assert.True(t, ok, "the second ask reads what the first read")
	assert.Equal(t, first, again)
}
