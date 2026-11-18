// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package e2e_test

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/ir"
)

// nullSharedRests are the rests of an allOf a union of a string and another
// branch conjoins with, an object that admits null or not, by version.
var nullSharedRests = []struct {
	name   string
	schema map[string]string
	null   bool
}{
	{"object", map[string]string{
		"3.0.3": "{type: object, properties: {a: {type: string}}}",
		"3.1.0": "{type: object, properties: {a: {type: string}}}",
	}, false},
	{"object or null", map[string]string{
		"3.0.3": "{type: object, nullable: true, properties: {a: {type: string}}}",
		"3.1.0": "{type: [object, 'null'], properties: {a: {type: string}}}",
	}, true},
}

// nullSharedBranches are the branches written beside a $ref to a string, each
// admitting no object, and null or not, by version: written inline, as a
// type list or nullable, or through a $ref.
var nullSharedBranches = []struct {
	name   string
	schema map[string]string
	null   bool
}{
	{"boolean", map[string]string{"3.0.3": "{type: boolean}", "3.1.0": "{type: boolean}"}, false},
	{"null alone", map[string]string{"3.0.3": "{enum: [null]}", "3.1.0": "{type: 'null'}"}, true},
	{"nullable boolean", map[string]string{
		"3.0.3": "{type: boolean, nullable: true}", "3.1.0": "{type: [boolean, 'null']}",
	}, true},
	{"ref to a nullable boolean", map[string]string{
		"3.0.3": "{$ref: '#/components/schemas/NB'}", "3.1.0": "{$ref: '#/components/schemas/NB'}",
	}, true},
}

// TestCompile_AUnionAndARestShareNullOnlyWhereBothAdmitIt holds that a union
// sharing no kind with the rest of its allOf shares a null with it only where
// both admit null: a union no branch of which admits null, beside a rest
// admitting it, draws once the warning that no instance satisfies the schema,
// the Model the rest gives keeping the union, and one with a branch admitting
// null draws none. At both versions, for a oneOf and an anyOf, beside the
// allOf, on an entry and beside a $ref, the rest inline and a $ref, in both
// orders of the branches and of the components (GitHub #2055).
func TestCompile_AUnionAndARestShareNullOnlyWhereBothAdmitIt(t *testing.T) {
	t.Parallel()
	for _, version := range bothVersions {
		for _, rest := range nullSharedRests {
			for _, branch := range nullSharedBranches {
				for _, keyword := range []string{"oneOf", "anyOf"} {
					for _, ref := range []bool{false, true} {
						for _, spelling := range []string{"beside", "entry", "beside the $ref"} {
							if spelling == "beside the $ref" && (!ref || version == "3.0.3") {
								continue // a union beside a $ref is one only from 3.1
							}
							name := fmt.Sprintf("%s/%s/%s/%s/ref=%t/%s", version, rest.name, branch.name, keyword, ref, spelling)
							t.Run(name, func(t *testing.T) {
								t.Parallel()
								checkNullShared(t, version, keyword, rest.schema[version], branch.schema[version], ref, spelling,
									rest.null && branch.null)
							})
						}
					}
				}
			}
		}
	}
}

// checkNullShared compiles P, the union of a $ref to a string and branch,
// written as spelling says: beside an allOf of rest, on an entry beside it, or
// beside the $ref to it, the rest a $ref where ref says, in both orders of the
// branches and of the components, and holds what P lowers to to shared,
// whether the two share a value: only null can be one.
func checkNullShared(t *testing.T, version, keyword, rest, branch string, ref bool, spelling string, shared bool) {
	t.Helper()
	for _, branches := range []string{"{$ref: '#/components/schemas/Str'}, " + branch, branch + ", {$ref: '#/components/schemas/Str'}"} {
		restAt := rest
		if ref {
			restAt = "{$ref: '#/components/schemas/R'}"
		}
		union := keyword + ": [" + branches + "]"
		p := map[string]string{
			"beside":          "P: {allOf: [" + restAt + "], " + union + "}",
			"entry":           "P: {allOf: [" + restAt + ", {" + union + "}]}",
			"beside the $ref": "P: {$ref: '#/components/schemas/R', " + union + "}",
		}[spelling]
		schemas := []string{"R: " + rest, "Str: {type: string}", "NB: {type: boolean, nullable: true}", p}
		if version != "3.0.3" {
			schemas[2] = "NB: {type: [boolean, 'null']}"
		}
		inBothOrders(t, schemas, func(t *testing.T, schemas []string) {
			doc, diags := compileAnnotationSpec(t, "null-shared", admissionSpec(version, "", schemas...))
			if !shared {
				requireErrorsAndWarnings(t, diags, []wantFinding{{code: diag.DegradedConstruct, pointer: "/components/schemas/P",
					contains: []string{"share no value", "so no instance satisfies this schema"}}})
				m, ok := typeByName(doc, "P").(*ir.Model)
				require.True(t, ok, "the schema is the Model the rest of its allOf gives")
				kept, ok := m.Unmodeled["openapi:"+keyword]
				require.True(t, ok, "the union is kept: %v", keptOn(m))
				assert.Equal(t, ir.ReasonDegradedLowering, kept.Reason)
				return
			}
			for _, d := range diags {
				assert.NotContains(t, d.Message, "so no instance satisfies this schema", "null is a value the two share")
			}
			if u, ok := typeByName(doc, "P").(*ir.Union); ok {
				assert.Len(t, u.Variants, 1, "the branch admitting null is the one variant")
				return
			}
			m, ok := typeByName(doc, "P").(*ir.Model)
			require.True(t, ok, "a union whose null branch alone lives keeps it on the Model")
			assert.Contains(t, keptOn(m), "openapi:"+keyword)
		})
	}
}
