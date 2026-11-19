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

	"github.com/dexpace/morphic/compilers/openapi/internal/specrules"
	"github.com/dexpace/morphic/internal/harness/openapicases"
	"github.com/dexpace/morphic/ir"
)

// schemaHolders are the objects of the rule table whose fields hold a schema.
func schemaHolders() []specrules.Kind {
	return []specrules.Kind{specrules.Components, specrules.Parameter, specrules.MediaType, specrules.Header, specrules.Schema}
}

// casePositions names, for each field of the rule table holding a schema, the
// value of the cases' position or spelling axis that writes a schema there.
func casePositions() map[string]string {
	return map[string]string{
		"Components Object.schemas": "component", "Parameter Object.schema": "parameter",
		"Media Type Object.schema": "requestBody", "Header Object.schema": "header",
		"Schema Object.allOf": "entry", "Schema Object.oneOf": "branch", "Schema Object.anyOf": "anyOfBranch",
		"Schema Object.items": "items", "Schema Object.properties": "property",
		"Schema Object.additionalProperties": "additionalProperties",
		"Schema Object.prefixItems":          "prefixItem", "Schema Object.patternProperties": "patternProperty",
		"Schema Object.contentSchema": "contentSchema",
	}
}

// keptWhole are the keywords holding a schema that the compiler keeps
// verbatim under Unmodeled as validation-only, lowering no node for the schema
// they hold (docs/ir/boundaries.md#validation-only-constructs), by the
// keyword's JSON beside its schema S.
func keptWhole() map[string]string {
	const s = `{"type": "string", "minLength": 2}`
	return map[string]string{
		"not": s, "contains": s, "propertyNames": s, "dependentSchemas": `{"a": ` + s + `}`,
		"if": s, "then": s, "else": s, "unevaluatedItems": s, "unevaluatedProperties": s,
	}
}

// notPositions names each field of the rule table holding a schema that the
// cases write no position for, with why not.
func notPositions() map[string]string {
	whole := "kept verbatim under Unmodeled, lowering no node for its schema"
	out := map[string]string{
		"Schema Object.$defs": "lowers no node of its own for what it holds",
		"Media Type Object.itemSchema": "defined at 3.2, which the core never runs: it runs 3.1.0 in its place, " +
			"so no core case writes a schema there",
	}
	for keyword := range keptWhole() {
		out["Schema Object."+keyword] = whole
	}
	return out
}

// TestCases_WritePositionsOfTheRuleTable holds the positions of the OpenAPI
// schema cases to the rule table: each field of an object holding a schema,
// at any version, is written by a value of the cases' position or spelling
// axis, or notPositions says why not. The draft dialects' keywords, which a
// schema declaring a draft reads in place of a version's, are not in the
// version rows and so are not asked.
func TestCases_WritePositionsOfTheRuleTable(t *testing.T) {
	t.Parallel()
	axes := openapicases.New().Axes()
	var names []string
	for _, a := range axes[4:] {
		for _, v := range a.Values {
			names = append(names, v.Name)
		}
	}
	var fields []string
	for _, kind := range schemaHolders() {
		object, ok := specrules.Table().Object(kind)
		require.True(t, ok)
		for _, f := range object.Fields {
			if f.Of == specrules.Schema && slices.Contains([]specrules.Shape{specrules.Nested, specrules.Map, specrules.List}, f.Shape) {
				fields = append(fields, object.Name+"."+f.Name)
			}
		}
	}
	slices.Sort(fields)
	fields = slices.Compact(fields)

	positions, excused := casePositions(), notPositions()
	for _, field := range fields {
		position, written := positions[field]
		why, excusedHere := excused[field]
		assert.NotEqual(t, written, excusedHere, "%s is written at a position, or excused, and not both: write it in "+
			"a position of openapicases, or say in notPositions why not", field)
		if written {
			assert.Contains(t, names, position, "%s: no value of the position or spelling axis", field)
		}
		if excusedHere {
			assert.NotEmpty(t, why, field)
		}
	}
	for _, m := range []map[string]string{positions, excused} {
		for field := range m {
			assert.Contains(t, fields, field, "%s is no field of the rule table holding a schema", field)
		}
	}
}

// TestCases_AKeywordKeptWholeLowersNoNodeForItsSchema holds each keyword
// notPositions excuses as kept verbatim to the compiler: the schema it holds
// stays whole in an Unmodeled entry of the component, and no node is lowered
// for it, so a bound it holds is no bound read.
func TestCases_AKeywordKeptWholeLowersNoNodeForItsSchema(t *testing.T) {
	t.Parallel()
	const head = "openapi: 3.1.0\ninfo: {title: t, version: \"1\"}\npaths: {}\ncomponents: {schemas: {K: "
	for keyword, value := range keptWhole() {
		t.Run(keyword, func(t *testing.T) {
			t.Parallel()
			doc := compileComponentSpec(t, fmt.Sprintf("%s{%s: %s}}}\n", head, keyword, value))
			for id, td := range doc.Types {
				if scalar, ok := td.(*ir.Scalar); ok && strings.Contains(string(id), "/schemas/K") {
					assert.Nil(t, scalar.Constraints, "%s lowers a node for the schema under %s", id, keyword)
				}
			}
			var kept []string
			for key, entries := range keptKeywordsOf(doc, "K") {
				for _, e := range entries {
					if e.reason == ir.ReasonValidationOnly && strings.Contains(e.value, `"minLength"`) {
						kept = append(kept, key)
					}
				}
			}
			assert.NotEmpty(t, kept, "%s keeps its schema whole under Unmodeled", keyword)
		})
	}
}
