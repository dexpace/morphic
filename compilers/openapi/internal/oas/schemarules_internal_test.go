// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package oas

import (
	"maps"
	"slices"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSchemaRules_ASecondShapeCountsFromItsOwnMinor pins that a field's second
// shape counts only from the minor its Alt names, though the field is defined
// before it. No Schema Object row spells that today, so a table of one row
// does.
func TestSchemaRules_ASecondShapeCountsFromItsOwnMinor(t *testing.T) {
	t.Parallel()
	spec := Spec{
		objects: []Object{{Kind: Schema, Fields: []Field{field("x", String).also(Boolean, From(V31))}}},
		index:   map[Kind]int{Schema: 0},
	}
	rules := func(version string) SchemaRules {
		r, _ := spec.SchemaRules(version, "")
		return r
	}
	assert.True(t, rules("3.0.3").Takes("x", String))
	assert.False(t, rules("3.0.3").Takes("x", Boolean), "3.0 admits no second shape")
	assert.True(t, rules("3.1.0").Takes("x", Boolean))
}

// TestDialects_EveryRowCitesItsText pins that each dialect cites the OpenAPI
// sentence admitting it, besides the JSON Schema sentence of a rule it states,
// and each keyword it reads in place of the minor's is a keyword defined
// wherever a schema can name a dialect, citing a sentence of the JSON Schema
// text it comes from rather than OpenAPI's.
func TestDialects_EveryRowCitesItsText(t *testing.T) {
	t.Parallel()
	dialects := schemaDialects()
	assert.NotEmpty(t, dialects)
	for _, d := range dialects {
		assert.True(t, slices.ContainsFunc(d.cites, func(c Cite) bool { return c.Minor == V31 && c.Text == 0 }),
			"%s cites the OpenAPI sentence admitting it", d.id)
		for _, c := range d.cites {
			assert.NotEmpty(t, c.Sentence, d.id)
			assert.NotEqual(t, c.Text == 0, c.Minor == 0, "%s cites one text: OpenAPI's at a minor, or another", d.id)
		}
		if d.rules != nil {
			for _, r := range d.rules() {
				assert.Equal(t, From(V31), r.Span, "%s rule %s", d.id, r.Field+r.Name)
				assert.NotEmpty(t, r.Cites, "%s rule %s", d.id, r.Field+r.Name)
				for _, c := range r.Cites {
					assert.Zero(t, c.Minor, "%s rule %s cites no OpenAPI minor", d.id, r.Field+r.Name)
					assert.NotZero(t, c.Text, d.id)
					assert.NotEmpty(t, c.Section, d.id)
					assert.NotEmpty(t, c.Sentence, d.id)
				}
			}
		}
		if d.keywords == nil {
			continue
		}
		for _, f := range d.keywords() {
			assert.True(t, f.Keyword, "%s %s", d.id, f.Name)
			assert.Equal(t, From(V31), f.Defined, "%s %s", d.id, f.Name)
			assert.NotEmpty(t, f.Cites, "%s %s", d.id, f.Name)
			for _, c := range f.Cites {
				assert.Zero(t, c.Minor, "%s %s cites no OpenAPI minor", d.id, f.Name)
				assert.NotZero(t, c.Text, "%s %s", d.id, f.Name)
				assert.NotEmpty(t, c.Section, "%s %s", d.id, f.Name)
				assert.NotEmpty(t, c.Sentence, "%s %s", d.id, f.Name)
			}
		}
	}
}

// TestSchemaRules_TheOwnRowsReadAsTheSchemaObject pins the object a minor's own
// rows give the check: the table's Schema Object with the fields that minor
// defines, field for field and in table order, and its rules whole, so a
// schema the check reads with them is held to what it was held to before the
// rows gave its object.
func TestSchemaRules_TheOwnRowsReadAsTheSchemaObject(t *testing.T) {
	t.Parallel()
	spec := Table()
	obj, ok := spec.Object(Schema)
	require.True(t, ok)
	for _, version := range []string{"3.0.3", "3.1.0", "3.2.0"} {
		minor, _ := ParseMinor(version)
		rules, _ := spec.SchemaRules(version, "")
		require.NotNil(t, rules.rows, version)
		want := obj
		want.Fields = slices.DeleteFunc(slices.Clone(obj.Fields), func(f Field) bool { return !f.Defined.Holds(minor) })
		want.Boolean = Span{}
		if obj.Boolean.Holds(minor) {
			want.Boolean = Always()
		}
		assert.Empty(t, cmp.Diff(want, rules.rows.object, cmpopts.EquateEmpty()), version)
	}
}

// TestSchemaRules_LaterKeywordsAreTheOwnRowsADialectLacks pins which keywords
// a schema read with a dialect's rows reads as the dialect the compiler reads
// natively does: at 3.0 each 3.1 added, in a draft each 2020-12 has and the
// draft lacks, bar the `$id` draft 4 spells `id`, and none in the minor's own.
func TestSchemaRules_LaterKeywordsAreTheOwnRowsADialectLacks(t *testing.T) {
	t.Parallel()
	d7 := []string{"$anchor", "$defs", "$dynamicAnchor", "$dynamicRef", "$vocabulary", "contentSchema", "dependentRequired",
		"dependentSchemas", "deprecated", "maxContains", "minContains", "prefixItems", "unevaluatedItems",
		"unevaluatedProperties"}
	d6 := slices.Concat(d7, []string{"$comment", "contentEncoding", "contentMediaType", "if", "then", "else", "readOnly",
		"writeOnly"})
	d4 := slices.Concat(d6, []string{"const", "contains", "examples", "propertyNames"})
	obj, _ := Table().Object(Schema)
	var at30 []string
	for _, f := range obj.Fields {
		if f.Defined.Holds(V31) && !slices.ContainsFunc(obj.Fields, func(g Field) bool { return g.Name == f.Name && g.Defined.Holds(V30) }) {
			at30 = append(at30, f.Name)
		}
	}
	for _, tc := range []struct {
		version, dialect string
		want             []string
	}{
		{"3.0.3", "", at30},
		{"3.1.0", "", nil},
		{"3.2.0", "https://json-schema.org/draft/2020-12/schema", nil},
		{"3.1.0", "https://json-schema.org/draft/2019-09/schema", []string{"$dynamicAnchor", "$dynamicRef", "prefixItems"}},
		{"3.1.0", "http://json-schema.org/draft-07/schema#", d7},
		{"3.2.0", "http://json-schema.org/draft-06/schema#", d6},
		{"3.1.0", "http://json-schema.org/draft-04/schema#", d4},
	} {
		rules, _ := Table().SchemaRules(tc.version, tc.dialect)
		got := slices.Sorted(maps.Keys(rules.rows.later))
		assert.Equal(t, slices.Sorted(slices.Values(tc.want)), got, "%s %s", tc.version, tc.dialect)
		for _, name := range got {
			assert.True(t, rules.Later(name), name)
			assert.False(t, rules.Defines(name), name)
		}
	}
	assert.Contains(t, at30, "$schema")
	assert.False(t, SchemaRules{}.Later("prefixItems"), "the zero rows")
}
