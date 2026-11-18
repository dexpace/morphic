// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package e2e_test

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
)

// pairAloneObjects is a union's branches, each an object declaring a member of
// its own.
const pairAloneObjects = "[{type: object, properties: {p: {type: string}}, required: [p]}, " +
	"{type: object, properties: {q: {type: string}}, required: [q]}]"

// unionPairsAlone are a oneOf and an anyOf written with nothing beside them, by
// the kinds of their branches, some sharing no value, each with what the
// position lowers to: the union elected, its variant count and the reason the
// other is kept under. A union of null alone
// elected is an Enum keeping both, and a null branch saying more is kept at
// its index, its oneOf's second branch where saysMore.
var unionPairsAlone = []struct {
	name, oneOf, anyOf, elected string
	variants                    int
	kept                        ir.UnmodeledReason
	null, saysMore              bool
}{
	{name: "an anyOf each oneOf branch satisfies", oneOf: pairAloneObjects, anyOf: "[{type: object}]",
		elected: "oneOf", variants: 2, kept: ir.ReasonDegradedLowering},
	{name: "a oneOf of null alone", oneOf: "[{type: 'null'}]", anyOf: "[{required: [a]}]",
		elected: "null", kept: ir.ReasonDegradedLowering, null: true},
	{name: "both of objects", oneOf: pairAloneObjects, elected: "oneOf", variants: 2, kept: ir.ReasonDegradedLowering,
		anyOf: "[{type: object, properties: {x: {type: string}}, required: [x]}, {type: object, properties: {y: {type: string}}}]"},
	{name: "an anyOf declaring no shape", oneOf: pairAloneObjects, anyOf: "[{required: [x]}, {required: [y]}]",
		elected: "oneOf", variants: 2, kept: ir.ReasonValidationOnly},
	{name: "a oneOf declaring no shape", oneOf: "[{required: [x]}, {}]", anyOf: pairAloneObjects,
		elected: "anyOf", variants: 2, kept: ir.ReasonValidationOnly},
	{name: "a oneOf with a null branch", oneOf: "[" + entryRef + ", {type: 'null'}]", anyOf: pairAloneObjects,
		elected: "oneOf", variants: 1, kept: ir.ReasonDegradedLowering, null: true},
	{name: "a oneOf with a null branch saying more", oneOf: "[" + entryRef + ", {type: 'null', description: none}]",
		anyOf: pairAloneObjects, elected: "oneOf", variants: 1, kept: ir.ReasonDegradedLowering, null: true, saysMore: true},
	{name: "scalars sharing a kind", oneOf: "[{type: string}, {type: integer}]",
		anyOf: "[{type: string, maxLength: 3}, {type: boolean}]", elected: "oneOf", variants: 2, kept: ir.ReasonDegradedLowering},
	{name: "scalars sharing a null alone", oneOf: "[{type: string}, {type: 'null'}]",
		anyOf: "[{type: integer}, {type: 'null'}]", elected: "oneOf", variants: 1, kept: ir.ReasonDegradedLowering, null: true},
	{name: "objects beside scalars", oneOf: pairAloneObjects, anyOf: "[{type: string}, {type: integer}]",
		elected: "oneOf", variants: 2, kept: ir.ReasonDegradedLowering},
	{name: "scalars beside objects", oneOf: "[{type: string}, {type: integer}]", anyOf: pairAloneObjects,
		elected: "oneOf", variants: 2, kept: ir.ReasonDegradedLowering},
	{name: "scalars sharing no kind", oneOf: "[{type: string}, {type: integer}]", anyOf: "[{type: boolean}]",
		elected: "oneOf", variants: 2, kept: ir.ReasonDegradedLowering},
	{name: "objects beside a null alone", oneOf: pairAloneObjects, anyOf: "[{type: 'null'}]",
		elected: "oneOf", variants: 2, kept: ir.ReasonValidationOnly, null: true},
}

// unionPairAloneSpellings are the ways to write the oneOf one and the anyOf
// with nothing beside them: on one schema, on one inline allOf entry and on
// two, each in either order, and one allOf deeper. The one-entry spelling is
// the one each other is compared to.
func unionPairAloneSpellings(one, anyOf string) map[string]string {
	return map[string]string{
		"one entry":                    "{allOf: [{oneOf: " + one + ", anyOf: " + anyOf + "}]}",
		"one entry, the anyOf first":   "{allOf: [{anyOf: " + anyOf + ", oneOf: " + one + "}]}",
		"one schema":                   "{oneOf: " + one + ", anyOf: " + anyOf + "}",
		"one schema, the anyOf first":  "{anyOf: " + anyOf + ", oneOf: " + one + "}",
		"two entries":                  "{allOf: [{oneOf: " + one + "}, {anyOf: " + anyOf + "}]}",
		"two entries, the anyOf first": "{allOf: [{anyOf: " + anyOf + "}, {oneOf: " + one + "}]}",
		"two entries one allOf deeper": "{allOf: [{allOf: [{oneOf: " + one + "}, {anyOf: " + anyOf + "}]}]}",
		"one entry one allOf deeper":   "{allOf: [{allOf: [{oneOf: " + one + ", anyOf: " + anyOf + "}]}]}",
	}
}

// reversedBranches is the branch list written as a YAML flow sequence of
// mappings, in reverse.
func reversedBranches(t *testing.T, list string) string {
	t.Helper()
	inner, ok := strings.CutPrefix(list, "[")
	require.True(t, ok)
	inner, ok = strings.CutSuffix(inner, "]")
	require.True(t, ok)
	var branches []string
	depth, start := 0, 0
	for i, r := range inner {
		switch {
		case r == '{' || r == '[':
			depth++
		case r == '}' || r == ']':
			depth--
		case r == ',' && depth == 0:
			branches = append(branches, strings.TrimSpace(inner[start:i]))
			start = i + 1
		}
	}
	branches = append(branches, strings.TrimSpace(inner[start:]))
	slices.Reverse(branches)
	return "[" + strings.Join(branches, ", ") + "]"
}

// TestElection_TwoUnionsAloneLowerAsOneSchemaWritingBoth holds that a oneOf and
// an anyOf with nothing beside them lower alike on one schema, on one allOf
// entry and on two, in either order of the entries, the keywords, the branches
// and the components, and one allOf deeper, at every version and position, by
// every kind of pair: the position is a Union of the union competing keywords
// elects, keeping the other under its keyword, as validation logic where its
// branches declare no shape, and draws no warning, though no instance may
// satisfy the two together (GitHub #2011).
func TestElection_TwoUnionsAloneLowerAsOneSchemaWritingBoth(t *testing.T) {
	t.Parallel()
	for _, version := range []string{"3.0.3", "3.1.0", "3.2.0"} {
		for _, site := range bothKeywordsSites {
			for _, pair := range unionPairsAlone {
				if pair.null && version == "3.0.3" {
					continue
				}
				for _, reversed := range []bool{false, true} {
					t.Run(fmt.Sprintf("%s/%s/%s/%t", version, site.name, pair.name, reversed), func(t *testing.T) {
						t.Parallel()
						one, anyOf := pair.oneOf, pair.anyOf
						if reversed {
							one, anyOf = reversedBranches(t, one), reversedBranches(t, anyOf)
						}
						doc, diags := compileSpellingsAlike(t, site, version, unionPairAloneSpellings(one, anyOf))
						requireErrorsAndWarnings(t, diags, nil)
						kept := map[string]ir.UnmodeledReason{}
						if pair.saysMore {
							kept[fmt.Sprintf("openapi:oneOf/%d", map[bool]int{false: 1, true: 0}[reversed])] = ir.ReasonNoIRHome
						}
						requirePairAlone(t, kindNode(t, doc, entryPositionID(site.at)), pair.elected, pair.variants, pair.kept, kept)
					})
				}
			}
		}
	}
}

// requirePairAlone holds node, what a oneOf and an anyOf alone lowered to, to a
// Union of variants variants of the union elected, keeping the other for
// reason beside what more holds, or, where elected is "null", to the Enum of
// null alone keeping both.
func requirePairAlone(t *testing.T, node ir.TypeDef, elected string, variants int, reason ir.UnmodeledReason,
	more map[string]ir.UnmodeledReason,
) {
	t.Helper()
	if elected == "null" {
		enum, ok := node.(*ir.Enum)
		require.True(t, ok, "the position is null alone")
		assert.Empty(t, enum.Members)
		assert.Equal(t, map[string]ir.UnmodeledReason{"openapi:oneOf": reason, "openapi:anyOf": reason},
			keptOn(enum), "the Enum keeps both unions")
		return
	}
	union, ok := node.(*ir.Union)
	require.True(t, ok, "the position is a Union of the union elected")
	assert.Len(t, union.Variants, variants)
	assert.Equal(t, elected == "oneOf", union.Exclusive, "the union elected")
	other := "oneOf"
	if elected == "oneOf" {
		other = "anyOf"
	}
	more["openapi:"+other] = reason
	assert.Equal(t, more, keptOn(union), "the Union keeps the other union")
}
