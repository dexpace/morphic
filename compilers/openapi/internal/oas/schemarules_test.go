// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package oas_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dexpace/morphic/compilers/openapi/internal/oas"
)

// TestSchemaRules_AnswerAsTheTableDoes pins each question the schema lowering
// asks at every minor the table covers, and at versions it does not, where
// every answer is false. The two exclusive bounds share one row's answer, as
// the lowering reads them through one.
func TestSchemaRules_AnswerAsTheTableDoes(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		version                                        string
		nullable, boolBound, numberBound, items, tuple bool
	}{
		{"3.0.3", true, true, false, true, false},
		{"3.1.0", false, false, true, false, true},
		{"3.2.0", false, false, true, false, true},
		{"", false, false, false, false, false},
		{"3.3.0", false, false, false, false, false},
	} {
		t.Run(tc.version, func(t *testing.T) {
			t.Parallel()
			r := oas.Table().SchemaRules(tc.version)
			assert.Equal(t, tc.nullable, r.Defines("nullable"))
			assert.Equal(t, tc.tuple, r.Defines("prefixItems"))
			for _, bound := range []string{"exclusiveMinimum", "exclusiveMaximum"} {
				assert.Equal(t, tc.boolBound, r.Takes(bound, oas.Boolean), bound)
				assert.Equal(t, tc.numberBound, r.Takes(bound, oas.Number), bound)
			}
			assert.Equal(t, tc.items, r.RequiredWhen("items", "type", "array"))
		})
	}
}

// TestSchemaRules_ReadEachRowAtItsMinor pins what the rows say beyond a field's
// own shape: a second shape counts only from the minor that admits it, and a
// required field only where the value beside it decides the rule.
func TestSchemaRules_ReadEachRowAtItsMinor(t *testing.T) {
	t.Parallel()
	r30, r31 := oas.Table().SchemaRules("3.0.3"), oas.Table().SchemaRules("3.1.0")
	assert.True(t, r30.Takes("additionalProperties", oas.Boolean), "3.0 admits a boolean beside the schema")
	assert.False(t, r30.Takes("type", oas.StringList), "3.0 types one name")
	assert.True(t, r31.Takes("type", oas.StringList))
	assert.False(t, r30.Takes("nonesuch", oas.String))
	assert.False(t, r30.RequiredWhen("items", "type", "object"), "an object needs no items")
	assert.False(t, r30.RequiredWhen("items", "format", "array"), "another field decides nothing")
	assert.False(t, r30.RequiredWhen("properties", "type", "array"))
	assert.False(t, oas.Spec{}.SchemaRules("3.0.3").Defines("nullable"), "a table with no Schema Object defines nothing")
}
