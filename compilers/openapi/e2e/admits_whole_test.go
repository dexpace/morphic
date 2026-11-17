// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package e2e_test

import (
	"encoding/json/v2"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/ir"
)

// fieldClosure is the JSON of the property wire of the model id and of every
// node a consumer of it reaches: each type a "target" names, and each component
// a $ref kept verbatim names. A keyword one of the field's declarations writes
// is held where it shows in it, as a type, a constraint or an Unmodeled entry.
func fieldClosure(t *testing.T, doc *ir.Document, id, wire string) string {
	t.Helper()
	tree, ok := documentTree(t, doc).(map[string]any)
	require.True(t, ok)
	types, ok := tree["types"].(map[string]any)
	require.True(t, ok)
	model, ok := types[id].(map[string]any)
	require.True(t, ok, "%s is a node", id)
	props, _ := model["properties"].([]any)
	var queue []any
	for _, p := range props {
		if m, ok := p.(map[string]any); ok && m["wireName"] == wire {
			queue = append(queue, m)
		}
	}
	require.Len(t, queue, 1, "%s declares %q once", id, wire)
	seen := map[string]bool{}
	var out []string
	for len(queue) > 0 {
		next := queue[0]
		queue = queue[1:]
		raw, err := json.Marshal(next, json.Deterministic(true))
		require.NoError(t, err)
		out = append(out, string(raw))
		walkObjects(next, func(m map[string]any) {
			reached, _ := m["target"].(string)
			if ref, ok := m["$ref"].(string); ok {
				if name, found := strings.CutPrefix(ref, "#/components/schemas/"); found {
					reached = "t/openapi/components/schemas/" + name
				}
			}
			if reached != "" && !seen[reached] && types[reached] != nil {
				seen[reached] = true
				queue = append(queue, types[reached])
			}
		})
	}
	return strings.Join(out, "\n")
}

// requireHeld fails t unless closure holds, for each fact, one of the
// spellings it lists: a keyword's value as the IR states it or as a verbatim
// entry writes it.
func requireHeld(t *testing.T, closure string, facts [][]string) {
	t.Helper()
	for _, spellings := range facts {
		held := false
		for _, s := range spellings {
			held = held || strings.Contains(closure, s)
		}
		assert.True(t, held, "the field holds one of %q:\n%s", spellings, closure)
	}
}

// partlyAdmitted is a structure writing properties beside what admits only some
// instances of a kind, each with the facts its declarations write: where the
// field keeps its node, and where it keeps it verbatim.
var partlyAdmitted = []struct {
	name, kind, structure string
	target                []string
	facts                 [][]string
}{
	{"bound", "integer", refStructure, []string{"T: {type: integer, minimum: 5}"},
		[][]string{{`"min":"5"`, `"minimum":5`}}},
	{"value set", "integer", refStructure, []string{"T: {enum: [1, 2]}"},
		[][]string{{`"num":"1"`}, {`"num":"2"`}}},
	{"pattern", "string", refStructure, []string{"T: {type: string, pattern: '^a'}"},
		[][]string{{`"pattern":"^a"`}}},
	{"union branch", "integer", refStructure, []string{"T: {anyOf: [{type: object}, {type: integer, minimum: 5}]}"},
		[][]string{{`"min":"5"`}}},
	{"untyped bound", "integer", refStructure, []string{"T: {minimum: 5}"},
		[][]string{{`"min":"5"`, `"minimum":5`}}},
	{"untyped format", "integer", refStructure, []string{"T: {format: int32}"},
		[][]string{{`"int32"`}}},
	{"bound beside", "integer", "{properties: {a: {type: string}}, minimum: 5}", nil,
		[][]string{{`"openapi:minimum"`, `"minimum":5`}}},
}

// refStructure is a structure composing T beside a property of its own.
const refStructure = "{allOf: [{$ref: '#/components/schemas/T'}], properties: {a: {type: string}}}"

// TestCompile_AStructureAdmittingPartOfAKindConflictsWithIt holds that a field
// declared as a kind in one allOf branch and, in the other, as a structure
// whose source admits that kind but refuses some instance of it, through a
// bound, a value set, a pattern, a format or a union branch narrowing it, is
// reported as a conflict: the later declaration's type is the field's and the
// earlier is kept verbatim, so every keyword either writes stays in the
// document, at 3.0 and 3.1, in both orders of the components and of the
// branches.
func TestCompile_AStructureAdmittingPartOfAKindConflictsWithIt(t *testing.T) {
	t.Parallel()
	for _, tc := range partlyAdmitted {
		for _, version := range []string{"3.0.3", "3.1.0"} {
			for _, swapped := range []bool{false, true} {
				kind := "{properties: {p: {type: " + tc.kind + "}}}"
				structure := "{properties: {p: " + tc.structure + "}}"
				first, later := kind, structure
				if swapped {
					first, later = structure, kind
				}
				schemas := append([]string{"C: {allOf: [" + first + ", " + later + "]}"}, tc.target...)
				t.Run(fmt.Sprintf("%s/%s/branches-swapped-%t", tc.name, version, swapped), func(t *testing.T) {
					t.Parallel()
					inBothOrders(t, schemas, func(t *testing.T, schemas []string) {
						doc, diags := compileAnnotationSpec(t, "partly", admissionSpec(version, "", schemas...))
						requireErrorsAndWarnings(t, diags, []wantFinding{{
							code: diag.ConflictingRedecl, pointer: "/components/schemas/C/allOf/0/properties/p",
							contains: []string{"incompatible types"},
						}})
						kept := "openapi:conflicting-redeclaration/components/schemas/C/allOf/0/properties/p"
						assert.Equal(t, []string{"/components/schemas/C/allOf/0/properties/p"}, keptEntries(t, doc, kept))
						target, _ := redeclaredField(t, doc, diags, "t/openapi/components/schemas/C", "p", "/components/schemas/C")
						facts := append([][]string{{`"wireName":"a"`, `"properties":{"a":`}}, tc.facts...)
						if swapped {
							assert.Equal(t, ir.TypeID("t/prim/"+tc.kind), target, "the later declaration's kind")
						} else {
							assert.Equal(t, ir.TypeID("t/anon/components/schemas/C/allOf/1/properties/p"), target)
							facts = append(facts, []string{`"value":{"type":"` + tc.kind + `"}`})
						}
						requireHeld(t, fieldClosure(t, doc, "t/openapi/components/schemas/C", "p"), facts)
					})
				})
			}
		}
	}
}
