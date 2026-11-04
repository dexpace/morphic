// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package oas

import (
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
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
		assert.True(t, slices.ContainsFunc(d.cites, func(c Cite) bool { return c.Minor == V31 && c.Text == "" }),
			"%s cites the OpenAPI sentence admitting it", d.id)
		for _, c := range d.cites {
			assert.NotEmpty(t, c.Sentence, d.id)
			assert.NotEqual(t, c.Text == "", c.Minor == 0, "%s cites one text: OpenAPI's at a minor, or another", d.id)
		}
		for _, f := range d.keywords {
			assert.True(t, f.Keyword, "%s %s", d.id, f.Name)
			assert.Equal(t, From(V31), f.Defined, "%s %s", d.id, f.Name)
			assert.NotEmpty(t, f.Cites, "%s %s", d.id, f.Name)
			for _, c := range f.Cites {
				assert.Zero(t, c.Minor, "%s %s cites no OpenAPI minor", d.id, f.Name)
				assert.NotEmpty(t, c.Text, "%s %s", d.id, f.Name)
				assert.NotEmpty(t, c.Section, "%s %s", d.id, f.Name)
				assert.NotEmpty(t, c.Sentence, "%s %s", d.id, f.Name)
			}
		}
	}
}
