// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package oas

import (
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
	assert.True(t, spec.SchemaRules("3.0.3").Takes("x", String))
	assert.False(t, spec.SchemaRules("3.0.3").Takes("x", Boolean), "3.0 admits no second shape")
	assert.True(t, spec.SchemaRules("3.1.0").Takes("x", Boolean))
}
