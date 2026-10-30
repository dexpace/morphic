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

	require.Equal(t, alike(nullAdmitted), (&nulls{conjuncts: budget.New(budget.AtMost(1))}).schema(nullable, nullSite{}),
		"the fixture admits null while there is budget to read it")

	assert.Equal(t, alike(nullSilent), (&nulls{conjuncts: budget.New(budget.AtMost(0))}).schema(nullable, nullSite{}),
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
	n := &nulls{conjuncts: budget.New(budget.AtMost(maxNullConjuncts))}
	assert.Equal(t, alike(nullSilent), n.conjunct(nil, nullSite{}))
	assert.Equal(t, alike(nullSilent), n.conjunct(openapitest.EmptyEitherSchema(), nullSite{}),
		"a branch whose either-value holds neither schema nor bool says nothing either")
}
