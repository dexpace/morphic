// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package statement_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/statement"
)

// TestParsed_HandsBackWhatTheStatementWasReadFrom pins the bridges the
// operation walk takes the parser's value back through.
func TestParsed_HandsBackWhatTheStatementWasReadFrom(t *testing.T) {
	t.Parallel()
	table, c := tableOver(t, document("3.1.0", `A: {type: string}`, `B: {properties: {t: true}}`), rulesAt(t, "3.1.0", ""))
	stated := c.Bindings().At(component(c, "A"))
	boolean := c.Bindings().At(c.Position("/components/schemas/B/properties/t"))
	require.NotNil(t, stated.GetSchema())

	assert.Same(t, stated, statement.ParsedAt(table.Of(stated)))
	assert.Same(t, stated.GetSchema(), statement.Parsed(table.Of(stated).Schema))
	assert.Same(t, boolean, statement.ParsedAt(table.Of(boolean)))
	assert.Nil(t, statement.Parsed(table.Of(boolean).Schema), "a boolean is no schema object")
	assert.Nil(t, statement.ParsedAt(table.Of(nil)))
	assert.Nil(t, statement.Parsed(table.Of(nil).Schema))
	assert.Nil(t, statement.ParsedAt(nil))
	assert.Nil(t, statement.Parsed(nil))
}
