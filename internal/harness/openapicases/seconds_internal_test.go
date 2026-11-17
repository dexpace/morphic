// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package openapicases

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSeconds_EachNamesARowOrWritesItsOwn holds each second construct to one
// source of what it writes: a second named as a construct row writes that
// row's first value, and one writing its own is named as no row, so a reader
// of the axes tells the two apart by name.
func TestSeconds_EachNamesARowOrWritesItsOwn(t *testing.T) {
	rows := constructs()
	for _, sec := range seconds()[1:] {
		named := slices.ContainsFunc(rows, func(k construct) bool { return k.keyword == sec.name })
		assert.NotEqual(t, named, sec.own != nil, sec.name)
	}
	assert.Equal(t, none, seconds()[0].name)
}

// TestWrite_PlacesASecondBesideItsConstruct pins where a second construct is
// written, and the use of the component position each case writes: after its
// construct in one unit, a second $ref as the allOf entry right after the
// target's in every spelling, and nowhere it would not say a keyword of its
// own.
func TestWrite_PlacesASecondBesideItsConstruct(t *testing.T) {
	c, ok := New().(cases)
	require.True(t, ok)
	for _, tc := range []struct{ name, use string }{
		{"3.0.3/string/minLength/description/component/beside",
			`{"$ref":"#/components/schemas/T","minLength":1,"description":"D"}`},
		{"3.0.3/string/minLength/description/component/entry",
			`{"allOf":[{"$ref":"#/components/schemas/T"},{"minLength":1,"description":"D"}]}`},
		{"3.0.3/object/minProperties/properties/component/beside",
			`{"$ref":"#/components/schemas/T","minProperties":1,"properties":{"p":{"type":"string"}}}`},
		{"3.0.3/string/none/targetType/component/beside", `{"$ref":"#/components/schemas/T","type":"string"}`},
		{"3.0.3/string/minLength/nullable/component/inline", `{"type":"string","minLength":1,"nullable":true}`},
		{"3.1.0/string/title/typeNull/component/inline", `{"type":["string","null"],"title":"T"}`},
		{"3.0.3/string/title/secondRef/component/beside",
			`{"$ref":"#/components/schemas/T","title":"T","allOf":[{"$ref":"#/components/schemas/S"}]}`},
		{"3.0.3/string/title/secondRef/component/wrap",
			`{"allOf":[{"$ref":"#/components/schemas/T"},{"$ref":"#/components/schemas/S"}],"title":"T"}`},
		{"3.0.3/string/title/secondRef/component/entry",
			`{"allOf":[{"$ref":"#/components/schemas/T"},{"$ref":"#/components/schemas/S"},{"title":"T"}]}`},
		{"3.0.3/string/title/secondRef/component/entryFirst",
			`{"allOf":[{"title":"T"},{"$ref":"#/components/schemas/T"},{"$ref":"#/components/schemas/S"}]}`},
		{"3.0.3/string/title/secondRef/component/nested",
			`{"allOf":[{"allOf":[{"$ref":"#/components/schemas/T"},{"$ref":"#/components/schemas/S"},{"title":"T"}]}]}`},
		{"3.0.3/string/title/secondRef/component/inline",
			`{"type":"string","title":"T","allOf":[{"$ref":"#/components/schemas/S"}]}`},
		{"3.0.3/string/none/secondRef/component/wrap",
			`{"allOf":[{"$ref":"#/components/schemas/T"},{"$ref":"#/components/schemas/S"}]}`},
		{"3.0.3/string/none/secondRef/component/entry", ""},  // wrap's document
		{"3.0.3/object/allOf/secondRef/component/entry", ""}, // a keyword the construct writes
		{"3.0.3/string/minLength/description/component/inline", `{"type":"string","minLength":1,"description":"D"}`},
		{"3.2.0/string/minLength/description/component/beside", ""},    // a version like another
		{"3.0.3/enum/minLength/description/component/beside", ""},      // a target like another
		{"3.0.3/string/minLength/properties/component/beside", ""},     // a row not said of the target
		{"3.0.3/union/title/targetType/component/beside", ""},          // a target writing no type
		{"3.0.3/string/title/typeNull/component/beside", ""},           // before 3.1, nullable says it
		{"3.0.3/string/writeOnly/readOnly/component/beside", ""},       // a construct it excludes
		{"3.0.3/union/type/enum/component/beside", ""},                 // a construct it excludes
		{"3.1.0/string/$id/secondRef/component/inline", ""},            // a construct it excludes
		{"3.0.3/string/description/description/component/beside", ""},  // a keyword the construct writes
		{"3.0.3/string/minLength/targetType/component/inline", ""},     // restating the body
		{"3.0.3/object/minProperties/properties/component/inline", ""}, // replacing a member of the body
		{"3.0.3/string/none/description/component/beside", ""},         // the row's own case
	} {
		doc, ok := c.Write(caseAt(t, c, strings.Split(tc.name, "/")...))
		if tc.use == "" {
			assert.False(t, ok, tc.name)
			continue
		}
		require.True(t, ok, tc.name)
		var spec struct {
			Components struct {
				Schemas map[string]jsontext.Value `json:"schemas"`
			} `json:"components"`
		}
		require.NoError(t, json.Unmarshal(doc.Sources[0].Data, &spec), tc.name)
		got := spec.Components.Schemas["C"]
		require.NoError(t, got.Compact(), tc.name)
		assert.Equal(t, tc.use, string(got), tc.name)
		if strings.Contains(tc.use, secondSchema) {
			assert.Contains(t, spec.Components.Schemas, secondSchema, "%s names the component it writes", tc.name)
		}
	}
}

// TestWrite_PanicsOnASecondOfNoRow holds a second named as no construct row
// and writing nothing of its own to a panic, which CheckCase reports, rather
// than a case written without it.
func TestWrite_PanicsOnASecondOfNoRow(t *testing.T) {
	c, ok := New().(cases)
	require.True(t, ok)
	c.seconds = []second{{name: none}, {name: "unwritten"}}
	assert.PanicsWithValue(t, `openapicases: second construct "unwritten" names no construct row and writes nothing`,
		func() { c.Write([]int{0, 1, 1, 1, 0, 1}) })
}
