// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package e2e_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
)

// TestAllOfRefine_ARefBesideANullOnlyTypeIsKeptAsTheInlineSpellingIs holds
// that a position declaring null alone, beside a $ref whose target the
// kind-inference policy reads as an array, admits null alone, as the same
// schema written inline does: the $ref is kept with one info, not taken as a
// nullable use of a list the position rules out.
func TestAllOfRefine_ARefBesideANullOnlyTypeIsKeptAsTheInlineSpellingIs(t *testing.T) {
	t.Parallel()
	schemas := []string{
		"L: {items: {type: string}}",
		"NullRef: {type: 'null', allOf: [{$ref: '#/components/schemas/L'}]}",
		"NullInline: {type: 'null', allOf: [{items: {type: string}}]}",
	}
	inBothOrders(t, schemas, func(t *testing.T, schemas []string) {
		doc, diags := compileAnnotationSpec(t, "nullonly", admissionSpec("3.1.0", "", schemas...))
		requireErrorsAndWarnings(t, diags, nil)
		ref, inline := kindNode(t, doc, componentID("NullRef")), kindNode(t, doc, componentID("NullInline"))
		require.Equal(t, inline.Kind(), ref.Kind(), "NullRef is what its inline spelling is")
		if scalar, ok := ref.(*ir.Scalar); ok {
			assert.NotEqual(t, componentID("L"), scalar.Base.Target, "NullRef admits no list")
		}
		assert.Equal(t, degradedKept("openapi:$ref"), keptOn(ref))
		assert.Equal(t, 1, degradedInfosUnder(diags, "/components/schemas/NullRef/allOf/0"))
	})
}

// TestAllOfRefine_ASetAsideRefIsReportedTrulyWhereNoKindIsDeclared holds that
// the info a $ref set aside draws claims no declared kind where none is: two
// $refs the policy reads as an array and an object, and a $ref admitting every
// kind beside a union whose branches name none.
func TestAllOfRefine_ASetAsideRefIsReportedTrulyWhereNoKindIsDeclared(t *testing.T) {
	t.Parallel()
	schemas := []string{
		"L: {items: {type: string}}",
		"U: {properties: {b: {type: integer}}}",
		"Open: {description: says nothing of its kind}",
		"Inferred: {allOf: [{$ref: '#/components/schemas/L'}, {$ref: '#/components/schemas/U'}]}",
		"Untyped: {allOf: [{$ref: '#/components/schemas/Open'}], oneOf: [{minLength: 1}, {maxLength: 2}]}",
	}
	inBothOrders(t, schemas, func(t *testing.T, schemas []string) {
		_, diags := compileAnnotationSpec(t, "undeclared", admissionSpec("3.1.0", "", schemas...))
		requireErrorsAndWarnings(t, diags, nil)
		for _, at := range []string{"/components/schemas/Inferred/allOf/0", "/components/schemas/Inferred/allOf/1"} {
			assert.NotContains(t, diagMessageOf(t, diags, at), "declare", at)
		}
		assert.NotContains(t, diagMessageOf(t, diags, "/components/schemas/Untyped/allOf/0"), "declare")
	})
}

// TestElection_AnAllOfElectingNoEntryIsReportedAsElectingNone holds that the
// info an entry draws where no entry is elected says so, rather than that the
// allOf lowered to a use of one of its conjuncts: two unions of one keyword,
// and two formats neither of which narrows the other, with nothing beside.
func TestElection_AnAllOfElectingNoEntryIsReportedAsElectingNone(t *testing.T) {
	t.Parallel()
	schemas := []string{
		"Unions: {allOf: [{oneOf: [{type: string}, {type: integer}]}, {oneOf: [{type: string}, {type: boolean}]}]}",
		"Formats: {allOf: [{type: string, format: email}, {type: string, format: uuid}]}",
	}
	inBothOrders(t, schemas, func(t *testing.T, schemas []string) {
		_, diags := compileAnnotationSpec(t, "none", admissionSpec("3.1.0", "", schemas...))
		requireErrorsAndWarnings(t, diags, nil)
		for _, name := range []string{"Unions", "Formats"} {
			for _, entry := range []string{"/allOf/0", "/allOf/1"} {
				at := "/components/schemas/" + name + entry
				assert.NotContains(t, diagMessageOf(t, diags, at), "a use of one of its conjuncts", at)
			}
		}
	})
}
