// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package statement_test

import (
	"encoding/json/jsontext"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/annotation"
	"github.com/dexpace/morphic/compilers/openapi/internal/bind/typed"
	"github.com/dexpace/morphic/compilers/openapi/internal/nodeview"
	"github.com/dexpace/morphic/compilers/openapi/internal/statement"
)

// TestOf_TruthReadsWhatThePositionPredicatesRead pins Truth to the readings
// the walk makes of a position today, on the same value: whether it is
// written, written as no schema, the `false` schema, or a mapping the parser
// read as a boolean.
func TestOf_TruthReadsWhatThePositionPredicatesRead(t *testing.T) {
	t.Parallel()
	chain := strings.Repeat("{type: array, items: ", typed.MaxMends+2) + "[{}]" + strings.Repeat("}", typed.MaxMends+2)
	table, c := tableOver(t, document("3.1.0",
		`Shapes: {properties: {t: true, f: false, n: null, s: x, l: [1], m: {type: string}}}`,
		"Chain: "+chain,
	), rulesAt(t, "3.1.0", ""))
	unread := "/components/schemas/Chain" + strings.Repeat("/items", typed.MaxMends+1)
	for _, tc := range []struct {
		name, pointer string
		want          statement.Truth
	}{
		{"absent", "/components/schemas/Nothing", statement.Unwritten},
		{"true", "/components/schemas/Shapes/properties/t", statement.Anything},
		{"false", "/components/schemas/Shapes/properties/f", statement.Nothing},
		{"null", "/components/schemas/Shapes/properties/n", statement.Unwritten},
		{"string", "/components/schemas/Shapes/properties/s", statement.Unwritten},
		{"list", "/components/schemas/Shapes/properties/l", statement.Unwritten},
		{"mapping", "/components/schemas/Shapes/properties/m", statement.Stated},
		{"mapping read as false", unread, statement.Unread},
	} {
		js := c.Bindings().At(c.Position(jsontext.Pointer(tc.pointer)))
		st := table.Of(js)
		require.Equal(t, tc.want, st.Truth, tc.name)
		assert.Equal(t, annotation.Written(js), st.Truth != statement.Unwritten, "%s: Written", tc.name)
		assert.Equal(t, annotation.IsNoSchema(js), js != nil && st.Truth == statement.Unwritten, "%s: IsNoSchema", tc.name)
		// The parser reads the mapping it fails beneath as `false` too.
		assert.Equal(t, annotation.IsFalseSchema(js), st.Truth.ReadsFalse(), "%s: IsFalseSchema", tc.name)
		mended := js != nil && js.IsBool() && nodeview.Deref(js.GetCore().GetRootNode()).Kind == yaml.MappingNode
		assert.Equal(t, mended, st.Truth == statement.Unread, "%s: a mapping read as a boolean", tc.name)
		assert.Same(t, annotation.SchemaOf(js), statement.Parsed(st.Schema), "%s: the schema object", tc.name)
	}
}

// TestParsed_HandsBackWhatTheStatementWasReadFrom pins the bridges a reader
// not yet reading a statement takes the parser's value back through.
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
