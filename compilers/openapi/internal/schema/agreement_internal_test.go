// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package schema

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dexpace/morphic/ir"
)

// TestBoundOf_FillsAFieldForEveryValueBound holds boundOf to
// valueConstraintKeywords: each keyword the constraint reader fills a field for
// projects a field of a full reading, so a keyword the list gains and the switch
// lacks agrees with nothing it should meet, and anything else projects none.
func TestBoundOf_FillsAFieldForEveryValueBound(t *testing.T) {
	t.Parallel()
	full := &ir.Constraints{
		Min: new(ir.BigVal("1")), ExclusiveMin: new(ir.BigVal("1")), Max: new(ir.BigVal("9")),
		ExclusiveMax: new(ir.BigVal("9")), MultipleOf: new(ir.BigVal("2")), MinLength: new(int64(1)),
		MaxLength: new(int64(9)), MinProps: new(int64(1)), MaxProps: new(int64(9)), Pattern: "^a$",
	}
	for _, keyword := range valueConstraintKeywords {
		assert.NotEqual(t, ir.Constraints{}, boundOf(keyword, full), keyword)
	}
	assert.Equal(t, ir.Constraints{}, boundOf("title", full), "a keyword that is no value bound fills nothing")
	assert.Equal(t, ir.Constraints{}, boundOf("maxLength", nil), "nor does a reading of nothing")
}
