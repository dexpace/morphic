package schema

import (
	"testing"

	"github.com/speakeasy-api/openapi/jsonschema/oas3"
	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/speakeasy-api/openapi/references"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/annotation"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/ir"
)

// TestConstraintsOf_NonSchemaInputs covers annotation.SchemaOf's nil and
// boolean early return, plus a well-formed reference besides: none of the three
// leaves a body with constraint keywords on it, so ConstraintsOf reports none
// either way.
func TestConstraintsOf_NonSchemaInputs(t *testing.T) {
	t.Parallel()
	l := NewRawLowerer(&soa.OpenAPI{})
	for _, js := range []*oas3.Schema{
		annotation.SchemaOf(nil),
		annotation.SchemaOf(oas3.NewJSONSchemaFromBool(true)),
		annotation.SchemaOf(oas3.NewJSONSchemaFromReference("#/components/schemas/Other")),
	} {
		var kept ir.Unmodeled
		cons, diags := ConstraintsOf(l.Ctx, &kept, js, "/p")
		assert.Nil(t, cons)
		assert.Empty(t, diags)
		assert.Empty(t, kept)
	}
}

// TestConstraintsOf_EmptyRefSchema covers a $ref pointer that is present but
// empty: annotation.At reads it as a declaration, since IsReference is false
// for it, and SchemaOf returns its body like any other, so ConstraintsOf meets
// a body that carries only Ref and finds no constraint keywords. The parser
// never emits this shape, so it is built by hand.
func TestConstraintsOf_EmptyRefSchema(t *testing.T) {
	t.Parallel()
	l := NewRawLowerer(&soa.OpenAPI{})
	js := oas3.NewJSONSchemaFromSchema[oas3.Referenceable](&oas3.Schema{Ref: new(references.Reference(""))})
	var kept ir.Unmodeled
	cons, diags := ConstraintsOf(l.Ctx, &kept, annotation.SchemaOf(js), "/p")
	assert.Nil(t, cons)
	assert.Empty(t, diags)
	assert.Empty(t, kept)
}

// TestStampConstraintDiags_RelocatesEveryDiagnosticToTheReadingPointer pins what
// makes two reads of one sub-schema dedup rather than report twice: the
// diagnostics a constraint read produces are located at the position that read
// it, not at the position that declared it.
func TestStampConstraintDiags_RelocatesEveryDiagnosticToTheReadingPointer(t *testing.T) {
	t.Parallel()
	l := NewRawLowerer(&soa.OpenAPI{})
	in := []ir.Diagnostic{
		{Code: diag.DegradedConstruct, Provenance: ir.Provenance{Pointer: "/elsewhere"}},
		{Code: diag.NumericPrecision, Provenance: ir.Provenance{Source: 9, Pointer: "/other"}},
	}

	got := stampConstraintDiags(l.Ctx, in, "/components/schemas/S")

	require.Len(t, got, 2)
	for _, d := range got {
		assert.Equal(t, ir.Provenance{Source: 0, Pointer: "/components/schemas/S"}, d.Provenance,
			"every diagnostic is relocated, not just the first")
	}
	assert.Equal(t, diag.NumericPrecision, got[1].Code, "only the provenance changes")
}
