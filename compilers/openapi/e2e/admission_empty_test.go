// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package e2e_test

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
)

// emptyConjunct is a schema C composing a conjunct that admits no value, with
// the errors and warnings the document draws. A pointer holding "{n}" names
// C's allOf entry written n-th in the case, wherever the order puts it.
type emptyConjunct struct {
	name     string
	versions []string
	others   []string
	entries  []string
	want     []wantFinding
}

// emptyConjuncts are the conjuncts admitting no value an allOf may compose:
// inline, where nothing but the allOf's report says so, and through a $ref or
// as `false`, where the schema or the entry says so itself.
var emptyConjuncts = []emptyConjunct{
	{"an inline type beside a value set of another", bothVersions, nil,
		[]string{"{type: string, enum: [1]}", "{type: object}"},
		[]wantFinding{
			{code: diag.DegradedConstruct, pointer: "/components/schemas/C", contains: []string{disjoint, "/type: string", "/enum: integer", "object"}},
			{code: diag.DegradedConstruct, pointer: "/components/schemas/C/allOf/{0}/enum/0", contains: []string{"no instance matches"}},
		}},
	{"an inline empty enum", bothVersions, nil,
		[]string{"{enum: []}", "{type: string}", "{type: integer}"},
		[]wantFinding{{code: diag.DegradedConstruct, pointer: "/components/schemas/C", contains: []string{disjoint, ": no value", "string", "integer"}}}},
	{"a lone inline conjunct", bothVersions, nil,
		[]string{"{type: string, enum: [1]}"},
		[]wantFinding{
			{code: diag.DegradedConstruct, pointer: "/components/schemas/C", contains: []string{disjoint, "allOf/0/type: string", "allOf/0/enum: integer"}},
			{code: diag.DegradedConstruct, pointer: "/components/schemas/C/allOf/0/enum/0", contains: []string{"no instance matches"}},
		}},
	{"a target its union beside a scalar reports", bothVersions, []string{"X: {type: string, anyOf: [{type: integer}, {type: boolean}]}"},
		[]string{"{$ref: '#/components/schemas/X'}", "{type: string}"},
		[]wantFinding{
			{code: diag.DegradedConstruct, pointer: "/components/schemas/X", contains: []string{"share no value", "anyOf: integer or boolean"}},
			{code: diag.DegradedConstruct, pointer: "/components/schemas/C/allOf/{1}", contains: []string{"declares a type that is not an object"}},
		}},
	{"a target its dead union reports", bothVersions, []string{"X: {type: object, anyOf: [{type: string}, {type: integer}]}"},
		[]string{"{$ref: '#/components/schemas/X'}", "{type: object}"},
		[]wantFinding{{code: diag.DegradedConstruct, pointer: "/components/schemas/X", contains: []string{"none of its 2 branches"}}}},
	{"a target its value set reports", bothVersions, []string{"S: {type: string, enum: [1]}"},
		[]string{"{$ref: '#/components/schemas/S'}", "{type: object}"},
		[]wantFinding{{code: diag.DegradedConstruct, pointer: "/components/schemas/S/enum/0", contains: []string{"no instance matches"}}}},
	{"an alias of a target its value set reports", bothVersions,
		[]string{"S: {type: string, enum: [1]}", "A: {$ref: '#/components/schemas/S'}"},
		[]string{"{$ref: '#/components/schemas/A'}", "{type: object}"},
		[]wantFinding{{code: diag.DegradedConstruct, pointer: "/components/schemas/S/enum/0", contains: []string{"no instance matches"}}}},
	{"a target its empty enum reports", bothVersions, []string{"E: {enum: []}"},
		[]string{"{$ref: '#/components/schemas/E'}", "{type: string}", "{type: integer}"},
		[]wantFinding{
			{code: diag.EmptyEnum, pointer: "/components/schemas/E"},
			{code: diag.DegradedConstruct, pointer: "/components/schemas/C/allOf/{1}", contains: []string{"not an object"}},
			{code: diag.DegradedConstruct, pointer: "/components/schemas/C/allOf/{2}", contains: []string{"not an object"}},
		}},
	{"a false entry", []string{"3.1.0"}, nil,
		[]string{"false", "{type: string}", "{type: integer}"},
		[]wantFinding{
			{code: diag.DegradedConstruct, pointer: "/components/schemas/C/allOf/{1}", contains: []string{"not an object"}},
			{code: diag.DegradedConstruct, pointer: "/components/schemas/C/allOf/{2}", contains: []string{"not an object"}},
		}},
}

// bothVersions is the two versions a case compiles at.
var bothVersions = []string{"3.0.3", "3.1.0"}

// TestCompile_AnEmptyConjunctIsReportedWhereNothingElseSaysSo holds that an
// allOf one of whose conjuncts admits no value is reported at the schema,
// naming it "no value", where nothing else says so: an inline empty enum, and
// a $ref to a schema whose type and union share nothing. An inline value set
// outside its own type lowers the position, so the report names its type and
// its set, as beside the allOf. A `false` entry, or a $ref to a schema whose
// value set or empty enum is reported, says so itself, and the allOf draws
// nothing more. Each holds at both versions, in every order.
func TestCompile_AnEmptyConjunctIsReportedWhereNothingElseSaysSo(t *testing.T) {
	t.Parallel()
	for _, tc := range emptyConjuncts {
		for _, version := range tc.versions {
			for _, swapped := range []bool{false, true} {
				order := make([]int, len(tc.entries))
				for i := range order {
					order[i] = i
				}
				if swapped {
					slices.Reverse(order)
				}
				written := make([]string, len(order))
				for at, n := range order {
					written[at] = tc.entries[n]
				}
				want := slices.Clone(tc.want)
				for i := range want {
					for at, n := range order {
						want[i].pointer = strings.ReplaceAll(want[i].pointer, fmt.Sprintf("{%d}", n), fmt.Sprint(at))
					}
				}
				schemas := append([]string{"C: {allOf: [" + strings.Join(written, ", ") + "]}"}, tc.others...)
				t.Run(fmt.Sprintf("%s/%s/entries-swapped-%t", tc.name, version, swapped), func(t *testing.T) {
					t.Parallel()
					inBothOrders(t, schemas, func(t *testing.T, schemas []string) {
						_, diags := compileAnnotationSpec(t, "empty", admissionSpec(version, "", schemas...))
						requireErrorsAndWarnings(t, diags, slices.Clone(want))
					})
				})
			}
		}
	}
}
