// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package openapicases

import (
	"bytes"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"slices"
	"strings"
	"testing"

	gocmp "github.com/google/go-cmp/cmp"
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

	beneath := relocation("/components/schemas/C/properties/")
	assert.Equal(t, "p/openapi/components/schemas/C/properties/t1", beneath("p/openapi/components/schemas/C/properties/t1/allOf/0"))
}

// caseAt returns the coordinates of the case naming each axis's value by name.
func caseAt(t *testing.T, c cases, names ...string) []int {
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
// inline writes, an entry saying nothing, a 3.0 nullable: true the policy reads
// apart in two spellings, and the reports a 3.0 use states, a nullable:
// true's beside the allOf only where no type is written with it.
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
		{[]string{"3.1.0", "string", "minLength", "none", "items", "besideEntry"}, true, entry},
		{[]string{"3.1.0", "string", "none", "none", "items", "besideEntry"}, false, ""},
		{[]string{"3.1.0", "string", "none", "secondRef", "items", "besideEntry"}, false, ""},
		{[]string{"3.1.0", "string", "$comment", "none", "items", "besideEntry"}, false, ""},
		{[]string{"3.0.3", "object", "nullable", "properties", "items", "entry"}, true, ""},
		{[]string{"3.0.3", "object", "nullable", "properties", "items", "besideEntry"}, true, ""},
		{[]string{"3.0.3", "object", "nullable", "properties", "items", "nested"}, true, entry},
		{[]string{"3.0.3", "object", "nullable~", "properties", "items", "entry"}, true, ""},
		{[]string{"3.0.3", "object", "nullable~", "properties", "items", "besideEntry"}, true, entry},
		{[]string{"3.0.3", "string", "nullable", "targetType", "items", "besideEntry"}, true, ""},
		{[]string{"3.0.3", "string", "nullable", "targetType", "items", "entry"}, true, wrap},
		{[]string{"3.0.3", "string", "nullable~", "targetType", "items", "besideEntry"}, true, entry},
	} {
		doc, ok := c.Write(caseAt(t, c, tc.at...))
		require.Equal(t, tc.written, ok, "%v", tc.at)
		if tc.partner == "" {
			assert.Nil(t, doc.Equal, "%v", tc.at)
			continue
		}
		require.NotNil(t, doc.Equal, "%v", tc.at)
		assert.Equal(t, tc.partner, c.spellings[doc.Equal.To[5]], "%v", tc.at)
	}

	eq := func(names ...string) *harness.Equal {
		doc, ok := c.Write(caseAt(t, c, names...))
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
	typed := eq("3.0.3", "string", "nullable", "targetType", "items", "beside")
	assert.Equal(t, []harness.Stated{warning("openapi/later-version-construct", item+"/nullable"),
		warning("openapi/later-version-construct", item+"/type")}, typed.Stated)
	assert.Empty(t, typed.Replaced, "a type beside nullable: true gives it its effect")
	assert.Empty(t, eq("3.0.3", "union", "type", "nullable", "items", "beside").Replaced, "nor is the type the second")
	assert.Equal(t, []harness.Stated{warning("openapi/later-version-construct", item+"/title"),
		warning("openapi/later-version-construct", item+"/allOf")},
		eq("3.0.3", "string", "title", "secondRef", "items", "beside").Stated, "a second $ref is an allOf beside it")
	onEntry := eq("3.0.3", "string", "nullable", "secondRef", "items", "besideEntry")
	assert.Equal(t, []harness.Stated{warning("openapi/later-version-construct", item+"/allOf/0/nullable")}, onEntry.Stated,
		"beside the $ref on its entry, and the second $ref an entry after it")
	assert.Equal(t, []harness.Stated{warning("openapi/validation/validation-no-effect", item+"/allOf/2/nullable")},
		onEntry.Replaced, "where the entry spelling writes it, after both references")
	assert.Empty(t, eq("3.1.0", "string", "minLength", "none", "items", "besideEntry").Stated, "3.1 admits it")
	assert.Empty(t, eq("3.1.0", "string", "minLength", "none", "items", "beside").Stated)
	assert.Equal(t, "docs/compilers/openapi.md#ref-use-site", nullable.Rule)
}

func TestSpell_RefusesAUseWritingAKeyTwice(t *testing.T) {
	ref := object{{"$ref", "#/components/schemas/T"}}
	_, ok := spell(beside, ref, object{{"$ref", "#/components/schemas/RefString"}}, nil)
	assert.False(t, ok)
	_, ok = spell(besideEntry, ref, object{{"$ref", "#/components/schemas/RefString"}}, nil)
	assert.False(t, ok, "the entry would write $ref twice")
	_, ok = spell(inline, ref, nil, nil)
	assert.False(t, ok, "inline writes the body, no use")
	use, ok := spell(nested, ref, object{{"title", "T"}}, nil)
	require.True(t, ok)
	got, err := encode(use)
	require.NoError(t, err)
	assert.JSONEq(t, `{"allOf": [{"allOf": [{"$ref": "#/components/schemas/T"}, {"title": "T"}]}]}`, string(got))
}

// TestGroups_EachSpellingNamesItsPartner pins a group's spellings: no inline,
// since a group's target is named by $ref alone, and each other one naming its
// partner, its uses relocated beneath the carriers the position writes.
func TestGroups_EachSpellingNamesItsPartner(t *testing.T) {
	c := Groups().(groupCases)
	require.NotContains(t, c.spellings, inline)
	want := map[string]string{"property": "/components/schemas/C/properties/",
		"parameter": "/paths/~1c/get/parameters/", "header": "/paths/~1c/get/responses/200/headers/"}
	for i, p := range c.positions {
		assert.Equal(t, want[p.name], carriersOf(p), p.name)
		for k, s := range c.spellings {
			doc, ok := c.Write([]int{0, 0, i, k})
			require.True(t, ok, "%s %s", p.name, s)
			partner, _, named := partnerOf(s)
			if !named {
				assert.Nil(t, doc.Equal, s)
				continue
			}
			require.NotNil(t, doc.Equal, s)
			assert.Equal(t, partner, c.spellings[doc.Equal.To[3]])
			assert.Equal(t, want[p.name]+"t0", doc.Equal.Relocate(want[p.name]+"t0/allOf/1"), "%s %s", p.name, s)
		}
	}
	_, ok := c.Write([]int{0, 0, 0, len(c.spellings)})
	assert.False(t, ok)
	c.spellings = append(c.spellings, inline)
	_, ok = c.Write([]int{0, 0, 0, len(c.spellings) - 1})
	assert.False(t, ok, "a spelling writing no use of a group's target writes nothing")
}

// TestWrite_TwoSpellingsDifferInTheUseAlone holds each case naming a partner,
// at the component position, to that partner: the two documents differ in the
// case's own schema alone, and each says there the same conjuncts, the
// references in one order and every other keyword alike. So a pair compares
// how the use is spelt and nothing a spelling carries along with it, such as
// which of two references comes first, an order a composition's mixins keep
// (docs/compilers/openapi.md#allof-classification).
func TestWrite_TwoSpellingsDifferInTheUseAlone(t *testing.T) {
	t.Parallel()
	c, ok := New().(cases)
	require.True(t, ok)
	each, err := harness.Each(c)
	require.NoError(t, err)
	axes := c.Axes()
	var pairs int
	var differ []string
	for _, at := range each {
		if c.positions[at[4]].name != "component" {
			continue
		}
		doc, ok := c.Write(at)
		if !ok || doc.Equal == nil {
			continue
		}
		partner, ok := c.Write(doc.Equal.To)
		require.True(t, ok, "%v", at)
		pairs++
		if diff := gocmp.Diff(useOf(t, partner), useOf(t, doc)); diff != "" {
			differ = append(differ, nameOf(axes, at)+"\n"+diff)
		}
	}
	assert.Positive(t, pairs)
	assert.Empty(t, differ, "%d of %d pairs differ beyond the use's spelling", len(differ), pairs)
}

// nameOf returns the name of the case at: its value names joined by "/".
func nameOf(axes []harness.Axis, at []int) string {
	names := make([]string, len(axes))
	for k, a := range axes {
		names[k] = a.Values[at[k]].Name
	}
	return strings.Join(names, "/")
}

// said is what a document says, read for TestWrite_TwoSpellingsDifferInTheUseAlone:
// everything but the case's schema as it stands, and that schema as the
// conjuncts it says.
type said struct {
	Rest       any
	Refs, Unit []string
}

// useOf reads doc, a case at the component position, as said.
func useOf(t *testing.T, doc harness.CaseDocument) said {
	t.Helper()
	data := doc.Sources[0].Data
	var out said
	require.NoError(t, json.Unmarshal(data, &out.Rest))
	var spec struct {
		Components struct {
			Schemas map[string]jsontext.Value `json:"schemas"`
		} `json:"components"`
	}
	require.NoError(t, json.Unmarshal(data, &spec))
	rest, ok := out.Rest.(map[string]any)["components"].(map[string]any)["schemas"].(map[string]any)
	require.True(t, ok)
	delete(rest, caseSchema)
	conjuncts(t, jsontext.NewDecoder(bytes.NewReader(spec.Components.Schemas[caseSchema])), &out, 0)
	slices.Sort(out.Unit)
	return out
}

// conjuncts reads the schema dec holds next into out, depth allOf entries
// deep: each $ref in the order written, each allOf entry in the allOf's
// place, and every other member as its name and value.
func conjuncts(t *testing.T, dec *jsontext.Decoder, out *said, depth int) {
	t.Helper()
	require.Less(t, depth, maxDepth)
	tok, err := dec.ReadToken()
	require.NoError(t, err)
	require.Equal(t, jsontext.Kind('{'), tok.Kind())
	for dec.PeekKind() != '}' {
		tok, err := dec.ReadToken()
		require.NoError(t, err)
		switch name := tok.String(); name {
		case "$ref":
			ref, err := dec.ReadToken()
			require.NoError(t, err)
			out.Refs = append(out.Refs, ref.String())
		case "allOf":
			_, err := dec.ReadToken()
			require.NoError(t, err)
			for dec.PeekKind() != ']' {
				conjuncts(t, dec, out, depth+1)
			}
			_, err = dec.ReadToken()
			require.NoError(t, err)
		default:
			value, err := dec.ReadValue()
			require.NoError(t, err)
			require.NoError(t, value.Compact())
			out.Unit = append(out.Unit, name+"="+string(value))
		}
	}
	_, err = dec.ReadToken()
	require.NoError(t, err)
}
