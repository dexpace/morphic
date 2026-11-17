// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package e2e_test

import (
	"fmt"
	"regexp"
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

// TestElection_BothKeywordsElectTheOneOfWhicheverIsWrittenFirst holds that an
// allOf of a oneOf and an anyOf, each naming its kinds, lowers to the oneOf
// whichever entry writes it, in both orders of the entries and of the
// components: the position is a use of the exclusive union, and the anyOf is
// kept under its own entry's index with one info there.
func TestElection_BothKeywordsElectTheOneOfWhicheverIsWrittenFirst(t *testing.T) {
	t.Parallel()
	for _, version := range electionVersions {
		t.Run(version, func(t *testing.T) {
			t.Parallel()
			doc, diags := compileInEveryOrder(t, version, func(swapped bool) []string {
				schemas := pairSchemas(swapped, "OneAny", anyNumBool, oneStrInt, "")
				return schemas
			})
			requireErrorsAndWarnings(t, diags, nil)

			union := electedUnion(doc, "OneAny")
			require.NotNil(t, union, "the oneOf is elected, though the anyOf is written first")
			assert.True(t, union.Exclusive)
			assert.Equal(t, []ir.TypeID{prim(ir.PrimString), prim(ir.PrimInteger)}, variantTargets(union))
			requireKeptEntries(t, kindNode(t, doc, componentID("OneAny")), "the anyOf, written first", 0)
			requireOneInfoAtEach(t, diags, "OneAny", "the anyOf is reported where it is written", 0)
		})
	}
}

// TestElection_TwoUnionsOfOneKeywordElectNeither holds that an allOf of two
// oneOf entries, or of two anyOf entries, none beside it, elects neither: no
// union stands for the position, both entries are kept whole under their own
// index and each is reported, and the two orders give one document.
func TestElection_TwoUnionsOfOneKeywordElectNeither(t *testing.T) {
	t.Parallel()
	cases := []struct{ name, a, b string }{
		{"OneOne", oneStrInt, oneIntBool},
		{"AnyAny", anyNumBool, anyStrInt},
	}
	for _, version := range electionVersions {
		for _, tc := range cases {
			t.Run(fmt.Sprintf("%s/%s", version, tc.name), func(t *testing.T) {
				t.Parallel()
				doc, diags := compileInEveryOrder(t, version, func(swapped bool) []string {
					schemas := pairSchemas(swapped, tc.name, tc.a, tc.b, "")
					return schemas
				})
				requireErrorsAndWarnings(t, diags, nil)

				assert.Empty(t, unionsUnder(doc, tc.name), "neither union stands for the position")
				assert.Nil(t, electedUnion(doc, tc.name))
				requireKeptEntries(t, kindNode(t, doc, componentID(tc.name)), "both entries", 0, 1)
				requireOneInfoAtEach(t, diags, tc.name, "both entries", 0, 1)
			})
		}
	}
}

// TestElection_TwoUnionsBesideABodyDistributeTheOneOf holds that a oneOf and an
// anyOf whose branches declare objects, each on an entry beside a body, lower
// to the union of the oneOf whichever is written first, each variant keeping
// the anyOf under its entry's index.
func TestElection_TwoUnionsBesideABodyDistributeTheOneOf(t *testing.T) {
	t.Parallel()
	for _, version := range electionVersions {
		t.Run(version, func(t *testing.T) {
			t.Parallel()
			doc, diags := compileInEveryOrder(t, version, func(swapped bool) []string {
				schemas := pairSchemas(swapped, "ObjTwo", "{"+anyObjBranches+"}", "{"+oneObjBranches+"}", objBody)
				return schemas
			})
			requireErrorsAndWarnings(t, diags, nil)

			union, ok := kindNode(t, doc, componentID("ObjTwo")).(*ir.Union)
			require.True(t, ok, "the oneOf distributes")
			assert.True(t, union.Exclusive)
			require.Len(t, union.Variants, 2)
			for _, v := range union.Variants {
				variant := kindNode(t, doc, v.Type.Target)
				assert.Equal(t, ir.KindModel, variant.Kind())
				assert.Equal(t, degradedKept(entryKey(0)), keptOn(variant), "the anyOf, written first, on each variant")
			}
		})
	}
}

// TestElection_ARefTieGoesByReferenceText holds that of two $refs neither of
// which lies within the other, or which are equal, the principal is the one
// whose reference text is the least, whichever is written first: the
// position is a use of it, and the other is kept under its entry's index with
// one info.
func TestElection_ARefTieGoesByReferenceText(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name, written, elected, kept string
		schemas                      []string
	}{
		{"EqualStrings", "Zed", "Amy", "Zed", []string{"Amy: {type: string}", "Zed: {type: string}"}},
		{"Neither", "Lo", "Hi", "Lo", []string{"Lo: {type: string, minLength: 1}", "Hi: {type: string, maxLength: 9}"}},
	}
	for _, version := range electionVersions {
		for _, tc := range cases {
			t.Run(fmt.Sprintf("%s/%s", version, tc.name), func(t *testing.T) {
				t.Parallel()
				ref := func(n string) string { return "{$ref: '#/components/schemas/" + n + "'}" }
				doc, diags := compileInEveryOrder(t, version, func(swapped bool) []string {
					schemas := pairSchemas(swapped, "Both", ref(tc.written), ref(tc.elected), "", tc.schemas...)
					return schemas
				})
				requireErrorsAndWarnings(t, diags, nil)
				requireModelsNameModels(t, doc)

				use := requireScalarOver(t, doc, "Both", componentID(tc.elected))
				kept := keptOn(use)
				require.Len(t, kept, 1, "the other $ref is kept")
				for key, reason := range kept {
					assert.Regexp(t, refEntryKey(0), key)
					assert.Equal(t, ir.ReasonDegradedLowering, reason)
				}
				assert.Equal(t, 1, degradedInfosAt(diags, "Both"))
			})
		}
	}
}

// TestElection_TwoInlineEntriesThatDisagreeOnItemsElectNeither holds that two
// array entries writing different items elect neither, in both orders: both
// are kept whole and reported, where the first written used to be the list.
func TestElection_TwoInlineEntriesThatDisagreeOnItemsElectNeither(t *testing.T) {
	t.Parallel()
	for _, version := range electionVersions {
		t.Run(version, func(t *testing.T) {
			t.Parallel()
			doc, diags := compileInEveryOrder(t, version, func(swapped bool) []string {
				schemas := pairSchemas(swapped, "Items",
					"{type: array, items: {type: string}}", "{type: array, items: {type: integer}}", "")
				return schemas
			})
			requireErrorsAndWarnings(t, diags, nil)
			requireKeptEntries(t, kindNode(t, doc, componentID("Items")), "both entries", 0, 1)
			requireOneInfoAtEach(t, diags, "Items", "both entries", 0, 1)
		})
	}
}

// electedUnion returns the union the component named is a use of, or is: the
// node itself where it is a union, the node it names as its Base where it is a
// scalar over one, and nil for anything else.
func electedUnion(doc *ir.Document, name string) *ir.Union {
	td := doc.Types[componentID(name)]
	if union, ok := td.(*ir.Union); ok {
		return union
	}
	scalar, ok := td.(*ir.Scalar)
	if !ok || scalar.Base == nil {
		return nil
	}
	union, _ := doc.Types[scalar.Base.Target].(*ir.Union)
	return union
}

// variantTargets lists the types a union's variants name, in order.
func variantTargets(union *ir.Union) []ir.TypeID {
	targets := make([]ir.TypeID, 0, len(union.Variants))
	for _, v := range union.Variants {
		targets = append(targets, v.Type.Target)
	}
	return targets
}

// refEntryKey matches the key a $ref entry of an allOf that was not composed is
// kept under: its index, or the one key a kept $ref takes.
func refEntryKey(index int) *regexp.Regexp {
	return regexp.MustCompile(fmt.Sprintf(`^openapi:(allOf/%d|\$ref)$`, index))
}
