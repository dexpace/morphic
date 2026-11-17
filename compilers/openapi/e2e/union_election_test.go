// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package e2e_test

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
)

// TestElection_TwoAnyOfUnionsBesideABodyDistributeNeither holds that two anyOf
// unions whose branches declare objects, beside a body, distribute neither:
// the position is the model of the body keeping both entries whole under
// their indices, each reported, in both orders.
func TestElection_TwoAnyOfUnionsBesideABodyDistributeNeither(t *testing.T) {
	t.Parallel()
	first := "{anyOf: [{type: object, properties: {a: {type: string}}}, {type: object, properties: {b: {type: string}}}]}"
	second := "{anyOf: [{type: object, properties: {c: {type: string}}}, {type: object, properties: {d: {type: string}}}]}"
	for _, version := range electionVersions {
		t.Run(version, func(t *testing.T) {
			t.Parallel()
			doc, diags := compileInEveryOrder(t, version, func(swapped bool) []string {
				schemas := pairSchemas(swapped, "AnyAnyObj", first, second, "type: object, properties: {x: {type: string}}")
				return schemas
			})
			requireErrorsAndWarnings(t, diags, nil)

			model := requireModelOf(t, doc, "AnyAnyObj")
			assert.Equal(t, degradedKept(entryKey(0), entryKey(1)), keptOn(model))
			assert.Empty(t, unionsUnder(doc, "AnyAnyObj"), "neither union distributes")
			requireOneInfoAtEach(t, diags, "AnyAnyObj", "both entries", 0, 1)
		})
	}
}

// TestElection_TwoAnyOfEntriesThatDeclareNoShapeAreKeptWhole holds the one
// shape the golden spec writes, an allOf of two anyOf entries and nothing
// else, each only naming required members: neither is a candidate, so both are
// kept whole under their indices and reported, in both orders of the entries
// and of the components, and no union stands for the position.
func TestElection_TwoAnyOfEntriesThatDeclareNoShapeAreKeptWhole(t *testing.T) {
	t.Parallel()
	for _, version := range electionVersions {
		t.Run(version, func(t *testing.T) {
			t.Parallel()
			doc, diags := compileInEveryOrder(t, version, func(swapped bool) []string {
				schemas := pairSchemas(swapped, "Golden",
					"{anyOf: [{required: [a]}, {required: [b]}]}", "{anyOf: [{required: [c]}, {required: [d]}]}", "")
				return schemas
			})
			requireErrorsAndWarnings(t, diags, nil)

			requireKeptEntries(t, kindNode(t, doc, componentID("Golden")), "both entries", 0, 1)
			requireOneInfoAtEach(t, diags, "Golden", "both entries", 0, 1)
			assert.Empty(t, unionsUnder(doc, "Golden"))
		})
	}
}

// TestElection_ARefIsThePrincipalOverAUnionWhichSaysNothingMoreOfIt holds that
// an allOf of a $ref to a string and a union of a string and an integer is a
// use of the $ref, nothing kept and nothing reported, whichever is written
// first.
func TestElection_ARefIsThePrincipalOverAUnionWhichSaysNothingMoreOfIt(t *testing.T) {
	t.Parallel()
	for _, version := range electionVersions {
		t.Run(version, func(t *testing.T) {
			t.Parallel()
			doc, diags := compileInEveryOrder(t, version, func(swapped bool) []string {
				schemas := pairSchemas(swapped, "RefAndUnion",
					"{$ref: '#/components/schemas/Zed'}", oneStrInt, "", "Zed: {type: string}")
				return schemas
			})
			requireErrorsAndWarnings(t, diags, nil)

			use := requireScalarOver(t, doc, "RefAndUnion", componentID("Zed"))
			assert.Empty(t, keptOn(use))
			assert.Zero(t, degradedInfosAt(diags, "RefAndUnion"))
		})
	}
}

// TestElection_TwoInlineEntriesTieOnTheOneWritingItems holds that of two array
// entries the one writing `items` is the principal whichever is written first,
// and that two writing the same items are one: the list holds the string,
// nothing is kept or reported, and the two orders differ in no entry's index,
// the entry a node stands at being all a tie among alike entries may move.
func TestElection_TwoInlineEntriesTieOnTheOneWritingItems(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name, a, b string
		compile    func(*testing.T, string, func(bool) []string) (*ir.Document, []ir.Diagnostic)
	}{
		{"OneWritesItems", "{type: array}", "{type: array, items: {type: string}}", compileInEveryOrder},
		{"BothAgree", "{type: array, items: {type: string}}", "{type: array, items: {type: string}}", withoutEntryIndices},
	}
	for _, version := range electionVersions {
		for _, tc := range cases {
			t.Run(fmt.Sprintf("%s/%s", version, tc.name), func(t *testing.T) {
				t.Parallel()
				doc, diags := tc.compile(t, version, func(swapped bool) []string {
					schemas := pairSchemas(swapped, tc.name, tc.a, tc.b, "")
					return schemas
				})
				requireErrorsAndWarnings(t, diags, nil)

				scalar, ok := kindNode(t, doc, componentID(tc.name)).(*ir.Scalar)
				require.True(t, ok)
				require.NotNil(t, scalar.Base)
				list, ok := kindNode(t, doc, scalar.Base.Target).(*ir.List)
				require.True(t, ok, "the position is a use of a list")
				assert.Equal(t, prim(ir.PrimString), list.Elem.Target)
				assert.Empty(t, keptOn(scalar))
				assert.Zero(t, degradedInfosAt(diags, tc.name))
			})
		}
	}
}
