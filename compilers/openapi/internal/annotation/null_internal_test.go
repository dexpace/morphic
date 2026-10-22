package annotation

import (
	"fmt"
	"strings"
	"testing"

	oas3 "github.com/speakeasy-api/openapi/jsonschema/oas3"
	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestIsNullLiteral_TellsANullFromFalse pins the three predicates that read a
// `null` written where a schema belongs. The parser decodes one as the boolean
// `false` schema, which admits no value where a null states nothing, so only
// the node tells them apart: IsNullLiteral reads every spelling of a null,
// through an alias, IsFalseSchema is true only for a written `false`, and
// Written is false for no schema and for a null.
func TestIsNullLiteral_TellsANullFromFalse(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, value string
		isNull      bool
		isFalse     bool
		written     bool
	}{
		{name: "the word null", value: "null", isNull: true},
		{name: "a tilde", value: "~", isNull: true},
		{name: "an empty value", value: "", isNull: true},
		{name: "a null that is anchored", value: "&n null", isNull: true},
		{name: "an alias to an anchored null", value: "*n", isNull: true},
		{name: "false", value: "false", isFalse: true, written: true},
		{name: "a false that is anchored", value: "&f false", isFalse: true, written: true},
		{name: "an alias to an anchored false", value: "*f", isFalse: true, written: true},
		{name: "true", value: "true", written: true},
		{name: "an object", value: "{type: string}", written: true},
		{name: "an empty object", value: "{}", written: true},
	}
	var props strings.Builder
	for i, tc := range tests {
		fmt.Fprintf(&props, "        p%d: %s\n", i, tc.value)
	}
	parsed, _, err := soa.Unmarshal(t.Context(), strings.NewReader("openapi: 3.1.0\n"+
		"info: {title: T, version: '1'}\npaths: {}\n"+
		"components:\n  schemas:\n    A:\n      properties:\n"+props.String()))
	require.NoError(t, err, "the parser reads a null schema; the load phase reports it")
	a, ok := parsed.Components.Schemas.Get("A")
	require.True(t, ok)
	require.Equal(t, len(tests), a.GetSchema().GetProperties().Len())

	for i, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			js, ok := a.GetSchema().GetProperties().Get(fmt.Sprintf("p%d", i))
			require.True(t, ok)
			require.NotNil(t, js)

			assert.Equal(t, tc.isNull, IsNullLiteral(js), "IsNullLiteral")
			assert.Equal(t, tc.isFalse, IsFalseSchema(js), "IsFalseSchema")
			assert.Equal(t, tc.written, Written(js), "Written")
		})
	}

	t.Run("no schema at all", func(t *testing.T) {
		t.Parallel()
		var absent *oas3.JSONSchema[oas3.Referenceable]
		assert.False(t, IsNullLiteral(absent))
		assert.False(t, IsFalseSchema(absent))
		assert.False(t, Written(absent))
	})

	t.Run("a false schema built in memory, which has no node", func(t *testing.T) {
		t.Parallel()
		built := oas3.NewJSONSchemaFromBool(false)
		assert.False(t, IsNullLiteral(built))
		assert.True(t, IsFalseSchema(built))
		assert.True(t, Written(built))
	})
}
