// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package e2e_test

import (
	"encoding/json/jsontext"
	"fmt"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/ir"
)

// nullableValues are the values a `nullable` is written with, as YAML, each with
// the JSON the document holds, whether the nullable-inference policy reads it
// as admitting null (only the boolean true), and whether it is a null, which
// is the keyword left out.
var nullableValues = []struct {
	yaml, json string
	read       bool
	leftOut    bool
}{
	{"true", "true", true, false},
	{"false", "false", false, false},
	{`"true"`, `"true"`, false, false},
	{"1", "1", false, false},
	{"[true]", "[true]", false, false},
	{"{a: 1}", `{"a":1}`, false, false},
	{`"yes"`, `"yes"`, false, false},
	{"null", "null", false, true},
}

// nullableSchemas writes each value of nullableValues at four positions: on a
// component (C), on a property's schema (P), beside a $ref (R) and on an inline
// entry of an allOf beside a $ref (A), each named with the value's index.
func nullableSchemas() []string {
	schemas := make([]string, 0, 1+4*len(nullableValues))
	schemas = append(schemas, "T: {type: string}")
	for i, v := range nullableValues {
		schemas = append(schemas,
			fmt.Sprintf("C%d: {type: string, nullable: %s}", i, v.yaml),
			fmt.Sprintf("P%d: {type: object, properties: {p: {type: string, nullable: %s}}}", i, v.yaml),
			fmt.Sprintf("R%d: {type: object, properties: {r: {$ref: '#/components/schemas/T', nullable: %s}}}", i, v.yaml),
			fmt.Sprintf("A%d: {allOf: [{$ref: '#/components/schemas/T'}, {nullable: %s}]}", i, v.yaml))
	}
	return schemas
}

// nullableFacts is what a document must say of one value written at the four
// positions: the Unmodeled entries kept for it, by node and key, and the infos
// that report it, by code, as pointers.
type nullableFacts struct {
	kept    map[string]string
	infos   map[string][]jsontext.Pointer
	reasons map[string]ir.UnmodeledReason
}

// factsOfNullable returns what the value at index i leaves: the keyword is kept
// beside its position when no one reads it, and with it an info of the dialect
// not defining it; an allOf entry holding it is kept whole. A value that is read
// leaves one info, of the null read in, and a null is the keyword left out.
func factsOfNullable(i int, policyOn bool) nullableFacts {
	v := nullableValues[i]
	f := nullableFacts{kept: map[string]string{}, infos: map[string][]jsontext.Pointer{}, reasons: map[string]ir.UnmodeledReason{}}
	at := func(name string) jsontext.Pointer {
		return jsontext.Pointer(fmt.Sprintf("/components/schemas/%s%d", name, i))
	}
	switch {
	case v.leftOut:
		return f
	case policyOn && v.read:
		f.infos[diag.InferredNullable] = []jsontext.Pointer{
			at("A") + "/allOf/1/nullable", at("C") + "/nullable", at("P") + "/properties/p/nullable", at("R") + "/properties/r/nullable",
		}
		return f
	}
	f.kept["C"], f.kept["P"], f.kept["R"] = v.json, v.json, v.json
	f.kept["A"] = `{"nullable":` + v.json + `}`
	f.reasons["C"], f.reasons["P"], f.reasons["R"] = ir.ReasonOutOfScope, ir.ReasonOutOfScope, ir.ReasonOutOfScope
	f.reasons["A"] = ir.ReasonDegradedLowering
	f.infos[diag.UnknownSchemaKeyword] = []jsontext.Pointer{
		at("C") + "/nullable", at("P") + "/properties/p/nullable", at("R") + "/properties/r/nullable",
	}
	f.infos[diag.DegradedConstruct] = []jsontext.Pointer{at("A") + "/allOf/1"}
	return f
}

// nullableEntries returns the Unmodeled entry the position kept for its
// nullable, by position letter: a component's own, a property's, and the entry
// of an allOf the component keeps. Each is absent where nothing was kept.
func nullableEntries(t *testing.T, doc *ir.Document, i int) map[string]ir.UnmodeledEntry {
	t.Helper()
	out := map[string]ir.UnmodeledEntry{}
	if e, ok := doc.Types[componentID(fmt.Sprintf("C%d", i))].Common().Unmodeled["openapi:nullable"]; ok {
		out["C"] = e
	}
	for letter, prop := range map[string]string{"P": "p", "R": "r"} {
		model, ok := doc.Types[componentID(fmt.Sprintf("%s%d", letter, i))].(*ir.Model)
		require.True(t, ok, "%s%d lowers to a model", letter, i)
		p := openapitest.PropsByWire(model.Properties)[prop]
		if e, ok := p.Unmodeled["openapi:nullable"]; ok {
			out[letter] = e
		}
		assert.NotContains(t, model.Unmodeled, "openapi:nullable", "%s%d keeps it beside the property", letter, i)
	}
	if e, ok := doc.Types[componentID(fmt.Sprintf("A%d", i))].Common().Unmodeled["openapi:allOf/1"]; ok {
		out["A"] = e
	}
	return out
}

// TestNullable_AValueThePolicyDoesNotReadIsKeptAtOpenAPI31 holds, for the eight
// ways a `nullable` is written and four positions (a component, a property, a
// schema beside a $ref and an allOf entry), that at 3.1 and 3.2, where no
// dialect defines the keyword, the policy takes out the boolean true it reads
// and nothing else: every other value is kept verbatim as an unknown keyword
// under either setting, a null being the keyword left out. With the policy off
// the true is kept as well.
func TestNullable_AValueThePolicyDoesNotReadIsKeptAtOpenAPI31(t *testing.T) {
	t.Parallel()
	for _, version := range []string{"3.1.0", "3.2.0"} {
		for _, policyOn := range []bool{true, false} {
			t.Run(fmt.Sprintf("%s policy on %v", version, policyOn), func(t *testing.T) {
				t.Parallel()
				inBothOrders(t, nullableSchemas(), func(t *testing.T, schemas []string) {
					doc, diags := compileNullable(t, admissionSpec(version, "", schemas...), !policyOn)
					requireVerifies(t, doc, diags)
					requireErrorsAndWarnings(t, diags, nil)

					wantInfos := map[string][]jsontext.Pointer{}
					for i := range nullableValues {
						want := factsOfNullable(i, policyOn)
						for code, pointers := range want.infos {
							wantInfos[code] = append(wantInfos[code], pointers...)
						}
						got := nullableEntries(t, doc, i)
						require.Len(t, got, len(want.kept), "value %s: where it is kept", nullableValues[i].yaml)
						for letter, json := range want.kept {
							assert.Equal(t, want.reasons[letter], got[letter].Reason, "%s%d", letter, i)
							assert.JSONEq(t, json, string(got[letter].Value), "%s%d", letter, i)
						}
					}
					for _, code := range []string{diag.InferredNullable, diag.UnknownSchemaKeyword, diag.DegradedConstruct} {
						slices.Sort(wantInfos[code])
						assert.Equal(t, wantInfos[code], openapitest.PointersOf(diags, code), code)
					}
				})
			})
		}
	}
}

// TestNullable_AValueNoPolicyReadsIsKeptOnANullBranch holds that a null branch
// of a union writing a `nullable` the policy does not read keeps its branch
// whole at 3.1, as it does with the policy off, while one writing the true it
// reads is the null the reference holds and keeps nothing.
func TestNullable_AValueNoPolicyReadsIsKeptOnANullBranch(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		value string
		kept  bool
	}{{"true", false}, {"false", true}, {`"true"`, true}} {
		t.Run(tc.value, func(t *testing.T) {
			t.Parallel()
			schemas := []string{
				"T: {type: string}",
				fmt.Sprintf("U: {oneOf: [{$ref: '#/components/schemas/T'}, {type: 'null', nullable: %s}]}", tc.value),
			}
			inBothOrders(t, schemas, func(t *testing.T, schemas []string) {
				doc, diags := compileNullable(t, admissionSpec("3.1.0", "", schemas...), false)
				requireVerifies(t, doc, diags)
				requireErrorsAndWarnings(t, diags, nil)
				_, kept := doc.Types[componentID("U")].Common().Unmodeled["openapi:oneOf/1"]
				assert.Equal(t, tc.kept, kept)
				want := []jsontext.Pointer(nil)
				if tc.kept {
					want = []jsontext.Pointer{"/components/schemas/U/oneOf/1"}
				}
				assert.Equal(t, want, openapitest.PointersOf(diags, diag.DegradedConstruct))
			})
		})
	}
}

// TestNullable_OpenAPI30KeepsNoValueAsAnUnknownKeyword holds that at 3.0.3,
// where the dialect defines `nullable`, no value is kept as an unknown keyword
// under either setting: a value that is no boolean is the validator's error and
// reads as not nullable, as it did before 3.1's handling of the keyword.
func TestNullable_OpenAPI30KeepsNoValueAsAnUnknownKeyword(t *testing.T) {
	t.Parallel()
	for _, policyOn := range []bool{true, false} {
		t.Run(fmt.Sprintf("policy on %v", policyOn), func(t *testing.T) {
			t.Parallel()
			doc, diags := compileNullable(t, admissionSpec("3.0.3", "", nullableSchemas()...), !policyOn)
			assert.Empty(t, openapitest.PointersOf(diags, diag.UnknownSchemaKeyword))
			for id, td := range doc.Types {
				for key := range td.Common().Unmodeled {
					assert.NotContains(t, key, "nullable", "%s keeps %s", id, key)
				}
			}
			for i := range nullableValues {
				entries := nullableEntries(t, doc, i)
				// An allOf entry the policy does not fold is kept whole at any version, as it was.
				delete(entries, "A")
				assert.Empty(t, entries, "value %s", nullableValues[i].yaml)
			}
		})
	}
}
