// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package e2e_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dexpace/morphic/ir"
)

// TestCompile_AUnionBesideAnyRestDrawsOneWarningAtMost holds every schema of a
// grid, a rest of the allOf (a $ref to a string, to an object, an enum, a
// union, a $ref naming no schema, the top type, `type: object`) beside a union
// of two branches (a string, a boolean, a $ref to an object, null), written
// beside the allOf and on an entry, as a oneOf and an anyOf, to one warning at
// its own pointer at most, and a dead-branch warning to a rest admitting
// objects alone (docs/compilers/openapi.md#union-beside-structure).
func TestCompile_AUnionBesideAnyRestDrawsOneWarningAtMost(t *testing.T) {
	t.Parallel()
	rests := map[string]string{
		"str": "{$ref: '#/components/schemas/Str'}", "obj": "{$ref: '#/components/schemas/Obj'}",
		"enum": "{enum: [a, b]}", "union": "{oneOf: [{$ref: '#/components/schemas/Str'}, {type: integer}]}",
		"missing": "{$ref: '#/components/schemas/Missing'}", "top": "{}", "objtype": "{type: object}",
	}
	branches := []string{"{type: string}", "{type: boolean}", "{$ref: '#/components/schemas/Obj'}", "{type: 'null'}"}
	schemas := []string{"Str: {type: string}", "Obj: {type: object, properties: {a: {type: string}}}"}
	objects := map[string]bool{}
	for rest, written := range rests {
		for i := range branches {
			for j := i + 1; j < len(branches); j++ {
				for _, keyword := range []string{"oneOf", "anyOf"} {
					union := fmt.Sprintf("%s: [%s, %s]", keyword, branches[i], branches[j])
					beside := fmt.Sprintf("G%s%d%d%sBeside", rest, i, j, keyword)
					entry := fmt.Sprintf("G%s%d%d%sEntry", rest, i, j, keyword)
					schemas = append(schemas, beside+": {allOf: ["+written+"], "+union+"}",
						entry+": {allOf: ["+written+", {"+union+"}]}")
					objects[beside] = rest == "obj" || rest == "objtype"
					objects[entry] = objects[beside]
				}
			}
		}
	}
	_, diags := compileAnnotationSpec(t, "grid", admissionSpec("3.1.0", "", schemas...))
	warned := map[string][]string{}
	for _, diagnostic := range diags {
		if diagnostic.Severity == ir.SeverityWarning {
			name := strings.TrimPrefix(string(diagnostic.Provenance.Pointer), "/components/schemas/")
			warned[name] = append(warned[name], diagnostic.Message)
		}
	}
	for name, messages := range warned {
		assert.LessOrEqual(t, len(messages), 1, "%s: %q", name, messages)
		for _, m := range messages {
			if strings.Contains(m, "branches") {
				assert.True(t, objects[name] || strings.HasPrefix(name, "Gunion"), "%s: a dead branch beside a rest of another kind: %s", name, m)
			}
		}
	}
	assert.NotEmpty(t, warned, "the grid reaches the reports")
}
