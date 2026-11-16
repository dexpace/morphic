// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package openapicases

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/internal/harness"
	"github.com/dexpace/morphic/ir"
)

// TestPositions_EachPointerNamesTheSchemaItPlaces resolves each position's
// pointer in the document its place writes: it is the schema placed.
func TestPositions_EachPointerNamesTheSchemaItPlaces(t *testing.T) {
	for _, p := range positions() {
		paths, schemas := p.place(object{{"x-mark", "here"}})
		data, err := encode(object{{"paths", paths}, {"components", object{{"schemas", schemas}}}})
		require.NoError(t, err)
		var v any
		require.NoError(t, json.Unmarshal(data, &v))
		for token := range jsontext.Pointer(p.pointer).Tokens() {
			switch node := v.(type) {
			case map[string]any:
				v = node[token]
			case []any:
				require.Equal(t, "0", token, p.name)
				v = node[0]
			default:
				t.Fatalf("%s: %q reaches no node", p.name, p.pointer)
			}
		}
		assert.Equal(t, map[string]any{"x-mark": "here"}, v, p.name)
	}
}

func TestRelocation_ErasesEachAllOfIndexAfterTheUse(t *testing.T) {
	use := "/components/schemas/C"
	relocate := relocation(use)
	for in, want := range map[string]string{
		"t/openapi/components/schemas/C/allOf/1/properties/x": "t/openapi/components/schemas/C/properties/x",
		use + "/allOf/0/allOf/1/minLength":                    use + "/minLength",
		use + "/allOf/1":                                      use,
		"/components/schemas/CX/allOf/1":                      "/components/schemas/CX/allOf/1",
		"/components/schemas/CX/allOf/1 " + use + "/allOf/2":  "/components/schemas/CX/allOf/1 " + use,
		use + "/allOf/x/allOf":                                use + "/allOf/x/allOf",
		"openapi:allOf/1":                                     "openapi:",
		"openapi:schema/allOf/1":                              "openapi:schema",
		"openapi:contentMediaType":                            "openapi:contentMediaType",
		"^a3":                                                 "^a3",
		"/components/schemas/CX":                              "/components/schemas/CX",
	} {
		assert.Equal(t, want, relocate(in), in)
	}
	assert.PanicsWithValue(t, "openapicases: a relocation of no use", func() { relocation("") })
}

// at returns the coordinates of the case naming each axis's value by name.
func at(t *testing.T, c cases, names ...string) []int {
	t.Helper()
	axes := c.Axes()
	out := make([]int, len(axes))
	for k, a := range axes {
		out[k] = slices.IndexFunc(a.Values, func(v harness.Value) bool { return v.Name == names[k] })
		require.GreaterOrEqual(t, out[k], 0, names[k])
	}
	return out
}

// TestWrite_EachSpellingNamesItsPartner pins the spellings a case writes and
// the partner each names: what stays beside the use, what no spelling but
// inline writes, an entry saying nothing, and the reports a 3.0 use states.
func TestWrite_EachSpellingNamesItsPartner(t *testing.T) {
	c := New().(cases)
	for _, tc := range []struct {
		at      []string
		written bool
		partner string
	}{
		{[]string{"3.1.0", "string", "minLength", "none", "items", "entryFirst"}, true, entry},
		{[]string{"3.1.0", "string", "minLength", "none", "items", "nested"}, true, entry},
		{[]string{"3.1.0", "string", "minLength", "none", "items", "entry"}, true, wrap},
		{[]string{"3.1.0", "string", "minLength", "none", "items", "wrap"}, true, ""},
		{[]string{"3.1.0", "string", "minLength", "none", "items", "inline"}, true, ""},
		{[]string{"3.1.0", "string", "$comment", "none", "items", "beside"}, true, ""},
		{[]string{"3.1.0", "string", "$comment", "none", "items", "wrap"}, false, ""},
		{[]string{"3.1.0", "string", "$id", "none", "items", "beside"}, false, ""},
		{[]string{"3.1.0", "string", "$ref", "none", "items", "inline"}, true, ""},
		{[]string{"3.1.0", "string", "$ref", "none", "items", "beside"}, false, ""},
		{[]string{"3.1.0", "object", "allOf", "none", "items", "wrap"}, false, ""},
		{[]string{"3.1.0", "object", "allOf", "none", "items", "entry"}, true, ""},
		{[]string{"3.1.0", "object", "allOf", "none", "items", "nested"}, true, entry},
		{[]string{"3.1.0", "string", "none", "none", "items", "entry"}, false, ""},
		{[]string{"3.1.0", "string", "none", "none", "items", "nested"}, true, wrap},
	} {
		doc, ok := c.Write(at(t, c, tc.at...))
		require.Equal(t, tc.written, ok, "%v", tc.at)
		if tc.partner == "" {
			assert.Nil(t, doc.Equal, "%v", tc.at)
			continue
		}
		require.NotNil(t, doc.Equal, "%v", tc.at)
		assert.Equal(t, tc.partner, c.spellings[doc.Equal.To[5]], "%v", tc.at)
	}

	eq := func(names ...string) *harness.Equal {
		doc, ok := c.Write(at(t, c, names...))
		require.True(t, ok)
		require.NotNil(t, doc.Equal)
		return doc.Equal
	}
	warning := func(code, pointer string) harness.Stated {
		return harness.Stated{Severity: ir.SeverityWarning, Code: code, Pointer: jsontext.Pointer(pointer)}
	}
	item := "/components/schemas/C/items"
	assert.Equal(t, []harness.Stated{warning("openapi/later-version-construct", item+"/maximum"),
		warning("openapi/later-version-construct", item+"/exclusiveMaximum")},
		eq("3.0.3", "integer", "exclusiveMaximum", "none", "items", "beside").Stated)
	nullable := eq("3.0.3", "string", "nullable", "none", "items", "beside")
	assert.Equal(t, []harness.Stated{warning("openapi/validation/validation-no-effect", item+"/nullable")}, nullable.Replaced)
	assert.Empty(t, eq("3.0.3", "string", "nullable~", "none", "items", "beside").Replaced, "nullable: false draws none")
	assert.Empty(t, eq("3.1.0", "string", "minLength", "none", "items", "beside").Stated)
	assert.Equal(t, "docs/compilers/openapi.md#ref-use-site", nullable.Rule)
}

func TestSpell_RefusesAUseWritingAKeyTwice(t *testing.T) {
	_, ok := spell(beside, object{{"$ref", "#/components/schemas/RefString"}})
	assert.False(t, ok)
	_, ok = spell(inline, nil)
	assert.False(t, ok, "inline writes the body, no use")
	use, ok := spell(nested, object{{"title", "T"}})
	require.True(t, ok)
	got, err := encode(use)
	require.NoError(t, err)
	assert.JSONEq(t, `{"allOf": [{"allOf": [{"$ref": "#/components/schemas/T"}, {"title": "T"}]}]}`, string(got))
}
