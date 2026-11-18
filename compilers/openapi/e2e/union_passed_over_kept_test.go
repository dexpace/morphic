// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package e2e_test

import (
	"encoding/json/v2"
	"fmt"
	"maps"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// unmodeledValues returns every value the document keeps under Unmodeled, on any
// node or member, as JSON text.
func unmodeledValues(t *testing.T, raw []byte) []string {
	t.Helper()
	var tree any
	require.NoError(t, json.Unmarshal(raw, &tree))
	var out []string
	var walk func(v any)
	walk = func(v any) {
		switch v := v.(type) {
		case map[string]any:
			if kept, ok := v["unmodeled"].(map[string]any); ok {
				for _, entry := range kept {
					if e, ok := entry.(map[string]any); ok {
						text, err := json.Marshal(e["value"], json.Deterministic(true))
						require.NoError(t, err)
						out = append(out, string(text))
					}
				}
			}
			for _, x := range v {
				walk(x)
			}
		case []any:
			for _, x := range v {
				walk(x)
			}
		}
	}
	walk(tree)
	return out
}

// TestElection_AUnionPassedOverThatSaysNothingIsKeptEverywhere holds that a
// oneOf or an anyOf whose branches declare nothing, passed over for the other
// declaring shape, is kept wherever the two are written, beside a string, a
// string or null, an array and an object, on one schema, beside an allOf, on
// one entry or two, in either order: a refinement folds neither union of a
// pair into its principal as said, as one schema writing both keeps the one it
// passes over (GitHub #2011).
func TestElection_AUnionPassedOverThatSaysNothingIsKeptEverywhere(t *testing.T) {
	t.Parallel()
	const shaped = "[{type: object, properties: {x: {type: string}}}, {type: string}]"
	bodies := []string{"{type: string}", "{type: [string, 'null']}", "{type: array, items: {type: string}}",
		"{type: object, properties: {a: {type: string}}}"}
	nothing := map[string]string{"[{}]": "[{}]", "[true]": "[true]", "[{}, {}]": "[{},{}]", "[{description: x}]": `[{"description":"x"}]`}
	for _, body := range bodies {
		for written, value := range nothing {
			for _, passed := range []string{"oneOf", "anyOf"} {
				t.Run(fmt.Sprintf("%s/%s/%s", body, written, passed), func(t *testing.T) {
					t.Parallel()
					one, anyOf := shaped, written
					if passed == "oneOf" {
						one, anyOf = written, shaped
					}
					spellings := unionPairSpellings(body, one, anyOf)
					spellings["beside the body"] = strings.TrimSuffix(body, "}") + ", oneOf: " + one + ", anyOf: " + anyOf + "}"
					for _, name := range slices.Sorted(maps.Keys(spellings)) {
						doc, _ := compileAnnotationSpec(t, "kept", bothKeywordsDoc("3.1.0", "", false, "P: "+spellings[name]))
						raw, err := json.Marshal(doc)
						require.NoError(t, err)
						assert.Contains(t, unmodeledValues(t, raw), value, "%s keeps the %s passed over", name, passed)
					}
				})
			}
		}
	}
}
