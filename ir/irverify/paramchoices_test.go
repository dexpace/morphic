// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package irverify_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/ir/irverify"
)

const choiceOp = "doc.Services[0].Groups[0].Operations[0]"

// choiceParams are the parameter IDs choiceOperation declares.
var choiceParams = []ir.ParamID{"param/x/S/op/a/query", "param/x/S/op/b/query", "param/x/S/op/c/query"}

// choiceOperation is an operation declaring the three parameters of
// choiceParams and stating choices.
func choiceOperation(choices ...ir.ParamChoice) ir.Operation {
	params := make([]ir.Parameter, 0, len(choiceParams))
	for _, id := range choiceParams {
		params = append(params, ir.Parameter{ID: id, Name: named("p")})
	}
	return ir.Operation{ID: "op/x/S/op", Name: named("op"), Params: params, ParamChoices: choices}
}

// variantNames are the names choiceOf gives its variants, in order.
var variantNames = []string{"alpha", "beta", "gamma"}

// choiceOf is a choice named name whose variants name the given parameters,
// one variant each, called alpha, beta and gamma.
func choiceOf(name string, variants ...[]ir.ParamID) ir.ParamChoice {
	vs := make([]ir.ParamVariant, 0, len(variants))
	for _, params := range variants {
		vs = append(vs, ir.ParamVariant{Name: named(variantNames[len(vs)]), Params: params})
	}
	return ir.ParamChoice{Name: named(name), Variants: vs}
}

// choiceFindings returns what Verify reports about op as code at path, for the
// codes a choice among parameters can draw.
func choiceFindings(op ir.Operation) []string {
	var out []string
	for _, v := range irverify.Verify(opDoc(op)) {
		switch v.Code {
		case "ir/param-choice-single", "ir/param-variant-empty", "ir/set-entry-duplicate", "ir/empty-param-ref":
			out = append(out, v.Code+" "+v.Path)
		case "ir/naming-absent":
			// The fixture's own group has no name; only a choice's matters here.
			if strings.Contains(v.Path, ".ParamChoices[") {
				out = append(out, v.Code+" "+v.Path)
			}
		}
	}
	return out
}

// TestVerify_ParamChoicesAreHeldToTheirMeaning drives the check of an
// operation's choices over each violation and the nearest valid document: a
// choice of one variant chooses nothing, a variant naming no parameter says
// nothing, a parameter repeated within a variant is a repeated set entry, and a
// parameter in two variants of one choice, or in two choices, is no fault.
func TestVerify_ParamChoicesAreHeldToTheirMeaning(t *testing.T) {
	t.Parallel()
	a, b, c := choiceParams[0], choiceParams[1], choiceParams[2]
	for _, tc := range []struct {
		name    string
		choices []ir.ParamChoice
		want    []string
	}{
		{"two variants of one parameter each", []ir.ParamChoice{choiceOf("x", []ir.ParamID{a}, []ir.ParamID{b})}, nil},
		{"two choices over the whole list", []ir.ParamChoice{
			choiceOf("x", []ir.ParamID{a}, []ir.ParamID{b, c}),
			choiceOf("y", []ir.ParamID{c}, []ir.ParamID{a}),
		}, nil},
		{"a parameter in two variants of one choice", []ir.ParamChoice{
			choiceOf("x", []ir.ParamID{a, b}, []ir.ParamID{b, c}),
		}, nil},
		{"one variant", []ir.ParamChoice{
			choiceOf("x", []ir.ParamID{a}),
		}, []string{"ir/param-choice-single " + choiceOp + ".ParamChoices[0]"}},
		{"the second choice has one variant", []ir.ParamChoice{
			choiceOf("x", []ir.ParamID{a}, []ir.ParamID{b}),
			choiceOf("y", []ir.ParamID{c}),
		}, []string{"ir/param-choice-single " + choiceOp + ".ParamChoices[1]"}},
		{"no variant", []ir.ParamChoice{
			{Name: named("x")},
		}, []string{"ir/param-choice-single " + choiceOp + ".ParamChoices[0]"}},
		{"a variant with no parameter", []ir.ParamChoice{
			choiceOf("x", []ir.ParamID{a}, nil),
		}, []string{"ir/param-variant-empty " + choiceOp + ".ParamChoices[0].Variants[1]"}},
		{"a parameter listed twice in a variant", []ir.ParamChoice{
			choiceOf("x", []ir.ParamID{a, a}, []ir.ParamID{b}),
		}, []string{"ir/set-entry-duplicate " + choiceOp + ".ParamChoices[0].Variants[0].Params[1]"}},
		{"an empty parameter ID", []ir.ParamChoice{
			choiceOf("x", []ir.ParamID{a}, []ir.ParamID{""}),
		}, []string{"ir/empty-param-ref " + choiceOp + ".ParamChoices[0].Variants[1].Params[0]"}},
		{"a choice with no name", []ir.ParamChoice{
			{Variants: choiceOf("x", []ir.ParamID{a}, []ir.ParamID{b}).Variants},
		}, []string{"ir/naming-absent " + choiceOp + ".ParamChoices[0].Name"}},
		{"a variant with no name", []ir.ParamChoice{{Name: named("x"), Variants: []ir.ParamVariant{
			{Name: named("one"), Params: []ir.ParamID{a}},
			{Params: []ir.ParamID{b}},
		}}}, []string{"ir/naming-absent " + choiceOp + ".ParamChoices[0].Variants[1].Name"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, choiceFindings(choiceOperation(tc.choices...)))
		})
	}
}

// allFindings returns every violation Verify reports about op as code and
// path.
func allFindings(op ir.Operation) []string {
	vs := irverify.Verify(opDoc(op))
	out := make([]string, 0, len(vs))
	for _, v := range vs {
		out = append(out, v.Code+" "+v.Path)
	}
	return out
}

// TestVerify_ParamChoicesAddNoFindingToAValidOperation holds the nearest valid
// document to what it reported before it stated any choice: the same findings
// in the same places.
func TestVerify_ParamChoicesAddNoFindingToAValidOperation(t *testing.T) {
	t.Parallel()
	a, b, c := choiceParams[0], choiceParams[1], choiceParams[2]
	stated := choiceOperation(
		choiceOf("x", []ir.ParamID{a, b}, []ir.ParamID{b, c}),
		choiceOf("y", []ir.ParamID{c}, []ir.ParamID{a}),
	)
	assert.Equal(t, allFindings(choiceOperation()), allFindings(stated))
}
