// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

//go:build pending

package statement_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Pending: failedEntry reads an allOf entry off the pointer's text, so a
// schema whose parent token is a property named allOf is read as an entry.

// TestOf_ASchemaUnderAPropertyNamedAllOfIsNoEntry pins that a failed $ref
// written beside other keys at the items of a property named allOf conjoins
// as at any position: only an allOf's indexed entry is read whole.
func TestOf_ASchemaUnderAPropertyNamedAllOfIsNoEntry(t *testing.T) {
	t.Parallel()
	table, c := tableOver(t, document("3.1.0",
		"H: {type: object, properties: {allOf: {type: array, items: {$ref: '#/components/schemas/Ghost', description: d}}}}"),
		rulesAt(t, "3.1.0", ""))
	st := table.At(c.Position("/components/schemas/H/properties/allOf/items"))
	require.NotNil(t, st)
	assert.Nil(t, st.Ref)
	assert.NotNil(t, st.BesideRef, "the items conjoins, as under any other property")
}
