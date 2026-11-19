// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package e2e_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/internal/harness"
	"github.com/dexpace/morphic/ir"
)

// TestCompile_AnEntryUnionNoBranchOfWhichLivesIsKeptOnTheEntry holds that a
// union on an allOf entry, a branch of it a $ref, none of whose branches an
// object satisfies, is kept under its keyword on the entry rather than
// distributed: the schema is the Model its other entry gives, and draws the
// warning docs/compilers/openapi.md#allof-disjoint-conjuncts gives, as the
// same union of inline branches does (GitHub #2012), at 3.0.3 and 3.1.0,
// under oneOf and anyOf, in both orders of the branches.
func TestCompile_AnEntryUnionNoBranchOfWhichLivesIsKeptOnTheEntry(t *testing.T) {
	t.Parallel()
	for _, version := range bothVersions {
		for _, keyword := range []string{"oneOf", "anyOf"} {
			for _, branches := range []string{"[{$ref: '#/components/schemas/Str'}, {type: boolean}]",
				"[{type: boolean}, {$ref: '#/components/schemas/Str'}]"} {
				spec := admissionSpec(version, "", "Obj: {type: object, properties: {a: {type: string}}}",
					"Str: {type: string}",
					fmt.Sprintf("P: {allOf: [{$ref: '#/components/schemas/Obj'}, {%s: %s}]}", keyword, branches))
				t.Run(version+"/"+keyword+"/"+branches, func(t *testing.T) {
					t.Parallel()
					res := harness.Check(t.Context(), "spec.yaml", []byte(spec))
					require.Equal(t, harness.OutcomeOK, res.Outcome, res.Detail)
					doc, diags := compileAnnotationSpec(t, "entry", spec)
					requireErrorsAndWarnings(t, diags, []wantFinding{{code: diag.DegradedConstruct,
						pointer: "/components/schemas/P", contains: []string{"share no value"}}})
					m, ok := typeByName(doc, "P").(*ir.Model)
					require.True(t, ok, "the schema is the Model its other entry gives")
					require.NotNil(t, m.Base)
					assert.Equal(t, componentID("Obj"), m.Base.Target)
					kept, ok := m.Unmodeled["openapi:"+keyword]
					require.True(t, ok, "the union is kept on the entry")
					assert.Equal(t, "/components/schemas/P/allOf/1/"+keyword, string(kept.Provenance.Pointer))
					for id := range doc.Types {
						assert.False(t, strings.HasPrefix(string(id), "t/composed/components/schemas/P/"),
							"no variant is composed: %s", id)
					}
				})
			}
		}
	}
}

// TestCompile_ABodyBesideANullBranchAndNoLiveOneAdmitsNull holds that the
// reports of a union none of whose branches but its null one an object
// satisfies, beside a body admitting null, say what is true: null satisfies
// the schema, and the union has as many branches as it writes.
func TestCompile_ABodyBesideANullBranchAndNoLiveOneAdmitsNull(t *testing.T) {
	t.Parallel()
	spec := admissionSpec("3.1.0", "",
		"S: {type: [object, 'null'], properties: {p: {type: string}}, oneOf: [{type: 'null'}, {type: integer}]}")
	_, diags := compileAnnotationSpec(t, "nullbranch", spec)
	requireErrorsAndWarnings(t, diags, []wantFinding{{code: diag.DegradedConstruct, pointer: "/components/schemas/S",
		contains: []string{"none of its 2 branches but a null one", "no instance but null satisfies this schema"}}})
	for _, d := range diags {
		if d.Provenance.Pointer == "/components/schemas/S" {
			assert.NotContains(t, d.Message, "no instance satisfies", "null satisfies it: %s", d.Message)
		}
	}
}

// TestCompile_AnEntryUnionBesideARestAdmittingNullIsKeptAndWarns holds that a
// union on an allOf entry, none of whose branches admits an object or null,
// beside a rest admitting both, is kept on its entry as one beside an object
// body is beside it, and draws the one warning that the conjuncts share no
// value: a oneOf or an anyOf no branch of which admits null refuses it, so the
// null the rest admits is no value they share
// (docs/compilers/openapi.md#allof-disjoint-conjuncts). The rest admits null by
// `type: [object, "null"]` at 3.1.0 and by `nullable: true` at 3.0.3, in both
// orders of the branches and of the components.
func TestCompile_AnEntryUnionBesideARestAdmittingNullIsKeptAndWarns(t *testing.T) {
	t.Parallel()
	bodies := map[string]string{"3.0.3": "type: object, nullable: true", "3.1.0": "type: [object, 'null']"}
	for _, version := range bothVersions {
		for _, keyword := range []string{"oneOf", "anyOf"} {
			for _, branches := range []string{"[{$ref: '#/components/schemas/Str'}, {type: boolean}]",
				"[{type: boolean}, {$ref: '#/components/schemas/Str'}]"} {
				schemas := []string{"ON: {" + bodies[version] + ", properties: {a: {type: string}}}",
					"Str: {type: string}",
					fmt.Sprintf("P: {allOf: [{$ref: '#/components/schemas/ON'}, {%s: %s}]}", keyword, branches)}
				t.Run(version+"/"+keyword+"/"+branches, func(t *testing.T) {
					t.Parallel()
					inBothOrders(t, schemas, func(t *testing.T, schemas []string) {
						spec := admissionSpec(version, "", schemas...)
						res := harness.Check(t.Context(), "spec.yaml", []byte(spec))
						require.Equal(t, harness.OutcomeOK, res.Outcome, res.Detail)
						doc, diags := compileAnnotationSpec(t, "entry-null", spec)
						requireErrorsAndWarnings(t, diags, []wantFinding{{code: diag.DegradedConstruct,
							pointer: "/components/schemas/P", contains: []string{
								"the conjuncts of this allOf share no value (allOf/0: object, allOf/1: string or boolean)",
								"so no instance satisfies this schema"}}})
						m, ok := typeByName(doc, "P").(*ir.Model)
						require.True(t, ok, "the schema is the Model the rest of its allOf gives")
						require.NotNil(t, m.Base)
						assert.Equal(t, componentID("ON"), m.Base.Target)
						kept, ok := m.Unmodeled["openapi:"+keyword]
						require.True(t, ok, "the union is kept on the entry")
						assert.Equal(t, "/components/schemas/P/allOf/1/"+keyword, string(kept.Provenance.Pointer))
					})
				})
			}
		}
	}
}

// TestCompile_AnEntryUnionWhoseNullBranchAloneLivesSaysWhatNullMeets holds the
// warning of a union on an allOf entry whose only branch not dead is its null
// one to the null the rest of the allOf admits: beside a rest admitting null,
// null alone satisfies the schema; beside one keeping it out, nothing does,
// which the report of conjuncts sharing no value says, and says too in either
// order where a cycle cuts short the read of another conjunct's null, as the
// object read on its own keeps null out.
func TestCompile_AnEntryUnionWhoseNullBranchAloneLivesSaysWhatNullMeets(t *testing.T) {
	t.Parallel()
	branches := "{oneOf: [{type: 'null'}, {$ref: '#/components/schemas/Str'}]}"
	spec := admissionSpec("3.1.0", "", "Obj: {type: object, properties: {a: {type: string}}}",
		"ON: {type: [object, 'null'], properties: {a: {type: string}}}", "Str: {type: string}",
		"Cyc: {properties: {n: {$ref: '#/components/schemas/Cyc'}}, allOf: [{$ref: '#/components/schemas/Cyc'}]}",
		"Admits: {allOf: [{$ref: '#/components/schemas/ON'}, "+branches+"]}",
		"Refuses: {allOf: [{$ref: '#/components/schemas/Obj'}, "+branches+"]}",
		"Unknown: {allOf: [{$ref: '#/components/schemas/Obj'}, {$ref: '#/components/schemas/Cyc'}, "+branches+"]}")
	_, diags := compileAnnotationSpec(t, "entry-null-branch", spec)
	requireErrorsAndWarnings(t, diags, []wantFinding{
		{code: diag.DegradedConstruct, pointer: "/components/schemas/Admits", contains: []string{
			"none of the 2 branches of the oneOf on allOf/1 but a null one admits a value the rest",
			"(allOf/1/oneOf/1: string)", "so no instance but null satisfies this schema"}},
		{code: diag.DegradedConstruct, pointer: "/components/schemas/Refuses", contains: []string{
			"the conjuncts of this allOf share no value (allOf/0: object, allOf/1: string)"}},
		{code: diag.DegradedConstruct, pointer: "/components/schemas/Unknown", contains: []string{
			"the conjuncts of this allOf share no value (allOf/0: object, allOf/2: string)"}},
	})
}
