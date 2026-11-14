// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package irverify_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/ir/irverify"
)

// TestVerify_ParamChoiceNamesAreDistinct pins that a consumer can key the
// choices of an operation, and the variants of a choice, by name: a source
// name given twice is reported at its later index, and names alike in their
// canonical words alone are not.
func TestVerify_ParamChoiceNamesAreDistinct(t *testing.T) {
	t.Parallel()
	a, b, c := choiceParams[0], choiceParams[1], choiceParams[2]
	twice := []ir.ParamVariant{{Name: named("v"), Params: []ir.ParamID{a}}, {Name: named("v"), Params: []ir.ParamID{b}}}
	for _, tc := range []struct {
		name    string
		choices []ir.ParamChoice
		want    []string
	}{
		{"two choices of one name", []ir.ParamChoice{
			choiceOf("x", []ir.ParamID{a}, []ir.ParamID{b}), choiceOf("y", []ir.ParamID{c}, []ir.ParamID{a}),
			choiceOf("x", []ir.ParamID{b}, []ir.ParamID{c}),
		}, []string{"ir/param-choice-name-duplicate " + choiceOp + ".ParamChoices[2].Name"}},
		{"two variants of one name", []ir.ParamChoice{{Name: named("x"), Variants: twice}},
			[]string{"ir/param-choice-name-duplicate " + choiceOp + ".ParamChoices[0].Variants[1].Name"}},
		{"a variant named as another choice", []ir.ParamChoice{
			choiceOf("alpha", []ir.ParamID{a}, []ir.ParamID{b}),
		}, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var got []string
			for _, v := range irverify.Verify(opDoc(choiceOperation(tc.choices...))) {
				if v.Code == "ir/param-choice-name-duplicate" {
					got = append(got, v.Code+" "+v.Path)
				}
			}
			assert.Equal(t, tc.want, got)
		})
	}
}
