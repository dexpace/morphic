// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package e2e_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
)

// TestElection_AUnionTyingWithTheWordsIsThePrincipal holds that an allOf of a
// union and words admitting what it does is the union, whichever is written
// first: the bound the words write lands on the union, standing at the
// property, nothing is kept and nothing reported. Words beside the allOf tie
// the same way, and the union is the principal there too.
func TestElection_AUnionTyingWithTheWordsIsThePrincipal(t *testing.T) {
	t.Parallel()
	const simple = "SimpleString: {type: string}"
	const union = "{oneOf: [{$ref: '#/components/schemas/SimpleString'}]}"
	for _, version := range electionVersions {
		t.Run(version, func(t *testing.T) {
			t.Parallel()
			doc, diags := compileInEveryOrder(t, version, func(swapped bool) []string {
				list, _, _ := composeEntries(swapped, union, "{type: string, minLength: 1}")
				return []string{simple, "N: {type: object, properties: {p: {allOf: " + list + "}}}"}
			})
			requireErrorsAndWarnings(t, diags, nil)
			assert.Zero(t, degradedInfosAt(diags, "N"))
			p := requireModelOf(t, doc, "N").Properties[0]
			u, ok := kindNode(t, doc, p.Type.Target).(*ir.Union)
			require.True(t, ok, "the property names the union")
			assert.Equal(t, ir.TypeID("t/anon/components/schemas/N/properties/p"), p.Type.Target, "standing at the property")
			require.NotNil(t, u.Constraints)
			require.NotNil(t, u.Constraints.MinLength)
			assert.EqualValues(t, 1, *u.Constraints.MinLength)
			assert.Nil(t, p.Constraints, "the property holds no bound the union does")
			assert.Empty(t, p.Unmodeled)

			beside, besideDiags := compileAnnotationSpec(t, "beside",
				admissionSpec(version, "", simple, "B: {type: string, minLength: 1, allOf: ["+union+"]}"))
			assert.Zero(t, degradedInfosAt(besideDiags, "B"))
			use, ok := kindNode(t, beside, componentID("B")).(*ir.Union)
			require.True(t, ok, "the union, not the words beside it")
			require.NotNil(t, use.Constraints)
			require.NotNil(t, use.Constraints.MinLength)
			assert.EqualValues(t, 1, *use.Constraints.MinLength)
			assert.Empty(t, keptOn(use))
		})
	}
}

// TestElection_ASchemaWrittenTwiceIsOneConjunct holds that two entries written
// alike, a $ref or a union, are one conjunct: the position is a use of it,
// keeping nothing and reporting nothing, in both orders. Which copy's node a
// union stands at is all the two orders may differ in.
func TestElection_ASchemaWrittenTwiceIsOneConjunct(t *testing.T) {
	t.Parallel()
	const amy = "{$ref: '#/components/schemas/Amy'}"
	for _, version := range electionVersions {
		t.Run(version, func(t *testing.T) {
			t.Parallel()
			doc, diags := compileInEveryOrder(t, version, func(swapped bool) []string {
				return pairSchemas(swapped, "Refs", amy, amy, "", "Amy: {type: string}")
			})
			requireErrorsAndWarnings(t, diags, nil)
			assert.Empty(t, keptOn(requireScalarOver(t, doc, "Refs", componentID("Amy"))))
			assert.Zero(t, degradedInfosAt(diags, "Refs"))

			doc, diags = withoutEntryIndices(t, version, func(swapped bool) []string {
				return pairSchemas(swapped, "Unions", oneStrInt, oneStrInt, "")
			})
			requireErrorsAndWarnings(t, diags, nil)
			union := electedUnion(doc, "Unions")
			require.NotNil(t, union, "the union stands for the position")
			assert.Empty(t, keptOn(kindNode(t, doc, componentID("Unions"))))
			assert.Zero(t, degradedInfosAt(diags, "Unions"))
		})
	}
}

// TestElection_TwoRefsWrittenAlikeGoByTheirEntries holds that two $refs naming
// one target, one entry writing more beside it, are one $ref written twice:
// what the entry writes beside its $ref is a conjunct of its own, so the
// position is the use of the target, the description its own, nothing kept,
// whichever is written first.
func TestElection_TwoRefsWrittenAlikeGoByTheirEntries(t *testing.T) {
	t.Parallel()
	doc, diags := compileInEveryOrder(t, "3.1.0", func(swapped bool) []string {
		return pairSchemas(swapped, "Refs", "{$ref: '#/components/schemas/Amy', description: d}",
			"{$ref: '#/components/schemas/Amy'}", "", "Amy: {type: string}")
	})
	requireErrorsAndWarnings(t, diags, nil)
	use := requireScalarOver(t, doc, "Refs", componentID("Amy"))
	assert.Empty(t, keptOn(use), "the $ref written twice is one")
	assert.Equal(t, "d", use.Docs.Description)
	assert.Zero(t, degradedInfosAt(diags, "Refs"))
}

// TestElection_WordsAdmittingAlikePartOnWhatTheirNodeReads holds that of two
// entries admitting alike, the pick turns on the keywords a node reads from
// its principal alone, whichever is written first: the one writing all the
// other does and more is the principal, standing at the position, keeping
// nothing of its own, and two
// writing them otherwise, or writing one keyword with values that differ,
// elect neither, the position the string both admit, or the top type where
// they admit an array, keeping what each writes under its own key, a type both
// write alike once, or both whole where they write one keyword apart.
func TestElection_WordsAdmittingAlikePartOnWhatTheirNodeReads(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name, a, b string
		elected    ir.TypeKind
		kept       map[string]ir.UnmodeledReason
		// prim is the primitive a position electing neither is, where elected
		// is a primitive.
		prim ir.PrimKind
	}{
		{"Content", "{type: string}", "{type: string, contentEncoding: base64}", ir.KindScalar, nil, ""},
		{"Tuple", "{type: array}", "{type: array, prefixItems: [{type: string}]}", ir.KindTuple, nil, ""},
		{"MoreContent", "{type: string, contentMediaType: application/json}",
			"{type: string, contentMediaType: application/json, contentSchema: {type: object}}", ir.KindScalar,
			map[string]ir.UnmodeledReason{"openapi:contentMediaType": ir.ReasonNoIRHome}, ""},
		{"OtherContent", "{type: string, contentEncoding: base64}",
			"{type: string, contentMediaType: application/json}", ir.KindPrimitive, map[string]ir.UnmodeledReason{
				"openapi:contentEncoding": ir.ReasonNoIRHome, "openapi:contentMediaType": ir.ReasonNoIRHome,
				"openapi:type": ir.ReasonDegradedLowering,
			}, ir.PrimString},
		{"DisagreeingContent", "{type: string, contentMediaType: application/json}",
			"{type: string, contentMediaType: text/plain}", ir.KindPrimitive, degradedKept(entryKey(0), entryKey(1)),
			ir.PrimString},
		{"DisagreeingTuple", "{type: array, prefixItems: [{type: string}]}",
			"{type: array, prefixItems: [{type: integer}]}", ir.KindPrimitive, degradedKept(entryKey(0), entryKey(1)),
			ir.PrimAny},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			doc, diags := compileInEveryOrder(t, "3.1.0", func(swapped bool) []string {
				return pairSchemas(swapped, tc.name, tc.a, tc.b, "")
			})
			requireErrorsAndWarnings(t, diags, nil)
			use := kindNode(t, doc, componentID(tc.name))
			if tc.elected == ir.KindPrimitive {
				alias, ok := use.(*ir.Scalar)
				require.True(t, ok)
				require.NotNil(t, alias.Base)
				assert.Equal(t, prim(tc.prim), alias.Base.Target, "neither is elected")
			} else {
				assert.Equal(t, tc.elected, use.Kind(), "the principal's node is the position's")
			}
			kept := tc.kept
			if kept == nil {
				kept = map[string]ir.UnmodeledReason{}
			}
			assert.Equal(t, kept, keptOn(use), "what the principal does not say")
		})
	}
}

// TestElection_ValueSetsAlikeGoByTheirText holds that two equal value sets,
// written differently, elect the one whose text is the least, whichever is
// written first: the enum keeps that entry's order of members.
func TestElection_ValueSetsAlikeGoByTheirText(t *testing.T) {
	t.Parallel()
	for _, version := range electionVersions {
		t.Run(version, func(t *testing.T) {
			t.Parallel()
			doc, diags := compileInEveryOrder(t, version, func(swapped bool) []string {
				return pairSchemas(swapped, "Sets", "{enum: [a, b]}", "{enum: [b, a], description: d}", "")
			})
			requireErrorsAndWarnings(t, diags, nil)
			enum, ok := kindNode(t, doc, componentID("Sets")).(*ir.Enum)
			require.True(t, ok, "the value set stands at the position")
			require.Len(t, enum.Members, 2)
			assert.Equal(t, "b", enum.Members[0].Value.Str, "the entry writing the description is the least text")
			assert.Zero(t, degradedInfosAt(diags, "Sets"))
		})
	}
}

// TestElection_TwoOneOfUnionsBesideABodyDistributeNeither holds that two oneOf
// unions whose branches declare objects, beside a body, distribute neither:
// the position is the model of the body keeping both entries, in both orders.
func TestElection_TwoOneOfUnionsBesideABodyDistributeNeither(t *testing.T) {
	t.Parallel()
	doc, diags := compileInEveryOrder(t, "3.1.0", func(swapped bool) []string {
		return pairSchemas(swapped, "OneOneObj", "{"+oneObjBranches+"}",
			"{oneOf: [{type: object, properties: {c: {type: string}}}, {type: object, properties: {d: {type: string}}}]}",
			objBody)
	})
	requireErrorsAndWarnings(t, diags, nil)
	assert.Equal(t, degradedKept(entryKey(0), entryKey(1)), keptOn(requireModelOf(t, doc, "OneOneObj")))
	assert.Empty(t, unionsUnder(doc, "OneOneObj"), "neither union distributes")
}

// TestElection_APositionUsedTwiceElectsAsOnce holds that each tie the election
// breaks gives the nodes and findings of a position used once when a response,
// a parameter and two properties use it.
func TestElection_APositionUsedTwiceElectsAsOnce(t *testing.T) {
	t.Parallel()
	const str = "{$ref: '#/components/schemas/Str'}"
	rows := []string{
		"{allOf: [" + anyNumBool + ", " + oneStrInt + "]}",
		"{allOf: [" + oneStrInt + ", " + oneIntBool + "]}",
		"{allOf: [{oneOf: [" + str + "]}, {type: string, minLength: 1}]}",
		"{allOf: [" + str + ", " + str + "]}",
		"{allOf: [" + oneStrInt + ", " + oneStrInt + "]}",
		"{allOf: [{type: string, format: email}, {type: string, format: hostname}]}",
		"{allOf: [{type: string}, {type: string, contentEncoding: base64}]}",
		"{allOf: [{enum: [a, b]}, {enum: [b, a], description: d}]}",
		"{" + objBody + ", allOf: [{" + anyObjBranches + "}, {" + oneObjBranches + "}]}",
	}
	op := func(name, param string) string {
		return "  /" + name + ":\n    get:\n      operationId: " + name + "\n" + param +
			"      responses: {'200': {$ref: '#/components/responses/R'}}\n"
	}
	spec := func(version, row string, twice bool) string {
		paths, holder := "paths:\n"+op("a", ""), "Holder: {type: object, properties: {p: {$ref: '#/components/schemas/X'}}}"
		if twice {
			paths += op("b", "      parameters: [{name: q, in: query, schema: {$ref: '#/components/schemas/X'}}]\n")
			holder = "Holder: {type: object, properties: {p: {$ref: '#/components/schemas/X'}, q: {$ref: '#/components/schemas/X'}}}"
		}
		return admissionSpec(version, paths, "Str: {type: string}", "X: "+row, holder) +
			"  responses:\n    R:\n      description: d\n      content: {application/json: {schema: " + row + "}}\n"
	}
	for _, version := range electionVersions {
		for _, row := range rows {
			t.Run(version+"/"+row, func(t *testing.T) {
				t.Parallel()
				once, onceDiags := compileAnnotationSpec(t, "once", spec(version, row, false))
				twice, twiceDiags := compileAnnotationSpec(t, "twice", spec(version, row, true))
				at := []string{"components/schemas/X", "components/responses/R"}
				require.NotEmpty(t, nodesUnder(once, at...))
				assert.Empty(t, cmp.Diff(nodesUnder(once, at...), nodesUnder(twice, at...)), "the nodes")
				assert.ElementsMatch(t, findingsUnder(onceDiags, "/"), findingsUnder(twiceDiags, "/"))
			})
		}
	}
}
